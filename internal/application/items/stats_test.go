// Package items: 商品统计服务的缓存、批量上限、最旧优先、风控即停与错误分支焦点测试。
package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeStatsStore 是内存统计仓储替身，按调用顺序记录写入以便断言串行与落库行为。
type fakeStatsStore struct {
	// rows 是已保存的统计，按商品标识索引。
	rows map[string]ItemStats
	// upserts 是按调用顺序记录的商品标识。
	upserts []string
	// listErr 是注入的批量查询失败。
	listErr error
	// upsertErr 是注入的写入失败。
	upsertErr error
}

// ListItemStats 返回内存中的统计副本，未抓取的商品不出现在结果中。
func (f *fakeStatsStore) ListItemStats(_ context.Context, _ string, itemIDs []string) (map[string]ItemStats, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	// out 是命中的统计集合。
	out := make(map[string]ItemStats, len(itemIDs))
	// itemID 是本次待查询的商品标识。
	for _, itemID := range itemIDs {
		// stats、ok 是该商品在内存仓储中的统计与是否存在。
		if stats, ok := f.rows[itemID]; ok {
			out[itemID] = stats
		}
	}
	return out, nil
}

// UpsertItemStats 写入或覆盖内存中的统计并记录调用顺序。
func (f *fakeStatsStore) UpsertItemStats(_ context.Context, stats ItemStats) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	if f.rows == nil {
		f.rows = make(map[string]ItemStats)
	}
	f.rows[stats.ItemID] = stats
	f.upserts = append(f.upserts, stats.ItemID)
	return nil
}

// fakeStatsFetcher 是抓取端口替身，按调用顺序记录商品标识。
type fakeStatsFetcher struct {
	// results 是按商品标识预设的计数。
	results map[string]ItemStatsCounts
	// errs 是按商品标识预设的失败。
	errs map[string]error
	// calls 是按调用顺序记录的商品标识。
	calls []string
}

// FetchItemStats 记录调用并按预设返回结果或失败。
func (f *fakeStatsFetcher) FetchItemStats(_ context.Context, itemID string) (ItemStatsCounts, error) {
	f.calls = append(f.calls, itemID)
	// err 是该商品预设的失败；为 nil 时返回预设计数。
	if err := f.errs[itemID]; err != nil {
		return ItemStatsCounts{}, err
	}
	return f.results[itemID], nil
}

// fakeStatsOwnership 是账号归属校验替身。
type fakeStatsOwnership struct {
	// owned 是归属判断结果。
	owned bool
	// err 是注入的失败。
	err error
}

// CookieOwned 返回预设的归属判断。
func (f fakeStatsOwnership) CookieOwned(context.Context, int64, string) (bool, error) {
	return f.owned, f.err
}

// statsTestNow 是测试统一使用的当前时刻，方便断言 FetchedAt 与新鲜度边界。
var statsTestNow = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

// newStatsService 构造被测服务，并固定时钟便于断言。
func newStatsService(t *testing.T, store ItemStatsRepository, fetcher ItemStatsFetcher, ownership ItemStatsOwnershipPort) *ItemStatsService {
	t.Helper()
	// service、err 是被测服务与构造错误。
	service, err := NewItemStatsService(store, fetcher, ownership)
	if err != nil {
		t.Fatalf("构造商品统计服务失败: %v", err)
	}
	service.now = func() time.Time { return statsTestNow }
	return service
}

