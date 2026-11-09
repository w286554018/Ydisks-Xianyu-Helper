package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/adapter"
	marketintel "xianyu-go/internal/application/marketintel"
	sourcingapp "xianyu-go/internal/application/sourcing"
)

// marketIntelNoopScan 是市场情报扫描持久化的空实现，仅供词表投影器测试使用。
type marketIntelNoopScan struct{}

// SnapshotExists 固定返回 false。
func (m *marketIntelNoopScan) SnapshotExists(context.Context, string, string) (bool, error) {
	return false, nil
}

// SaveScanRecord 固定成功。
func (m *marketIntelNoopScan) SaveScanRecord(context.Context, marketintel.ScanRecord) error {
	return nil
}

// DeleteSnapshotsBefore 固定返回 0。
func (m *marketIntelNoopScan) DeleteSnapshotsBefore(context.Context, string) (int64, error) {
	return 0, nil
}

// ListPendingVerdicts 固定返回空。
func (m *marketIntelNoopScan) ListPendingVerdicts(context.Context, string, string) ([]marketintel.VerdictInput, error) {
	return nil, nil
}

// ClassifyError 固定返回 other。
func (m *marketIntelNoopScan) ClassifyError(error) string { return "other" }

// ListLatestAggregates 固定返回空。
func (m *marketIntelNoopScan) ListLatestAggregates(context.Context) ([]marketintel.AggregateView, error) {
	return nil, nil
}

// ListAggregates 固定返回空。
func (m *marketIntelNoopScan) ListAggregates(context.Context, string, int) ([]marketintel.AggregateView, error) {
	return nil, nil
}

// ListSellerStats 固定返回空。
func (m *marketIntelNoopScan) ListSellerStats(context.Context, int) ([]marketintel.SellerStat, error) {
	return nil, nil
}

// fakeSourcingPort 是选品参考用例替身，记录调用参数并返回预置结果，避免测试访问真实平台。
type fakeSourcingPort struct {
	// searchErr 是查货返回的错误，用于覆盖错误映射。
	searchErr error
	// copyErr 是文案生成返回的错误。
	copyErr error
	// refreshErr 是热词采集返回的错误。
	refreshErr error
	// lastUserID 是最近一次需要用户身份的调用所传入的用户标识，用于验证数据隔离。
	lastUserID int64
	// lastWindow 是最近一次畅销榜请求的窗口参数。
	lastWindow string
	// momentumErr 是涨幅榜返回的错误。
	momentumErr error
}

// HotKeywords 返回固定热词快照。
func (f *fakeSourcingPort) HotKeywords(context.Context) (sourcingapp.HotKeywordView, error) {
	return sourcingapp.HotKeywordView{Date: "2026-10-29", Keywords: []string{"耳机", "电动车"}, Status: sourcingapp.StatusOK}, nil
}

// RefreshHotKeywords 返回预置采集结果。
func (f *fakeSourcingPort) RefreshHotKeywords(context.Context) error { return f.refreshErr }

// SearchItems 记录用户并返回固定商品或预置错误。
func (f *fakeSourcingPort) SearchItems(_ context.Context, userID int64, _ string, _ string) ([]sourcingapp.SearchItem, error) {
	f.lastUserID = userID
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return []sourcingapp.SearchItem{{ItemID: "1", Title: "蓝牙耳机", Price: "19", WantCount: "485", Location: "广东", DetailURL: "https://www.goofish.com/item?id=1"}}, nil
}

// ShopRanking 记录用户与窗口并返回固定排行。
func (f *fakeSourcingPort) ShopRanking(_ context.Context, userID int64, window string) (sourcingapp.ShopRankingView, error) {
	f.lastUserID, f.lastWindow = userID, window
	return sourcingapp.ShopRankingView{Date: "2026-10-29", Window: "7d", Status: sourcingapp.StatusOK, Entries: []sourcingapp.RankingEntry{{ItemID: "a", ItemTitle: "商品甲", SoldQuantity: 3, Revenue: 30, OrderCount: 2}}}, nil
}

