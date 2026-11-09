// Package server: 我的商品统计端点（刷新/批量/列表附带统计）的契约与错误映射测试。
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	itemapp "xianyu-go/internal/application/items"
)

// fakeItemStatsPort 是商品统计用例替身；记录入参并返回固定数据或预置错误。
type fakeItemStatsPort struct {
	// stats 是单个刷新返回的统计。
	stats itemapp.ItemStats
	// batch 是批量刷新返回的结果。
	batch itemapp.StatsBatchResult
	// listRows 是列表读取返回的统计映射。
	listRows map[string]itemapp.ItemStats
	// refreshErr、batchErr、listErr 是三个用例各自的预置错误。
	refreshErr error
	batchErr   error
	listErr    error
	// lastItemIDs、lastLimit 记录批量刷新的入参，用于断言透传。
	lastItemIDs []string
	lastLimit   int
}

// RefreshItemStats 返回预置的单商品统计或错误。
func (f *fakeItemStatsPort) RefreshItemStats(_ context.Context, _ int64, _, _ string) (itemapp.ItemStats, error) {
	return f.stats, f.refreshErr
}

// RefreshStatsBatch 记录入参并返回预置的批量结果或错误。
func (f *fakeItemStatsPort) RefreshStatsBatch(_ context.Context, _ int64, _ string, itemIDs []string, limit int) (itemapp.StatsBatchResult, error) {
	f.lastItemIDs, f.lastLimit = itemIDs, limit
	return f.batch, f.batchErr
}

// ListItemStatsForItems 返回预置的列表统计或错误。
func (f *fakeItemStatsPort) ListItemStatsForItems(_ context.Context, _ []itemapp.ItemStatsRef) (map[string]itemapp.ItemStats, error) {
	return f.listRows, f.listErr
}

// defaultItemStats 是契约场景使用的统计样本。
var defaultItemStats = itemapp.ItemStats{
	CookieID: "acc1", ItemID: "item-v1",
	BrowseCnt: 22, WantCnt: 2, CollectCnt: 1, SoldCnt: 0,
	FetchedAt: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC), Fresh: true,
}

// itemStatsRequest 发起一次带管理员会话的商品统计请求并返回响应与请求。
func itemStatsRequest(handler http.Handler, sessionCookie *http.Cookie, method, path, body string) (*httptest.ResponseRecorder, *http.Request) {
	// request 是本次统计请求；无正文时使用 nil body。
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	if sessionCookie != nil {
		request.AddCookie(sessionCookie)
	}
	// recorder 捕获真实 Router 的响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder, request
}

