package sourcing

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeRepository 是选品参考仓储的内存替身，用于确定性断言快照与订单读取行为。
type fakeRepository struct {
	// snapshots 以"类别|日期"为键保存快照。
	snapshots map[string]Snapshot
	// orders 是 ListOrderRows 返回的订单原始行。
	orders []OrderRow
	// listCalls 记录 ListOrderRows 调用次数，用于验证畅销榜路径。
	listCalls int
	// deletedBefore 保存最近一次清理请求的截止日。
	deletedBefore string
}

// SaveSnapshot 写入内存快照。
func (f *fakeRepository) SaveSnapshot(_ context.Context, snapshot Snapshot) error {
	f.snapshots[snapshot.Kind+"|"+snapshot.Date] = snapshot
	return nil
}

// GetSnapshot 读取内存快照；不存在时返回 ErrSnapshotNotFound。
func (f *fakeRepository) GetSnapshot(_ context.Context, kind, date string) (Snapshot, error) {
	// snapshot 是内存中按“类别|日期”保存的快照；ok 表示该快照是否存在。
	snapshot, ok := f.snapshots[kind+"|"+date]
	if !ok {
		return Snapshot{}, ErrSnapshotNotFound
	}
	return snapshot, nil
}

// DeleteSnapshotsBefore 记录清理请求，不实际删除内存快照。
func (f *fakeRepository) DeleteSnapshotsBefore(_ context.Context, beforeDate string) error {
	f.deletedBefore = beforeDate
	return nil
}

// ListOrderRows 返回预置订单行并计数。
func (f *fakeRepository) ListOrderRows(_ context.Context, _ int64, _ time.Time) ([]OrderRow, error) {
	f.listCalls++
	return f.orders, nil
}

// fakeBrowser 是浏览器采集边界替身，记录调用次数并返回预置结果。
type fakeBrowser struct {
	// hotKeywords 是热词采集返回值。
	hotKeywords []string
	// hotErr 是热词采集返回错误。
	hotErr error
	// items 是搜索返回值。
	items []SearchItem
	// searchErr 是搜索返回错误。
	searchErr error
	// searchCalls 记录搜索调用次数，用于验证冷却期内不发出请求。
	searchCalls int
	// hotCalls 记录热词调用次数。
	hotCalls int
}

// FetchHotKeywords 返回预置热词并计数。
func (f *fakeBrowser) FetchHotKeywords(_ context.Context) ([]string, error) {
	f.hotCalls++
	return f.hotKeywords, f.hotErr
}

// SearchItems 返回预置商品并计数。
func (f *fakeBrowser) SearchItems(_ context.Context, _, _, _ string) ([]SearchItem, error) {
	f.searchCalls++
	return f.items, f.searchErr
}

// fakeCredentials 是凭证读取替身。
type fakeCredentials struct {
	// cookie 是返回的 cookie 文本。
	cookie string
	// err 是返回错误。
	err error
}

// ReadCookie 返回预置 cookie。
func (f *fakeCredentials) ReadCookie(_ context.Context, _ int64, _ string) (string, error) {
	return f.cookie, f.err
}

// fakeChat 是 AI 补全替身。
type fakeChat struct {
	// content 是返回的补全文本。
	content string
	// err 是返回错误。
	err error
	// calls 记录调用次数。
	calls int
}

// Complete 返回预置内容并计数。
func (f *fakeChat) Complete(_ context.Context, _ int64, _, _ string) (string, error) {
	f.calls++
	return f.content, f.err
}

// fixedClock 返回固定时刻，保证日期断言确定。
func fixedClock(moment time.Time) func() time.Time {
	return func() time.Time { return moment }
}

// newTestService 构造带内存替身的服务与固定时钟。
func newTestService(t *testing.T) (*Service, *fakeRepository, *fakeBrowser, *fakeChat) {
	t.Helper()
	// repository 是内存仓储替身。
	repository := &fakeRepository{snapshots: make(map[string]Snapshot)}
	// browser 是浏览器替身。
	browser := &fakeBrowser{}
	// chat 是 AI 替身。
	chat := &fakeChat{content: "示例文案"}
	// service 是被测服务，时钟固定在 2026-10-29 10:00 北京时间。
	service := NewService(repository, browser, &fakeCredentials{cookie: "c=1"}, chat).
		WithClock(fixedClock(time.Date(2026, 10, 29, 10, 0, 0, 0, beijingLocation)))
	return service, repository, browser, chat
}

