// Package localfetch 实现「本机抓取助手」：在用户本机的真实 Chrome 中打开闲鱼商品页，
// 读取页面自发请求的详情接口响应，供服务器经回环 HTTP 调用。
//
// 该包只负责本机浏览器侧的取数与 HTTP 服务，不包含任何平台请求构造：
// 详情数据一律来自真实页面自己发出的请求。
package localfetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// 抓取相关的固定参数。
const (
	// detailAPIMarker 是详情接口的特征串，用于从页面所有响应中识别目标请求。
	detailAPIMarker = "mtop.taobao.idle.pc.detail"
	// detailItemURLTemplate 是本机自己拼接的商品页地址；接口只接受 item_id，不接受任意 URL。
	detailItemURLTemplate = "https://www.goofish.com/item?id=%s"
	// chromeReadyTimeout 是等待 Chrome 调试端口就绪的上限。
	chromeReadyTimeout = 25 * time.Second
	// cdpePollInterval 是轮询调试端口的间隔。
	cdpePollInterval = 500 * time.Millisecond
	// tabCloseTimeout 是关闭临时标签页的上限。
	tabCloseTimeout = 5 * time.Second
)

// chromeTab 是调试端口返回的标签页描述。
type chromeTab struct {
	// ID 是标签页标识，用于结束时关闭。
	ID string `json:"id"`
	// WebSocketDebuggerURL 是该标签页的 CDP 会话地址。
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// ChromeClient 管理本机 Chrome 的调试端口会话：必要时拉起浏览器，并按需打开临时标签页取数。
// 该类型可并发使用；debugBase 与 httpClient 在构造后只读。
type ChromeClient struct {
	// debugPort 是 Chrome 远程调试端口。
	debugPort int
	// chromePath 是 Chrome 可执行文件路径。
	chromePath string
	// profileDir 是专用用户数据目录，与用户日常浏览器隔离。
	profileDir string
	// baseURL 是调试端口的 HTTP 基地址，构造后只读。
	baseURL string
	// httpClient 是与调试端口通信的客户端，构造后只读。
	httpClient *http.Client
	// logger 是本机助手日志器。
	logger *slog.Logger
}

// NewChromeClient 构造 Chrome 客户端；logger 为空时使用默认日志器。
func NewChromeClient(debugPort int, chromePath, profileDir string, logger *slog.Logger) *ChromeClient {
	// effectiveLogger 是实际使用的日志器。
	effectiveLogger := logger
	if effectiveLogger == nil {
		effectiveLogger = slog.Default()
	}
	return &ChromeClient{
		debugPort:  debugPort,
		chromePath: chromePath,
		profileDir: profileDir,
		baseURL:    fmt.Sprintf("http://127.0.0.1:%d", debugPort),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     effectiveLogger,
	}
}

// EnsureRunning 确保调试端口可用：已就绪直接返回，否则用专用 profile 拉起 Chrome 并等待就绪。
func (c *ChromeClient) EnsureRunning(ctx context.Context) error {
	if c.debugReady(ctx) {
		return nil
	}
	if strings.TrimSpace(c.chromePath) == "" {
		return fmt.Errorf("%w: 未配置 Chrome 可执行文件路径", ErrChromeUnavailable)
	}
	if // statErr 是 Chrome 可执行文件探测失败原因。
	_, statErr := os.Stat(c.chromePath); statErr != nil {
		return fmt.Errorf("%w: 找不到 Chrome 可执行文件 %s", ErrChromeUnavailable, c.chromePath)
	}
	if // mkErr 是专用 profile 目录创建失败原因。
	mkErr := os.MkdirAll(c.profileDir, 0o700); mkErr != nil {
		return fmt.Errorf("%w: 创建专用 profile 目录失败: %v", ErrChromeUnavailable, mkErr)
	}
	// cmd 即以专用 profile 与调试端口启动的 Chrome 进程。
	cmd := exec.Command(c.chromePath, c.chromeArgs()...)
	// logFile 收集 Chrome 自身的标准输出，便于排查启动失败。
	logFile, logErr := os.OpenFile(filepath.Join(c.profileDir, "chrome-launch.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if logErr == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if // startErr 是 Chrome 进程启动失败原因。
	startErr := cmd.Start(); startErr != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return fmt.Errorf("%w: 启动 Chrome 失败: %v", ErrChromeUnavailable, startErr)
	}
	// 进程交由操作系统托管，本进程退出后 Chrome 继续常驻；因此只回收句柄，不等待退出。
	go func() { _ = cmd.Wait() }()
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
	}
	c.logger.Info("已用专用 profile 启动 Chrome", "debug_port", c.debugPort, "profile_dir", c.profileDir)

	// deadline 是等待调试端口就绪的最后时刻。
	deadline := time.Now().Add(chromeReadyTimeout)
	for time.Now().Before(deadline) {
		if c.debugReady(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: 等待 Chrome 调试端口被取消", ErrChromeUnavailable)
		case <-time.After(cdpePollInterval):
		}
	}
	return fmt.Errorf("%w: 等待 Chrome 调试端口超时（%s）", ErrChromeUnavailable, chromeReadyTimeout)
}

// chromeArgs 返回以专用 profile 启动 Chrome 所需的参数；不注入任何自动化开关。
func (c *ChromeClient) chromeArgs() []string {
	return []string{
		"--user-data-dir=" + c.profileDir,
		fmt.Sprintf("--remote-debugging-port=%d", c.debugPort),
		"--no-first-run",
		"--no-default-browser-check",
		"--restore-last-session=false",
		"about:blank",
	}
}

// debugReady 探测调试端口是否可用。
func (c *ChromeClient) debugReady(ctx context.Context) bool {
	// req 是探测请求；err 表示请求构造失败。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/json/version", nil)
	if err != nil {
		return false
	}
	// resp 是调试端口响应。
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// openTab 打开一个新标签页并返回其 CDP 会话地址；Chrome 111+ 要求使用 PUT。
func (c *ChromeClient) openTab(ctx context.Context, targetURL string) (chromeTab, error) {
	// endpoint 是新建标签页的调试端点，目标地址整体转义后作为查询串。
	endpoint := c.baseURL + "/json/new?" + url.QueryEscape(targetURL)
	// req 是新建标签页请求；err 表示请求构造失败。
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return chromeTab{}, fmt.Errorf("%w: 构造新建标签页请求失败: %v", ErrChromeUnavailable, err)
	}
	// resp 是新建标签页响应。
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return chromeTab{}, fmt.Errorf("%w: 打开标签页失败: %v", ErrChromeUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return chromeTab{}, fmt.Errorf("%w: 打开标签页返回状态 %d", ErrChromeUnavailable, resp.StatusCode)
	}
	// tab 是解析后的标签页描述。
	var tab chromeTab
	if // decodeErr 是标签页描述解析失败原因。
	decodeErr := json.NewDecoder(resp.Body).Decode(&tab); decodeErr != nil {
		return chromeTab{}, fmt.Errorf("%w: 解析标签页描述失败: %v", ErrChromeUnavailable, decodeErr)
	}
	if strings.TrimSpace(tab.WebSocketDebuggerURL) == "" {
		return chromeTab{}, fmt.Errorf("%w: 标签页缺少 CDP 会话地址", ErrChromeUnavailable)
	}
	return tab, nil
}

// closeTab 关闭不再需要的标签页；关闭失败只记录日志，不影响取数结果。
func (c *ChromeClient) closeTab(ctx context.Context, tabID string) {
	if strings.TrimSpace(tabID) == "" {
		return
	}
	// closeCtx 是关闭标签页使用的独立上下文，避免调用方上下文已取消时无法回收。
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tabCloseTimeout)
	defer cancel()
	// req 是关闭标签页请求；err 表示请求构造失败。
	req, err := http.NewRequestWithContext(closeCtx, http.MethodGet, c.baseURL+"/json/close/"+url.PathEscape(tabID), nil)
	if err != nil {
		c.logger.Warn("关闭标签页请求构造失败", "err", err)
		return
	}
	// resp 是关闭响应。
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Warn("关闭标签页失败", "err", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
}

// cdpMessage 是一条 CDP 消息：带 id 的是命令回执，带 method 的是事件通知。
type cdpMessage struct {
	// ID 是命令序号；事件通知为 0。
	ID int64 `json:"id"`
	// Method 是事件名；命令回执为空。
	Method string `json:"method"`
	// Params 是事件参数。
	Params json.RawMessage `json:"params"`
	// Result 是命令结果。
	Result json.RawMessage `json:"result"`
	// Error 是命令错误。
	Error *struct {
		// Code 是错误码。
		Code int `json:"code"`
		// Message 是错误描述。
		Message string `json:"message"`
	} `json:"error"`
}

// cdpSession 是与单个标签页的 CDP 会话；命令回执与事件在同一读取协程中分发。
type cdpSession struct {
	// conn 是底层 WebSocket 连接。
	conn *websocket.Conn
	// mu 保护 seq 与 pending。
	mu sync.Mutex
	// seq 是自增命令序号。
	seq int64
	// pending 保存等待回执的命令通道。
	pending map[int64]chan cdpMessage
	// handlerMu 保护 handlers。
	handlerMu sync.Mutex
	// handlers 是事件回调列表，按注册顺序调用。
	handlers []func(cdpMessage)
	// readErr 保存读取协程结束时的错误。
	readErr error
	// readDone 在读取协程退出时关闭。
	readDone chan struct{}
}

// dialCDPSession 与标签页建立 CDP 会话并启动读取协程。
func dialCDPSession(ctx context.Context, wsURL string) (*cdpSession, error) {
	// conn 是 WebSocket 连接；err 表示连接失败。
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: 连接 CDP 失败: %v", ErrChromeUnavailable, err)
	}
	conn.SetReadLimit(64 << 20)
	// session 是已建立的双向会话。
	session := &cdpSession{
		conn:     conn,
		pending:  make(map[int64]chan cdpMessage),
		readDone: make(chan struct{}),
	}
	go session.readLoop()
	return session, nil
}

