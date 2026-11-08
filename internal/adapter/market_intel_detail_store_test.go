// Package adapter: 市场情报详情缓存、词商品列表与扫描账号解析的焦点测试。
package adapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	marketintel "xianyu-go/internal/application/marketintel"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
)

// newMarketIntelAdapterStore 打开全新测试数据库并写入一个用户与一个账号，返回仓储与用户主键。
func newMarketIntelAdapterStore(t *testing.T) (*db.Store, int64) {
	t.Helper()
	// store、cleanup 是测试数据库与关闭函数。
	store, cleanup := newDeliveryTemplateAdapterStore(t)
	t.Cleanup(cleanup)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 账号外键需要用户，先写入测试用户。
	result, err := store.DB.ExecContext(ctx, `INSERT INTO users (username, email, password_hash) VALUES ('mi-scan','mi-scan@example.test','x')`)
	if err != nil {
		t.Fatalf("写入测试用户失败: %v", err)
	}
	// userID 是新建用户主键。
	userID, _ := result.LastInsertId()
	// 写入账号供扫描账号自动选择与 cookie 读取使用。
	if saveErr := store.Cookies.Save(ctx, "scan-account", "unb=1; _m_h5_tk=abc", userID); saveErr != nil {
		t.Fatalf("写入测试账号失败: %v", saveErr)
	}
	return store, userID
}

// TestResolveCookieID 验证扫描账号解析：显式配置优先、无账号目录与无可用账号的错误分支。
func TestResolveCookieID(t *testing.T) {
	// store、_ 是含一个账号的测试数据库。
	store, _ := newMarketIntelAdapterStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// 显式配置优先，且去除首尾空白。
	explicit := NewMarketIntelSearchPort(nil, nil, nil, 1, "  explicit-account  ", nil)
	// err、got 是本次调用的返回错误；本次调用的实际返回值。
	if got, err := explicit.resolveCookieID(ctx); err != nil || got != "explicit-account" {
		t.Fatalf("显式账号解析不符: %q err=%v", got, err)
	}
	// 未配置账号且无账号目录时返回明确错误。
	autoWithoutAdmin := NewMarketIntelSearchPort(nil, nil, nil, 1, "", nil)
	// err 是本次调用的返回错误。
	if _, err := autoWithoutAdmin.resolveCookieID(ctx); err == nil {
		t.Fatal("无账号目录且未配置账号应报错")
	}
	// 有账号目录时自动选择第一个账号。
	auto := NewMarketIntelSearchPort(nil, nil, store, 1, "  ", nil)
	// err、got 是本次调用的返回错误；本次调用的实际返回值。
	if got, err := auto.resolveCookieID(ctx); err != nil || got != "scan-account" {
		t.Fatalf("自动选择账号不符: %q err=%v", got, err)
	}
	// 账号目录读取失败时返回包裹错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatalf("关闭测试数据库失败: %v", closeErr)
	}
	// err 是本次调用的返回错误。
	if _, err := auto.resolveCookieID(ctx); err == nil {
		t.Fatal("账号目录读取失败应报错")
	}
}

