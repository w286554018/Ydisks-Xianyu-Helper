package localfetch

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"xianyu-go/internal/browser"
)

// 助手对外接口与安全相关的固定参数。
const (
	// DefaultListen 是助手默认监听地址；只绑回环，绝不监听公网地址。
	DefaultListen = "127.0.0.1:17901"
	// DefaultDebugPort 是助手为专用 profile Chrome 使用的调试端口。
	DefaultDebugPort = 9222
	// DefaultChromeProfileDirName 是专用 profile 目录名（位于用户主目录下）。
	DefaultChromeProfileDirName = "Library/Application Support/ydisks-fetch-chrome"
	// DefaultTokenFileName 是共享令牌文件名（位于用户主目录下的隐藏目录）。
	DefaultTokenFileName = ".ydisks-fetch/token"
	// DefaultMacChromePath 是 macOS 上 Chrome 的默认可执行文件路径。
	DefaultMacChromePath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	// tokenHeaderName 是共享令牌请求头。
	tokenHeaderName = "X-Auth-Token"
	// requestTimeout 是单次取详情请求的超时上限。
	requestTimeout = 150 * time.Second
)

// itemIDPattern 限定 item_id 只能是纯数字：助手不接受任意 URL，避免成为通用代理。
var itemIDPattern = regexp.MustCompile(`^[0-9]{1,32}$`)

// Config 是本机抓取助手的运行配置。
type Config struct {
	// Listen 是监听地址；必须显式使用回环地址。
	Listen string
	// ChromePath 是 Chrome 可执行文件路径。
	ChromePath string
	// ProfileDir 是专用浏览器用户数据目录。
	ProfileDir string
	// DebugPort 是 Chrome 调试端口。
	DebugPort int
	// TokenFile 是共享令牌文件路径（内容为十六进制随机串，权限 0600）。
	TokenFile string
	// Logger 是日志器；为空时使用默认日志器。
	Logger *slog.Logger
}

// FetchDetailRequest 是取详情请求体。
type FetchDetailRequest struct {
	// ItemID 是闲鱼商品标识，只接受纯数字。
	ItemID string `json:"item_id"`
}