// GenerateCopywriting 返回固定文案或预置错误。
func (f *fakeSourcingPort) GenerateCopywriting(_ context.Context, userID int64, req sourcingapp.CopywritingRequest) (sourcingapp.Copywriting, error) {
	f.lastUserID = userID
	if f.copyErr != nil {
		return sourcingapp.Copywriting{}, f.copyErr
	}
	return sourcingapp.Copywriting{Mode: req.Mode, Content: "标题\n描述"}, nil
}

// Momentum 返回固定涨幅榜或预置错误。
func (f *fakeSourcingPort) Momentum(context.Context) (sourcingapp.MomentumSummary, error) {
	if f.momentumErr != nil {
		return sourcingapp.MomentumSummary{}, f.momentumErr
	}
	return sourcingapp.MomentumSummary{WindowDays: 14, LatestDate: "2026-11-05", Items: []sourcingapp.KeywordMomentum{
		{Keyword: "新词", Status: sourcingapp.MomentumNew, Position: 5, DaysOnList: 1},
		{Keyword: "耳机", Status: sourcingapp.MomentumSteady, Position: 1, PreviousPosition: 2, DaysOnList: 5},
	}}, nil
}

// sourcingRequest 以给定会话向路由发送请求并返回记录器与请求。
func sourcingRequest(handler http.Handler, cookie *http.Cookie, method, path, body string) (*httptest.ResponseRecorder, *http.Request) {
	// request 是发往被测路由的模拟请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	// recorder 记录被测处理器写出的响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder, request
}