// TestResolveCookieIDFromSetting 验证系统设置指定的抓取账号优先于自动选择，并校验账号有效性。
func TestResolveCookieIDFromSetting(t *testing.T) {
	// store、userID 是含一个账号的测试数据库与账号归属用户。
	store, userID := newMarketIntelAdapterStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 再写入一个账号，并显式固定两个账号的创建时间，避免同秒写入导致排序不确定。
	if saveErr := store.Cookies.Save(ctx, "newer-account", "unb=2", userID); saveErr != nil {
		t.Fatalf("写入第二个账号失败: %v", saveErr)
	}
	// stampErr 是固定旧账号创建时间返回的错误。
	if _, stampErr := store.DB.ExecContext(ctx, `UPDATE cookies SET created_at='2026-10-01 00:00:00' WHERE id='scan-account'`); stampErr != nil {
		t.Fatalf("固定旧账号创建时间失败: %v", stampErr)
	}
	// stampErr 是固定新账号创建时间返回的错误。
	if _, stampErr := store.DB.ExecContext(ctx, `UPDATE cookies SET created_at='2026-10-02 00:00:00' WHERE id='newer-account'`); stampErr != nil {
		t.Fatalf("固定新账号创建时间失败: %v", stampErr)
	}
	// port 是未显式指定账号的抓取端口。
	port := NewMarketIntelSearchPort(nil, nil, store, userID, "", nil)
	// 未配置设置时回落为账号目录第一个（创建时间最新）。
	if got, err := port.resolveCookieID(ctx); err != nil || got != "newer-account" {
		t.Fatalf("未配置设置应自动选择最新账号: %q err=%v", got, err)
	}
	// 配置为较早创建的账号后必须优先使用它。
	if setErr := store.Settings.Set(ctx, SettingMarketIntelScanAccount, "scan-account"); setErr != nil {
		t.Fatalf("写入抓取账号设置失败: %v", setErr)
	}
	// got、err 是本次调用的实际返回值与返回错误。
	if got, err := port.resolveCookieID(ctx); err != nil || got != "scan-account" {
		t.Fatalf("设置指定的账号应优先: %q err=%v", got, err)
	}
	// 配置的账号已不存在时必须明确报错，不能静默换账号。
	if setErr := store.Settings.Set(ctx, SettingMarketIntelScanAccount, "deleted-account"); setErr != nil {
		t.Fatalf("写入失效账号设置失败: %v", setErr)
	}
	// missingErr 是配置账号不存在时的解析错误。
	_, missingErr := port.resolveCookieID(ctx)
	if missingErr == nil || !strings.Contains(missingErr.Error(), "不存在") {
		t.Fatalf("配置账号不存在应报错: %v", missingErr)
	}
	// 清空设置后应回到自动选择。
	if setErr := store.Settings.Set(ctx, SettingMarketIntelScanAccount, ""); setErr != nil {
		t.Fatalf("清空抓取账号设置失败: %v", setErr)
	}
	// got、err 是本次调用的实际返回值与返回错误。
	if got, err := port.resolveCookieID(ctx); err != nil || got != "newer-account" {
		t.Fatalf("清空设置应回到自动选择: %q err=%v", got, err)
	}
	// 数据库不可用时读取设置必须报错，不能静默回落到自动选择。
	if setErr := store.Settings.Set(ctx, SettingMarketIntelScanAccount, "scan-account"); setErr != nil {
		t.Fatalf("重新写入抓取账号设置失败: %v", setErr)
	}
	// closeErr 是关闭测试数据库时返回的错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatalf("关闭测试数据库失败: %v", closeErr)
	}
	// closedErr 是数据库不可用时的解析错误。
	if _, closedErr := port.resolveCookieID(ctx); closedErr == nil {
		t.Fatal("数据库不可用时读取设置应报错")
	}
}

// TestMarketIntelSearchPortFetchDetailErrors 验证竞品详情抓取的账号解析与凭证读取失败分支。
func TestMarketIntelSearchPortFetchDetailErrors(t *testing.T) {
	// store、userID 是含账号的测试数据库与账号归属用户。
	store, userID := newMarketIntelAdapterStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// 未配置账号且无账号目录时，详情抓取必须在浏览器调用前失败。
	noAccount := NewMarketIntelSearchPort(nil, NewSourcingCredentialReader(store), nil, userID, "", nil)
	// err 是本次调用的返回错误。
	if _, err := noAccount.FetchCompetitorDetail(ctx, "item-1"); err == nil {
		t.Fatal("无扫描账号应报错")
	}
	// 账号存在但归属用户不匹配时，凭证读取必须拒绝且不泄露登录态。
	foreign := NewMarketIntelSearchPort(nil, NewSourcingCredentialReader(store), store, userID+999, "scan-account", nil)
	// err 是本次调用的返回错误。
	if _, err := foreign.FetchCompetitorDetail(ctx, "item-1"); err == nil {
		t.Fatal("账号不属于当前用户时应报错")
	}
}