// TestNewItemStatsServiceRequiresDependencies 验证依赖缺失时拒绝构造，避免出现半可用状态。
func TestNewItemStatsServiceRequiresDependencies(t *testing.T) {
	// store、fetcher、ownership 是替代依赖。
	store, fetcher, ownership := &fakeStatsStore{}, &fakeStatsFetcher{}, fakeStatsOwnership{owned: true}
	// cases 是依赖组合与期望是否构造成功。
	cases := []struct {
		// name 是用例名。
		name string
		// store/fetcher/owner 是被替换为 nil 的依赖。
		store   ItemStatsRepository
		fetcher ItemStatsFetcher
		owner   ItemStatsOwnershipPort
		// wantOK 是期望是否构造成功。
		wantOK bool
	}{
		{name: "依赖齐全", store: store, fetcher: fetcher, owner: ownership, wantOK: true},
		{name: "缺少仓储", fetcher: fetcher, owner: ownership, wantOK: false},
		{name: "缺少抓取端口", store: store, owner: ownership, wantOK: false},
		{name: "缺少归属端口", store: store, fetcher: fetcher, wantOK: false},
	}
	// tc 是当前依赖组合用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// service、err 是构造结果与错误。
			service, err := NewItemStatsService(tc.store, tc.fetcher, tc.owner)
			if tc.wantOK && (err != nil || service == nil) {
				t.Fatalf("依赖齐全时应构造成功: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("依赖缺失时应构造失败")
			}
		})
	}
}

// TestRefreshItemStatsForcesFetchAndPersists 验证单个刷新强制抓取、忽略缓存并立即落库。
func TestRefreshItemStatsForcesFetchAndPersists(t *testing.T) {
	// store 是已有一条新鲜数据的仓储，用于证明强制刷新不受缓存影响。
	store := &fakeStatsStore{rows: map[string]ItemStats{
		"item-1": {CookieID: "acc", ItemID: "item-1", BrowseCnt: 1, FetchedAt: statsTestNow},
	}}
	// fetcher 是抓取替身。
	fetcher := &fakeStatsFetcher{results: map[string]ItemStatsCounts{
		"item-1": {BrowseCnt: 22, WantCnt: 2, CollectCnt: 1, SoldCnt: 0},
	}}
	// service 是被测服务。
	service := newStatsService(t, store, fetcher, fakeStatsOwnership{owned: true})

	// stats、err 是刷新结果与错误。
	stats, err := service.RefreshItemStats(context.Background(), 1, "acc", " item-1 ")
	if err != nil {
		t.Fatalf("刷新统计不应报错: %v", err)
	}
	if len(fetcher.calls) != 1 || fetcher.calls[0] != "item-1" {
		t.Fatalf("必须强制抓取一次且使用去空白后的标识，实际 %v", fetcher.calls)
	}
	if stats.BrowseCnt != 22 || stats.WantCnt != 2 || stats.CollectCnt != 1 || stats.SoldCnt != 0 {
		t.Fatalf("刷新结果计数不符: %+v", stats)
	}
	if !stats.FetchedAt.Equal(statsTestNow) || !stats.Fresh {
		t.Fatalf("刷新结果必须使用当前时间且标记为最新: %+v", stats)
	}
	if len(store.upserts) != 1 || store.rows["item-1"].BrowseCnt != 22 {
		t.Fatalf("刷新结果必须落库: upserts=%v rows=%+v", store.upserts, store.rows)
	}
}

