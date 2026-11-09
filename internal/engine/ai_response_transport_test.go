// Package engine: AI 响应形状归一的确定性测试（含用真实 SDK 验证解析）。
package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sashabaranov/go-openai"
)

// fakeRoundTripFunc 是测试用的传输实现。
type fakeRoundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip 直接调用测试函数。
func (f fakeRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// newJSONResponse 构造带 JSON 正文的响应。
func newJSONResponse(contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestUnwrapChatResponseTransportNormalizesWrappedBody 验证被 data 包裹的 JSON 响应在传输层被归一。
func TestUnwrapChatResponseTransportNormalizesWrappedBody(t *testing.T) {
	// transport 是安装了归一的传输。
	transport := &unwrapChatResponseTransport{base: fakeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return newJSONResponse("application/json", `{"data":{"choices":[{"message":{"content":"包装正文"}}]}}`), nil
	})}
	// response、err 是归一后的响应与错误。
	response, err := transport.RoundTrip(mustRequest(t))
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	// raw 是归一后的正文。
	raw, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		t.Fatalf("读取正文失败: %v", readErr)
	}
	_ = response.Body.Close()
	// payload 是用于断言顶层候选的结构。
	var payload struct {
		// Choices 是候选列表。
		Choices []json.RawMessage `json:"choices"`
	}
	if // err 是归一结果的 JSON 解析错误。
	err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("归一结果不是合法 JSON: %v", err)
	}
	if len(payload.Choices) != 1 {
		t.Fatalf("当归一为顶层 choices，实际正文: %s", raw)
	}
	if response.ContentLength != int64(len(raw)) {
		t.Fatalf("ContentLength 必须与正文一致，实际 %d / %d", response.ContentLength, len(raw))
	}
}

// TestUnwrapChatResponseTransportLeavesNonJSONUntouched 验证非 JSON 响应原样返回，不改变行为。
func TestUnwrapChatResponseTransportLeavesNonJSONUntouched(t *testing.T) {
	// body 是纯文本正文。
	body := "data: {\"data\":{\"choices\":[]}}\n\n"
	// transport 是安装了归一的传输。
	transport := &unwrapChatResponseTransport{base: fakeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return newJSONResponse("text/event-stream", body), nil
	})}
	// response、err 是响应与错误。
	response, err := transport.RoundTrip(mustRequest(t))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	// raw 是读取到的正文。
	raw, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(raw) != body {
		t.Fatalf("非 JSON 响应不得被改写，实际 %q", string(raw))
	}
}

// TestUnwrapChatResponseTransportPropagatesUpstreamError 验证原始传输的错误按原样返回。
func TestUnwrapChatResponseTransportPropagatesUpstreamError(t *testing.T) {
	// transport 是返回错误的传输。
	transport := &unwrapChatResponseTransport{base: fakeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, http.ErrHandlerTimeout
	})}
	// err 是原始传输返回的错误，必须按原样向上传播。
	if _, err := transport.RoundTrip(mustRequest(t)); err == nil {
		t.Fatal("原始传输错误必须向上返回")
	}
}

// TestWithChatResponseUnwrapInstallsOnce 验证归一传输只安装一次，且不修改入参客户端。
func TestWithChatResponseUnwrapInstallsOnce(t *testing.T) {
	// original 是调用方传入的客户端。
	original := &http.Client{}
	// wrapped 是安装归一后的副本。
	wrapped := withChatResponseUnwrap(original)
	// ok 表示副本的传输是否为归一传输。
	if _, ok := wrapped.Transport.(*unwrapChatResponseTransport); !ok {
		t.Fatalf("未安装归一传输: %T", wrapped.Transport)
	}
	if original.Transport != nil {
		t.Fatal("不得修改入参客户端的传输")
	}
	// twice 是对已安装的客户端再次安装的结果。
	twice := withChatResponseUnwrap(wrapped)
	// ok 表示重复安装后仍然是归一传输。
	if _, ok := twice.Transport.(*unwrapChatResponseTransport); !ok {
		t.Fatalf("重复安装后类型异常: %T", twice.Transport)
	}
	// inner 是归一传输内部被包装的传输，重复安装时不得出现嵌套归一。
	inner := twice.Transport.(*unwrapChatResponseTransport).base
	// nested 表示内层又出现一层归一传输，即发生了重复安装。
	if _, nested := inner.(*unwrapChatResponseTransport); nested {
		t.Fatal("不得重复嵌套归一传输")
	}
}

// TestWithChatResponseUnwrapDefaultsTransport 验证入参客户端没有传输时回退到默认传输，而不是空指针。
func TestWithChatResponseUnwrapDefaultsTransport(t *testing.T) {
	// wrapped 是安装归一后的副本。
	wrapped := withChatResponseUnwrap(&http.Client{})
	// transport 是归一传输。
	transport, ok := wrapped.Transport.(*unwrapChatResponseTransport)
	if !ok {
		t.Fatalf("未安装归一传输: %T", wrapped.Transport)
	}
	if transport.base == nil {
		t.Fatal("缺少传输时必须回退到默认传输")
	}
}

// TestWrappedGatewayEndToEndWithSDK 验证经归一传输后，真实 OpenAI SDK 能解析被 data 包裹的响应。
func TestWrappedGatewayEndToEndWithSDK(t *testing.T) {
	// server 是模拟被包装响应的自建网关。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"choices":[{"index":0,"message":{"role":"assistant","content":"网关包装后的回复"},"finish_reason":"stop"}],"id":"gen-1"},"extra":1}`))
	}))
	defer server.Close()

	// client 是在 SDK 之上安装了归一的客户端。
	client := withChatResponseUnwrap(server.Client())
	// sdk 是复用引擎同一 SDK 的客户端。
	sdk := openai.NewClientWithConfig(openai.ClientConfig{
		BaseURL: server.URL + "/v1", HTTPClient: client,
	})
	// response 是 SDK 解析后的响应。
	response, err := sdk.CreateChatCompletion(context.Background(), openai.ChatCompletionRequest{
		Model:    "test-model",
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "你好"}},
	})
	if err != nil {
		t.Fatalf("SDK 解析失败: %v", err)
	}
	if len(response.Choices) != 1 || response.Choices[0].Message.Content != "网关包装后的回复" {
		t.Fatalf("SDK 未取得正确回复: %+v", response.Choices)
	}
}

// mustRequest 构造一个最小请求，仅用于触发传输测试。
func mustRequest(t *testing.T) *http.Request {
	t.Helper()
	// request、err 是构造出的请求与失败原因。
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	return request
}