// onEvent 注册事件回调；可在命令发送前调用。
func (s *cdpSession) onEvent(handler func(cdpMessage)) {
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	s.handlers = append(s.handlers, handler)
}

// readLoop 持续读取 CDP 消息并分发：命令回执唤醒等待方，事件交给回调。
func (s *cdpSession) readLoop() {
	defer close(s.readDone)
	for {
		// messageType 是消息类型；payload 是消息正文。
		messageType, payload, err := s.conn.Read(context.Background())
		if err != nil {
			s.mu.Lock()
			s.readErr = err
			// 通道读取方已无法再收到回执，统一关闭以唤醒全部等待者。
			for id, ch := range s.pending {
				close(ch)
				delete(s.pending, id)
			}
			s.mu.Unlock()
			return
		}
		if messageType != websocket.MessageText {
			continue
		}
		// msg 是解析后的 CDP 消息。
		var msg cdpMessage
		if json.Unmarshal(payload, &msg) != nil {
			continue
		}
		if msg.ID != 0 {
			s.mu.Lock()
			// ch 是等待该命令回执的通道。
			ch, ok := s.pending[msg.ID]
			if ok {
				delete(s.pending, msg.ID)
			}
			s.mu.Unlock()
			if ok {
				ch <- msg
				close(ch)
			}
			continue
		}
		s.handlerMu.Lock()
		// handlers 是回调副本，避免回调内再注册造成死锁。
		handlers := make([]func(cdpMessage), len(s.handlers))
		copy(handlers, s.handlers)
		s.handlerMu.Unlock()
		// handler 是本轮要调用的回调。
		for _, handler := range handlers {
			handler(msg)
		}
	}
}