// TestRefreshItemStatsErrorBranches 验证单个刷新的归属、参数与依赖错误分支。
func TestRefreshItemStatsErrorBranches(t *testing.T) {
	// 账号不属于当前用户时必须拒绝，且不得发起抓取。
	// deniedStore、deniedFetcher 是拒绝场景的替身。
	deniedStore, deniedFetcher := &fakeStatsStore{}, &fakeStatsFetcher{}
	// deniedService 是账号不归属的用例服务。
	deniedService := newStatsService(t, deniedStore, deniedFetcher, fakeStatsOwnership{owned: false})
	// deniedErr 是账号不归属时返回的错误。
	if _, deniedErr := deniedService.RefreshItemStats(context.Background(), 1, "acc", "item-1"); !errors.Is(deniedErr, ErrItemStatsNotOwned) {
		t.Fatalf("账号不归属应返回 ErrItemStatsNotOwned，实际 %v", deniedErr)
	}
	if len(deniedFetcher.calls) != 0 {
		t.Fatal("账号不归属时不得发起抓取")
	}

	// 商品标识为空时必须直接拒绝，不得发起抓取。
	// blankFetcher 是空标识用例的抓取替身。
	blankFetcher := &fakeStatsFetcher{}
	// blankService 是空标识用例服务。
	blankService := newStatsService(t, &fakeStatsStore{}, blankFetcher, fakeStatsOwnership{owned: true})
	// blankErr 是空商品标识返回的错误。
	if _, blankErr := blankService.RefreshItemStats(context.Background(), 1, "acc", " "); !errors.Is(blankErr, ErrItemStatsNoItems) {
		t.Fatalf("空商品标识应返回 ErrItemStatsNoItems，实际 %v", blankErr)
	}
	if len(blankFetcher.calls) != 0 {
		t.Fatal("空商品标识时不得发起抓取")
	}

	// 抓取失败必须原样上抛，且不得落库。
	// failingStore 是抓取失败场景的空仓储。
	failingStore := &fakeStatsStore{}
	// failingFetcher 是在指定商品上返回失败的替身。
	failingFetcher := &fakeStatsFetcher{errs: map[string]error{"item-1": ErrItemStatsRiskControl}}
	// failingService 是抓取失败用例服务。
	failingService := newStatsService(t, failingStore, failingFetcher, fakeStatsOwnership{owned: true})
	// fetchErr 是抓取失败返回的错误。
	if _, fetchErr := failingService.RefreshItemStats(context.Background(), 1, "acc", "item-1"); !errors.Is(fetchErr, ErrItemStatsRiskControl) {
		t.Fatalf("抓取失败应原样上抛，实际 %v", fetchErr)
	}
	if len(failingStore.upserts) != 0 {
		t.Fatal("抓取失败时不得落库")
	}

	// 落库失败必须显式暴露，不得静默吞掉。
	// saveErr 是注入的落库失败。
	saveErr := errors.New("写入失败")
	// saveFailService 是落库失败用例服务。
	saveFailService := newStatsService(t, &fakeStatsStore{upsertErr: saveErr}, &fakeStatsFetcher{}, fakeStatsOwnership{owned: true})
	// writeErr 是落库失败返回的错误。
	if _, writeErr := saveFailService.RefreshItemStats(context.Background(), 1, "acc", "item-1"); !errors.Is(writeErr, saveErr) {
		t.Fatalf("落库失败应显式暴露，实际 %v", writeErr)
	}
}

// TestRefreshStatsBatchSkipsFreshAndOrdersOldestFirst 验证批量刷新跳过新鲜数据并按最旧优先抓取。
func TestRefreshStatsBatchSkipsFreshAndOrdersOldestFirst(t *testing.T) {
	// store 中有三种数据：刚抓过（新鲜）、两小时前（过期）、从未抓取（零值时间）。
	store := &fakeStatsStore{rows: map[string]ItemStats{
		"fresh":   {CookieID: "acc", ItemID: "fresh", FetchedAt: statsTestNow.Add(-time.Hour)},
		"aged":    {CookieID: "acc", ItemID: "aged", FetchedAt: statsTestNow.Add(-30 * time.Hour)},
		"older":   {CookieID: "acc", ItemID: "older", FetchedAt: statsTestNow.Add(-48 * time.Hour)},
		"unknown": {CookieID: "acc", ItemID: "unknown", FetchedAt: time.Time{}},
	}}
	// fetcher 是抓取替身，所有商品都返回同一组计数。
	fetcher := &fakeStatsFetcher{results: map[string]ItemStatsCounts{
		"aged": {BrowseCnt: 1}, "older": {BrowseCnt: 2}, "unknown": {BrowseCnt: 3},
	}}
	// service 是被测服务。
	service := newStatsService(t, store, fetcher, fakeStatsOwnership{owned: true})

	// result、err 是批量结果与错误。
	result, err := service.RefreshStatsBatch(context.Background(), 1, "acc",
		[]string{"fresh", "aged", "older", "unknown", "aged", " "}, ItemStatsBatchLimit)
	if err != nil {
		t.Fatalf("批量刷新不应报错: %v", err)
	}
	if result.Skipped != 1 {
		t.Fatalf("新鲜数据应被跳过一次（重复项与空值不重复计入），实际 %d", result.Skipped)
	}
	// wantOrder 是期望的抓取顺序：未抓过最前，其后按抓取时间升序。
	wantOrder := []string{"unknown", "older", "aged"}
	if len(fetcher.calls) != len(wantOrder) {
		t.Fatalf("抓取次数不符: calls=%v", fetcher.calls)
	}
	// index 是当前比较位置。
	for index, itemID := range wantOrder {
		if fetcher.calls[index] != itemID {
			t.Fatalf("抓取顺序不符: got=%v want=%v", fetcher.calls, wantOrder)
		}
	}
	if len(result.Refreshed) != 3 || result.Remaining != 0 || result.Stopped {
		t.Fatalf("批量结果不符: %+v", result)
	}
}

