// Package localfetch: 本机抓取助手 HTTP 契约、计数透传与错误映射的焦点测试。
package localfetch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/browser"
)

// localFetchTestToken 是测试使用的共享令牌明文。
const localFetchTestToken = "0123456789abcdef0123456789abcdef"

// newTestAgent 用临时令牌文件构造助手，并可替换取数实现；返回助手与共享令牌。
// DebugPort 指向必然不通的端口，健康检查据此得到 chrome_connected=false。
func newTestAgent(t *testing.T, fetch func(ctx context.Context, itemID string) (browser.CompetitorDetail, error)) (*Agent, string) {
	t.Helper()
	// tokenPath 是本次测试使用的临时令牌文件路径。
	tokenPath := filepath.Join(t.TempDir(), "token")
	if // writeErr 是令牌文件写入失败原因。
	writeErr := os.WriteFile(tokenPath, []byte(localFetchTestToken+"\n"), 0o600); writeErr != nil {
		t.Fatalf("写入令牌文件失败: %v", writeErr)
	}
	// agent、err 是被测助手与构造错误。
	agent, err := NewAgent(Config{
		Listen:     "127.0.0.1:0",
		ChromePath: "/nonexistent/chrome",
		ProfileDir: t.TempDir(),
		DebugPort:  1,
		TokenFile:  tokenPath,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("构造助手失败: %v", err)
	}
	if fetch != nil {
		agent.fetch = fetch
	}
	return agent, localFetchTestToken
}

// doAgentRequest 向助手处理器发起一次请求并返回记录器。
func doAgentRequest(t *testing.T, agent *Agent, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	// request 是构造出的请求；为空方法体时使用 nil 正文。
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if token != "" {
		request.Header.Set(tokenHeaderName, token)
	}
	// recorder 是响应记录器。
	recorder := httptest.NewRecorder()
	agent.Handler().ServeHTTP(recorder, request)
	return recorder
}

// TestNewAgentRequiresUsableToken 验证令牌文件缺失或为空时拒绝启动，避免无鉴权运行。
func TestNewAgentRequiresUsableToken(t *testing.T) {
	// dir 是本次测试使用的临时目录。
	dir := t.TempDir()
	// missingErr 是令牌文件缺失时的构造错误。
	if _, missingErr := NewAgent(Config{TokenFile: filepath.Join(dir, "absent")}); missingErr == nil {
		t.Fatal("令牌文件缺失时构造应失败")
	}
	// emptyPath 是内容为空的令牌文件路径。
	emptyPath := filepath.Join(dir, "empty")
	if // blankErr 是空令牌文件写入失败原因。
	blankErr := os.WriteFile(emptyPath, []byte("  \n"), 0o600); blankErr != nil {
		t.Fatalf("写入令牌文件失败: %v", blankErr)
	}
	// blankTokenErr 是令牌文件为空时的构造错误。
	if _, blankTokenErr := NewAgent(Config{TokenFile: emptyPath}); blankTokenErr == nil {
		t.Fatal("令牌文件为空时构造应失败")
	}
}

// TestAgentHealthAndMethodGuards 验证健康检查免令牌可用，且不支持的方法被明确拒绝。
func TestAgentHealthAndMethodGuards(t *testing.T) {
	// agent、token 是被测助手与共享令牌。
	agent, token := newTestAgent(t, nil)

	// 健康检查不需要令牌，便于本机自检。
	health := doAgentRequest(t, agent, http.MethodGet, "/health", "", "")
	if health.Code != http.StatusOK {
		t.Fatalf("健康检查应返回 200，实际 %d", health.Code)
	}
	// healthBody 是健康检查响应体。
	var healthBody HealthResponse
	if // decodeErr 是健康检查响应解析失败原因。
	decodeErr := json.Unmarshal(health.Body.Bytes(), &healthBody); decodeErr != nil {
		t.Fatalf("健康检查响应不是合法 JSON: %v", decodeErr)
	}
	if healthBody.Status != "ok" || healthBody.ChromeConnected {
		t.Fatalf("健康检查响应不符: %+v", healthBody)
	}
	if strings.Contains(health.Body.String(), token) {
		t.Fatal("健康检查响应不得包含共享令牌")
	}

	// 不支持的方法必须被拒绝，且不进入取数流程。
	for _, method := range []string{http.MethodPost} {
		if // healthCode 是非 GET 健康检查的状态码。
		healthCode := doAgentRequest(t, agent, method, "/health", "", "").Code; healthCode != http.StatusMethodNotAllowed {
			t.Fatalf("健康检查 %s 应返回 405，实际 %d", method, healthCode)
		}
	}
	if // detailCode 是 GET 取详情请求的状态码。
	detailCode := doAgentRequest(t, agent, http.MethodGet, "/fetch-detail", token, "").Code; detailCode != http.StatusMethodNotAllowed {
		t.Fatalf("取详情 GET 应返回 405，实际 %d", detailCode)
	}
}

// TestAgentFetchDetailValidation 验证令牌与 item_id 校验：只接受纯数字，拒绝任意输入。
func TestAgentFetchDetailValidation(t *testing.T) {
	// called 记录取数实现是否被调用；校验失败时绝不允许进入取数流程。
	called := false
	// agent、token 是被测助手与共享令牌。
	agent, token := newTestAgent(t, func(context.Context, string) (browser.CompetitorDetail, error) {
		called = true
		return browser.CompetitorDetail{}, nil
	})
	// cases 是校验失败用例：请求体、令牌与期望状态码。
	cases := []struct {
		// name 是用例名。
		name string
		// token 是请求携带的令牌。
		token string
		// body 是请求体。
		body string
		// wantStatus 是期望状态码。
		wantStatus int
		// wantCode 是期望错误码。
		wantCode string
	}{
		{name: "缺少令牌", body: `{"item_id":"1"}`, wantStatus: http.StatusUnauthorized, wantCode: "unauthorized"},
		{name: "错误令牌", token: "wrong-token", body: `{"item_id":"1"}`, wantStatus: http.StatusUnauthorized, wantCode: "unauthorized"},
		{name: "请求体非法", token: token, body: "not-json", wantStatus: http.StatusBadRequest, wantCode: "bad_request"},
		{name: "item_id 非数字", token: token, body: `{"item_id":"abc"}`, wantStatus: http.StatusBadRequest, wantCode: "bad_item_id"},
		{name: "item_id 为路径", token: token, body: `{"item_id":"../../etc/passwd"}`, wantStatus: http.StatusBadRequest, wantCode: "bad_item_id"},
		{name: "item_id 为空", token: token, body: `{"item_id":"  "}`, wantStatus: http.StatusBadRequest, wantCode: "bad_item_id"},
	}
	// tc 是当前校验用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// recorder 是本次请求的响应记录器。
			recorder := doAgentRequest(t, agent, http.MethodPost, "/fetch-detail", tc.token, tc.body)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("状态码不符: got=%d want=%d body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			// errBody 是错误响应体。
			var errBody ErrorResponse
			if // decodeErr 是错误响应解析失败原因。
			decodeErr := json.Unmarshal(recorder.Body.Bytes(), &errBody); decodeErr != nil {
				t.Fatalf("错误响应不是合法 JSON: %v", decodeErr)
			}
			if errBody.Code != tc.wantCode {
				t.Fatalf("错误码不符: got=%s want=%s", errBody.Code, tc.wantCode)
			}
			if strings.Contains(recorder.Body.String(), token) {
				t.Fatal("错误响应不得包含共享令牌")
			}
		})
	}
	if called {
		t.Fatal("校验失败时不得进入取数流程")
	}
}