// TestSourcingEndpointsContract 验证选品参考 5 个端点的成功响应契约、错误状态映射、鉴权与未启用降级。
func TestSourcingEndpointsContract(t *testing.T) {
	// srv、cleanup 是测试 Server 与清理函数；默认测试装配未启用选品参考。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是真实路由；cookie 是管理员会话。
	handler := srv.Router()
	// cookie 是管理员登录后获得的会话 Cookie。
	cookie := loginHelper(t, handler)

	// 未装配选品参考时必须返回 503 而不是空指针崩溃。
	disabled, _ := sourcingRequest(handler, cookie, http.MethodGet, "/api/v1/sourcing/hot-keywords", "")
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用时应返回 503, got %d", disabled.Code)
	}

	// fake 是注入的选品参考用例替身。
	fake := &fakeSourcingPort{}
	srv.applications.sourcing = fake

	// 未登录访问必须被拒绝。
	anonymous, _ := sourcingRequest(handler, nil, http.MethodGet, "/api/v1/sourcing/hot-keywords", "")
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应返回 401, got %d", anonymous.Code)
	}

	// recorder 记录热词接口的真实响应，request 是对应的模拟请求，二者供契约校验复用。
	recorder, request := sourcingRequest(handler, cookie, http.MethodGet, "/api/v1/sourcing/hot-keywords", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	recorder, request = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/sourcing/hot-keywords/refresh", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 热词涨幅榜返回固定词条并通过契约校验。
	recorder, request = sourcingRequest(handler, cookie, http.MethodGet, "/api/v1/sourcing/hot-keywords/momentum", "")
	assertOpenAPISuccessResponse(t, request, recorder)
	// momentum 是解析后的涨幅响应。
	var momentum sourcingHotKeywordMomentumResponse
	// momentumErr 是解析涨幅响应 JSON 的错误，期望为空。
	if err := json.Unmarshal(recorder.Body.Bytes(), &momentum); err != nil || len(momentum.Items) != 2 || momentum.Items[0].Status != "new" {
		t.Fatalf("涨幅响应不符: %+v err=%v", momentum, err)
	}
	// 涨幅榜失败必须映射为 500。
	fake.momentumErr = errors.New("读取热词快照序列失败")
	// failed 是涨幅失败场景的响应记录器。
	failed, _ := sourcingRequest(handler, cookie, http.MethodGet, "/api/v1/sourcing/hot-keywords/momentum", "")
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("涨幅失败应返回 500, got %d", failed.Code)
	}
	fake.momentumErr = nil

	recorder, request = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/sourcing/search", `{"cookie_id":"acc1","keyword":"耳机"}`)
	assertOpenAPISuccessResponse(t, request, recorder)
	// search 是解析后的查货响应。
	var search sourcingSearchResponse
	// err 是解析查货响应 JSON 的错误，期望为空。
	if err := json.Unmarshal(recorder.Body.Bytes(), &search); err != nil || len(search.Items) != 1 || search.Items[0].WantCount != "485" {
		t.Fatalf("查货响应不符: %+v err=%v", search, err)
	}
	if fake.lastUserID <= 0 {
		t.Fatalf("查货应传入当前登录用户, got %d", fake.lastUserID)
	}

	recorder, request = sourcingRequest(handler, cookie, http.MethodGet, "/api/v1/sourcing/shop-ranking?window=30d", "")
	assertOpenAPISuccessResponse(t, request, recorder)
	if fake.lastWindow != "30d" || fake.lastUserID <= 0 {
		t.Fatalf("畅销榜应按当前用户与窗口查询: user=%d window=%s", fake.lastUserID, fake.lastWindow)
	}

	recorder, request = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/sourcing/copywriting", `{"mode":"listing","topic":"蓝牙耳机"}`)
	assertOpenAPISuccessResponse(t, request, recorder)

	// 无结果按 200 空列表返回，不使用 200 + 错误信封。
	fake.searchErr = sourcingapp.ErrNoResult
	recorder, request = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/sourcing/search", `{"cookie_id":"acc1","keyword":"冷门"}`)
	assertOpenAPIResponse(t, request, recorder)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"items":[]`) {
		t.Fatalf("无结果应返回空列表: %d %s", recorder.Code, recorder.Body.String())
	}

	// cases 覆盖查货错误到 HTTP 状态码的映射，每个响应都必须符合错误信封契约。
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"冷却", sourcingapp.ErrSearchCooling, http.StatusTooManyRequests},
		{"风控", sourcingapp.ErrRiskControl, http.StatusConflict},
		{"会话过期", sourcingapp.ErrSessionExpired, http.StatusConflict},
		{"账号不属于当前用户", sourcingapp.ErrAccountNotOwned, http.StatusForbidden},
		{"空关键词", sourcingapp.ErrEmptyKeyword, http.StatusBadRequest},
		{"浏览器不可用", sourcingapp.ErrBrowserUnavailable, http.StatusServiceUnavailable},
		{"未知错误", context.DeadlineExceeded, http.StatusInternalServerError},
	}
	// c 是当前用例的输入与期望结果。
	for _, c := range cases {
		fake.searchErr = c.err
		recorder, request = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/sourcing/search", `{"cookie_id":"acc1","keyword":"耳机"}`)
		if recorder.Code != c.want {
			t.Fatalf("%s: 状态码 %d, want %d", c.name, recorder.Code, c.want)
		}
		assertOpenAPIResponse(t, request, recorder)
	}

	// 文案错误映射：AI 未配置 409、非法模式 400。
	fake.copyErr = sourcingapp.ErrAIConfigMissing
	recorder, request = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/sourcing/copywriting", `{"mode":"listing","topic":"耳机"}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("AI 未配置应返回 409, got %d", recorder.Code)
	}
	assertOpenAPIResponse(t, request, recorder)
	fake.copyErr = sourcingapp.ErrInvalidCopywritingMode
	recorder, request = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/sourcing/copywriting", `{"mode":"listing","topic":"耳机"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("非法模式应返回 400, got %d", recorder.Code)
	}
	assertOpenAPIResponse(t, request, recorder)

	// 热词采集无结果返回 503。
	fake.refreshErr = sourcingapp.ErrHotKeywordUnavailable
	recorder, request = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/sourcing/hot-keywords/refresh", "")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("热词无结果应返回 503, got %d", recorder.Code)
	}
	assertOpenAPIResponse(t, request, recorder)

	// 非法 JSON 返回 400。
	recorder, _ = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/sourcing/search", `{bad`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应返回 400, got %d", recorder.Code)
	}
}

// fakeMarketIntelPort 是市场情报用例替身，返回固定词表与聚合视图。
type fakeMarketIntelPort struct {
	// scanErr 是扫描触发的返回错误。
	scanErr error
	// scanResult 是扫描触发的返回摘要。
	scanResult marketintel.ScanResult
	// carryErr 是搬运触发的返回错误。
	carryErr error
	// rewriteErr 是 AI 改写触发的返回错误。
	rewriteErr error
	// analyzeErr 是卖点分析触发的返回错误。
	analyzeErr error
}