// TestRefreshStatsBatchLimitIsEnforced 验证单轮上限由服务端强制，且剩余数量如实返回。
func TestRefreshStatsBatchLimitIsEnforced(t *testing.T) {
	// itemIDs 是 12 个从未抓取的商品。
	itemIDs := make([]string, 0, 12)
	// index 是当前构造序号。
	for index := 0; index < 12; index++ {
		itemIDs = append(itemIDs, "item-"+string(rune('a'+index)))
	}
	// cases 是入参上限与期望实际抓取数量。
	cases := []struct {
		// name 是用例名。
		name string
		// limit 是调用方传入的上限。
		limit int
		// wantRefreshed 是期望抓取数量。
		wantRefreshed int
		// wantRemaining 是期望剩余数量。
		wantRemaining int
	}{
		{name: "按上限截断", limit: ItemStatsBatchLimit, wantRefreshed: 10, wantRemaining: 2},
		{name: "超上限被硬截断", limit: 100, wantRefreshed: 10, wantRemaining: 2},
		{name: "零值退回默认上限", limit: 0, wantRefreshed: 10, wantRemaining: 2},
		{name: "负值退回默认上限", limit: -3, wantRefreshed: 10, wantRemaining: 2},
		{name: "更小上限生效", limit: 3, wantRefreshed: 3, wantRemaining: 9},
	}
	// tc 是当前上限用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// fetcher 是抓取替身；store 是空仓储（全部需要抓取）。
			fetcher := &fakeStatsFetcher{}
			// service 是被测服务。
			service := newStatsService(t, &fakeStatsStore{}, fetcher, fakeStatsOwnership{owned: true})
			// result、err 是批量结果与错误。
			result, err := service.RefreshStatsBatch(context.Background(), 1, "acc", itemIDs, tc.limit)
			if err != nil {
				t.Fatalf("批量刷新不应报错: %v", err)
			}
			if len(result.Refreshed) != tc.wantRefreshed || len(fetcher.calls) != tc.wantRefreshed {
				t.Fatalf("抓取数量不符: refreshed=%d calls=%d want=%d", len(result.Refreshed), len(fetcher.calls), tc.wantRefreshed)
			}
			if result.Remaining != tc.wantRemaining {
				t.Fatalf("剩余数量不符: got=%d want=%d", result.Remaining, tc.wantRemaining)
			}
		})
	}
}

// TestRefreshStatsBatchStopsOnRiskControl 验证命中风控立即停止、保留已抓结果且不再请求后续商品。
func TestRefreshStatsBatchStopsOnRiskControl(t *testing.T) {
	// fetcher 在第二个商品上返回风控错误。
	fetcher := &fakeStatsFetcher{errs: map[string]error{"item-b": ErrItemStatsRiskControl}}
	// store 是内存仓储，用于确认已抓结果被保留。
	store := &fakeStatsStore{}
	// service 是被测服务。
	service := newStatsService(t, store, fetcher, fakeStatsOwnership{owned: true})

	// result、err 是批量结果与错误。
	result, err := service.RefreshStatsBatch(context.Background(), 1, "acc", []string{"item-a", "item-b", "item-c"}, ItemStatsBatchLimit)
	if err != nil {
		t.Fatalf("风控停止不应返回错误: %v", err)
	}
	if !result.Stopped || result.StopReason != ItemStatsStopRiskControl {
		t.Fatalf("风控必须标记停止原因: %+v", result)
	}
	if len(fetcher.calls) != 2 || len(result.Refreshed) != 1 || result.Refreshed[0].ItemID != "item-a" {
		t.Fatalf("风控后必须保留已抓结果并停止请求: calls=%v result=%+v", fetcher.calls, result)
	}
	if len(store.upserts) != 1 || store.upserts[0] != "item-a" {
		t.Fatalf("风控前已抓结果必须落库: %v", store.upserts)
	}
}

