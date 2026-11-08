package localfetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"time"

	"xianyu-go/internal/browser"
)

// 本机抓取助手对外的错误语义；服务器按这些错误映射 HTTP 状态与可操作提示。
var (
	// ErrChromeUnavailable 表示本机 Chrome 调试端口不可用（未安装、未启动或端口被占用）。
	ErrChromeUnavailable = errors.New("本机 Chrome 调试端口不可用")
	// ErrRiskControl 表示平台仍要求人机验证，需在助手浏览器中人工完成。
	ErrRiskControl = errors.New("闲鱼要求人机验证，请在助手浏览器中完成验证")
	// ErrNotLoggedIn 表示专用 profile 尚未登录闲鱼。
	ErrNotLoggedIn = errors.New("专用浏览器尚未登录闲鱼")
	// ErrBadItemID 表示 item_id 非法（只接受纯数字，不接受任意 URL）。
	ErrBadItemID = errors.New("item_id 非法")
	// ErrDetailUnavailable 表示页面未返回可用的详情数据。
	ErrDetailUnavailable = errors.New("闲鱼未返回可用的商品详情")
)

// 取数相关的固定参数。
const (
	// detailAttempts 是单次请求内的最大取数次数；平台限流（RGV587「被挤爆啦」）属短时限流，重试即可恢复。
	detailAttempts = 3
	// detailRetryBaseDelay 是重试的基础间隔。
	detailRetryBaseDelay = 1200 * time.Millisecond
	// detailRetryJitter 是重试间隔的随机抖动上限，避免固定节奏。
	detailRetryJitter = 1500 * time.Millisecond
	// detailCaptureTimeout 是单次取数的整体超时。
	detailCaptureTimeout = 45 * time.Second
	// riskControlRetMarker 是平台限流/风控返回码特征。
	riskControlRetMarker = "FAIL_SYS_USER_VALIDATE"
	// riskControlDetailMarker 是平台限流的详细描述特征。
	riskControlDetailMarker = "RGV587"
	// loginCookieMarker 是登录态 Cookie 特征（淘宝用户标识）。
	loginCookieMarker = "unb="
)

// captureResult 是单次标签页取数的原始结果。
type captureResult struct {
	// Body 是详情接口响应正文。
	Body []byte
	// ResponseURL 是响应地址（已去除 query，避免记录签名与验证参数）。
	ResponseURL string
	// PunishURL 是平台处罚验证地址；仅内存使用、不写日志。
	PunishURL string
}

// responseReceivedParams 是 CDP Network.responseReceived 事件的参数。
type responseReceivedParams struct {
	// RequestID 是请求标识，用于回读响应正文。
	RequestID string `json:"requestId"`
	// Response 是响应描述。
	Response struct {
		// URL 是响应地址。
		URL string `json:"url"`
		// Status 是 HTTP 状态码。
		Status int `json:"status"`
	} `json:"response"`
}

// getResponseBodyResult 是 CDP Network.getResponseBody 的结果。
type getResponseBodyResult struct {
	// Body 是响应正文。
	Body string `json:"body"`
	// Base64Encoded 表示正文是否为 base64 编码。
	Base64Encoded bool `json:"base64Encoded"`
}

// evaluateResult 是 CDP Runtime.evaluate 的结果。
type evaluateResult struct {
	// Result 是被求值表达式的返回值。
	Result struct {
		// Value 是返回值的字面量。
		Value json.RawMessage `json:"value"`
	} `json:"result"`
}

