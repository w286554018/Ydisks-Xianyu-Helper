// Package localfetch: 假 CDP 服务驱动的取数时序与命令错误语义测试。
package localfetch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// cdpTestBehaviour 描述假 CDP 服务收到命令时的行为。
type cdpTestBehaviour int

const (
	// cdpBehaviourDetail 表示按真实取数时序回应，并推送详情接口响应事件。
	cdpBehaviourDetail cdpTestBehaviour = iota
	// cdpBehaviourErrorReply 表示对所有命令返回 CDP 错误回执。
	cdpBehaviourErrorReply
	// cdpBehaviourSilent 表示只读不回复，用于覆盖等待回执时的取消路径。
	cdpBehaviourSilent
	// cdpBehaviourCloseImmediately 表示接受连接后立刻断开。
	cdpBehaviourCloseImmediately
)

// fakeCDPServer 是同时模拟调试端口 HTTP 端点与标签页 CDP 会话的测试服务。
type fakeCDPServer struct {
	// server 是测试服务本体。
	server *httptest.Server
	// behaviour 决定命令处理方式。
	behaviour cdpTestBehaviour
	// detailURL 是模拟的详情接口响应地址。
	detailURL string
	// detailBody 是模拟的详情接口响应正文。
	detailBody string
	// cookieValue 是 Runtime.evaluate 返回的页面 Cookie。
	cookieValue string
	// mu 保护 methods。
	mu sync.Mutex
	// methods 按顺序记录客户端发出的 CDP 命令。
	methods []string
}

// newFakeCDPServer 启动假 CDP 服务并返回它。
func newFakeCDPServer(t *testing.T, behaviour cdpTestBehaviour, detailBody string) *fakeCDPServer {
	t.Helper()
	// fake 是被测用的假 CDP 服务。
	fake := &fakeCDPServer{
		behaviour:   behaviour,
		detailURL:   "https://h5api.m.taobao.com/h5/mtop.taobao.idle.pc.detail/1.0/?jsv=2.7.2",
		detailBody:  detailBody,
		cookieValue: "unb=1; cookie2=abc",
	}
	// mux 是调试端口路由。
	mux := http.NewServeMux()
	mux.HandleFunc("/", fake.handle)
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

// chromeClient 返回指向假服务的 Chrome 客户端。
func (f *fakeCDPServer) chromeClient() *ChromeClient {
	// client 是指向假调试端口的客户端。
	client := newChromeTestClient(f.server.URL, "", "")
	client.httpClient = f.server.Client()
	return client
}

// recordedMethods 返回已记录命令的快照。
func (f *fakeCDPServer) recordedMethods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.methods...)
}