// call 发送 CDP 命令并等待回执，结果写入 result（可为 nil）。
func (s *cdpSession) call(ctx context.Context, method string, params any, result any) error {
	s.mu.Lock()
	if s.readErr != nil {
		// readErr 是读取协程已终止的原因。
		readErr := s.readErr
		s.mu.Unlock()
		return fmt.Errorf("%w: CDP 会话已断开: %v", ErrChromeUnavailable, readErr)
	}
	s.seq++
	// id 是本次命令序号。
	id := s.seq
	// ch 用于接收本次命令回执。
	ch := make(chan cdpMessage, 1)
	s.pending[id] = ch
	s.mu.Unlock()

	// payload 是编码后的命令正文。
	payload, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		s.discardPending(id)
		return fmt.Errorf("编码 CDP 命令失败: %v", err)
	}
	if // writeErr 是 CDP 命令写入失败原因。
	writeErr := s.conn.Write(ctx, websocket.MessageText, payload); writeErr != nil {
		s.discardPending(id)
		return fmt.Errorf("%w: 发送 CDP 命令失败: %v", ErrChromeUnavailable, writeErr)
	}
	select {
	case <-ctx.Done():
		s.discardPending(id)
		return ctx.Err()
	// msg、ok 是收到的命令回执与该通道是否仍打开。
	case msg, ok := <-ch:
		if !ok {
			return fmt.Errorf("%w: CDP 会话在等待回执时关闭", ErrChromeUnavailable)
		}
		if msg.Error != nil {
			return fmt.Errorf("CDP 命令 %s 失败: %s", method, msg.Error.Message)
		}
		if result != nil && len(msg.Result) > 0 {
			if // decodeErr 是 CDP 命令结果解析失败原因。
			decodeErr := json.Unmarshal(msg.Result, result); decodeErr != nil {
				return fmt.Errorf("解析 CDP 命令结果失败: %v", decodeErr)
			}
		}
		return nil
	}
}

// discardPending 移除等待中的命令登记，避免会话在错误路径上泄漏通道。
func (s *cdpSession) discardPending(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, id)
}

// Close 关闭会话。
func (s *cdpSession) Close() {
	_ = s.conn.Close(websocket.StatusNormalClosure, "")
	select {
	case <-s.readDone:
	case <-time.After(2 * time.Second):
	}
}
