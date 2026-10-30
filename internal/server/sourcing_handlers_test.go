package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sourcingapp "xianyu-go/internal/application/sourcing"
)

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