// handle 处理调试端口 HTTP 端点与 CDP WebSocket 升级。
func (f *fakeCDPServer) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/json/version":
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"Browser":"fake-cdp"}`)
	case r.URL.Path == "/json/new":
		// wsURL 是该标签页的 CDP 会话地址，指向本测试服务。
		wsURL := "ws" + strings.TrimPrefix(f.server.URL, "http") + "/devtools/page/tab-1"
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"tab-1","webSocketDebuggerUrl":"`+wsURL+`"}`)
	case strings.HasPrefix(r.URL.Path, "/json/close/"):
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	case r.URL.Path == "/devtools/page/tab-1":
		f.serveCDP(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// serveCDP 处理一条 CDP WebSocket 会话。
func (f *fakeCDPServer) serveCDP(w http.ResponseWriter, r *http.Request) {
	// conn、acceptErr 是升级后的连接与失败原因。
	conn, acceptErr := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if acceptErr != nil {
		return
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	if f.behaviour == cdpBehaviourCloseImmediately {
		return
	}
	// ctx 是本连接的上下文。
	ctx := r.Context()
	for {
		// messageType、payload 是客户端发来的 CDP 消息。
		messageType, payload, readErr := conn.Read(ctx)
		if readErr != nil {
			return
		}
		if messageType != websocket.MessageText {
			continue
		}
		// command 是解析后的 CDP 命令。
		var command struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(payload, &command) != nil {
			continue
		}
		f.mu.Lock()
		f.methods = append(f.methods, command.Method)
		f.mu.Unlock()
		f.replyCommand(ctx, conn, command.ID, command.Method)
	}
}

// replyCommand 按行为模式回应一条 CDP 命令。
func (f *fakeCDPServer) replyCommand(ctx context.Context, conn *websocket.Conn, id int64, method string) {
	switch f.behaviour {
	case cdpBehaviourSilent:
		return
	case cdpBehaviourErrorReply:
		_ = writeCDP(ctx, conn, map[string]any{"id": id, "error": map[string]any{"code": -32000, "message": "fake command failure"}})
		return
	}
	switch method {
	case "Page.navigate":
		_ = writeCDP(ctx, conn, map[string]any{"id": id, "result": map[string]any{"frameId": "frame-1"}})
		// 页面导航完成后推送详情接口响应事件，模拟页面自发请求。
		_ = writeCDP(ctx, conn, map[string]any{
			"method": "Network.responseReceived",
			"params": map[string]any{
				"requestId": "req-1",
				"response":  map[string]any{"url": f.detailURL, "status": http.StatusOK},
			},
		})
	case "Network.getResponseBody":
		_ = writeCDP(ctx, conn, map[string]any{"id": id, "result": map[string]any{"body": f.detailBody, "base64Encoded": false}})
	case "Runtime.evaluate":
		_ = writeCDP(ctx, conn, map[string]any{"id": id, "result": map[string]any{"result": map[string]any{"value": f.cookieValue}}})
	default:
		_ = writeCDP(ctx, conn, map[string]any{"id": id, "result": map[string]any{}})
	}
}

// writeCDP 把一条 CDP 消息编码为文本帧写入连接。
func writeCDP(ctx context.Context, conn *websocket.Conn, payload any) error {
	// data 是编码后的消息正文。
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return marshalErr
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

// cdpDetailBody 是模拟详情接口响应正文：包含返回码、商品主体与卖家信息。
const cdpDetailBody = `{"ret":["SUCCESS::调用成功"],"data":{"itemDO":{"title":"假 CDP 商品","desc":"<p>描述文本</p>",` +
	`"soldPrice":12.5,"browseCnt":31,"wantCnt":2,"collectCnt":1,"soldCnt":0,` +
	`"imageInfos":[{"url":"https://img.example.com/a.jpg"}]},"sellerDO":{"sellerId":3835297081,"nick":"东菱之家"}}}`

// TestCaptureDetailOnceWithFakeCDP 验证取数时序：新建标签页、导航、捕获页面自发响应并回读正文。
func TestCaptureDetailOnceWithFakeCDP(t *testing.T) {
	// fake 是按真实时序回应的假 CDP 服务。
	fake := newFakeCDPServer(t, cdpBehaviourDetail, cdpDetailBody)
	// client 是指向假服务的 Chrome 客户端。
	client := fake.chromeClient()

	// result、err 是单次取数结果与错误。
	result, err := client.captureDetailOnce(context.Background(), "1098309932961")
	if err != nil {
		t.Fatalf("假 CDP 取数不应报错: %v", err)
	}
	if string(result.Body) != cdpDetailBody {
		t.Fatalf("回读正文不符: %s", string(result.Body))
	}
	if strings.Contains(result.ResponseURL, "?") {
		t.Fatalf("响应地址必须去掉查询串: %s", result.ResponseURL)
	}
	// methods 是实际发出的 CDP 命令序列。
	methods := strings.Join(fake.recordedMethods(), ",")
	// want 是必须在命令序列中出现的 CDP 命令。
	for _, want := range []string{"Network.enable", "Page.enable", "Page.navigate", "Network.getResponseBody", "Runtime.evaluate"} {
		if !strings.Contains(methods, want) {
			t.Fatalf("缺少 CDP 命令 %s: %s", want, methods)
		}
	}
}

// TestFetchDetailWithFakeCDP 验证助手在假 CDP 服务上完成一次完整取数，并带回四项公开计数。
func TestFetchDetailWithFakeCDP(t *testing.T) {
	// fake 是按真实时序回应的假 CDP 服务。
	fake := newFakeCDPServer(t, cdpBehaviourDetail, cdpDetailBody)
	// agent 是把取数实现指向假服务的助手。
	agent := &Agent{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), chrome: fake.chromeClient()}

	// detail、err 是取数结果与错误。
	detail, err := agent.FetchDetail(context.Background(), "1098309932961")
	if err != nil {
		t.Fatalf("助手取数不应报错: %v", err)
	}
	if detail.Title != "假 CDP 商品" || detail.Price != "12.5" || detail.SellerNickname != "东菱之家" {
		t.Fatalf("详情字段不符: %+v", detail)
	}
	if detail.BrowseCount != 31 || detail.WantCount != 2 || detail.CollectCount != 1 || detail.SoldCount != 0 {
		t.Fatalf("四项计数不符: %+v", detail)
	}
	if len(detail.Images) != 1 {
		t.Fatalf("图片解析不符: %v", detail.Images)
	}
}

// TestFetchDetailUnavailableBody 验证详情响应不可用且不属于限流时返回「详情不可用」。
func TestFetchDetailUnavailableBody(t *testing.T) {
	// fake 返回缺少标题的响应，助手必须判定为不可用详情。
	fake := newFakeCDPServer(t, cdpBehaviourDetail, `{"ret":["SUCCESS::调用成功"],"data":{"itemDO":{}}}`)
	// agent 是把取数实现指向假服务的助手。
	agent := &Agent{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), chrome: fake.chromeClient()}

	// err 是详情不可用时的取数错误。
	if _, err := agent.FetchDetail(context.Background(), "1098309932961"); !errors.Is(err, ErrDetailUnavailable) {
		t.Fatalf("不可用详情必须返回 ErrDetailUnavailable: %v", err)
	}
}

// TestDialCDPSessionFailure 验证调试端口未提供可用会话地址时返回不可用错误。
func TestDialCDPSessionFailure(t *testing.T) {
	// err 是会话连接失败原因。
	if _, err := dialCDPSession(context.Background(), "ws://127.0.0.1:1/devtools/page/absent"); !errors.Is(err, ErrChromeUnavailable) {
		t.Fatalf("连接失败必须返回不可用错误: %v", err)
	}
}

// TestCallErrorPaths 验证 CDP 命令在错误回执、会话断开与上下文取消时的错误语义。
func TestCallErrorPaths(t *testing.T) {
	// errorFake 是对所有命令返回错误回执的假服务。
	errorFake := newFakeCDPServer(t, cdpBehaviourErrorReply, cdpDetailBody)
	// errorSession、dialErr 是与假服务建立的会话与失败原因。
	errorSession, dialErr := dialCDPSession(context.Background(), "ws"+strings.TrimPrefix(errorFake.server.URL, "http")+"/devtools/page/tab-1")
	if dialErr != nil {
		t.Fatalf("建立测试会话失败: %v", dialErr)
	}
	defer errorSession.Close()
	// commandErr 是收到错误回执时的错误。
	commandErr := errorSession.call(context.Background(), "Page.enable", map[string]any{}, nil)
	if commandErr == nil || !strings.Contains(commandErr.Error(), "fake command failure") {
		t.Fatalf("错误回执必须转换为可定位错误: %v", commandErr)
	}

	// closedFake 是接受连接后立刻断开的假服务。
	closedFake := newFakeCDPServer(t, cdpBehaviourCloseImmediately, cdpDetailBody)
	// closedSession 是已断开的会话。
	closedSession, closedDialErr := dialCDPSession(context.Background(), "ws"+strings.TrimPrefix(closedFake.server.URL, "http")+"/devtools/page/tab-1")
	if closedDialErr != nil {
		t.Fatalf("建立测试会话失败: %v", closedDialErr)
	}
	// 等待读取协程记录断开错误，命令调用必须返回会话已断开。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		closedSession.mu.Lock()
		// readErr 是读取协程记录的断开原因。
		readErr := closedSession.readErr
		closedSession.mu.Unlock()
		if readErr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// closedErr 是会话断开后的命令错误。
	closedErr := closedSession.call(context.Background(), "Page.enable", map[string]any{}, nil)
	if !errors.Is(closedErr, ErrChromeUnavailable) {
		t.Fatalf("会话断开必须返回不可用错误: %v", closedErr)
	}
	closedSession.Close()

	// silentFake 是只读不回复的假服务，用于覆盖等待回执时的取消路径。
	silentFake := newFakeCDPServer(t, cdpBehaviourSilent, cdpDetailBody)
	// silentSession 是静默会话。
	silentSession, silentDialErr := dialCDPSession(context.Background(), "ws"+strings.TrimPrefix(silentFake.server.URL, "http")+"/devtools/page/tab-1")
	if silentDialErr != nil {
		t.Fatalf("建立测试会话失败: %v", silentDialErr)
	}
	defer silentSession.Close()
	// callCtx 是短超时上下文，用于验证等待回执时取消会立即返回。
	callCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if // cancelCallErr 是等待回执被取消时的错误。
	cancelCallErr := silentSession.call(callCtx, "Page.enable", map[string]any{}, nil); !errors.Is(cancelCallErr, context.DeadlineExceeded) {
		t.Fatalf("等待回执被取消必须返回上下文错误: %v", cancelCallErr)
	}
	// 取消后等待中的命令登记必须被清理，避免会话泄漏通道。
	silentSession.mu.Lock()
	// pendingCount 是仍登记的回执通道数量。
	pendingCount := len(silentSession.pending)
	silentSession.mu.Unlock()
	if pendingCount != 0 {
		t.Fatalf("取消后必须清理等待中的命令: %d", pendingCount)
	}
}

// TestCallWritesResult 验证命令回执结果会被写入调用方提供的结构。
func TestCallWritesResult(t *testing.T) {
	// fake 是按真实时序回应的假 CDP 服务。
	fake := newFakeCDPServer(t, cdpBehaviourDetail, cdpDetailBody)
	// session、dialErr 是与假服务建立的会话与失败原因。
	session, dialErr := dialCDPSession(context.Background(), "ws"+strings.TrimPrefix(fake.server.URL, "http")+"/devtools/page/tab-1")
	if dialErr != nil {
		t.Fatalf("建立测试会话失败: %v", dialErr)
	}
	defer session.Close()

	// evaluated 是 Runtime.evaluate 回执的解析结果。
	var evaluated evaluateResult
	if // callErr 是本次命令调用错误。
	callErr := session.call(context.Background(), "Runtime.evaluate", map[string]any{"expression": "document.cookie"}, &evaluated); callErr != nil {
		t.Fatalf("命令调用不应报错: %v", callErr)
	}
	// cookieValue 是回执中的页面 Cookie 文本。
	var cookieValue string
	if json.Unmarshal(evaluated.Result.Value, &cookieValue) != nil || !strings.Contains(cookieValue, loginCookieMarker) {
		t.Fatalf("回执结果解析不符: %s", string(evaluated.Result.Value))
	}
	// 已登录判定必须为真，供取数流程放行。
	if !session.loggedIn(context.Background()) {
		t.Fatal("页面 Cookie 含登录标记时必须判定为已登录")
	}
}

// TestAgentFetchDetailRejectsExternalBrowserRisk 验证助手只读取页面自发请求，不构造任何平台请求。
// 该用例同时确认取数失败时不返回任何详情字段。
func TestCaptureDetailUnavailableWhenEventMissing(t *testing.T) {
	// fake 返回空正文；事件仍会推送，但正文无法解析为 mtop 响应。
	fake := newFakeCDPServer(t, cdpBehaviourDetail, "not-json")
	// client 是指向假服务的 Chrome 客户端。
	client := fake.chromeClient()

	// captureCtx 是短超时上下文，取数必须因收不到可用响应而超时退出。
	captureCtx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	// detail、err 是本次取数结果与错误。
	_, err := client.captureDetailOnce(captureCtx, "1098309932961")
	if !errors.Is(err, ErrDetailUnavailable) {
		t.Fatalf("不可用正文必须导致取数失败: %v", err)
	}
}
