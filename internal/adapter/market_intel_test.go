// Package adapter: 市场情报仓储适配器焦点测试——快照落库、聚合重算与补判定查询的往返。
package adapter

import (
	"context"
	"path/filepath"
	"testing"

	marketintel "xianyu-go/internal/application/marketintel"
	sourcingapp "xianyu-go/internal/application/sourcing"
	"xianyu-go/internal/db"
)

// newMarketIntelTestStore 打开全新 SQLite 数据库并创建测试账号，返回仓储聚合与用户 ID。
func newMarketIntelTestStore(t *testing.T) (*db.Store, int64) {
	t.Helper()
	// database、dialect、openErr 保存全新 SQLite 数据库的打开结果。
	database, dialect, openErr := db.Open(context.Background(), filepath.Join(t.TempDir(), "market-intel.db"))
	if openErr != nil {
		t.Fatalf("打开数据库失败: %v", openErr)
	}
	t.Cleanup(func() { _ = database.Close() })
	// store 是全新数据库的 repository 聚合入口。
	store := db.NewStore(database, dialect)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// ok、createErr 保存测试用户创建结果。
	ok, createErr := store.Users.Create(ctx, "mi-user", "mi@example.com", "password")
	if createErr != nil || !ok {
		t.Fatalf("创建测试用户失败 ok=%v err=%v", ok, createErr)
	}
	// user、userErr 保存测试用户查询结果。
	user, userErr := store.Users.GetByUsername(ctx, "mi-user")
	if userErr != nil {
		t.Fatal(userErr)
	}
	// cookieErr 保存测试账号写入错误。
	if cookieErr := store.Cookies.Save(ctx, "acc", "unb=1", user.ID); cookieErr != nil {
		t.Fatal(cookieErr)
	}
	return store, user.ID
}