// TestHotKeywordsWithoutSnapshot 验证没有快照时返回空词条而不是错误。
func TestHotKeywordsWithoutSnapshot(t *testing.T) {
	// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务。
	service, _, _, _ := newTestService(t)

	// view、err 是热词读取结果。
	view, err := service.HotKeywords(context.Background())
	if err != nil {
		t.Fatalf("无快照时不应报错: %v", err)
	}
	if view.Date != "2026-10-29" || len(view.Keywords) != 0 || view.Status != "" {
		t.Fatalf("空快照视图不符: %+v", view)
	}
}

// TestRefreshHotKeywordsWritesSnapshot 验证热词采集成功写入当日快照并触发清理。
func TestRefreshHotKeywordsWritesSnapshot(t *testing.T) {
	// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务；repository 是记录快照写入与订单读取次数的内存仓储替身；browser 是记录热词与搜索调用次数的浏览器替身，用于断言是否触达平台。
	service, repository, browser, _ := newTestService(t)
	browser.hotKeywords = []string{"耳机", "电动车"}

	// err 是调用 service.RefreshHotKeywords 的结果，按本用例期望断言成功或特定领域错误。
	if err := service.RefreshHotKeywords(context.Background()); err != nil {
		t.Fatalf("热词采集应成功: %v", err)
	}
	// snapshot 是写入的快照。
	snapshot, ok := repository.snapshots[KindHotKeywords+"|2026-10-29"]
	if !ok || snapshot.Status != StatusOK {
		t.Fatalf("热词快照未写入或状态错误: %+v", snapshot)
	}
	if repository.deletedBefore != "2026-09-30" {
		t.Fatalf("保留窗口截止日不符: %s", repository.deletedBefore)
	}
	if browser.hotCalls != 1 {
		t.Fatalf("热词采集应只调用一次, got %d", browser.hotCalls)
	}
}

// TestRefreshHotKeywordsRecordsFailure 验证采集失败写入失败快照并返回错误。
func TestRefreshHotKeywordsRecordsFailure(t *testing.T) {
	// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务；repository 是记录快照写入与订单读取次数的内存仓储替身；browser 是记录热词与搜索调用次数的浏览器替身，用于断言是否触达平台。
	service, repository, browser, _ := newTestService(t)
	browser.hotErr = ErrRiskControl

	// err 是调用 service.RefreshHotKeywords 的结果，按本用例期望断言成功或特定领域错误。
	err := service.RefreshHotKeywords(context.Background())
	if !errors.Is(err, ErrRiskControl) {
		t.Fatalf("应返回风控错误, got %v", err)
	}
	// snapshot 是写入的失败快照。
	snapshot, ok := repository.snapshots[KindHotKeywords+"|2026-10-29"]
	if !ok || snapshot.Status != StatusFailed || snapshot.ErrorMessage == "" {
		t.Fatalf("失败快照未记录原因: %+v", snapshot)
	}
}

// TestRefreshHotKeywordsWithoutBrowser 验证浏览器不可用时明确降级。
func TestRefreshHotKeywordsWithoutBrowser(t *testing.T) {
	// service 是未注入浏览器能力的服务。
	service := NewService(&fakeRepository{snapshots: make(map[string]Snapshot)}, nil, nil, nil)

	// err 是调用 service.RefreshHotKeywords 的结果，按本用例期望断言成功或特定领域错误。
	if err := service.RefreshHotKeywords(context.Background()); !errors.Is(err, ErrBrowserUnavailable) {
		t.Fatalf("应返回浏览器不可用, got %v", err)
	}
}