// FetchDetailResponse 是取详情响应体；字段与服务器侧详情结构保持一致。
type FetchDetailResponse struct {
	// ItemID 是商品标识。
	ItemID string `json:"item_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// Price 是价格文本。
	Price string `json:"price"`
	// DescText 是商品描述纯文本。
	DescText string `json:"desc_text"`
	// Images 是商品图片地址列表。
	Images []string `json:"images"`
	// SellerNickname 是卖家昵称。
	SellerNickname string `json:"seller_nickname"`
	// SellerID 是卖家标识。
	SellerID string `json:"seller_id"`
	// BrowseCnt 是商品浏览量。
	BrowseCnt int64 `json:"browse_cnt"`
	// WantCnt 是商品想要人数。
	WantCnt int64 `json:"want_cnt"`
	// CollectCnt 是商品收藏数。
	CollectCnt int64 `json:"collect_cnt"`
	// SoldCnt 是商品已售数。
	SoldCnt int64 `json:"sold_cnt"`
}

// ErrorResponse 是助手统一错误响应；message 为可操作中文提示，不含任何凭据。
type ErrorResponse struct {
	// Code 是稳定错误码，供服务器映射。
	Code string `json:"code"`
	// Message 是中文提示。
	Message string `json:"message"`
}

// HealthResponse 是健康检查响应。
type HealthResponse struct {
	// Status 固定为 ok。
	Status string `json:"status"`
	// ChromeConnected 表示调试端口当前是否可用。
	ChromeConnected bool `json:"chrome_connected"`
}

// Agent 是本机抓取助手：对外提供回环 HTTP 服务，对内驱动真实 Chrome 取数。
type Agent struct {
	// config 是运行配置，构造后只读。
	config Config
	// token 是共享令牌明文；只用于恒定时间比较，绝不写日志或响应。
	token string
	// chrome 是 Chrome 调试端口客户端。
	chrome *ChromeClient
	// logger 是日志器。
	logger *slog.Logger
	// fetch 是取详情实现，默认指向 FetchDetail，测试可替换。
	fetch func(ctx context.Context, itemID string) (browser.CompetitorDetail, error)
}

// NewAgent 构造助手；令牌文件缺失或不可读时直接失败，避免无鉴权运行。
func NewAgent(cfg Config) (*Agent, error) {
	// logger 是实际使用的日志器。
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// tokenPath 是生效的令牌文件路径，为空时取用户主目录下的默认位置。
	tokenPath := strings.TrimSpace(cfg.TokenFile)
	if tokenPath == "" {
		// home、homeErr 是用户主目录与读取失败原因。
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return nil, fmt.Errorf("读取用户主目录失败: %w", homeErr)
		}
		tokenPath = home + "/" + DefaultTokenFileName
	}
	// rawToken 是令牌文件原始内容；readErr 表示读取失败。
	rawToken, readErr := os.ReadFile(tokenPath)
	if readErr != nil {
		return nil, fmt.Errorf("读取令牌文件失败（%s）；可执行: openssl rand -hex 32 > %s && chmod 600 %s", tokenPath, tokenPath, tokenPath)
	}
	// token 是去除空白后的令牌明文。
	token := strings.TrimSpace(string(rawToken))
	if token == "" {
		return nil, fmt.Errorf("令牌文件为空: %s", tokenPath)
	}
	// agent 是本助手实例。
	agent := &Agent{
		config: cfg,
		token:  token,
		logger: logger,
		chrome: NewChromeClient(cfg.DebugPort, cfg.ChromePath, cfg.ProfileDir, logger),
	}
	agent.fetch = agent.FetchDetail
	return agent, nil
}

// Handler 返回助手的 HTTP 处理器。
func (a *Agent) Handler() http.Handler {
	// mux 是助手的路由表。
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/fetch-detail", a.handleFetchDetail)
	return mux
}

// handleHealth 处理健康检查；不需要令牌，便于本机自检。
func (a *Agent) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAgentError(w, http.StatusMethodNotAllowed, "method_not_allowed", "只支持 GET")
		return
	}
	// 健康检查只暴露调试端口是否可用，不暴露任何凭据或登录信息。
	writeAgentJSON(w, http.StatusOK, HealthResponse{Status: "ok", ChromeConnected: a.chrome.debugReady(r.Context())})
}

// handleFetchDetail 处理取详情请求：先校验令牌，再校验 item_id，最后走取数实现。
func (a *Agent) handleFetchDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAgentError(w, http.StatusMethodNotAllowed, "method_not_allowed", "只支持 POST")
		return
	}
	if !a.authorized(r) {
		a.logger.Warn("助手请求令牌校验失败", "remote", r.RemoteAddr)
		writeAgentError(w, http.StatusUnauthorized, "unauthorized", "令牌缺失或错误")
		return
	}
	// request 是解析后的请求体。
	var request FetchDetailRequest
	if // decodeErr 是请求体解析失败原因。
	decodeErr := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); decodeErr != nil {
		writeAgentError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	// itemID 是去除空白后的商品标识。
	itemID := strings.TrimSpace(request.ItemID)
	if !itemIDPattern.MatchString(itemID) {
		// 只接受纯数字，拒绝 URL、路径与任何其它输入。
		writeAgentError(w, http.StatusBadRequest, "bad_item_id", "item_id 必须是纯数字商品 ID")
		return
	}

	// fetchCtx 是取数上下文，带整体超时。
	fetchCtx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	// start 用于计算取数耗时。
	start := time.Now()
	// detail 是取数结果；fetchErr 是失败原因。
	detail, fetchErr := a.fetch(fetchCtx, itemID)
	if fetchErr != nil {
		// status、code 是把内部错误映射成对外语义后的结果。
		status, code := agentErrorStatus(fetchErr)
		a.logger.Warn("助手取详情失败", "item_id", itemID, "code", code, "elapsed_ms", time.Since(start).Milliseconds())
		writeAgentError(w, status, code, fetchErr.Error())
		return
	}
	a.logger.Info("助手取详情成功", "item_id", itemID, "elapsed_ms", time.Since(start).Milliseconds())
	writeAgentJSON(w, http.StatusOK, FetchDetailResponse{
		ItemID:         detail.ItemID,
		Title:          detail.Title,
		Price:          detail.Price,
		DescText:       detail.DescText,
		Images:         detail.Images,
		SellerNickname: detail.SellerNickname,
		SellerID:       detail.SellerID,
		BrowseCnt:      detail.BrowseCount,
		WantCnt:        detail.WantCount,
		CollectCnt:     detail.CollectCount,
		SoldCnt:        detail.SoldCount,
	})
}

// authorized 用恒定时间比较校验共享令牌。
func (a *Agent) authorized(r *http.Request) bool {
	// provided 是请求携带的令牌。
	provided := strings.TrimSpace(r.Header.Get(tokenHeaderName))
	if provided == "" || a.token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(a.token)) == 1
}

// agentErrorStatus 把内部错误映射为对外 HTTP 状态与稳定错误码。
func agentErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, ErrBadItemID):
		return http.StatusBadRequest, "bad_item_id"
	case errors.Is(err, ErrNotLoggedIn):
		return http.StatusConflict, "not_logged_in"
	case errors.Is(err, ErrRiskControl):
		return http.StatusConflict, "risk_control"
	case errors.Is(err, ErrChromeUnavailable):
		return http.StatusServiceUnavailable, "chrome_unavailable"
	case errors.Is(err, ErrDetailUnavailable):
		return http.StatusBadGateway, "detail_unavailable"
	default:
		return http.StatusBadGateway, "fetch_failed"
	}
}

// writeAgentJSON 写出 JSON 响应。
func writeAgentJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeAgentError 写出统一错误响应。
func writeAgentError(w http.ResponseWriter, status int, code, message string) {
	writeAgentJSON(w, status, ErrorResponse{Code: code, Message: message})
}