// ListKeywords 返回固定词表。
func (f *fakeMarketIntelPort) ListKeywords(context.Context) ([]marketintel.WatchedKeyword, error) {
	return []marketintel.WatchedKeyword{{ID: 1, Keyword: "AI课程", Enabled: true, SortOrder: 0}}, nil
}

// CreateKeyword 返回固定创建结果。
func (f *fakeMarketIntelPort) CreateKeyword(_ context.Context, keyword string, enabled bool) (marketintel.WatchedKeyword, error) {
	return marketintel.WatchedKeyword{ID: 2, Keyword: keyword, Enabled: enabled, SortOrder: 1}, nil
}

// UpdateKeyword 固定成功。
func (f *fakeMarketIntelPort) UpdateKeyword(context.Context, int64, string, bool) error { return nil }

// DeleteKeyword 固定成功。
func (f *fakeMarketIntelPort) DeleteKeyword(context.Context, int64) error { return nil }

// RunDailyScan 返回预置摘要或错误。
func (f *fakeMarketIntelPort) RunDailyScan(context.Context) (marketintel.ScanResult, error) {
	if f.scanErr != nil {
		return marketintel.ScanResult{}, f.scanErr
	}
	return f.scanResult, nil
}

// LatestAggregates 返回固定聚合。
func (f *fakeMarketIntelPort) LatestAggregates(context.Context) ([]marketintel.AggregateView, error) {
	return []marketintel.AggregateView{{Keyword: "AI课程", Date: "2026-11-05", TotalCount: 30, AICount: 12, TotalWant: 4560, VerdictStatus: "done"}}, nil
}

// KeywordTrend 返回固定趋势。
func (f *fakeMarketIntelPort) KeywordTrend(_ context.Context, keywordID int64, days int) (marketintel.TrendView, error) {
	return marketintel.TrendView{KeywordID: keywordID, Keyword: "AI课程", Days: days, Points: []marketintel.TrendPoint{{Date: "2026-11-05", TotalWant: 4560, AICount: 12, TotalCount: 30}}}, nil
}

// SellerBoard 返回固定卖家榜。
func (f *fakeMarketIntelPort) SellerBoard(_ context.Context, days int) (marketintel.SellerBoardView, error) {
	return marketintel.SellerBoardView{Days: days, Sellers: []marketintel.SellerStat{{Nickname: "卖家甲", Appearances: 5, AvgPrice: "99.00"}}}, nil
}

// EnsureSeedKeywords 固定成功。
func (f *fakeMarketIntelPort) EnsureSeedKeywords(context.Context) error { return nil }

// GetCompetitorDetail 返回固定详情。
func (f *fakeMarketIntelPort) GetCompetitorDetail(_ context.Context, itemID string) (marketintel.CompetitorDetail, error) {
	return marketintel.CompetitorDetail{ItemID: itemID, Title: "AI课程代做", Price: "99", DescText: "专业代做", Images: []string{"http://127.0.0.1:1/unreachable.jpg"}, SellerNickname: "卖家甲", SellerID: "u1"}, nil
}

// RewriteCompetitorCopy 返回固定改写结果或注入的错误。
func (f *fakeMarketIntelPort) RewriteCompetitorCopy(context.Context, string, string) (marketintel.RewrittenCopy, error) {
	if f.rewriteErr != nil {
		return marketintel.RewrittenCopy{}, f.rewriteErr
	}
	return marketintel.RewrittenCopy{Title: "AI课程辅导定制", Desc: "一对一辅导"}, nil
}

// AnalyzeCompetitorSellingPoints 返回固定分析或注入的错误。
func (f *fakeMarketIntelPort) AnalyzeCompetitorSellingPoints(context.Context, string, string, string, string) (string, error) {
	if f.analyzeErr != nil {
		return "", f.analyzeErr
	}
	return "核心卖点：定制化；文案套路：价格锚点", nil
}

