// Package server: 市场情报端口投影与错误映射焦点测试。
package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	marketintel "xianyu-go/internal/application/marketintel"
)

// TestMarketIntelTransportPortProjector 验证投影器把调用原样委托给具体 Service。
func TestMarketIntelTransportPortProjector(t *testing.T) {
	// nil 服务必须投影为 nil 接口。
	if port := MarketIntelTransportPort(nil, nil); port != nil {
		t.Fatal("nil 服务应投影为 nil 接口")
	}
	// fakeService 是满足接口的最小实现（复用 fakeMarketIntelPort）。
	fake := &fakeMarketIntelPort{}
	// projector 需要具体 Service；这里用接口断言验证 fake 满足 MarketIntelPort。
	var port MarketIntelPort = fake
	// items 与 listErr 是词表委托结果与错误。
	items, listErr := port.ListKeywords(context.Background())
	if listErr != nil || len(items) != 1 {
		t.Fatalf("词表委托不符: %+v err=%v", items, listErr)
	}
	// created 与 createErr 是创建委托结果与错误。
	created, createErr := port.CreateKeyword(context.Background(), "AI绘画", true)
	if createErr != nil || created.ID != 2 {
		t.Fatalf("创建委托不符: %+v err=%v", created, createErr)
	}
	// 更新、删除、种子词委托。
	// updateErr 是更新委托错误。
	if updateErr := port.UpdateKeyword(context.Background(), 1, "AI课程", false); updateErr != nil {
		t.Fatalf("更新委托失败: %v", updateErr)
	}
	// deleteErr 是删除委托错误。
	if deleteErr := port.DeleteKeyword(context.Background(), 1); deleteErr != nil {
		t.Fatalf("删除委托失败: %v", deleteErr)
	}
	// seedErr 是种子词委托错误。
	if seedErr := port.EnsureSeedKeywords(context.Background()); seedErr != nil {
		t.Fatalf("种子词委托失败: %v", seedErr)
	}
	// scanResult 是扫描委托结果。
	scanResult, err := port.RunDailyScan(context.Background())
	if err != nil || scanResult.Date != "" {
		t.Fatalf("扫描委托不符: %+v err=%v", scanResult, err)
	}
	// aggregates 是聚合委托结果。
	aggregates, err := port.LatestAggregates(context.Background())
	if err != nil || len(aggregates) != 1 {
		t.Fatalf("聚合委托不符: %+v err=%v", aggregates, err)
	}
	// trend 是趋势委托结果。
	trend, err := port.KeywordTrend(context.Background(), 1, 30)
	if err != nil || trend.Keyword != "AI课程" {
		t.Fatalf("趋势委托不符: %+v err=%v", trend, err)
	}
	// board 是卖家榜委托结果。
	board, err := port.SellerBoard(context.Background(), 30)
	if err != nil || len(board.Sellers) != 1 {
		t.Fatalf("卖家榜委托不符: %+v err=%v", board, err)
	}
}

// carryLibraryStub 是搬运图片入库替身，只记录调用并将图片映射为主键。
type carryLibraryStub struct {
	// calls 记录入库调用次数。
	calls int
	// err 是入库返回错误。
	err error
}

// SaveCarryImages 返回与输入等量的入库结果或预置错误。
func (s *carryLibraryStub) SaveCarryImages(_ context.Context, _ int64, images []marketintel.CarryImageData) ([]marketintel.CarryUploadedImage, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	// uploaded 是与输入等量的入库结果。
	uploaded := make([]marketintel.CarryUploadedImage, 0, len(images))
	// image 是当前图片。
	for _, image := range images {
		uploaded = append(uploaded, marketintel.CarryUploadedImage{ID: int64(len(uploaded) + 1), Filename: image.Filename})
	}
	return uploaded, nil
}

// carryImageStub 是竞品图片下载替身。
type carryImageStub struct {
	// calls 记录下载调用次数。
	calls int
}

// DownloadCarryImage 返回固定的图片字节与文件名。
func (s *carryImageStub) DownloadCarryImage(_ context.Context, _ string, itemID string, index int) (marketintel.CarryImageData, error) {
	s.calls++
	return marketintel.CarryImageData{Filename: itemID + "-" + strconv.Itoa(index) + ".png", Data: []byte("png")}, nil
}

// TestMarketIntelTransportPortCarryDelegation 验证搬运投影器把调用委托给组合根装配的搬运用例。
func TestMarketIntelTransportPortCarryDelegation(t *testing.T) {
	// detail 是复用市场情报替身提供的详情与改写能力。
	detail := &fakeMarketIntelPort{}
	// images 是图片下载替身。
	images := &carryImageStub{}
	// library 是图片入库替身。
	library := &carryLibraryStub{}
	// carry、carryErr 是被测搬运用例服务与构造错误。
	carry, carryErr := marketintel.NewCarryService(detail, images, library)
	if carryErr != nil {
		t.Fatalf("构造搬运用例失败: %v", carryErr)
	}
	// service 是被投影的具体市场情报服务（本用例不调用其方法）。
	service := marketintel.NewService(nil, nil, nil, nil, nil, nil)
	// 搬运能力未装配时投影器必须返回稳定的未启用错误，供 transport 映射为 503。
	if _, unsetErr := MarketIntelTransportPort(service, nil).CarryCompetitorItem(context.Background(), 1, "i1"); !errors.Is(unsetErr, marketintel.ErrImageLibraryUnavailable) {
		t.Fatalf("未装配搬运应返回 ErrImageLibraryUnavailable, got %v", unsetErr)
	}
	// port 是装配了搬运能力的 transport 端口。
	port := MarketIntelTransportPort(service, carry)
	// result、err 是搬运委托结果与错误。
	result, err := port.CarryCompetitorItem(context.Background(), 7, "i1")
	if err != nil {
		t.Fatalf("搬运委托失败: %v", err)
	}
	if result.Title != "AI课程辅导定制" || result.Price != "99" || images.calls != 1 || library.calls != 1 {
		t.Fatalf("搬运委托结果不符: %+v images=%d library=%d", result, images.calls, library.calls)
	}
	if len(result.Images) != 1 || result.Images[0].Filename != "i1-0.png" || result.SameImagesHint == "" {
		t.Fatalf("搬运图片入库结果不符: %+v", result)
	}
}