// TestItemStatsEndpointsContract 验证两条商品统计端点的真实成功响应与其他关键状态码契约。
func TestItemStatsEndpointsContract(t *testing.T) {
	// srv、cleanup 是测试 Server 与资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是真实路由；cookie 是管理员会话。
	handler := srv.Router()
	// cookie 是登录会话。
	cookie := loginHelper(t, handler)

	// 未登录访问必须返回 401，且不触发任何用例。
	unauthorized, _ := itemStatsRequest(handler, nil, http.MethodPost, "/api/v1/items/acc1/item-v1/stats/refresh", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("未登录刷新应返回 401，实际 %d", unauthorized.Code)
	}

	// fake 是可成功返回的统计替身。
	fake := &fakeItemStatsPort{
		stats: defaultItemStats,
		batch: itemapp.StatsBatchResult{Refreshed: []itemapp.ItemStats{defaultItemStats}, Skipped: 1, Remaining: 2, Stopped: true, StopReason: itemapp.ItemStatsStopRiskControl},
	}
	srv.applications.itemStats = fake

	// 单个刷新成功。
	recorder, request := itemStatsRequest(handler, cookie, http.MethodPost, "/api/v1/items/acc1/item-v1/stats/refresh", "")
	assertOpenAPISuccessResponse(t, request, recorder)
	// single 是解析后的单商品统计响应。
	var single itemStatsResponse
	if // decodeErr 是单商品统计响应解析失败原因。
	decodeErr := json.Unmarshal(recorder.Body.Bytes(), &single); decodeErr != nil {
		t.Fatalf("解析单商品统计响应失败: %v", decodeErr)
	}
	if single.BrowseCnt != 22 || single.WantCnt != 2 || single.CollectCnt != 1 || single.SoldCnt != 0 || !single.Fresh {
		t.Fatalf("单商品统计响应不符: %+v", single)
	}

	// 批量刷新成功。
	recorder, request = itemStatsRequest(handler, cookie, http.MethodPost, "/api/v1/items/stats/refresh-batch",
		`{"cookie_id":"acc1","item_ids":["item-v1","item-v2"],"limit":5}`)
	assertOpenAPISuccessResponse(t, request, recorder)
	// batch 是解析后的批量响应。
	var batch itemStatsBatchResponse
	if // decodeErr 是批量响应解析失败原因。
	decodeErr := json.Unmarshal(recorder.Body.Bytes(), &batch); decodeErr != nil {
		t.Fatalf("解析批量统计响应失败: %v", decodeErr)
	}
	if len(batch.Refreshed) != 1 || batch.Skipped != 1 || batch.Remaining != 2 || !batch.Stopped || batch.StopReason != itemapp.ItemStatsStopRiskControl {
		t.Fatalf("批量统计响应不符: %+v", batch)
	}
	if len(fake.lastItemIDs) != 2 || fake.lastLimit != 5 {
		t.Fatalf("批量入参未透传: itemIDs=%v limit=%d", fake.lastItemIDs, fake.lastLimit)
	}

	// 批量请求缺少账号时必须返回 400。
	missingCookie, _ := itemStatsRequest(handler, cookie, http.MethodPost, "/api/v1/items/stats/refresh-batch", `{"item_ids":["item-v1"]}`)
	if missingCookie.Code != http.StatusBadRequest {
		t.Fatalf("缺少 cookie_id 应返回 400，实际 %d", missingCookie.Code)
	}
	// 请求体不是合法 JSON 时必须返回 400。
	badBody, _ := itemStatsRequest(handler, cookie, http.MethodPost, "/api/v1/items/stats/refresh-batch", "not-json")
	if badBody.Code != http.StatusBadRequest {
		t.Fatalf("非法请求体应返回 400，实际 %d", badBody.Code)
	}
	// 路径参数为空时必须返回 400（空白账号名经 URL 编码后仍应被拒绝）。
	blankPath, _ := itemStatsRequest(handler, cookie, http.MethodPost, "/api/v1/items/%20/item-v1/stats/refresh", "")
	if blankPath.Code != http.StatusBadRequest {
		t.Fatalf("空白账号路径参数应返回 400，实际 %d", blankPath.Code)
	}

	// 账号不归属时返回 403。
	fake.refreshErr = itemapp.ErrItemStatsNotOwned
	// forbidden 是账号不归属时的刷新响应记录器。
	forbidden, _ := itemStatsRequest(handler, cookie, http.MethodPost, "/api/v1/items/acc1/item-v1/stats/refresh", "")
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("账号不归属应返回 403，实际 %d", forbidden.Code)
	}
}

// TestItemStatsErrorMapping 验证抓取类错误与未归类错误的状态码映射与脱敏要求。
func TestItemStatsErrorMapping(t *testing.T) {
	// srv、cleanup 是测试 Server 与资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是真实路由；cookie 是管理员会话。
	handler := srv.Router()
	// cookie 是登录会话。
	cookie := loginHelper(t, handler)
	// fake 是可注入错误的统计替身。
	fake := &fakeItemStatsPort{}
	srv.applications.itemStats = fake

	// cases 是预置错误与期望状态码的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// err 是用例返回的错误。
		err error
		// wantStatus 是期望状态码。
		wantStatus int
	}{
		{name: "助手不可用", err: itemapp.ErrItemStatsUnavailable, wantStatus: http.StatusServiceUnavailable},
		{name: "共享令牌不一致", err: itemapp.ErrItemStatsUnauthorized, wantStatus: http.StatusServiceUnavailable},
		{name: "助手未登录", err: itemapp.ErrItemStatsNotLoggedIn, wantStatus: http.StatusServiceUnavailable},
		{name: "平台要求人工验证", err: itemapp.ErrItemStatsRiskControl, wantStatus: http.StatusConflict},
		{name: "未归类错误", err: errors.New("底层无法识别"), wantStatus: http.StatusInternalServerError},
	}
	// tc 是当前错误映射用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake.refreshErr = tc.err
			// recorder 是单商品刷新响应记录器。
			recorder, _ := itemStatsRequest(handler, cookie, http.MethodPost, "/api/v1/items/acc1/item-v1/stats/refresh", "")
			if recorder.Code != tc.wantStatus {
				t.Fatalf("状态码不符: got=%d want=%d body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			// 未归类错误必须返回固定文案，不得透传内部错误文本。
			if tc.wantStatus == http.StatusInternalServerError && strings.Contains(recorder.Body.String(), "底层无法识别") {
				t.Fatalf("错误响应不得透传内部错误文本: %s", recorder.Body.String())
			}
			// 批量路径必须复用同一映射。
			fake.batchErr = tc.err
			// batchRecorder 是批量刷新响应记录器。
			batchRecorder, _ := itemStatsRequest(handler, cookie, http.MethodPost, "/api/v1/items/stats/refresh-batch", `{"cookie_id":"acc1","item_ids":["item-v1"]}`)
			if batchRecorder.Code != tc.wantStatus {
				t.Fatalf("批量路径状态码不符: got=%d want=%d", batchRecorder.Code, tc.wantStatus)
			}
		})
	}
}