// CarryCompetitorItem 返回固定搬运结果（图片入库由应用层完成，此处只验证 transport 转换）。
func (f *fakeMarketIntelPort) CarryCompetitorItem(_ context.Context, userID int64, itemID string) (marketintel.CarryResult, error) {
	if f.carryErr != nil {
		return marketintel.CarryResult{}, f.carryErr
	}
	return marketintel.CarryResult{
		Title: "AI课程辅导定制", Desc: "一对一辅导", Price: "99",
		Images:         []marketintel.CarryUploadedImage{{ID: 9, Filename: "carry-" + itemID + "-0.jpg"}},
		SameImagesHint: marketintel.CarrySameImagesHint,
	}, nil
}

// ListKeywordItems 返回固定商品列表。
func (f *fakeMarketIntelPort) ListKeywordItems(_ context.Context, keyword string) ([]marketintel.ItemSummary, error) {
	return []marketintel.ItemSummary{{ItemID: "i1", Title: "AI课程代做", Price: "99", WantCountText: "120人想要", SellerNickname: "卖家甲"}}, nil
}

// marketIntelRequest 以给定会话向市场情报路由发送请求。
func marketIntelRequest(handler http.Handler, cookie *http.Cookie, method, path, body string) (*httptest.ResponseRecorder, *http.Request) {
	// req 是模拟请求。
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	// recorder 是响应记录器。
	recorder := httptest.NewRecorder()
	if cookie != nil {
		req.AddCookie(cookie)
	}
	handler.ServeHTTP(recorder, req)
	return recorder, req
}

