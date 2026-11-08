package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/application/marketintel"
)

// 测试使用的固定令牌与商品标识。
const (
	localFetchTestToken  = "test-token-9081"
	localFetchTestItemID = "1095623904703"
)

// newLocalFetchTestClient 用模拟助手服务器构造客户端，并返回收集日志的缓冲。
func newLocalFetchTestClient(t *testing.T, handler http.HandlerFunc) (*LocalFetchAgentClient, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	// server 是模拟本机助手的测试服务器。
	server := httptest.NewServer(handler)
	// logBuf 收集客户端日志，用于断言不泄漏令牌。
	logBuf := &bytes.Buffer{}
	// client 是被测助手客户端。
	client := NewLocalFetchAgentClient(server.URL, localFetchTestToken, slog.New(slog.NewTextHandler(logBuf, nil)))
	if client == nil {
		t.Fatal("已配置地址与令牌时必须构造出客户端")
	}
	return client, server, logBuf
}

// TestNewLocalFetchAgentClientDisabled 验证未配置地址或令牌时构造为 nil，且调用立即返回不可用错误。
func TestNewLocalFetchAgentClientDisabled(t *testing.T) {
	// cases 是地址与令牌的组合。
	cases := []struct {
		// name 是用例名。
		name string
		// baseURL 是助手地址。
		baseURL string
		// token 是共享令牌。
		token string
	}{
		{name: "地址为空", baseURL: "  ", token: localFetchTestToken},
		{name: "令牌为空", baseURL: "http://127.0.0.1:17901", token: ""},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		if // got 是构造结果，未配置时必须为 nil。
		got := NewLocalFetchAgentClient(tc.baseURL, tc.token, nil); got != nil {
			t.Fatalf("%s 时必须返回 nil，实际 %+v", tc.name, got)
		}
	}
	// 未启用时的 nil 客户端也必须给出明确场景错误，而不是 panic。
	var disabled *LocalFetchAgentClient
	// detail、err 是调用结果与错误。
	detail, err := disabled.FetchCompetitorDetail(context.Background(), localFetchTestItemID)
	if !errors.Is(err, marketintel.ErrLocalFetchUnavailable) {
		t.Fatalf("nil 客户端必须返回不可用错误，实际 %v", err)
	}
	if detail.Title != "" {
		t.Fatalf("失败时不得返回详情: %+v", detail)
	}
}