// TestRefreshStatsBatchHardFailures 验证助手不可用类故障直接上抛并保留已抓结果。
func TestRefreshStatsBatchHardFailures(t *testing.T) {
	// fetcher 在第二个商品上返回助手不可用。
	fetcher := &fakeStatsFetcher{errs: map[string]error{"item-b": ErrItemStatsUnavailable}}
	// service 是被测服务。
	service := newStatsService(t, &fakeStatsStore{}, fetcher, fakeStatsOwnership{owned: true})
	// result、err 是批量结果与错误。
	result, hardErr := service.RefreshStatsBatch(context.Background(), 1, "acc", []string{"item-a", "item-b", "item-c"}, ItemStatsBatchLimit)
	if !errors.Is(hardErr, ErrItemStatsUnavailable) {
		t.Fatalf("助手不可用应直接上抛，实际 %v", hardErr)
	}
	if len(result.Refreshed) != 1 || result.Stopped {
		t.Fatalf("硬故障不应标记为风控停止: %+v", result)
	}
	if len(fetcher.calls) != 2 {
		t.Fatalf("硬故障后不得继续抓取: %v", fetcher.calls)
	}
}

// TestRefreshStatsBatchValidation 验证归属、参数、查询失败与上下文取消分支。
func TestRefreshStatsBatchValidation(t *testing.T) {
	// 账号不归属时必须拒绝。
	// deniedFetcher 是拒绝场景的抓取替身。
	deniedFetcher := &fakeStatsFetcher{}
	// deniedService 是账号不归属用例服务。
	deniedService := newStatsService(t, &fakeStatsStore{}, deniedFetcher, fakeStatsOwnership{owned: false})
	// deniedErr 是账号不归属时返回的错误。
	if _, deniedErr := deniedService.RefreshStatsBatch(context.Background(), 1, "acc", []string{"item-a"}, 5); !errors.Is(deniedErr, ErrItemStatsNotOwned) {
		t.Fatalf("账号不归属应返回 ErrItemStatsNotOwned，实际 %v", deniedErr)
	}
	if len(deniedFetcher.calls) != 0 {
		t.Fatal("账号不归属时不得发起抓取")
	}

	// 商品集合为空（或全为空白）时必须拒绝。
	// emptyService 是空商品集合用例服务。
	emptyService := newStatsService(t, &fakeStatsStore{}, &fakeStatsFetcher{}, fakeStatsOwnership{owned: true})
	// itemIDs 是本次用例的商品集合（空集或全为空白）。
	for _, itemIDs := range [][]string{nil, {"  ", ""}} {
		// emptyErr 是空商品集合返回的错误。
		if _, emptyErr := emptyService.RefreshStatsBatch(context.Background(), 1, "acc", itemIDs, 5); !errors.Is(emptyErr, ErrItemStatsNoItems) {
			t.Fatalf("空商品集合应返回 ErrItemStatsNoItems，实际 %v", emptyErr)
		}
	}

	// 候选查询失败必须显式暴露。
	// listErr 是注入的查询失败。
	listErr := errors.New("查询失败")
	// listFailService 是查询失败用例服务。
	listFailService := newStatsService(t, &fakeStatsStore{listErr: listErr}, &fakeStatsFetcher{}, fakeStatsOwnership{owned: true})
	// queryErr 是候选查询失败返回的错误。
	if _, queryErr := listFailService.RefreshStatsBatch(context.Background(), 1, "acc", []string{"item-a"}, 5); !errors.Is(queryErr, listErr) {
		t.Fatalf("查询失败应显式暴露，实际 %v", queryErr)
	}

	// 上下文已取消时必须立即返回取消错误，不得继续抓取。
	// cancelledCtx 是已取消的上下文。
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	// cancelFetcher 是取消场景的抓取替身。
	cancelFetcher := &fakeStatsFetcher{}
	// cancelService 是取消场景服务。
	cancelService := newStatsService(t, &fakeStatsStore{}, cancelFetcher, fakeStatsOwnership{owned: true})
	// cancelErr 是上下文取消返回的错误。
	if _, cancelErr := cancelService.RefreshStatsBatch(cancelledCtx, 1, "acc", []string{"item-a"}, 5); !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("取消后应返回取消错误，实际 %v", cancelErr)
	}
	if len(cancelFetcher.calls) != 0 {
		t.Fatal("取消后不得发起抓取")
	}
}