// captureDetailOnce 打开临时标签页导航到商品页，返回页面自发请求的详情接口响应正文。
// 只读取页面自己发出的请求，不构造任何平台请求。
func (c *ChromeClient) captureDetailOnce(ctx context.Context, itemID string) (captureResult, error) {
	// tab 是本次取数使用的临时标签页。
	tab, err := c.openTab(ctx, "about:blank")
	if err != nil {
		return captureResult{}, err
	}
	defer c.closeTab(ctx, tab.ID)

	// session 是与该标签页的 CDP 会话。
	session, err := dialCDPSession(ctx, tab.WebSocketDebuggerURL)
	if err != nil {
		return captureResult{}, err
	}
	defer session.Close()

	// results 汇总详情接口响应；容量大于 1 以容纳页面重复请求。
	results := make(chan captureResult, 4)
	session.onEvent(func(msg cdpMessage) {
		if msg.Method != "Network.responseReceived" {
			return
		}
		// params 是本事件的参数。
		var params responseReceivedParams
		if json.Unmarshal(msg.Params, &params) != nil {
			return
		}
		if !strings.Contains(params.Response.URL, detailAPIMarker) {
			return
		}
		// 平台处罚页面自身的地址同样包含详情接口特征串，必须先排除，
		// 否则会把验证页 HTML 当成接口响应。
		if strings.Contains(params.Response.URL, punishMarker) {
			return
		}
		go func() {
			// body 是回读到的响应正文。
			body, bodyErr := session.responseBody(ctx, params.RequestID)
			if bodyErr != nil {
				return
			}
			// 只接受形如 mtop 响应（含非空 ret 数组）的 JSON：
			// 页面还会发出其它 JSON（埋点、页面数据等），且地址中可能含同一特征串，必须排除。
			// envelope 是仅用于判定响应形态的最小结构。
			var envelope struct {
				// Ret 是接口返回码数组。
				Ret []string `json:"ret"`
			}
			if json.Unmarshal(body, &envelope) != nil || len(envelope.Ret) == 0 {
				return
			}
			// captured 是本条响应整理后的结果。
			captured := captureResult{Body: body, ResponseURL: sanitizeResponseURL(params.Response.URL)}
			c.logger.Info("捕获详情接口响应", "url", captured.ResponseURL,
				"body_len", len(body), "ret_count", len(envelope.Ret))
			captured.PunishURL = findPunishURL(body)
			select {
			case results <- captured:
			default:
			}
		}()
	})

	if // enableErr 是启用网络域失败原因。
	enableErr := session.call(ctx, "Network.enable", map[string]any{}, nil); enableErr != nil {
		return captureResult{}, enableErr
	}
	if // pageErr 是启用页面域失败原因。
	pageErr := session.call(ctx, "Page.enable", map[string]any{}, nil); pageErr != nil {
		return captureResult{}, pageErr
	}
	// itemURL 是本机按 item_id 拼接的商品页地址。
	itemURL := fmt.Sprintf(detailItemURLTemplate, itemID)
	if // navErr 是导航到商品页失败原因。
	navErr := session.call(ctx, "Page.navigate", map[string]any{"url": itemURL}, nil); navErr != nil {
		return captureResult{}, navErr
	}

	// captureCtx 是本次取数的超时上下文。
	captureCtx, cancel := context.WithTimeout(ctx, detailCaptureTimeout)
	defer cancel()
	for {
		select {
		case <-captureCtx.Done():
			return captureResult{}, fmt.Errorf("%w: 等待详情接口响应超时", ErrDetailUnavailable)
		// captured 是收到的详情接口响应。
		case captured := <-results:
			if len(captured.Body) == 0 {
				continue
			}
			if !session.loggedIn(captureCtx) {
				return captureResult{}, ErrNotLoggedIn
			}
			return captured, nil
		}
	}
}

// responseBody 回读指定请求的响应正文。
func (s *cdpSession) responseBody(ctx context.Context, requestID string) ([]byte, error) {
	// bodyCtx 是回读正文的超时上下文。
	bodyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// result 是回读结果。
	var result getResponseBodyResult
	if // err 是响应正文回读失败原因。
	err := s.call(bodyCtx, "Network.getResponseBody", map[string]any{"requestId": requestID}, &result); err != nil {
		return nil, err
	}
	if result.Base64Encoded {
		return nil, errors.New("响应正文为 base64 编码，本助手不支持")
	}
	return []byte(result.Body), nil
}

// loggedIn 判断专用 profile 是否已登录闲鱼；读取的是页面自身的 Cookie 视图。
func (s *cdpSession) loggedIn(ctx context.Context) bool {
	// cookieCtx 是读取 Cookie 的超时上下文。
	cookieCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// evaluated 是求值结果。
	var evaluated evaluateResult
	if // err 是读取页面 Cookie 失败原因。
	err := s.call(cookieCtx, "Runtime.evaluate",
		map[string]any{"expression": "document.cookie", "returnByValue": true}, &evaluated); err != nil {
		// 读取失败时不阻断取数：详情正文本身可判定风控与会话状态。
		return true
	}
	// cookieValue 是页面 Cookie 字符串。
	var cookieValue string
	if json.Unmarshal(evaluated.Result.Value, &cookieValue) != nil {
		return true
	}
	return strings.Contains(cookieValue, loginCookieMarker)
}