// TestMarketIntelScanStoreDetailCache 验证详情缓存行的写入、读取与缺失错误映射。
func TestMarketIntelScanStoreDetailCache(t *testing.T) {
	// store、_ 是测试数据库。
	store, _ := newMarketIntelAdapterStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// scanStore 是被测扫描持久化适配器。
	scanStore := NewMarketIntelScanStore(store)

	// 未写入前读取必须返回稳定的未找到错误。
	if _, err := scanStore.LoadDetailRow(ctx, "item-1"); !errors.Is(err, db.ErrCompetitorDetailNotFound) {
		t.Fatalf("无缓存应返回 ErrCompetitorDetailNotFound, got %v", err)
	}
	// row 是待写入的缓存行。
	row := marketintel.DetailCacheRowAlias{
		ItemID: "item-1", Title: "标题", Price: "99", DescText: "描述",
		ImagesJSON: `["https://example.com/a.jpg"]`, SellerNickname: "卖家甲", SellerID: "u1", WantCountText: "10人想要",
	}
	// err 是本次调用的返回错误。
	if err := scanStore.SaveDetailRow(ctx, row); err != nil {
		t.Fatalf("写入缓存行失败: %v", err)
	}
	// loaded、err 是读取结果与错误。
	loaded, err := scanStore.LoadDetailRow(ctx, "item-1")
	if err != nil {
		t.Fatalf("读取缓存行失败: %v", err)
	}
	if loaded.Title != "标题" || loaded.Price != "99" || loaded.SellerID != "u1" || loaded.ImagesJSON != row.ImagesJSON || loaded.FetchedAt.IsZero() {
		t.Fatalf("缓存行字段不符: %+v", loaded)
	}
	// 数据库不可用时读写都必须返回错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatalf("关闭测试数据库失败: %v", closeErr)
	}
	// readErr 是数据库不可用时的读取错误。
	if _, readErr := scanStore.LoadDetailRow(ctx, "item-1"); readErr == nil {
		t.Fatal("数据库不可用时读取应报错")
	}
	// writeErr 是数据库不可用时的写入错误。
	if writeErr := scanStore.SaveDetailRow(ctx, row); writeErr == nil {
		t.Fatal("数据库不可用时写入应报错")
	}
}

// TestMarketIntelScanStoreListKeywordItems 验证词商品列表：无快照返回空集合、有快照按最新日映射。
func TestMarketIntelScanStoreListKeywordItems(t *testing.T) {
	// store、_ 是测试数据库。
	store, _ := newMarketIntelAdapterStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// scanStore 是被测扫描持久化适配器。
	scanStore := NewMarketIntelScanStore(store)

	// 该词没有任何快照时必须返回空集合而不是错误。
	emptyItems, emptyErr := scanStore.ListKeywordItems(ctx, "AI课程")
	if emptyErr != nil || emptyItems == nil || len(emptyItems) != 0 {
		t.Fatalf("无快照应返回空集合: %+v err=%v", emptyItems, emptyErr)
	}
	// 写入两天快照，列表必须只取最新一天。
	if _, err := store.MarketIntel.SaveSnapshots(ctx, []db.MarketIntelSnapshotRow{
		{Keyword: "AI课程", SnapshotDate: "2026-11-01", ItemID: "old", Title: "旧商品", Price: "1"},
		{Keyword: "AI课程", SnapshotDate: "2026-11-05", ItemID: "new-1", Title: "新商品一", Price: "99", WantCountText: "120人想要", SellerNickname: "卖家甲"},
		{Keyword: "AI课程", SnapshotDate: "2026-11-05", ItemID: "new-2", Title: "新商品二", Price: "88"},
	}); err != nil {
		t.Fatalf("写入快照失败: %v", err)
	}
	// items、err 是商品列表与错误。
	items, err := scanStore.ListKeywordItems(ctx, "AI课程")
	if err != nil || len(items) != 2 {
		t.Fatalf("商品列表不符: %+v err=%v", items, err)
	}
	if items[0].ItemID != "new-1" || items[0].Title != "新商品一" || items[0].WantCountText != "120人想要" || items[0].SellerNickname != "卖家甲" {
		t.Fatalf("商品摘要字段不符: %+v", items[0])
	}
	// 数据库不可用时必须返回错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatalf("关闭测试数据库失败: %v", closeErr)
	}
	// listErr 是数据库不可用时的列表错误。
	if _, listErr := scanStore.ListKeywordItems(ctx, "AI课程"); listErr == nil {
		t.Fatal("数据库不可用时列表应报错")
	}
}