// TestMarketIntelEndpointsContract 验证市场情报端点的成功契约、鉴权与未启用降级。
func TestMarketIntelEndpointsContract(t *testing.T) {
	// srv、store、cleanup 是测试 Server、真实数据库仓储与清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是真实路由；cookie 是管理员会话。
	handler := srv.Router()
	// cookie 是登录后的会话 Cookie。
	cookie := loginHelper(t, handler)

	// 未装配时必须返回 503。
	disabled, _ := marketIntelRequest(handler, cookie, http.MethodGet, "/api/v1/market-intel/keywords", "")
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用时应返回 503, got %d", disabled.Code)
	}

	// fake 是注入的市场情报替身；概览同时依赖选品参考（热词涨幅），一并注入。
	fake := &fakeMarketIntelPort{scanResult: marketintel.ScanResult{Date: "2026-11-05", Scanned: 1, Skipped: 1}}
	srv.applications.marketIntel = fake
	srv.applications.sourcing = &fakeSourcingPort{}

	// 投影器验证：用真实 Service（绑定测试 store）构造端口，走一遍词表 CRUD 覆盖投影器路径。
	// store 是测试服务器的真实数据库仓储。
	realService := marketintel.NewService(
		adapter.NewMarketIntelStore(store),
		&marketIntelNoopScan{},
		nil, nil, nil, nil,
	)
	srv.applications.marketIntel = MarketIntelTransportPort(realService, nil)
	// 词表读取走投影器。
	projectionList, projectionErr := srv.applications.marketIntel.ListKeywords(context.Background())
	if projectionErr != nil || projectionList == nil {
		t.Fatalf("投影器词表读取不符: %+v err=%v", projectionList, projectionErr)
	}
	// 种子词写入走投影器。
	if seedErr := srv.applications.marketIntel.EnsureSeedKeywords(context.Background()); seedErr != nil {
		t.Fatalf("投影器种子词写入失败: %v", seedErr)
	}
	// 创建、更新、删除、扫描、聚合、趋势与卖家榜全部走投影器。
	// created 是投影器创建结果。
	created, createErr := srv.applications.marketIntel.CreateKeyword(context.Background(), "投影测试词", false)
	if createErr != nil {
		t.Fatalf("投影器创建失败: %v", createErr)
	}
	// updateErr 验证词表更新经 HTTP 应用端口投影后可写入真实服务。
	if updateErr := srv.applications.marketIntel.UpdateKeyword(context.Background(), created.ID, "投影测试词", true); updateErr != nil {
		t.Fatalf("投影器更新失败: %v", updateErr)
	}
	// deleteErr 验证端口投影删除测试关注词时保留底层错误语义。
	if deleteErr := srv.applications.marketIntel.DeleteKeyword(context.Background(), created.ID); deleteErr != nil {
		t.Fatalf("投影器删除失败: %v", deleteErr)
	}
	// 投影器扫描：真实 Service 无搜索能力（浏览器不可用），启用词存在时应返回明确错误。
	// 先加一个启用词触发搜索依赖检查。
	// enabledForScan 是为扫描检查创建的启用词。
	enabledForScan, enableErr := srv.applications.marketIntel.CreateKeyword(context.Background(), "扫描依赖检查词", true)
	if enableErr != nil {
		t.Fatalf("投影器扫描前置创建失败: %v", enableErr)
	}
	// scanErr 应保留真实服务缺少搜索能力的错误，避免投影器误报扫描成功。
	if _, scanErr := srv.applications.marketIntel.RunDailyScan(context.Background()); scanErr == nil {
		t.Fatal("无搜索能力时扫描应返回错误")
	}
	_ = enabledForScan
	// aggregates 是投影器聚合结果；无扫描数据时允许空集合或 nil。
	aggregates, aggregatesErr := srv.applications.marketIntel.LatestAggregates(context.Background())
	if aggregatesErr != nil || len(aggregates) != 0 {
		t.Fatalf("投影器聚合不符: len=%d err=%v", len(aggregates), aggregatesErr)
	}
	// keywords 是投影器词表（种子词应存在）。
	keywords, keywordsErr := srv.applications.marketIntel.ListKeywords(context.Background())
	if keywordsErr != nil || len(keywords) == 0 {
		t.Fatalf("投影器词表不符: %+v err=%v", keywords, keywordsErr)
	}
	// trend 是投影器趋势结果（种子词无聚合数据，点为空）。
	trend, trendErr := srv.applications.marketIntel.KeywordTrend(context.Background(), keywords[0].ID, 30)
	if trendErr != nil || trend.Keyword != keywords[0].Keyword {
		t.Fatalf("投影器趋势不符: %+v err=%v", trend, trendErr)
	}
	// board 是投影器卖家榜结果；无数据时允许空集合。
	board, boardErr := srv.applications.marketIntel.SellerBoard(context.Background(), 30)
	if boardErr != nil || len(board.Sellers) != 0 {
		t.Fatalf("投影器卖家榜不符: len=%d err=%v", len(board.Sellers), boardErr)
	}
	// 恢复 fake 供后续断言使用。
	srv.applications.marketIntel = fake

	// 未登录必须 401。
	anonymous, _ := marketIntelRequest(handler, nil, http.MethodGet, "/api/v1/market-intel/keywords", "")
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应返回 401, got %d", anonymous.Code)
	}

	// 词表读取。
	recorder, request := marketIntelRequest(handler, cookie, http.MethodGet, "/api/v1/market-intel/keywords", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 词表创建（201）。
	recorder, request = marketIntelRequest(handler, cookie, http.MethodPost, "/api/v1/market-intel/keywords", `{"keyword":"AI绘画","enabled":true}`)
	assertOpenAPIExpectedStatusResponse(t, request, recorder, http.StatusCreated)

	// 词表更新。
	recorder, request = marketIntelRequest(handler, cookie, http.MethodPut, "/api/v1/market-intel/keywords/1", `{"keyword":"AI课程","enabled":false}`)
	assertOpenAPISuccessResponse(t, request, recorder)

	// 词表删除。
	recorder, request = marketIntelRequest(handler, cookie, http.MethodDelete, "/api/v1/market-intel/keywords/1", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 概览。
	recorder, request = marketIntelRequest(handler, cookie, http.MethodGet, "/api/v1/market-intel/overview", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 趋势。
	recorder, request = marketIntelRequest(handler, cookie, http.MethodGet, "/api/v1/market-intel/keywords/1/trend?days=7", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 卖家榜。
	recorder, request = marketIntelRequest(handler, cookie, http.MethodGet, "/api/v1/market-intel/sellers?days=7", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 手动扫描成功。
	recorder, request = marketIntelRequest(handler, cookie, http.MethodPost, "/api/v1/market-intel/scan", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 扫描风控中止必须 409。
	fake.scanErr = marketintel.ErrRiskControl
	// conflict 是风控中止场景的响应记录器。
	conflict, _ := marketIntelRequest(handler, cookie, http.MethodPost, "/api/v1/market-intel/scan", "")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("风控中止应返回 409, got %d", conflict.Code)
	}
	fake.scanErr = nil
}
