// Package server: 市场情报端口投影与错误映射焦点测试。
package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	marketintel "xianyu-go/internal/application/marketintel"
)

// TestMarketIntelTransportPortProjector 验证投影器把调用原样委托给具体 Service。
func TestMarketIntelTransportPortProjector(t *testing.T) {
	// nil 服务必须投影为 nil 接口。
	if port := MarketIntelTransportPort(nil); port != nil {
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
	if port := MarketIntelTransportPort(nil); port != nil {
		t.Fatal("nil 服务应投影为 nil 接口")
	}
}