// TestAggregateRankingFiltersAndSorts 验证畅销榜口径：只统计有效状态、跳过空商品、按销量排序。
func TestAggregateRankingFiltersAndSorts(t *testing.T) {
	// rows 覆盖有效状态、无效状态、非数字数量与空商品标识。
	rows := []OrderRow{
		{ItemID: "a", ItemTitle: "商品甲", Quantity: "2", Amount: "20", OrderStatus: "completed"},
		{ItemID: "a", ItemTitle: "商品甲", Quantity: "3", Amount: "30", OrderStatus: "shipped"},
		{ItemID: "b", ItemTitle: "商品乙", Quantity: "10", Amount: "50", OrderStatus: "paid"},
		{ItemID: "c", ItemTitle: "商品丙", Quantity: "很多", Amount: "abc", OrderStatus: "completed"},
		{ItemID: "d", ItemTitle: "商品丁", Quantity: "5", Amount: "5", OrderStatus: "unknown_status"},
		{ItemID: "", ItemTitle: "无标识", Quantity: "5", Amount: "5", OrderStatus: "completed"},
	}
	// entries 是聚合后的排行。
	entries := aggregateRanking(rows)
	if len(entries) != 3 {
		t.Fatalf("聚合条目数不符, want 3 got %d: %+v", len(entries), entries)
	}
	if entries[0].ItemID != "b" || entries[0].SoldQuantity != 10 {
		t.Fatalf("销量最高条目不符: %+v", entries[0])
	}
	if entries[1].ItemID != "a" || entries[1].SoldQuantity != 5 || entries[1].OrderCount != 2 {
		t.Fatalf("累计条目不符: %+v", entries[1])
	}
	if entries[2].ItemID != "c" || entries[2].SoldQuantity != 0 || entries[2].Revenue != 0 {
		t.Fatalf("非数字应计为 0: %+v", entries[2])
	}
}

// TestShopRankingUsesLocalDataOnly 验证畅销榜只读本地数据、按用户实时计算且不写全局快照。
func TestShopRankingUsesLocalDataOnly(t *testing.T) {
	// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务；repository 是记录快照写入与订单读取次数的内存仓储替身；browser 是记录热词与搜索调用次数的浏览器替身，用于断言是否触达平台。
	service, repository, browser, _ := newTestService(t)
	repository.orders = []OrderRow{
		{ItemID: "a", ItemTitle: "商品甲", Quantity: "2", Amount: "20", OrderStatus: "completed"},
	}

	// view、err 是 30 天窗口的畅销榜结果。
	view, err := service.ShopRanking(context.Background(), 1, "30d")
	if err != nil {
		t.Fatalf("畅销榜读取应成功: %v", err)
	}
	if view.Window != RankingWindow30Days || view.Status != StatusOK || len(view.Entries) != 1 || view.Entries[0].SoldQuantity != 2 {
		t.Fatalf("畅销榜结果不符: %+v", view)
	}
	if repository.listCalls != 1 {
		t.Fatalf("应只读取一次本地订单, got %d", repository.listCalls)
	}
	if browser.searchCalls != 0 || browser.hotCalls != 0 {
		t.Fatalf("畅销榜不得触达浏览器能力: search=%d hot=%d", browser.searchCalls, browser.hotCalls)
	}
	if len(repository.snapshots) != 0 {
		t.Fatalf("畅销榜不得写入全局快照: %+v", repository.snapshots)
	}
	// empty 是无订单时的空榜结果，Entries 必须是非 nil 空数组。
	repository.orders = nil
	// empty 是无订单时的 7 天畅销榜结果；err 是读取错误，期望为空。
	empty, err := service.ShopRanking(context.Background(), 1, "")
	if err != nil || empty.Entries == nil || len(empty.Entries) != 0 || empty.Window != RankingWindow7Days {
		t.Fatalf("空榜结果不符: %+v, err=%v", empty, err)
	}
}

// TestSchedulerSkipsWhenTodayCollected 验证当日已成功采集时调度不再访问浏览器。
func TestSchedulerSkipsWhenTodayCollected(t *testing.T) {
	// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务；repository 是记录快照写入与订单读取次数的内存仓储替身；browser 是记录热词与搜索调用次数的浏览器替身，用于断言是否触达平台。
	service, repository, browser, _ := newTestService(t)
	repository.snapshots[KindHotKeywords+"|2026-10-29"] = Snapshot{Kind: KindHotKeywords, Date: "2026-10-29", PayloadJSON: `{"keywords":["耳机"]}`, Status: StatusOK}
	// scheduler 是被测调度器，时钟固定在当天 10 点，已过执行时刻。
	scheduler := NewScheduler(service, nil)
	scheduler.runIfDue(context.Background())
	if browser.hotCalls != 0 {
		t.Fatalf("当日已采集时不应再访问平台, got %d", browser.hotCalls)
	}
	// 当日未采集时应采集且只采集一次，重复触发不再执行。
	delete(repository.snapshots, KindHotKeywords+"|2026-10-29")
	browser.hotKeywords = []string{"耳机"}
	// fresh 是新的调度器实例，模拟服务重启。
	fresh := NewScheduler(service, nil)
	fresh.runIfDue(context.Background())
	fresh.runIfDue(context.Background())
	if browser.hotCalls != 1 {
		t.Fatalf("同一天只应采集一次, got %d", browser.hotCalls)
	}
}