// TestListItemStatsMarksFreshness 验证列表读取按当前时间标注新鲜度。
func TestListItemStatsMarksFreshness(t *testing.T) {
	// store 中一条新鲜、一条过期。
	store := &fakeStatsStore{rows: map[string]ItemStats{
		"fresh": {CookieID: "acc", ItemID: "fresh", FetchedAt: statsTestNow.Add(-time.Hour)},
		"aged":  {CookieID: "acc", ItemID: "aged", FetchedAt: statsTestNow.Add(-25 * time.Hour)},
	}}
	// service 是被测服务。
	service := newStatsService(t, store, &fakeStatsFetcher{}, fakeStatsOwnership{owned: true})
	// rows、err 是列表读取结果与错误。
	rows, err := service.ListItemStats(context.Background(), "acc", []string{"fresh", "aged", "missing"})
	if err != nil {
		t.Fatalf("列表读取不应报错: %v", err)
	}
	if !rows["fresh"].Fresh || rows["aged"].Fresh {
		t.Fatalf("新鲜度标注不符: %+v", rows)
	}
	// ok 表示未抓取商品是否意外出现在列表结果里。
	if _, ok := rows["missing"]; ok {
		t.Fatal("未抓取商品不应出现在列表结果中")
	}

	// 查询失败必须显式暴露。
	// listErr 是注入的查询失败。
	listErr := errors.New("查询失败")
	// failService 是查询失败用例服务。
	failService := newStatsService(t, &fakeStatsStore{listErr: listErr}, &fakeStatsFetcher{}, fakeStatsOwnership{owned: true})
	// failErr 是列表查询失败返回的错误。
	if _, failErr := failService.ListItemStats(context.Background(), "acc", []string{"fresh"}); !errors.Is(failErr, listErr) {
		t.Fatalf("查询失败应显式暴露，实际 %v", failErr)
	}
}

// TestStatsOwnershipFailurePropagates 验证归属校验自身失败时不得继续抓取。
func TestStatsOwnershipFailurePropagates(t *testing.T) {
	// ownershipErr 是注入的归属校验失败。
	ownershipErr := errors.New("归属校验失败")
	// fetcher 是抓取替身。
	fetcher := &fakeStatsFetcher{}
	// service 是被测服务。
	service := newStatsService(t, &fakeStatsStore{}, fetcher, fakeStatsOwnership{owned: true, err: ownershipErr})
	// oneErr 是单个刷新路径归属校验失败返回的错误。
	if _, oneErr := service.RefreshItemStats(context.Background(), 1, "acc", "item-a"); !errors.Is(oneErr, ownershipErr) {
		t.Fatalf("归属校验失败应显式暴露，实际 %v", oneErr)
	}
	// batchErr 是批量刷新路径归属校验失败返回的错误。
	if _, batchErr := service.RefreshStatsBatch(context.Background(), 1, "acc", []string{"item-a"}, 5); !errors.Is(batchErr, ownershipErr) {
		t.Fatalf("批量路径归属校验失败应显式暴露，实际 %v", batchErr)
	}
	if len(fetcher.calls) != 0 {
		t.Fatal("归属校验失败时不得发起抓取")
	}
}

// accountAwareStatsStore 是按账号隔离的内存仓储替身，用于验证列表统计的分组读取。
type accountAwareStatsStore struct {
	// rows 是按「账号 + 商品」索引的统计。
	rows map[string]ItemStats
	// listErr 是注入的查询失败。
	listErr error
}