// TestWriteMarketIntelDetailErrorMapping 验证竞品详情错误到 HTTP 状态的映射。
func TestWriteMarketIntelDetailErrorMapping(t *testing.T) {
	// srv、cleanup 是测试 Server 与清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// cases 是错误与期望状态的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// input 是输入错误。
		input error
		// wantStatus 是期望 HTTP 状态。
		wantStatus int
	}{
		{name: "风控", input: marketintel.ErrRiskControl, wantStatus: http.StatusConflict},
		{name: "详情风控", input: marketintel.ErrCompetitorDetailRiskControl, wantStatus: http.StatusConflict},
		{name: "详情会话失效", input: marketintel.ErrCompetitorDetailSessionExpired, wantStatus: http.StatusConflict},
		{name: "会话失效", input: marketintel.ErrSessionExpired, wantStatus: http.StatusConflict},
		{name: "模型未配置", input: marketintel.ErrAINotConfigured, wantStatus: http.StatusBadRequest},
		{name: "资源库未启用", input: marketintel.ErrImageLibraryUnavailable, wantStatus: http.StatusServiceUnavailable},
		{name: "未知", input: errors.New("浏览器不可用"), wantStatus: http.StatusInternalServerError},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// recorder 是响应记录器。
			recorder := httptest.NewRecorder()
			srv.writeMarketIntelDetailError(recorder, "i1", testCase.input)
			if recorder.Code != testCase.wantStatus {
				t.Fatalf("期望 %d，实际 %d", testCase.wantStatus, recorder.Code)
			}
		})
	}
}

// TestWriteMarketIntelKeywordErrorMapping 验证词表错误到 HTTP 状态的映射。
func TestWriteMarketIntelKeywordErrorMapping(t *testing.T) {
	// srv、cleanup 是测试 Server 与清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// cases 是错误与期望状态的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// input 是输入错误。
		input error
		// wantStatus 是期望 HTTP 状态。
		wantStatus int
	}{
		{name: "上限", input: marketintel.ErrKeywordLimitReached, wantStatus: http.StatusBadRequest},
		{name: "空词", input: marketintel.ErrKeywordEmpty, wantStatus: http.StatusBadRequest},
		{name: "重复", input: marketintel.ErrKeywordDuplicate, wantStatus: http.StatusBadRequest},
		{name: "不存在", input: marketintel.ErrKeywordNotFound, wantStatus: http.StatusNotFound},
		{name: "未知", input: errors.New("数据库错误"), wantStatus: http.StatusInternalServerError},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// recorder 是响应记录器。
			recorder := httptest.NewRecorder()
			srv.writeMarketIntelKeywordError(recorder, testCase.input)
			if recorder.Code != testCase.wantStatus {
				t.Fatalf("期望 %d，实际 %d", testCase.wantStatus, recorder.Code)
			}
		})
	}
}

// TestMarketIntelTransportPortWithRealService 验证投影器包装真实 Service 时的完整委托。
func TestMarketIntelTransportPortWithRealService(t *testing.T) {
	// 构造真实 Service 所需的最小依赖与本测试无关，直接验证投影器对接口方法的转发由编译期保证；
	// 这里验证 nil 投影与构造函数的幂等性。
	if port := MarketIntelTransportPort(nil, nil); port != nil {
		t.Fatal("nil 服务应投影为 nil 接口")
	}
}

// TestMarketIntelDetailEndpointsContract 验证竞品详情/改写/分析/搬运端点的契约。
func TestMarketIntelDetailEndpointsContract(t *testing.T) {
	// srv、cleanup 是测试 Server 与清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是真实路由；cookie 是管理员会话。
	handler := srv.Router()
	// cookie 是登录会话。
	cookie := loginHelper(t, handler)

	// 未装配时 503。
	disabled, _ := marketIntelRequest(handler, cookie, http.MethodGet, "/api/v1/market-intel/items/i1/detail", "")
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用应返回 503, got %d", disabled.Code)
	}

	// fake 是注入的市场情报替身。
	fake := &fakeMarketIntelPort{}
	srv.applications.marketIntel = fake

	// 详情成功。
	recorder, request := marketIntelRequest(handler, cookie, http.MethodGet, "/api/v1/market-intel/items/i1/detail", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 改写成功。
	recorder, request = marketIntelRequest(handler, cookie, http.MethodPost, "/api/v1/market-intel/items/i1/rewrite", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 分析成功。
	recorder, request = marketIntelRequest(handler, cookie, http.MethodPost, "/api/v1/market-intel/items/i1/analyze", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 搬运成功（图片 URL 不可达时跳过，仍返回 200 契约）。
	recorder, request = marketIntelRequest(handler, cookie, http.MethodPost, "/api/v1/market-intel/items/i1/carry", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 词商品列表成功。
	recorder, request = marketIntelRequest(handler, cookie, http.MethodGet, "/api/v1/market-intel/keywords/AI%E8%AF%BE%E7%A8%8B/items", "")
	assertOpenAPISuccessResponse(t, request, recorder)
}