// TestSchedulerWaitsUntilDailyHour 验证北京时间 8 点前不执行采集。
func TestSchedulerWaitsUntilDailyHour(t *testing.T) {
	// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务；browser 是记录热词与搜索调用次数的浏览器替身，用于断言是否触达平台。
	service, _, browser, _ := newTestService(t)
	service.WithClock(fixedClock(time.Date(2026, 10, 29, 7, 0, 0, 0, beijingLocation)))
	// scheduler 是被测调度器。
	scheduler := NewScheduler(service, nil)
	scheduler.runIfDue(context.Background())
	if browser.hotCalls != 0 {
		t.Fatalf("执行时刻前不应采集, got %d", browser.hotCalls)
	}
}

// TestSearchItemsCooldownBlocksSecondRequest 验证冷却期内第二次查询被拒绝且不发出浏览器请求。
func TestSearchItemsCooldownBlocksSecondRequest(t *testing.T) {
	// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务；browser 是记录热词与搜索调用次数的浏览器替身，用于断言是否触达平台。
	service, _, browser, _ := newTestService(t)
	browser.items = []SearchItem{{ItemID: "x", Title: "商品"}}

	// first 是第一次查询，应成功。
	if _, err := service.SearchItems(context.Background(), 1, "acc", "耳机"); err != nil {
		t.Fatalf("首次查询应成功: %v", err)
	}
	// secondErr 是冷却期内的第二次查询错误。
	_, secondErr := service.SearchItems(context.Background(), 1, "acc", "耳机")
	if !errors.Is(secondErr, ErrSearchCooling) {
		t.Fatalf("冷却期内应返回冷却错误, got %v", secondErr)
	}
	if browser.searchCalls != 1 {
		t.Fatalf("冷却期内不得再次发出浏览器请求, got %d", browser.searchCalls)
	}
	// 不同关键词不受同一冷却限制。
	if _, err := service.SearchItems(context.Background(), 1, "acc", "电动车"); err != nil {
		t.Fatalf("不同关键词应可查询: %v", err)
	}
	if browser.searchCalls != 2 {
		t.Fatalf("不同关键词应产生第二次请求, got %d", browser.searchCalls)
	}
}

// TestSearchItemsMapsBrowserErrors 验证浏览器错误映射为稳定的领域错误。
func TestSearchItemsMapsBrowserErrors(t *testing.T) {
	// cases 覆盖风控、会话过期与空结果。
	cases := []struct {
		name      string
		browserEr error
		want      error
	}{
		{"风控", ErrRiskControl, ErrRiskControl},
		{"会话过期", ErrSessionExpired, ErrSessionExpired},
		{"空结果", ErrNoResult, ErrNoResult},
	}
	// index 用于为每个用例生成不同关键词以避开冷却；c 是当前错误映射用例。
	for index, c := range cases {
		// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务；browser 是记录热词与搜索调用次数的浏览器替身，用于断言是否触达平台。
		service, _, browser, _ := newTestService(t)
		browser.searchErr = c.browserEr
		// keyword 为每个用例使用不同关键词，避免冷却互相影响。
		keyword := "关键词" + string(rune('a'+index))
		// err 是调用 service.SearchItems 的结果，按本用例期望断言成功或特定领域错误。
		if _, err := service.SearchItems(context.Background(), 1, "acc", keyword); !errors.Is(err, c.want) {
			t.Fatalf("%s: 错误映射不符, got %v", c.name, err)
		}
	}
}

// TestSearchItemsWithoutBrowser 验证浏览器不可用时明确降级且不读凭证。
func TestSearchItemsWithoutBrowser(t *testing.T) {
	// service 是没有浏览器与凭证能力的服务。
	service := NewService(&fakeRepository{snapshots: make(map[string]Snapshot)}, nil, nil, nil)

	// err 是调用 service.SearchItems 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := service.SearchItems(context.Background(), 1, "acc", "耳机"); !errors.Is(err, ErrBrowserUnavailable) {
		t.Fatalf("应返回浏览器不可用, got %v", err)
	}
}