// TestClassifyDetailPlatformError 验证详情抓取的平台错误被归类为应用层稳定错误。
func TestClassifyDetailPlatformError(t *testing.T) {
	// cases 是浏览器错误与期望应用错误的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// input 是浏览器层错误。
		input error
		// want 是期望的应用层错误；nil 表示保持原错误。
		want error
	}{
		{name: "风控", input: browser.ErrSourcingRiskControl, want: marketintel.ErrCompetitorDetailRiskControl},
		{name: "会话失效", input: browser.ErrSourcingSessionExpired, want: marketintel.ErrCompetitorDetailSessionExpired},
		{name: "无结果保持原样", input: browser.ErrSourcingNoResult, want: browser.ErrSourcingNoResult},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// wrapped 是被包裹一层后的输入，验证 errors.Is 归类对包裹错误同样生效。
			wrapped := fmt.Errorf("抓取上下文: %w", testCase.input)
			// got 是归类后的应用层错误。
			got := classifyDetailPlatformError(wrapped)
			if !errors.Is(got, testCase.want) {
				t.Fatalf("归类结果不符: got=%v want=%v", got, testCase.want)
			}
		})
	}
}

// TestPersistRefreshedCookie 验证人机验证刷新后的 Cookie 落库与空值跳过。
func TestPersistRefreshedCookie(t *testing.T) {
	// store、userID 是含一个账号的测试数据库与账号归属用户。
	store, userID := newMarketIntelAdapterStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// port 是被测抓取端口。
	port := NewMarketIntelSearchPort(nil, nil, store, userID, "", nil)

	// 空刷新值必须直接跳过，不触碰数据库。
	port.persistRefreshedCookie(ctx, "scan-account", "   ")
	// value、err 是落库后读回的凭据与读取错误。
	if value, err := store.Cookies.GetValue(ctx, "scan-account"); err != nil || value != "unb=1; _m_h5_tk=abc" {
		t.Fatalf("空刷新值不应改写现存凭据: err=%v", err)
	}

	// 正常刷新值必须写入同一账号。
	refreshed := "unb=1; _m_h5_tk=newtk; x5sec=passed"
	port.persistRefreshedCookie(ctx, "scan-account", refreshed)
	// saved、readErr 是落库后的凭据与读取错误。
	saved, readErr := store.Cookies.GetValue(ctx, "scan-account")
	if readErr != nil || saved != refreshed {
		t.Fatalf("刷新后的凭据未落库: err=%v", readErr)
	}

	// 未注入存储时必须安全跳过（不 panic）。
	// bare 是未装配存储的端口。
	bare := NewMarketIntelSearchPort(nil, nil, nil, userID, "", nil)
	bare.persistRefreshedCookie(ctx, "scan-account", refreshed)

	// 账号不存在时只记录日志，不 panic、不影响调用方。
	port.persistRefreshedCookie(ctx, "missing-account", refreshed)
}