// sanitizeResponseURL 去掉响应地址的 query 与 fragment，避免把签名或验证参数写进日志。
func sanitizeResponseURL(raw string) string {
	// parsed、err 是解析后的地址与解析失败原因。
	parsed, err := url.Parse(raw)
	if err != nil {
		if len(raw) > 200 {
			return raw[:200]
		}
		return raw
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// findPunishURL 从详情响应正文中取出平台处罚验证地址；找不到时返回空串。
func findPunishURL(body []byte) string {
	// payload 是详情接口响应的通用结构，只取 data.url。
	var payload struct {
		// Data 是业务数据。
		Data struct {
			// URL 在命中处罚时是验证地址。
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	// candidate 是候选验证地址。
	candidate := strings.TrimSpace(payload.Data.URL)
	if !strings.Contains(candidate, punishMarker) {
		return ""
	}
	return candidate
}

// punishMarker 是平台处罚地址特征串。
const punishMarker = "_____tmd_____"

// riskControlRets 判断详情响应是否属于平台限流或风控要求。
func riskControlRets(rets []string) bool {
	// ret 是接口返回码。
	for _, ret := range rets {
		if strings.Contains(ret, riskControlRetMarker) || strings.Contains(ret, riskControlDetailMarker) {
			return true
		}
	}
	return false
}

// FetchDetail 抓取一次商品详情：先确保 Chrome 可用，再按需要重试平台短时限流。
// 平台限流属可恢复情况（真实浏览器实测重试即成功），因此做有限次重试；其它错误立即返回。
func (a *Agent) FetchDetail(ctx context.Context, itemID string) (browser.CompetitorDetail, error) {
	if a == nil || a.chrome == nil {
		return browser.CompetitorDetail{}, ErrChromeUnavailable
	}
	if // ensureErr 是 Chrome 调试端口就绪失败原因。
	ensureErr := a.chrome.EnsureRunning(ctx); ensureErr != nil {
		return browser.CompetitorDetail{}, ensureErr
	}
	// lastRiskControl 保存最近一次风控错误，用于全部重试失败后的返回。
	var lastRiskControl error
	// attempt 是本轮取数的尝试序号。
	for attempt := 1; attempt <= detailAttempts; attempt++ {
		// captured 是本次取数结果；err 表示本次取数是否失败。
		captured, err := a.chrome.captureDetailOnce(ctx, itemID)
		if errors.Is(err, ErrNotLoggedIn) || errors.Is(err, ErrChromeUnavailable) {
			return browser.CompetitorDetail{}, err
		}
		if err == nil {
			// detail、rets、usable 是解析出的详情、接口返回码与是否可用。
			detail, rets, usable := browser.ParseCompetitorDetailResponse(itemID, captured.Body)
			if usable {
				a.logger.Info("本机抓取详情成功", "item_id", itemID, "attempt", attempt)
				return detail, nil
			}
			if riskControlRets(rets) {
				lastRiskControl = fmt.Errorf("%w（第 %d 次）", ErrRiskControl, attempt)
				a.logger.Warn("本机抓取命中平台限流，准备重试", "item_id", itemID, "attempt", attempt)
				if // sleepErr 是退避等待被取消的原因。
				sleepErr := sleepWithJitter(ctx, attempt, detailRetryBaseDelay, detailRetryJitter); sleepErr != nil {
					return browser.CompetitorDetail{}, sleepErr
				}
				continue
			}
			// 响应不可用且不属于风控：只记录安全元信息（地址、长度、是否合法 JSON、返回码），不记录正文。
			a.logger.Warn("本机抓取响应不可用", "item_id", itemID, "response_url", captured.ResponseURL,
				"body_len", len(captured.Body), "json_valid", json.Valid(captured.Body), "rets", rets)
			return browser.CompetitorDetail{}, fmt.Errorf("%w: 接口返回码 %v", ErrDetailUnavailable, rets)
		}
		lastRiskControl = err
		a.logger.Warn("本机抓取本次失败", "item_id", itemID, "attempt", attempt, "err", err)
		if // sleepErr 是退避等待被取消的原因。
		sleepErr := sleepWithJitter(ctx, attempt, detailRetryBaseDelay, detailRetryJitter); sleepErr != nil {
			return browser.CompetitorDetail{}, sleepErr
		}
	}
	if lastRiskControl != nil {
		return browser.CompetitorDetail{}, lastRiskControl
	}
	return browser.CompetitorDetail{}, ErrDetailUnavailable
}

// sleepWithJitter 按尝试序号做带抖动的退避等待；上下文取消时立即返回。
func sleepWithJitter(ctx context.Context, attempt int, base, jitter time.Duration) error {
	// delay 是本次退避时长。
	delay := time.Duration(attempt) * base
	// extra 是随机抖动。
	extra := time.Duration(rand.Int63n(int64(jitter))) //nolint:gosec // 抖动只需随机分布，不用于安全用途
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay + extra):
		return nil
	}
}