// TestAgentFetchDetailCarriesStats 验证成功响应带回四项公开计数，且使用去空白后的 item_id。
func TestAgentFetchDetailCarriesStats(t *testing.T) {
	// gotItemID 记录取数实现收到的商品标识。
	var gotItemID string
	// agent、token 是被测助手与共享令牌。
	agent, token := newTestAgent(t, func(_ context.Context, itemID string) (browser.CompetitorDetail, error) {
		gotItemID = itemID
		return browser.CompetitorDetail{
			ItemID: itemID, Title: "测试商品", Price: "20", DescText: "描述",
			Images:         []string{"https://img.example.com/a.jpg"},
			SellerNickname: "东菱之家", SellerID: "3835297081",
			BrowseCount: 22, WantCount: 2, CollectCount: 1, SoldCount: 3,
		}, nil
	})

	// recorder 是成功响应记录器。
	recorder := doAgentRequest(t, agent, http.MethodPost, "/fetch-detail", token, `{"item_id":" 1098309932961 "}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("取详情成功应返回 200，实际 %d body=%s", recorder.Code, recorder.Body.String())
	}
	if gotItemID != "1098309932961" {
		t.Fatalf("item_id 必须先去除空白，实际 %q", gotItemID)
	}
	// response 是解析后的成功响应。
	var response FetchDetailResponse
	if // decodeErr 是成功响应解析失败原因。
	decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("成功响应不是合法 JSON: %v", decodeErr)
	}
	if response.BrowseCnt != 22 || response.WantCnt != 2 || response.CollectCnt != 1 || response.SoldCnt != 3 {
		t.Fatalf("四项计数未正确透传: %+v", response)
	}
	if response.Title != "测试商品" || response.SellerID != "3835297081" || len(response.Images) != 1 {
		t.Fatalf("基础字段透传错误: %+v", response)
	}
	if strings.Contains(recorder.Body.String(), token) {
		t.Fatal("成功响应不得包含共享令牌")
	}
}

// TestAgentFetchDetailErrorMapping 验证内部取数错误到 HTTP 状态与错误码的映射。
func TestAgentFetchDetailErrorMapping(t *testing.T) {
	// cases 是内部错误与期望响应语义的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// fetchErr 是取数实现返回的错误。
		fetchErr error
		// wantStatus 是期望状态码。
		wantStatus int
		// wantCode 是期望错误码。
		wantCode string
	}{
		{name: "item_id 非法", fetchErr: ErrBadItemID, wantStatus: http.StatusBadRequest, wantCode: "bad_item_id"},
		{name: "未登录", fetchErr: ErrNotLoggedIn, wantStatus: http.StatusConflict, wantCode: "not_logged_in"},
		{name: "风控", fetchErr: ErrRiskControl, wantStatus: http.StatusConflict, wantCode: "risk_control"},
		{name: "Chrome 不可用", fetchErr: ErrChromeUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "chrome_unavailable"},
		{name: "详情不可用", fetchErr: ErrDetailUnavailable, wantStatus: http.StatusBadGateway, wantCode: "detail_unavailable"},
		{name: "未归类错误", fetchErr: errors.New("未知失败"), wantStatus: http.StatusBadGateway, wantCode: "fetch_failed"},
	}
	// tc 是当前错误映射用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// agent、token 是被测助手与共享令牌。
			agent, token := newTestAgent(t, func(context.Context, string) (browser.CompetitorDetail, error) {
				return browser.CompetitorDetail{}, tc.fetchErr
			})
			// recorder 是失败响应记录器。
			recorder := doAgentRequest(t, agent, http.MethodPost, "/fetch-detail", token, `{"item_id":"1098309932961"}`)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("状态码不符: got=%d want=%d", recorder.Code, tc.wantStatus)
			}
			// errBody 是错误响应体。
			var errBody ErrorResponse
			if // decodeErr 是错误响应解析失败原因。
			decodeErr := json.Unmarshal(recorder.Body.Bytes(), &errBody); decodeErr != nil {
				t.Fatalf("错误响应不是合法 JSON: %v", decodeErr)
			}
			if errBody.Code != tc.wantCode {
				t.Fatalf("错误码不符: got=%s want=%s", errBody.Code, tc.wantCode)
			}
		})
	}
}

// TestFetchDetailWithoutChrome 验证未注入 Chrome 客户端时取数立即返回不可用错误。
func TestFetchDetailWithoutChrome(t *testing.T) {
	// nilAgent 是零值助手；不得 panic，必须返回不可用错误。
	var nilAgent *Agent
	// nilErr 是零值助手取数返回的错误。
	if _, nilErr := nilAgent.FetchDetail(context.Background(), "1098309932961"); !errors.Is(nilErr, ErrChromeUnavailable) {
		t.Fatalf("零值助手必须返回不可用错误，实际 %v", nilErr)
	}
	// agent 是没有 Chrome 客户端的助手。
	agent := &Agent{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	// chromeErr 是缺少 Chrome 客户端时取数返回的错误。
	if _, chromeErr := agent.FetchDetail(context.Background(), "1098309932961"); !errors.Is(chromeErr, ErrChromeUnavailable) {
		t.Fatalf("缺少 Chrome 客户端必须返回不可用错误，实际 %v", chromeErr)
	}
}

// TestSanitizeResponseURLAndPunishURL 验证响应地址脱敏与处罚地址提取。
func TestSanitizeResponseURLAndPunishURL(t *testing.T) {
	// raw 是带 query 与 fragment 的响应地址。
	raw := "https://h5api.m.taobao.com/h5/mtop.taobao.idle.pc.detail/1.0/?jsv=2.7.2&sign=SHOULD-NOT-LEAK#frag"
	// sanitized 是脱敏后的地址。
	sanitized := sanitizeResponseURL(raw)
	if sanitized != "https://h5api.m.taobao.com/h5/mtop.taobao.idle.pc.detail/1.0/" {
		t.Fatalf("响应地址脱敏结果不符: %s", sanitized)
	}
	// 非法地址按原样返回（超长时截断），不得 panic。
	if // brokenResult 是非法地址的脱敏结果。
	brokenResult := sanitizeResponseURL("://bad url"); brokenResult == "" {
		t.Fatal("非法地址不应返回空串")
	}
	// found 是含处罚地址的响应正文提取结果。
	found := findPunishURL([]byte(`{"data":{"url":"https://h5api.m.taobao.com/punish/_____tmd_____/index.htm?x=1"}}`))
	if !strings.Contains(found, punishMarker) {
		t.Fatalf("应提取到处罚地址，实际 %q", found)
	}
	// notPunish 是普通响应的提取结果，必须为空串。
	if notPunish := findPunishURL([]byte(`{"data":{"url":"https://www.goofish.com/item?id=1"}}`)); notPunish != "" {
		t.Fatalf("非处罚响应不应返回地址，实际 %q", notPunish)
	}
	if // invalidJSON 是非 JSON 正文的提取结果。
	invalidJSON := findPunishURL([]byte("not-json")); invalidJSON != "" {
		t.Fatalf("非 JSON 正文不应返回地址，实际 %q", invalidJSON)
	}
}

// TestRiskControlRets 验证限流/风控返回码识别。
func TestRiskControlRets(t *testing.T) {
	// cases 是返回码集合与期望识别结果。
	cases := []struct {
		// name 是用例名。
		name string
		// rets 是接口返回码集合。
		rets []string
		// want 是期望是否识别为限流或风控。
		want bool
	}{
		{name: "空返回码", rets: nil, want: false},
		{name: "正常返回", rets: []string{"SUCCESS::调用成功"}, want: false},
		{name: "限流返回", rets: []string{"FAIL_SYS_USER_VALIDATE::被挤爆啦"}, want: true},
		{name: "风控细节返回", rets: []string{"FAIL_SYS_USER_VALIDATE::RGV587"}, want: true},
	}
	// tc 是当前识别用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if // got 是本次识别结果。
			got := riskControlRets(tc.rets); got != tc.want {
				t.Fatalf("识别结果不符: got=%v want=%v", got, tc.want)
			}
		})
	}
}

// TestSleepWithJitterCancellation 验证退避等待可被上下文取消，且正常等待可完成。
func TestSleepWithJitterCancellation(t *testing.T) {
	// cancelledCtx 是已取消的上下文；等待必须立即返回取消错误。
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if // cancelErr 是等待被取消时返回的错误。
	cancelErr := sleepWithJitter(cancelledCtx, 1, time.Hour, time.Hour); !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("取消后必须返回取消错误，实际 %v", cancelErr)
	}
	// 零间隔等待必须立即成功返回。
	if // okErr 是零间隔等待返回的错误。
	okErr := sleepWithJitter(context.Background(), 0, 0, 0); okErr != nil {
		t.Fatalf("零间隔等待不应报错，实际 %v", okErr)
	}
}

// TestWriteAgentErrorEnvelope 验证统一错误响应信封的字段与内容。
func TestWriteAgentErrorEnvelope(t *testing.T) {
	// recorder 是直接调用写出函数的记录器。
	recorder := httptest.NewRecorder()
	writeAgentError(recorder, http.StatusConflict, "risk_control", "需要人工验证")
	// errBody 是解析后的错误响应。
	var errBody ErrorResponse
	if // decodeErr 是错误响应解析失败原因。
	decodeErr := json.Unmarshal(recorder.Body.Bytes(), &errBody); decodeErr != nil {
		t.Fatalf("错误响应不是合法 JSON: %v", decodeErr)
	}
	if recorder.Code != http.StatusConflict || errBody.Code != "risk_control" || errBody.Message != "需要人工验证" {
		t.Fatalf("错误响应不符: status=%d body=%+v", recorder.Code, errBody)
	}
	if // contentType 是错误响应内容类型。
	contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("内容类型必须是 JSON，实际 %q", contentType)
	}
	// second 是确认 writeAgentJSON 也写出 JSON 的记录器。
	second := httptest.NewRecorder()
	writeAgentJSON(second, http.StatusOK, HealthResponse{Status: "ok"})
	if !strings.Contains(second.Body.String(), `"status":"ok"`) {
		t.Fatalf("成功响应内容不符: %s", second.Body.String())
	}
}
