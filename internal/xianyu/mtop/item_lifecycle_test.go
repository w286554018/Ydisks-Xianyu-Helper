package mtop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// TestItemDownshelfAndDeleteRequestsCaptureShape 验证下架与删除请求的 wire 形态
// 与抓包 ground truth 一致：URL 查询参数、表单体 data={"itemId":"<id>"} 与成功判定。
func TestItemDownshelfAndDeleteRequestsCaptureShape(t *testing.T) {
	// cases 覆盖下架与删除两个端点的共同契约。
	cases := []struct {
		// name 是用例名称。
		name string
		// invoke 是发起调用的动作。
		invoke func(c *ClientImpl) (bool, []string, string, error)
		// wantAPI 是请求 URL 查询中必须出现的 api 名。
		wantAPI string
		// wantVersion 是请求 URL 查询中必须出现的版本号。
		wantVersion string
	}{
		{name: "下架", wantAPI: "mtop.taobao.idle.item.downshelf", wantVersion: "2.0",
			invoke: func(c *ClientImpl) (bool, []string, string, error) {
				return c.DownshelfItemContext(context.Background(), consignCookies, "1097378253052")
			}},
		{name: "删除", wantAPI: "com.taobao.idle.item.delete", wantVersion: "1.1",
			invoke: func(c *ClientImpl) (bool, []string, string, error) {
				return c.DeleteItemContext(context.Background(), consignCookies, "1097378253052")
			}},
	}
	// tc 是当前子测试闭包独占的用例。
	for _, tc := range cases {
		// tc 保存当前子测试闭包独占的用例。
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// requests 记录命中次数；query、body 保存最后一次请求的可观测形态。
			var requests atomic.Int32
			// query、body 保存最近一次请求的查询串与正文。
			var query, body string
			// server 是本地替身端点，校验请求形态并返回成功响应。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				query = r.URL.RawQuery
				// parseErr 保存请求表单解析错误。
				if parseErr := r.ParseForm(); parseErr != nil {
					t.Errorf("解析表单失败: %v", parseErr)
				}
				body = r.Form.Get("data")
				fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"]}`)
			}))
			defer server.Close()
			// client 注入本地替身端点。
			client := &ClientImpl{HTTPClient: server.Client()}
			if tc.wantAPI == "mtop.taobao.idle.item.downshelf" {
				client.DownshelfItemURL = server.URL + "/"
			} else {
				client.DeleteItemURL = server.URL + "/"
			}
			// ok、ret、updated、err 保存调用结果。
			ok, ret, updated, err := tc.invoke(client)
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if !ok || len(ret) == 0 || !strings.HasPrefix(ret[0], "SUCCESS::") {
				t.Fatalf("ok=%v ret=%v", ok, ret)
			}
			if updated != consignCookies {
				t.Fatalf("无 Set-Cookie 时 updated 应保持原样: %q", updated)
			}
			if requests.Load() != 1 {
				t.Fatalf("requests=%d want 1", requests.Load())
			}
			// parsed 解析查询串断言签名参数。
			parsed, parseErr := url.ParseQuery(query)
			if parseErr != nil {
				t.Fatalf("解析查询串失败: %v", parseErr)
			}
			if parsed.Get("api") != tc.wantAPI || parsed.Get("v") != tc.wantVersion {
				t.Fatalf("查询参数不符 api=%q v=%q", parsed.Get("api"), parsed.Get("v"))
			}
			if parsed.Get("appKey") == "" || parsed.Get("sign") == "" || parsed.Get("accountSite") != "xianyu" {
				t.Fatalf("签名参数缺失或错误: %q", query)
			}
			if body != fmt.Sprintf(`{"itemId":"%s"}`, "1097378253052") {
				t.Fatalf("请求体与抓包 ground truth 不符: %q", body)
			}
		})
	}
}

// TestItemDownshelfRiskVerificationStopsImmediately 验证风控响应立即终止：
// 一次用户动作只允许一次平台请求，风控（FAIL_SYS_USER_VALIDATE/RGV587）必须立即停止，不得重试。
func TestItemDownshelfRiskVerificationStopsImmediately(t *testing.T) {
	// requests 记录命中次数。
	var requests atomic.Int32
	// server 始终返回 FAIL 业务响应。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_SYS_USER_VALIDATE]令牌过期_改进版体验"]}`)
	}))
	defer server.Close()
	// client 注入本地替身端点。
	client := &ClientImpl{HTTPClient: server.Client(), DownshelfItemURL: server.URL + "/"}
	// ok、ret、updated、err 保存调用结果。
	ok, ret, _, err := client.DownshelfItemContext(context.Background(), consignCookies, "item-x")
	if ok {
		t.Fatal("风控响应不应判定成功")
	}
	if err == nil {
		t.Fatal("风控响应应返回错误供上层中止批次")
	}
	if len(ret) == 0 {
		t.Fatal("风控响应应保留 ret 供分类")
	}
	if requests.Load() != 1 {
		t.Fatalf("风控必须立即停止，请求次数应恰为 1，实际 %d", requests.Load())
	}
}

// TestItemDownshelfBusinessRejectReturnsQuietly 验证普通业务拒绝（FAIL_BIZ_）按业务错误返回：
// 不重试、请求次数恰为 1，且 ok=false、err=nil（与既有 consign 业务失败语义一致）。
func TestItemDownshelfBusinessRejectReturnsQuietly(t *testing.T) {
	// requests 记录命中次数。
	var requests atomic.Int32
	// server 返回普通业务拒绝响应。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["FAIL_BIZ_ITEM_NOT_EXIST::宝贝不存在"]}`)
	}))
	defer server.Close()
	// client 注入本地替身端点。
	client := &ClientImpl{HTTPClient: server.Client(), DownshelfItemURL: server.URL + "/"}
	// ok、ret、updated、err 保存调用结果。
	ok, ret, _, err := client.DownshelfItemContext(context.Background(), consignCookies, "item-biz")
	if ok {
		t.Fatal("业务拒绝不应判定成功")
	}
	if err != nil {
		t.Fatalf("业务拒绝应静默返回（ok=false），实际 err=%v", err)
	}
	if len(ret) == 0 || !strings.Contains(ret[0], "FAIL_BIZ_ITEM_NOT_EXIST") {
		t.Fatalf("ret 应保留平台原文 ret=%v", ret)
	}
	if requests.Load() != 1 {
		t.Fatalf("业务拒绝不应重试，请求次数应恰为 1，实际 %d", requests.Load())
	}
}

// TestItemDeleteHTTPErrorKeepsRet 验证非 2xx 响应时保留 ret 且返回错误。
func TestItemDeleteHTTPErrorKeepsRet(t *testing.T) {
	// server 返回 500。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"ret":["FAIL_SYS_SERVER_ERROR"]}`)
	}))
	defer server.Close()
	// client 注入本地替身端点。
	client := &ClientImpl{HTTPClient: server.Client(), DeleteItemURL: server.URL + "/"}
	// ok、ret、updated、err 保存调用结果。
	ok, ret, _, err := client.DeleteItemContext(context.Background(), consignCookies, "item-y")
	if ok {
		t.Fatal("500 响应不应判定成功")
	}
	if err == nil {
		t.Fatal("500 响应应返回错误")
	}
	if len(ret) == 0 || !strings.Contains(ret[0], "FAIL_SYS_SERVER_ERROR") {
		t.Fatalf("ret 应保留平台原文 ret=%v", ret)
	}
}