// TestMarketIntelScanStoreSnapshotAndAggregate 验证快照落库、聚合重算与幂等检查。
func TestMarketIntelScanStoreSnapshotAndAggregate(t *testing.T) {
	// store、_ 是带种子用户与账号的仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// scanStore 是被测扫描持久化适配器。
	scanStore := NewMarketIntelScanStore(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// keyword 是测试关注词。
	keyword := "AI课程"
	// 幂等检查：未扫描时返回 false。
	if exists, err := scanStore.SnapshotExists(ctx, keyword, "2026-11-05"); err != nil || exists {
		t.Fatalf("未扫描应返回 false exists=%v err=%v", exists, err)
	}
	// record 是一次扫描记录：两条商品、一条判定。
	record := marketintel.ScanRecord{
		Keyword: keyword, SnapshotDate: "2026-11-05",
		Items: []marketintel.SearchResultItem{
			{ItemID: "1", Title: "AI课程代做", Price: "99", WantCountText: "120人想要", SellerNickname: "卖家甲"},
			{ItemID: "2", Title: "蓝牙耳机", Price: "19.9", WantCountText: "1.2万人想要", SellerNickname: "卖家乙"},
		},
		Verdicts: []marketintel.VerdictResult{
			{SnapshotID: 1, IsAIRelated: true, Category: "AI代做", Reason: "标题含AI"},
			{SnapshotID: 2, IsAIRelated: false, Category: "", Reason: "普通商品"},
		},
	}
	// saveErr 是落库扫描记录错误。
	if saveErr := scanStore.SaveScanRecord(ctx, record); saveErr != nil {
		t.Fatalf("落库扫描记录失败: %v", saveErr)
	}
	// 幂等检查：已扫描时返回 true。
	if exists, err := scanStore.SnapshotExists(ctx, keyword, "2026-11-05"); err != nil || !exists {
		t.Fatalf("已扫描应返回 true exists=%v err=%v", exists, err)
	}
	// pending 是补判定查询结果；两条都已有判定，应为空。
	// err 是补判定查询错误。
	pending, err := scanStore.ListPendingVerdicts(ctx, keyword, "2026-11-05")
	if err != nil || len(pending) != 0 {
		t.Fatalf("全部已判定时补判定应为空 pending=%d err=%v", len(pending), err)
	}
	// 聚合行应正确统计：总数 2、AI 1、want 合计 120+12000。
	// aggregates 与 err 是聚合查询结果与错误。
	aggregates, err := scanStore.ListLatestAggregates(ctx)
	if err != nil || len(aggregates) != 1 {
		t.Fatalf("聚合查询异常 aggregates=%d err=%v", len(aggregates), err)
	}
	// aggregate 是唯一聚合行。
	aggregate := aggregates[0]
	if aggregate.Keyword != keyword || aggregate.TotalCount != 2 || aggregate.AICount != 1 || aggregate.TotalWant != 12120 {
		t.Fatalf("聚合统计不符: %+v", aggregate)
	}
	if aggregate.VerdictStatus != db.MarketIntelVerdictDone {
		t.Fatalf("判定状态应为 done: %s", aggregate.VerdictStatus)
	}
	// 清理功能应可调用。
	if _, err := scanStore.DeleteSnapshotsBefore(ctx, "2026-01-01"); err != nil {
		t.Fatalf("清理调用失败: %v", err)
	}
}

// TestMarketIntelScanStorePendingVerdicts 验证无判定快照进入补判定列表。
func TestMarketIntelScanStorePendingVerdicts(t *testing.T) {
	// store、_ 是带种子用户与账号的仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// scanStore 是被测扫描持久化适配器。
	scanStore := NewMarketIntelScanStore(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// record 只落快照不落判定（模拟判定失败场景）。
	record := marketintel.ScanRecord{
		Keyword: "AI绘画", SnapshotDate: "2026-11-05",
		Items: []marketintel.SearchResultItem{
			{ItemID: "9", Title: "AI绘画接单", Price: "50", WantCountText: "30人想要", SellerNickname: "卖家丙"},
		},
	}
	// saveErr 是落库快照错误。
	if saveErr := scanStore.SaveScanRecord(ctx, record); saveErr != nil {
		t.Fatalf("落库快照失败: %v", saveErr)
	}
	// pending 是待判定列表；应有 1 条。
	// err 是待判定查询错误。
	pending, err := scanStore.ListPendingVerdicts(ctx, "AI绘画", "2026-11-05")
	if err != nil || len(pending) != 1 {
		t.Fatalf("待判定应 1 条 pending=%d err=%v", len(pending), err)
	}
	if pending[0].Title != "AI绘画接单" {
		t.Fatalf("待判定标题不符: %+v", pending[0])
	}
	// aggregates 与 err 是聚合查询结果与错误。
	aggregates, err := scanStore.ListLatestAggregates(ctx)
	if err != nil || len(aggregates) != 1 || aggregates[0].VerdictStatus != db.MarketIntelVerdictPending {
		t.Fatalf("无判定时聚合状态应为 pending: %+v err=%v", aggregates, err)
	}
}

// TestMarketIntelStoreKeywordCRUD 验证词表仓储适配器的完整往返。
func TestMarketIntelStoreKeywordCRUD(t *testing.T) {
	// store、_ 是带种子用户与账号的仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// keywordStore 是被测词表适配器。
	keywordStore := NewMarketIntelStore(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// id 是新增词主键。
	id, err := keywordStore.CreateKeyword(ctx, "AI课程", true, 0)
	if err != nil {
		t.Fatalf("新增关注词失败: %v", err)
	}
	// 重复新增应返回应用哨兵。
	if _, dupErr := keywordStore.CreateKeyword(ctx, "AI课程", true, 1); dupErr != marketintel.ErrKeywordDuplicate {
		t.Fatalf("重复词应返回 ErrKeywordDuplicate，实际 %v", dupErr)
	}
	// 更新与查询。
	if err := keywordStore.UpdateKeyword(ctx, id, "AI课程", false, 0); err != nil {
		t.Fatalf("更新关注词失败: %v", err)
	}
	// items 与 err 是词表查询结果与错误。
	items, err := keywordStore.ListKeywords(ctx)
	if err != nil || len(items) != 1 || items[0].Enabled {
		t.Fatalf("词表查询不符: %+v err=%v", items, err)
	}
	// total、enabled 是计数结果。
	total, enabled, countErr := keywordStore.CountKeywords(ctx)
	if countErr != nil || total != 1 || enabled != 0 {
		t.Fatalf("计数不符 total=%d enabled=%d err=%v", total, enabled, countErr)
	}
	// 不存在的更新应返回应用哨兵。
	if err := keywordStore.UpdateKeyword(ctx, 999, "x", true, 0); err != marketintel.ErrKeywordNotFound {
		t.Fatalf("缺失词更新应返回 ErrKeywordNotFound，实际 %v", err)
	}
	// 删除后列表为空。
	if err := keywordStore.DeleteKeyword(ctx, id); err != nil {
		t.Fatalf("删除关注词失败: %v", err)
	}
	// remainingItems 与 remainingErr 是删除后的词表与错误。
	remainingItems, remainingErr := keywordStore.ListKeywords(ctx)
	if remainingErr != nil || len(remainingItems) != 0 {
		t.Fatalf("删除后词表应为空: %+v err=%v", remainingItems, remainingErr)
	}
}

// TestMarketIntelStoreErrorTranslation 验证 NotFound 错误的哨兵转换。
func TestMarketIntelStoreErrorTranslation(t *testing.T) {
	// store、_ 是带种子用户与账号的仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// keywordStore 是被测词表适配器。
	keywordStore := NewMarketIntelStore(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 删除不存在的词应返回应用哨兵。
	if err := keywordStore.DeleteKeyword(ctx, 999); err != marketintel.ErrKeywordNotFound {
		t.Fatalf("缺失词删除应返回 ErrKeywordNotFound，实际 %v", err)
	}
}

// TestMarketIntelStoreListEnabledAndAggregates 验证启用词过滤与趋势聚合查询。
func TestMarketIntelStoreListEnabledAndAggregates(t *testing.T) {
	// store、_ 是带种子用户与账号的仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// keywordStore 是被测词表适配器。
	keywordStore := NewMarketIntelStore(store)
	// scanStore 是被测扫描持久化适配器。
	scanStore := NewMarketIntelScanStore(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 写入一启一停两个词。
	if _, err := keywordStore.CreateKeyword(ctx, "启用词", true, 0); err != nil {
		t.Fatal(err)
	}
	// disabledID 是禁用词主键。
	disabledID, err := keywordStore.CreateKeyword(ctx, "禁用词", false, 1)
	if err != nil {
		t.Fatal(err)
	}
	// enabledItems 是启用词查询结果；应只有 1 条。
	enabledItems, err := keywordStore.ListEnabledKeywords(ctx)
	if err != nil || len(enabledItems) != 1 || enabledItems[0].Keyword != "启用词" {
		t.Fatalf("启用词查询不符: %+v err=%v", enabledItems, err)
	}
	// 落两天聚合数据。
	for _, date := range []string{"2026-11-04", "2026-11-05"} {
		// saveErr 是逐日落库错误。
		if saveErr := scanStore.SaveScanRecord(ctx, marketintel.ScanRecord{
			Keyword: "启用词", SnapshotDate: date,
			Items: []marketintel.SearchResultItem{{ItemID: "1", Title: "AI课程", Price: "9", WantCountText: "10人想要"}},
		}); saveErr != nil {
			t.Fatal(saveErr)
		}
	}
	// trendRows 与 err 是趋势聚合查询结果与错误。
	trendRows, err := scanStore.ListAggregates(ctx, "启用词", 30)
	if err != nil || len(trendRows) != 2 || trendRows[0].Date > trendRows[1].Date {
		t.Fatalf("趋势聚合查询不符: %+v err=%v", trendRows, err)
	}
	// 删除禁用词后启用词仍不变。
	if err := keywordStore.DeleteKeyword(ctx, disabledID); err != nil {
		t.Fatal(err)
	}
	// finalItems 与 err 是删除后的词表与错误。
	finalItems, err := keywordStore.ListKeywords(ctx)
	if err != nil || len(finalItems) != 1 {
		t.Fatalf("删除后词表不符: %+v err=%v", finalItems, err)
	}
}

// TestMarketIntelScanStoreSellerStats 验证卖家榜统计只统计 AI 相关商品。
func TestMarketIntelScanStoreSellerStats(t *testing.T) {
	// store、_ 是带种子用户与账号的仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// scanStore 是被测扫描持久化适配器。
	scanStore := NewMarketIntelScanStore(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 落两条商品：一条 AI 相关（卖家甲），一条非 AI（卖家乙）。
	if err := scanStore.SaveScanRecord(ctx, marketintel.ScanRecord{
		Keyword: "AI课程", SnapshotDate: "2026-11-05",
		Items: []marketintel.SearchResultItem{
			{ItemID: "1", Title: "AI课程代做", Price: "100", WantCountText: "10人想要", SellerNickname: "卖家甲"},
			{ItemID: "2", Title: "蓝牙耳机", Price: "20", WantCountText: "5人想要", SellerNickname: "卖家乙"},
		},
		Verdicts: []marketintel.VerdictResult{
			{SnapshotID: 1, IsAIRelated: true, Category: "AI代做", Reason: "AI"},
			{SnapshotID: 2, IsAIRelated: false, Category: "", Reason: "普通"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// sellers 与 err 是卖家榜查询结果与错误。
	sellers, err := scanStore.ListSellerStats(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(sellers) != 1 || sellers[0].Nickname != "卖家甲" || sellers[0].Appearances != 1 {
		t.Fatalf("卖家榜应只含 AI 相关卖家: %+v", sellers)
	}
	// 均价应为 100.00。
	if sellers[0].AvgPrice != "100.00" {
		t.Fatalf("均价不符: %s", sellers[0].AvgPrice)
	}
}

// TestMarketIntelScanStoreClassifyError 验证平台错误分类。
func TestMarketIntelScanStoreClassifyError(t *testing.T) {
	// store、_ 是带种子用户与账号的仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// scanStore 是被测扫描持久化适配器。
	scanStore := NewMarketIntelScanStore(store)
	// 风控错误应分类为 risk。
	if got := scanStore.ClassifyError(sourcingapp.ErrRiskControl); got != "risk" {
		t.Fatalf("风控应分类为 risk，实际 %s", got)
	}
	// 会话失效应分类为 session。
	if got := scanStore.ClassifyError(sourcingapp.ErrSessionExpired); got != "session" {
		t.Fatalf("会话失效应分类为 session，实际 %s", got)
	}
	// 普通错误应分类为 other。
	if got := scanStore.ClassifyError(context.Canceled); got != "other" {
		t.Fatalf("普通错误应分类为 other，实际 %s", got)
	}
}

// TestMarketIntelSearchPortConstruction 验证搜索端口构造与凭证缺失降级。
func TestMarketIntelSearchPortConstruction(t *testing.T) {
	// store、userID 是带种子用户与账号的仓储聚合和测试用户。
	store, userID := newMarketIntelTestStore(t)
	// gateway 是既有选品浏览器网关（nil manager 时仍可构造端口对象）。
	gateway := &SourcingBrowserGateway{}
	// credentials 是凭证读取适配器。
	credentials := NewSourcingCredentialReader(store)
	// port 是被测搜索端口。
	port := NewMarketIntelSearchPort(gateway, credentials, userID, "acc")
	if port == nil {
		t.Fatal("搜索端口构造不应为 nil")
	}
	// 构造 VerdictPort。
	verdictPort := NewMarketIntelVerdictPort(store, userID)
	if verdictPort == nil {
		t.Fatal("判定端口构造不应为 nil")
	}
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 判定端口在未配置密钥时应返回明确错误。
	if _, err := verdictPort.JudgeBatch(ctx, "AI课程", []marketintel.VerdictInput{{SnapshotID: 1, Title: "AI课程"}}); err == nil {
		t.Fatal("未配置密钥应返回错误")
	}
}

// TestSourcingRepositoryListRecentSnapshots 验证选品参考仓储的最近快照序列查询。
func TestSourcingRepositoryListRecentSnapshots(t *testing.T) {
	// store、_ 是带种子用户与账号的仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// repository 是选品参考持久化适配器。
	repository := NewSourcingRepository(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 写入两天快照（日期倒序写入以验证排序）。
	for _, date := range []string{"2026-11-05", "2026-11-04"} {
		// saveErr 是逐日写入错误。
		if saveErr := repository.SaveSnapshot(ctx, sourcingapp.Snapshot{
			Kind: "hot_keywords", Date: date, PayloadJSON: `{"keywords":["A"]}`, Status: "ok",
		}); saveErr != nil {
			t.Fatal(saveErr)
		}
	}
	// rows 是最近快照序列；应按日期倒序返回。
	rows, err := repository.ListRecentSnapshots(ctx, "hot_keywords", 10)
	if err != nil || len(rows) != 2 || rows[0].Date != "2026-11-05" || rows[1].Date != "2026-11-04" {
		t.Fatalf("最近快照序列不符: %+v err=%v", rows, err)
	}
}
