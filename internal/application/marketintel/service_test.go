package marketintel

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeStore 是词表仓储替身。
type fakeStore struct {
	// keywords 是内存词表，键为主键。
	keywords map[int64]*WatchedKeyword
	// nextID 是下一个主键。
	nextID int64
}

// newFakeStore 构造空词表替身。
func newFakeStore() *fakeStore {
	return &fakeStore{keywords: make(map[int64]*WatchedKeyword), nextID: 1}
}

// ListKeywords 返回全部词，按排序值升序。
func (f *fakeStore) ListKeywords(_ context.Context) ([]WatchedKeyword, error) {
	// out 是结果集合。
	out := make([]WatchedKeyword, 0, len(f.keywords))
	// item 遍历内存词表。
	for _, item := range f.keywords {
		out = append(out, *item)
	}
	sortKeywordsByID(out)
	return out, nil
}

// ListEnabledKeywords 返回启用中的词。
func (f *fakeStore) ListEnabledKeywords(_ context.Context) ([]WatchedKeyword, error) {
	// all 是全部词。
	all, err := f.ListKeywords(context.Background())
	if err != nil {
		return nil, err
	}
	// out 只保留启用词。
	out := make([]WatchedKeyword, 0, len(all))
	// item 遍历全部词。
	for _, item := range all {
		if item.Enabled {
			out = append(out, item)
		}
	}
	return out, nil
}

// CreateKeyword 新增词；重复返回 ErrKeywordDuplicate。
func (f *fakeStore) CreateKeyword(_ context.Context, keyword string, enabled bool, sortOrder int) (int64, error) {
	// item 遍历检查重复。
	for _, item := range f.keywords {
		if item.Keyword == keyword {
			return 0, ErrKeywordDuplicate
		}
	}
	// id 是新主键。
	id := f.nextID
	f.nextID++
	f.keywords[id] = &WatchedKeyword{ID: id, Keyword: keyword, Enabled: enabled, SortOrder: sortOrder}
	return id, nil
}

// UpdateKeyword 更新词；不存在返回 ErrKeywordNotFound。
func (f *fakeStore) UpdateKeyword(_ context.Context, id int64, keyword string, enabled bool, sortOrder int) error {
	// item 是目标词。
	item, ok := f.keywords[id]
	// ok 表示目标词是否存在。
	if !ok {
		return ErrKeywordNotFound
	}
	// other 遍历检查重复。
	for _, other := range f.keywords {
		if other.ID != id && other.Keyword == keyword {
			return ErrKeywordDuplicate
		}
	}
	item.Keyword, item.Enabled, item.SortOrder = keyword, enabled, sortOrder
	return nil
}

// DeleteKeyword 删除词；不存在返回 ErrKeywordNotFound。
func (f *fakeStore) DeleteKeyword(_ context.Context, id int64) error {
	// ok 表示目标词是否存在。
	if _, ok := f.keywords[id]; !ok {
		return ErrKeywordNotFound
	}
	delete(f.keywords, id)
	return nil
}

// CountKeywords 返回总数与启用数。
func (f *fakeStore) CountKeywords(_ context.Context) (int, int, error) {
	// total、enabled 是计数结果。
	total, enabled := 0, 0
	// item 遍历内存词表。
	for _, item := range f.keywords {
		total++
		if item.Enabled {
			enabled++
		}
	}
	return total, enabled, nil
}

// fakeScanStore 是扫描持久化替身，记录调用与状态。
type fakeScanStore struct {
	// doneKeywords 是已完成扫描的「词|日期」集合。
	doneKeywords map[string]bool
	// saved 是保存过的扫描记录。
	saved []ScanRecord
	// deletedBefore 是最近一次清理截止日。
	deletedBefore string
	// pendingVerdicts 是补判定查询返回值。
	pendingVerdicts []VerdictInput
	// classify 是错误分类函数。
	classify func(error) string
}

// SnapshotExists 返回预置幂等状态。
func (f *fakeScanStore) SnapshotExists(_ context.Context, keyword, snapshotDate string) (bool, error) {
	return f.doneKeywords[keyword+"|"+snapshotDate], nil
}