// TestLocalFetchAgentFetchSuccess 验证成功路径：请求形状正确、字段映射完整、令牌不落日志。
func TestLocalFetchAgentFetchSuccess(t *testing.T) {
	// client、server、logBuf 是被测客户端、模拟助手与日志缓冲。
	client, server, logBuf := newLocalFetchTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("必须使用 POST，实际 %s", r.Method)
		}
		if r.URL.Path != localFetchDetailPath {
			t.Errorf("路径必须是 %s，实际 %s", localFetchDetailPath, r.URL.Path)
		}
		if // got 是请求携带的令牌。
		got := r.Header.Get(localFetchTokenHeader); got != localFetchTestToken {
			t.Errorf("令牌请求头不正确，实际 %q", got)
		}
		// request 是助手收到的请求体。
		var request map[string]string
		if // err 是请求体解析失败原因。
		err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("请求体不是合法 JSON: %v", err)
		}
		if request["item_id"] != localFetchTestItemID {
			t.Errorf("item_id 不正确，实际 %q", request["item_id"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"item_id":"`+localFetchTestItemID+`","title":"测试商品","price":"20",`+
			`"desc_text":"描述","images":["https://img.example.com/a.jpg"],`+
			`"seller_nickname":"东菱之家","seller_id":"3835297081"}`)
	})
	defer server.Close()

	// detail、err 是抓取结果与错误。
	detail, err := client.FetchCompetitorDetail(context.Background(), localFetchTestItemID)
	if err != nil {
		t.Fatalf("成功路径不应报错: %v", err)
	}
	if detail.ItemID != localFetchTestItemID || detail.Title != "测试商品" || detail.Price != "20" {
		t.Fatalf("基础字段映射错误: %+v", detail)
	}
	if detail.DescText != "描述" || len(detail.Images) != 1 || detail.Images[0] != "https://img.example.com/a.jpg" {
		t.Fatalf("描述或图片映射错误: %+v", detail)
	}
	if detail.SellerNickname != "东菱之家" || detail.SellerID != "3835297081" {
		t.Fatalf("卖家字段映射错误: %+v", detail)
	}
	if strings.Contains(logBuf.String(), localFetchTestToken) {
		t.Fatal("日志不得包含共享令牌")
	}
}

// TestLocalFetchAgentFailureMapping 验证助手错误码与状态码到应用层场景错误的映射。
func TestLocalFetchAgentFailureMapping(t *testing.T) {
	// cases 是各类失败响应与期望错误。
	cases := []struct {
		// name 是用例名。
		name string
		// status 是助手返回的 HTTP 状态码。
		status int
		// body 是助手返回的响应体。
		body string
		// want 是期望的应用层错误。
		want error
	}{
		{name: "未登录", status: http.StatusConflict, body: `{"code":"not_logged_in"}`, want: marketintel.ErrLocalFetchNotLoggedIn},
		{name: "风控", status: http.StatusConflict, body: `{"code":"risk_control"}`, want: marketintel.ErrLocalFetchRiskControl},
		{name: "令牌错误", status: http.StatusUnauthorized, body: `{"code":"unauthorized"}`, want: marketintel.ErrLocalFetchUnauthorized},
		{name: "Chrome 不可用", status: http.StatusServiceUnavailable, body: `{"code":"chrome_unavailable"}`, want: marketintel.ErrLocalFetchUnavailable},
		{name: "仅状态码 401", status: http.StatusUnauthorized, body: ``, want: marketintel.ErrLocalFetchUnauthorized},
		{name: "仅状态码 503", status: http.StatusServiceUnavailable, body: ``, want: marketintel.ErrLocalFetchUnavailable},
		{name: "仅状态码 409", status: http.StatusConflict, body: ``, want: marketintel.ErrLocalFetchRiskControl},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// client、server、logBuf 是被测客户端、模拟助手与日志缓冲。
			client, server, logBuf := newLocalFetchTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			defer server.Close()
			// err 是抓取错误。
			_, err := client.FetchCompetitorDetail(context.Background(), localFetchTestItemID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("期望 %v，实际 %v", tc.want, err)
			}
			if strings.Contains(logBuf.String(), localFetchTestToken) {
				t.Fatal("日志不得包含共享令牌")
			}
		})
	}
}

// TestLocalFetchAgentUnreachableAndBadBody 验证网络不可达与响应不可解析时的错误语义。
func TestLocalFetchAgentUnreachableAndBadBody(t *testing.T) {
	// 不可达：先启动再关闭，端口随即失效。
	// client、server、_ 是被测客户端、模拟助手与日志缓冲。
	client, server, logBuf := newLocalFetchTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "not-json")
	})
	// detailErr 是响应不可解析时的错误。
	_, detailErr := client.FetchCompetitorDetail(context.Background(), localFetchTestItemID)
	if detailErr == nil || !strings.Contains(detailErr.Error(), "解析助手响应失败") {
		t.Fatalf("非 JSON 响应必须报解析失败，实际 %v", detailErr)
	}
	server.Close()
	// unreachableErr 是助手已停止时的错误。
	_, unreachableErr := client.FetchCompetitorDetail(context.Background(), localFetchTestItemID)
	if !errors.Is(unreachableErr, marketintel.ErrLocalFetchUnavailable) {
		t.Fatalf("助手不可达必须返回不可用错误，实际 %v", unreachableErr)
	}
	if strings.Contains(logBuf.String(), localFetchTestToken) {
		t.Fatal("日志不得包含共享令牌")
	}
	if strings.Contains(unreachableErr.Error(), localFetchTestToken) {
		t.Fatal("错误文本不得包含共享令牌")
	}
}

// TestLocalFetchAgentUnexpectedStatus 验证未归类状态码返回固定文案而非透传响应体。
func TestLocalFetchAgentUnexpectedStatus(t *testing.T) {
	// client、server、_ 是被测客户端、模拟助手与日志缓冲。
	client, server, _ := newLocalFetchTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, `{"secret":"must-not-leak"}`)
	})
	defer server.Close()
	// err 是抓取错误。
	_, err := client.FetchCompetitorDetail(context.Background(), localFetchTestItemID)
	if err == nil {
		t.Fatal("异常状态必须报错")
	}
	if !strings.Contains(err.Error(), "418") {
		t.Fatalf("错误文本应包含状态码，实际 %v", err)
	}
	if strings.Contains(err.Error(), "must-not-leak") {
		t.Fatal("错误文本不得透传助手响应体")
	}
}