// TestGenerateCopywritingListingAndAnalysis 验证两种模式都能生成且调用 AI 一次。
func TestGenerateCopywritingListingAndAnalysis(t *testing.T) {
	// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务；chat 是记录调用次数的 AI 替身。
	service, _, _, chat := newTestService(t)

	// listing 是上架文案结果。
	listing, err := service.GenerateCopywriting(context.Background(), 1, CopywritingRequest{Mode: CopywritingModeListing, Topic: "蓝牙耳机"})
	if err != nil {
		t.Fatalf("上架文案应成功: %v", err)
	}
	if listing.Mode != CopywritingModeListing || listing.Content != "示例文案" {
		t.Fatalf("上架文案内容不符: %+v", listing)
	}
	// analysis 是品类分析结果。
	analysis, err := service.GenerateCopywriting(context.Background(), 1, CopywritingRequest{Mode: CopywritingModeAnalysis, Topic: "蓝牙耳机", Reference: "价格 19 元，485 人想要"})
	if err != nil {
		t.Fatalf("品类分析应成功: %v", err)
	}
	if analysis.Mode != CopywritingModeAnalysis {
		t.Fatalf("品类分析模式不符: %+v", analysis)
	}
	if chat.calls != 2 {
		t.Fatalf("两种模式应各调用一次 AI, got %d", chat.calls)
	}
}

// TestGenerateCopywritingRejectsInvalidInput 验证非法模式与空主题被拒绝。
func TestGenerateCopywritingRejectsInvalidInput(t *testing.T) {
	// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务；chat 是记录调用次数的 AI 替身。
	service, _, _, chat := newTestService(t)

	// err 是调用 service.GenerateCopywriting 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := service.GenerateCopywriting(context.Background(), 1, CopywritingRequest{Mode: "unknown", Topic: "耳机"}); !errors.Is(err, ErrInvalidCopywritingMode) {
		t.Fatalf("非法模式应被拒绝, got %v", err)
	}
	// err 是调用 service.GenerateCopywriting 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := service.GenerateCopywriting(context.Background(), 1, CopywritingRequest{Mode: CopywritingModeListing, Topic: "  "}); !errors.Is(err, ErrInvalidCopywritingMode) {
		t.Fatalf("空主题应被拒绝, got %v", err)
	}
	if chat.calls != 0 {
		t.Fatalf("非法请求不得调用 AI, got %d", chat.calls)
	}
}

// TestGenerateCopywritingWithoutAI 验证 AI 未配置时明确报错。
func TestGenerateCopywritingWithoutAI(t *testing.T) {
	// service 是没有 AI 能力的服务。
	service := NewService(&fakeRepository{snapshots: make(map[string]Snapshot)}, nil, nil, nil)

	// err 是调用 service.GenerateCopywriting 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := service.GenerateCopywriting(context.Background(), 1, CopywritingRequest{Mode: CopywritingModeListing, Topic: "耳机"}); !errors.Is(err, ErrAIConfigMissing) {
		t.Fatalf("应返回 AI 未配置, got %v", err)
	}
}

// TestGenerateCopywritingEmptyContent 验证 AI 返回空内容按失败处理而不是成功。
func TestGenerateCopywritingEmptyContent(t *testing.T) {
	// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务；chat 是记录调用次数的 AI 替身。
	service, _, _, chat := newTestService(t)
	chat.content = "   "

	// err 是调用 service.GenerateCopywriting 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := service.GenerateCopywriting(context.Background(), 1, CopywritingRequest{Mode: CopywritingModeListing, Topic: "耳机"}); !errors.Is(err, ErrAIEmptyContent) {
		t.Fatalf("空内容应报错, got %v", err)
	}
}

// TestSchedulerStopIsIdempotent 验证调度器重复关闭安全。
func TestSchedulerStopIsIdempotent(t *testing.T) {
	// service 是时钟固定在 2026-10-29 10:00（北京时间）的被测服务。
	service, _, _, _ := newTestService(t)
	// scheduler 是被测调度器，不注入用户集合以避免真实写入。
	scheduler := NewScheduler(service, nil)

	// 未启动时关闭应安全。
	scheduler.Stop()

	scheduler.Start(context.Background())
	scheduler.Stop()
	scheduler.Stop()
}

// TestNormalizeRankingWindow 验证窗口标识归一化。
func TestNormalizeRankingWindow(t *testing.T) {
	if NormalizeRankingWindow("30d") != RankingWindow30Days {
		t.Fatal("30d 应保持不变")
	}
	if NormalizeRankingWindow("") != RankingWindow7Days {
		t.Fatal("空值应按 7 天处理")
	}
	if NormalizeRankingWindow("weird") != RankingWindow7Days {
		t.Fatal("未知值应按 7 天处理")
	}
}