// SaveScanRecord 记录扫描记录。
func (f *fakeScanStore) SaveScanRecord(_ context.Context, record ScanRecord) error {
	f.saved = append(f.saved, record)
	f.doneKeywords[record.Keyword+"|"+record.SnapshotDate] = true
	return nil
}

// DeleteSnapshotsBefore 记录清理截止日。
func (f *fakeScanStore) DeleteSnapshotsBefore(_ context.Context, beforeDate string) (int64, error) {
	f.deletedBefore = beforeDate
	return 0, nil
}

// ListPendingVerdicts 返回预置待判定快照。
func (f *fakeScanStore) ListPendingVerdicts(_ context.Context, _, _ string) ([]VerdictInput, error) {
	return f.pendingVerdicts, nil
}

// ClassifyError 使用预置分类函数。
func (f *fakeScanStore) ClassifyError(err error) string { return f.classify(err) }

// fakeSearch 是搜索端口替身。
type fakeSearch struct {
	// results 是每次搜索返回的结果。
	results []SearchResultItem
	// err 是搜索返回错误。
	err error
	// calls 记录搜索调用次数与词序。
	calls []string
}

// SearchKeyword 记录词并返回预置结果。
func (f *fakeSearch) SearchKeyword(_ context.Context, keyword string) ([]SearchResultItem, error) {
	f.calls = append(f.calls, keyword)
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

// fakeVerdict 是 AI 判定端口替身。
type fakeVerdict struct {
	// err 是判定返回错误。
	err error
	// calls 记录每次判定的输入数量。
	calls []int
}

// JudgeBatch 记录输入规模并返回预置错误或空结果。
func (f *fakeVerdict) JudgeBatch(_ context.Context, _ string, items []VerdictInput) ([]VerdictResult, error) {
	f.calls = append(f.calls, len(items))
	if f.err != nil {
		return nil, f.err
	}
	// out 是与输入等长的结果。
	out := make([]VerdictResult, 0, len(items))
	// index 遍历输入。
	for index, item := range items {
		out = append(out, VerdictResult{SnapshotID: item.SnapshotID, IsAIRelated: index%2 == 0, Category: "AI课程", Reason: "标题含 AI"})
	}
	return out, nil
}

// newTestService 构造被测服务与全部替身；时钟固定为 2026-11-05 10:00 北京时间。
func newTestService(t *testing.T) (*Service, *fakeStore, *fakeScanStore, *fakeSearch, *fakeVerdict) {
	t.Helper()
	// store 是词表替身。
	store := newFakeStore()
	// scanStore 是扫描持久化替身。
	scanStore := &fakeScanStore{doneKeywords: make(map[string]bool), classify: func(error) string { return "other" }}
	// search 是搜索端口替身。
	search := &fakeSearch{results: []SearchResultItem{{ItemID: "1", Title: "AI课程代做", Price: "99", WantCountText: "120人想要", SellerNickname: "卖家甲"}}}
	// verdict 是判定端口替身。
	verdict := &fakeVerdict{}
	// service 是被测服务。
	service := NewService(store, scanStore, search, verdict, nil).
		WithClock(func() time.Time { return time.Date(2026, 11, 5, 10, 0, 0, 0, time.FixedZone("CST", 8*3600)) })
	return service, store, scanStore, search, verdict
}

// TestCreateKeywordLimitEnforced 验证启用词上限 30 的硬校验。
func TestCreateKeywordLimitEnforced(t *testing.T) {
	// service、store 是被测服务与词表替身。
	service, store, _, _, _ := newTestService(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// index 填满 30 个启用词。
	for index := 0; index < MaxEnabledKeywords; index++ {
		// createErr 是逐词创建错误。
		if _, createErr := service.CreateKeyword(ctx, "词"+strconv.Itoa(index), true); createErr != nil {
			t.Fatalf("第 %d 个词创建失败: %v", index, createErr)
		}
	}
	// 第 31 个启用词必须被拒绝。
	if _, err := service.CreateKeyword(ctx, "超额词", true); !errors.Is(err, ErrKeywordLimitReached) {
		t.Fatalf("第 31 个启用词应被拒绝，实际 %v", err)
	}
	// 禁用词不受上限约束。
	if _, err := service.CreateKeyword(ctx, "禁用词", false); err != nil {
		t.Fatalf("禁用词不应受限: %v", err)
	}
	_ = store
}

// TestRunDailyScanIdempotentAndStopsOnRisk 验证幂等跳过与风控即停。
func TestRunDailyScanIdempotentAndStopsOnRisk(t *testing.T) {
	// service、store、scanStore、search、verdict 是被测服务与替身。
	service, store, scanStore, search, _ := newTestService(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// 添加两个启用词。
	if _, err := service.CreateKeyword(ctx, "词A", true); err != nil {
		t.Fatal(err)
	}
	// errB 是词 B 创建错误。
	if _, errB := service.CreateKeyword(ctx, "词B", true); errB != nil {
		t.Fatal(errB)
	}
	// 第一次扫描：两个词都执行。
	result, err := service.RunDailyScan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 2 || result.Skipped != 0 {
		t.Fatalf("首次扫描应扫 2 词: %+v", result)
	}
	if len(search.calls) != 2 {
		t.Fatalf("应发出 2 次搜索: %v", search.calls)
	}
	// 第二次扫描：全部幂等跳过，零搜索请求。
	result, err = service.RunDailyScan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 0 || result.Skipped != 2 {
		t.Fatalf("二次扫描应全部跳过: %+v", result)
	}
	if len(search.calls) != 2 {
		t.Fatalf("幂等跳过后不应再发搜索: %v", search.calls)
	}
	// 风控错误：第三个词触发风控，批次立即中止。
	if _, err := service.CreateKeyword(ctx, "词C", true); err != nil {
		t.Fatal(err)
	}
	scanStore.classify = func(error) string { return "risk" }
	search.err = errors.New("FAIL_SYS_USER_VALIDATE")
	// 清掉词 A/B 的幂等状态以强制重新扫描。
	scanStore.doneKeywords = make(map[string]bool)
	_, err = service.RunDailyScan(ctx)
	if !errors.Is(err, ErrRiskControl) {
		t.Fatalf("风控应中止批次: %v", err)
	}
	// 词 B/C 不应被扫描（词 A 风控后立即停止）。
	if len(search.calls) != 3 {
		t.Fatalf("风控后不应继续扫描: %v", search.calls)
	}
	_ = store
}

// TestRunDailyScanVerdictFailureDegrades 验证 AI 判定失败时快照仍落库。
func TestRunDailyScanVerdictFailureDegrades(t *testing.T) {
	// service、store、scanStore、search、verdict 是被测服务与替身。
	service, _, scanStore, _, verdict := newTestService(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// errA 是词 A 创建错误。
	if _, errA := service.CreateKeyword(ctx, "词A", true); errA != nil {
		t.Fatal(errA)
	}
	verdict.err = errors.New("模型返回非法 JSON")
	// result 是扫描结果。
	result, err := service.RunDailyScan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.VerdictFailed != 1 {
		t.Fatalf("判定失败应计数: %+v", result)
	}
	// 快照必须已落库。
	if len(scanStore.saved) != 1 || len(scanStore.saved[0].Items) != 1 {
		t.Fatalf("判定失败时快照仍应落库: %+v", scanStore.saved)
	}
	if scanStore.saved[0].Verdicts != nil {
		t.Fatal("判定失败时不应有判定结果")
	}
}

// TestBackfillVerdicts 验证补判定路径。
func TestBackfillVerdicts(t *testing.T) {
	// service、scanStore、verdict 是被测服务与替身。
	service, _, scanStore, _, verdict := newTestService(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// 无待判定快照时直接返回。
	if err := service.BackfillVerdicts(ctx, "词A"); err != nil {
		t.Fatal(err)
	}
	if len(verdict.calls) != 0 {
		t.Fatal("无待判定时不应调用模型")
	}
	// 有待判定快照时补判定。
	scanStore.pendingVerdicts = []VerdictInput{{SnapshotID: 7, Title: "AI绘画接单"}}
	// backfillErr 是补判定错误。
	if backfillErr := service.BackfillVerdicts(ctx, "词A"); backfillErr != nil {
		t.Fatal(backfillErr)
	}
	if len(verdict.calls) != 1 || verdict.calls[0] != 1 {
		t.Fatalf("应补判定 1 条: %v", verdict.calls)
	}
	// 补判定记录应保存。
	if len(scanStore.saved) != 1 || len(scanStore.saved[0].Verdicts) != 1 {
		t.Fatalf("补判定结果应落库: %+v", scanStore.saved)
	}
}

// TestEnsureSeedKeywords 验证种子词幂等初始化。
func TestEnsureSeedKeywords(t *testing.T) {
	// service、store 是被测服务与词表替身。
	service, store, _, _, _ := newTestService(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// seedErr 是种子词初始化错误。
	if seedErr := service.EnsureSeedKeywords(ctx); seedErr != nil {
		t.Fatal(seedErr)
	}
	// total 是首次初始化后的词数。
	total, _, err := store.CountKeywords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total != len(DefaultSeedKeywords) {
		t.Fatalf("种子词应写入 %d 个，实际 %d", len(DefaultSeedKeywords), total)
	}
	// 二次调用必须跳过。
	if err := service.EnsureSeedKeywords(ctx); err != nil {
		t.Fatal(err)
	}
	total, _, err = store.CountKeywords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total != len(DefaultSeedKeywords) {
		t.Fatalf("二次初始化不应重复写入: %d", total)
	}
}

// TestKeywordServiceDelegates 验证词表服务的查询、更新与删除委托路径。
func TestKeywordServiceDelegates(t *testing.T) {
	// service、store 是被测服务与词表替身。
	service, store, _, _, _ := newTestService(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// created 是创建结果。
	created, createErr := service.CreateKeyword(ctx, "AI课程", true)
	if createErr != nil {
		t.Fatal(createErr)
	}
	// items 是词表查询结果。
	items, listErr := service.ListKeywords(ctx)
	if listErr != nil || len(items) != 1 || items[0].Keyword != "AI课程" {
		t.Fatalf("词表查询不符: %+v err=%v", items, listErr)
	}
	// 更新为禁用。
	if err := service.UpdateKeyword(ctx, created.ID, "AI课程", false); err != nil {
		t.Fatalf("更新关注词失败: %v", err)
	}
	// updated 是更新后的词表。
	updated, listErr := service.ListKeywords(ctx)
	if listErr != nil || updated[0].Enabled {
		t.Fatalf("更新后词表不符: %+v err=%v", updated, listErr)
	}
	// 更新不存在的词应返回 ErrKeywordNotFound。
	if err := service.UpdateKeyword(ctx, 999, "x", true); err != ErrKeywordNotFound {
		t.Fatalf("缺失词更新应返回 ErrKeywordNotFound，实际 %v", err)
	}
	// 空白词应被拒绝。
	if _, emptyErr := service.CreateKeyword(ctx, "  ", true); emptyErr != ErrKeywordEmpty {
		t.Fatalf("空白词应返回 ErrKeywordEmpty，实际 %v", emptyErr)
	}
	// 删除。
	if err := service.DeleteKeyword(ctx, created.ID); err != nil {
		t.Fatalf("删除关注词失败: %v", err)
	}
	// finalItems 是删除后的词表。
	finalItems, listErr := service.ListKeywords(ctx)
	if listErr != nil || len(finalItems) != 0 {
		t.Fatalf("删除后词表应为空: %+v err=%v", finalItems, listErr)
	}
	_ = store
}

// TestSchedulerRunIfDueAndStop 验证调度器的时间门控与幂等停止。
func TestSchedulerRunIfDueAndStop(t *testing.T) {
	// service、scanStore、search 是被测服务与替身。
	service, _, scanStore, search, _ := newTestService(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// createErr 是测试词创建错误。
	if _, createErr := service.CreateKeyword(ctx, "词A", true); createErr != nil {
		t.Fatal(createErr)
	}
	// scheduler 是被测调度器。
	scheduler := NewScheduler(service, nil)
	// 未到执行小时（时钟固定 10 点已过 8 点，但用 lastRunDate 门控验证幂等）。
	scheduler.runIfDue(ctx)
	if len(search.calls) != 1 {
		t.Fatalf("首次执行应扫描 1 词: %v", search.calls)
	}
	// 同日第二次执行必须幂等跳过。
	scheduler.runIfDue(ctx)
	if len(search.calls) != 1 {
		t.Fatalf("同日重复执行应跳过: %v", search.calls)
	}
	// Stop 幂等。
	scheduler.Stop()
	scheduler.Stop()
	_ = scanStore
}

// TestSchedulerStartRunsAndStops 验证 Start 启动后台协程且 Stop 等待退出。
func TestSchedulerStartRunsAndStops(t *testing.T) {
	// service 是被测服务（无词表，扫描为空操作）。
	service, _, _, _, _ := newTestService(t)
	// scheduler 是被测调度器。
	scheduler := NewScheduler(service, nil)
	// ctx 是可取消的上下文。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler.Start(ctx)
	// 重复 Start 不得启动第二个协程。
	scheduler.Start(ctx)
	scheduler.Stop()
}

// TestViewDelegatesWithFakeScanPersistence 验证视图查询在 scan 实现完整接口时的委托路径。
func TestViewDelegatesWithFakeScanPersistence(t *testing.T) {
	// service、scanStore 是被测服务与替身。
	service, scanStore, _, _, _ := newTestService(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// fakeScanStore 未实现 queryPersistence，LatestAggregates 应返回空集合。
	empty, err := service.LatestAggregates(ctx)
	if err != nil || len(empty) != 0 {
		t.Fatalf("无查询实现应返回空集合: %+v err=%v", empty, err)
	}
	// 趋势查询：词不存在应返回 ErrKeywordNotFound。
	if _, err := service.KeywordTrend(ctx, 999, 30); err != ErrKeywordNotFound {
		t.Fatalf("缺失词趋势应返回 ErrKeywordNotFound，实际 %v", err)
	}
	// 创建词后再查趋势：fakeScanStore 未实现查询接口，返回空点。
	created, createErr := service.CreateKeyword(ctx, "AI课程", true)
	if createErr != nil {
		t.Fatal(createErr)
	}
	// trend 是趋势查询结果。
	trend, trendErr := service.KeywordTrend(ctx, created.ID, 30)
	if trendErr != nil || trend.Keyword != "AI课程" || len(trend.Points) != 0 {
		t.Fatalf("趋势查询不符: %+v err=%v", trend, trendErr)
	}
	// 卖家榜：未实现查询接口时返回空集合。
	board, boardErr := service.SellerBoard(ctx, 30)
	if boardErr != nil || len(board.Sellers) != 0 {
		t.Fatalf("卖家榜应返回空集合: %+v err=%v", board, boardErr)
	}
	_ = scanStore
}

// TestBuildCategoryCountsAndHelpers 验证类目统计与 JSON 序列化辅助。
func TestBuildCategoryCountsAndHelpers(t *testing.T) {
	// verdicts 是判定集合。
	verdicts := []VerdictResult{
		{IsAIRelated: true, Category: "AI课程"},
		{IsAIRelated: true, Category: "AI课程"},
		{IsAIRelated: true, Category: "AI代做"},
		{IsAIRelated: false, Category: ""},
	}
	// counts 是类目统计结果。
	counts := buildCategoryCounts(verdicts)
	if len(counts) != 2 || counts[0].Name != "AI课程" || counts[0].Count != 2 || counts[1].Count != 1 {
		t.Fatalf("类目统计不符: %+v", counts)
	}
	// 空判定应返回空列表。
	if empty := buildCategoryCounts(nil); len(empty) != 0 {
		t.Fatalf("空判定应返回空列表: %+v", empty)
	}
	// marshalJSONOrNull：空列表与 nil 返回空串。
	if got := marshalJSONOrNull([]string{}); got != "" {
		t.Fatalf("空列表应返回空串: %q", got)
	}
	// 非空列表应返回 JSON。
	if got := marshalJSONOrNull([]string{"a"}); got != `["a"]` {
		t.Fatalf("非空列表应返回 JSON: %q", got)
	}
	// scanSummary 应包含关键计数字段。
	summary := scanSummary(ScanResult{Date: "2026-11-05", Scanned: 2, Skipped: 1, VerdictFailed: 1, StoppedReason: "x"})
	if summary == "" || !containsAll(summary, []string{"2026-11-05", "scanned=2", "skipped=1"}) {
		t.Fatalf("扫描摘要不符: %q", summary)
	}
}

// containsAll 判断文本是否包含全部子串。
func containsAll(text string, substrings []string) bool {
	// substring 遍历子串。
	for _, substring := range substrings {
		if !strings.Contains(text, substring) {
			return false
		}
	}
	return true
}