// TestItemListCarriesStats 验证商品列表附带统计：有统计返回对象、无统计返回 null、查询失败降级不失败。
func TestItemListCarriesStats(t *testing.T) {
	// srv、store、cleanup 是测试 Server、数据库聚合与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是写入夹具使用的请求上下文。
	ctx := context.Background()
	// fixtureErr 是商品与统计夹具写入失败原因。
	if _, fixtureErr := store.DB.ExecContext(ctx,
		`INSERT INTO item_info (cookie_id, item_id, item_title, item_description, item_category, item_price, item_detail, is_multi_spec, multi_quantity_delivery)
		 VALUES ('acc1','item-with-stats','有统计商品','','资料','12.00','{}',0,0), ('acc1','item-no-stats','无统计商品','','资料','10.00','{}',0,0)`); fixtureErr != nil {
		t.Fatalf("写入商品夹具失败: %v", fixtureErr)
	}
	// statsErr 是统计夹具写入失败原因。
	if _, statsErr := store.DB.ExecContext(ctx,
		`INSERT INTO item_stats (cookie_id, item_id, browse_cnt, want_cnt, collect_cnt, sold_cnt, fetched_at)
		 VALUES ('acc1','item-with-stats',22,2,1,0,'2026-11-20 10:00:00')`); statsErr != nil {
		t.Fatalf("写入统计夹具失败: %v", statsErr)
	}
	// handler 是真实路由；cookie 是管理员会话。
	handler := srv.Router()
	// cookie 是登录会话。
	cookie := loginHelper(t, handler)

	// recorder 是商品列表响应记录器。
	recorder, request := itemStatsRequest(handler, cookie, http.MethodGet, "/api/v1/items?cookie_id=acc1", "")
	assertOpenAPISuccessResponse(t, request, recorder)
	// rows 是解析后的商品列表。
	var rows []itemListResponse
	if // decodeErr 是商品列表解析失败原因。
	decodeErr := json.Unmarshal(recorder.Body.Bytes(), &rows); decodeErr != nil {
		t.Fatalf("解析商品列表失败: %v", decodeErr)
	}
	if len(rows) != 2 {
		t.Fatalf("商品列表数量不符: %+v", rows)
	}
	// row 是当前遍历到的商品行；统计必须按商品标识一一对应。
	for _, row := range rows {
		switch row.ItemID {
		case "item-with-stats":
			if row.Stats == nil || row.Stats.BrowseCnt != 22 || row.Stats.WantCnt != 2 {
				t.Fatalf("有统计商品未附带统计: %+v", row.Stats)
			}
		case "item-no-stats":
			if row.Stats != nil {
				t.Fatalf("无统计商品必须返回 null: %+v", row.Stats)
			}
		default:
			t.Fatalf("出现未预期的商品: %+v", row)
		}
	}

	// 统计查询失败时必须降级为不展示统计，而不是让整个商品列表失败。
	srv.applications.itemStats = &fakeItemStatsPort{listErr: errors.New("统计查询失败")}
	// degraded 是降级后的商品列表响应记录器。
	degraded, degradedRequest := itemStatsRequest(handler, cookie, http.MethodGet, "/api/v1/items?cookie_id=acc1", "")
	assertOpenAPISuccessResponse(t, degradedRequest, degraded)
	// degradedRows 是降级后的商品列表。
	var degradedRows []itemListResponse
	if // decodeErr 是降级列表解析失败原因。
	decodeErr := json.Unmarshal(degraded.Body.Bytes(), &degradedRows); decodeErr != nil {
		t.Fatalf("解析降级商品列表失败: %v", decodeErr)
	}
	// degradedRow 是降级后的商品行；统计必须全部为 null。
	for _, degradedRow := range degradedRows {
		if degradedRow.Stats != nil {
			t.Fatalf("统计故障时应降级为 null: %+v", degradedRow.Stats)
		}
	}
}