// ListItemStats 只返回指定账号下的统计，未抓取的商品不出现在结果中。
func (f *accountAwareStatsStore) ListItemStats(_ context.Context, cookieID string, itemIDs []string) (map[string]ItemStats, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	// out 是命中该账号的统计集合。
	out := make(map[string]ItemStats)
	// itemID 是本次待查询的商品标识。
	for _, itemID := range itemIDs {
		if // stats、ok 是该商品在该账号下的统计与是否存在。
		stats, ok := f.rows[ItemStatsKey(cookieID, itemID)]; ok {
			out[itemID] = stats
		}
	}
	return out, nil
}

// UpsertItemStats 在本替身中不参与断言，直接成功返回。
func (f *accountAwareStatsStore) UpsertItemStats(context.Context, ItemStats) error { return nil }

// TestItemStatsKeyIncludesAccount 验证统计索引键同时包含账号与商品。
func TestItemStatsKeyIncludesAccount(t *testing.T) {
	if ItemStatsKey("acc1", "i1") == ItemStatsKey("acc2", "i1") {
		t.Fatal("不同账号下的同一商品必须得到不同索引键")
	}
}

// TestListItemStatsForItemsGroupsByAccount 验证列表统计按账号分组读取、按键合并并跳过无效引用。
func TestListItemStatsForItemsGroupsByAccount(t *testing.T) {
	// store 保存两个账号下各一条一小时内抓取的统计。
	store := &accountAwareStatsStore{rows: map[string]ItemStats{
		ItemStatsKey("acc1", "i1"): {CookieID: "acc1", ItemID: "i1", BrowseCnt: 11, FetchedAt: statsTestNow.Add(-time.Hour)},
		ItemStatsKey("acc2", "i1"): {CookieID: "acc2", ItemID: "i1", BrowseCnt: 22, FetchedAt: statsTestNow.Add(-time.Hour)},
	}}
	// service 是被测服务。
	service := newStatsService(t, store, &fakeStatsFetcher{}, fakeStatsOwnership{owned: true})

	// rows、err 是分组读取结果与错误；引用刻意包含空账号、空商品与未抓取商品。
	rows, err := service.ListItemStatsForItems(context.Background(), []ItemStatsRef{
		{CookieID: "acc1", ItemID: "i1"},
		{CookieID: "acc2", ItemID: "i1"},
		{CookieID: " ", ItemID: "i1"},
		{CookieID: "acc1", ItemID: " "},
		{CookieID: "acc1", ItemID: "i-missing"},
	})
	if err != nil {
		t.Fatalf("分组读取统计不应报错: %v", err)
	}
	if len(rows) != 2 || rows[ItemStatsKey("acc1", "i1")].BrowseCnt != 11 || rows[ItemStatsKey("acc2", "i1")].BrowseCnt != 22 {
		t.Fatalf("分组读取结果不符: %+v", rows)
	}
	// ok 表示该账号下的该商品是否有统计；未抓取商品必须为 false。
	if _, ok := rows[ItemStatsKey("acc1", "i-missing")]; ok {
		t.Fatal("未抓取商品不应出现在分组结果中")
	}
	if !rows[ItemStatsKey("acc1", "i1")].Fresh {
		t.Fatalf("分组读取也必须标注新鲜度: %+v", rows[ItemStatsKey("acc1", "i1")])
	}

	// 查询失败必须显式暴露。
	// listErr 是注入的分组查询失败。
	listErr := errors.New("分组查询失败")
	// failService 是查询失败用例服务。
	failService := newStatsService(t, &accountAwareStatsStore{listErr: listErr}, &fakeStatsFetcher{}, fakeStatsOwnership{owned: true})
	// failErr 是分组查询失败返回的错误。
	if _, failErr := failService.ListItemStatsForItems(context.Background(), []ItemStatsRef{{CookieID: "acc1", ItemID: "i1"}}); !errors.Is(failErr, listErr) {
		t.Fatalf("分组查询失败应显式暴露，实际 %v", failErr)
	}
}
