package engine

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"xianyu-go/internal/aicompat"
)

// aiChatResponseLimit 是传输层允许读取的 AI 响应上限；超过后按已读取内容处理，避免恶意大响应耗尽内存。
const aiChatResponseLimit = 8 << 20

// unwrapChatResponseTransport 在传输层对 OpenAI 兼容网关的 JSON 响应做形状归一。
//
// AI 智能回复使用第三方 SDK 解析响应，SDK 只认顶层 choices；而部分自建网关会把整个响应
// 包进 data 字段。把归一放在传输层，SDK 无需改动即可获得标准形状。
// 该类型无状态且只读，可被多个请求并发使用。
type unwrapChatResponseTransport struct {
	// base 是被包装的原始传输实现；构造时保证非空。
	base http.RoundTripper
}

// RoundTrip 完成一次请求，并在响应为 JSON 时把被 data 包裹的响应归一为标准形状。
func (t *unwrapChatResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// response、err 是原始传输的响应与错误。
	response, err := t.base.RoundTrip(request)
	if err != nil || response == nil {
		return response, err
	}
	// 只处理 JSON 响应；其它类型（含流式响应）原样返回，避免改变行为。
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "json") {
		return response, nil
	}
	// raw 是受大小限制的响应正文。
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, aiChatResponseLimit))
	_ = response.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("读取 AI 响应失败: %w", readErr)
	}
	// normalized 是形状归一后的正文。
	normalized := aicompat.UnwrapChatResponseEnvelope(raw)
	response.Body = io.NopCloser(bytes.NewReader(normalized))
	response.ContentLength = int64(len(normalized))
	response.Header.Set("Content-Length", strconv.Itoa(len(normalized)))
	return response, nil
}

// withChatResponseUnwrap 返回安装了响应归一传输的客户端副本。
// 返回副本而不修改入参，避免影响 netguard 可能共享或缓存的原客户端。
func withChatResponseUnwrap(client *http.Client) *http.Client {
	// cloned 是待返回的客户端副本。
	cloned := *client
	// base 是原始传输；为空时按 Go 语义回退到默认传输，保证包装后仍可正常工作。
	base := cloned.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if // already 表示原始传输已是归一传输，避免重复嵌套。
	_, already := base.(*unwrapChatResponseTransport); !already {
		cloned.Transport = &unwrapChatResponseTransport{base: base}
	}
	return &cloned
}
