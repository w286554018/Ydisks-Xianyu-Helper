package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"xianyu-go/internal/application/items"
	"xianyu-go/internal/application/marketintel"
)

// 本机抓取助手接口相关的固定参数。
const (
	// localFetchDetailPath 是助手的取详情路径。
	localFetchDetailPath = "/fetch-detail"
	// localFetchTokenHeader 是共享令牌请求头。
	localFetchTokenHeader = "X-Auth-Token"
	// localFetchTimeout 是单次取详情的超时上限；助手内部最长 150 秒，这里留出余量。
	localFetchTimeout = 160 * time.Second
	// localFetchMaxBodyBytes 是助手响应体上限。
	localFetchMaxBodyBytes = 1 << 20
)

// 助手错误码常量，与 internal/localfetch 的响应保持一致。
const (
	// localFetchCodeNotLoggedIn 表示助手浏览器未登录闲鱼。
	localFetchCodeNotLoggedIn = "not_logged_in"
	// localFetchCodeRiskControl 表示平台仍要求人机验证。
	localFetchCodeRiskControl = "risk_control"
	// localFetchCodeUnauthorized 表示共享令牌不匹配。
	localFetchCodeUnauthorized = "unauthorized"
	// localFetchCodeChromeUnavailable 表示助手侧 Chrome 调试端口不可用。
	localFetchCodeChromeUnavailable = "chrome_unavailable"
)

// LocalFetchAgentClient 通过回环 HTTP 调用用户本机的抓取助手获取竞品详情。
//
// 地址或令牌为空时视为未启用（构造返回 nil）。令牌只用于请求头，绝不写日志、错误文本或响应。
// 助手不可用时立即返回场景错误，不回退服务器侧浏览器：该路径已被平台确定性判罚。
type LocalFetchAgentClient struct {
	// endpoint 是取详情地址。
	endpoint string
	// token 是共享令牌。
	token string
	// httpClient 是调用助手用的客户端，构造后只读。
	httpClient *http.Client
	// logger 是日志器。
	logger *slog.Logger
}

// NewLocalFetchAgentClient 构造助手客户端；地址或令牌为空时返回 nil 表示未启用该通道。
func NewLocalFetchAgentClient(baseURL, token string, logger *slog.Logger) *LocalFetchAgentClient {
	// trimmedBase 是去除首尾空白与结尾斜杠后的助手地址。
	trimmedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	// trimmedToken 是去除首尾空白后的共享令牌。
	trimmedToken := strings.TrimSpace(token)
	if trimmedBase == "" || trimmedToken == "" {
		return nil
	}
	// effectiveLogger 是实际使用的日志器。
	effectiveLogger := logger
	if effectiveLogger == nil {
		effectiveLogger = slog.Default()
	}
	return &LocalFetchAgentClient{
		endpoint:   trimmedBase + localFetchDetailPath,
		token:      trimmedToken,
		httpClient: &http.Client{Timeout: localFetchTimeout},
		logger:     effectiveLogger,
	}
}

// localFetchDetailResponse 是助手返回的详情结构。
type localFetchDetailResponse struct {
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

// localFetchErrorResponse 是助手统一错误结构。
type localFetchErrorResponse struct {
	// Code 是稳定错误码。
	Code string `json:"code"`
	// Message 是中文提示。
	Message string `json:"message"`
}

// FetchCompetitorDetail 调用本机助手抓取详情，并把失败映射为应用层场景错误。
func (c *LocalFetchAgentClient) FetchCompetitorDetail(ctx context.Context, itemID string) (marketintel.CompetitorDetail, error) {
	// detail、err 是助手返回的原始详情与错误。
	detail, err := c.fetchLocalDetail(ctx, itemID)
	if err != nil {
		return marketintel.CompetitorDetail{}, err
	}
	return marketintel.CompetitorDetail{
		ItemID:         itemID,
		Title:          detail.Title,
		Price:          detail.Price,
		DescText:       detail.DescText,
		Images:         detail.Images,
		SellerNickname: detail.SellerNickname,
		SellerID:       detail.SellerID,
	}, nil
}

// FetchItemStats 读取自己商品详情返回的公开计数（浏览量/想要量/收藏/已售）。
// 与竞品详情共用同一通道与超时语义；错误在此翻译成商品统计服务自有哨兵，
// 使商品统计用例不必反向依赖市场情报包。
func (c *LocalFetchAgentClient) FetchItemStats(ctx context.Context, itemID string) (items.ItemStatsCounts, error) {
	// detail、err 是助手返回的原始详情与错误。
	detail, err := c.fetchLocalDetail(ctx, itemID)
	if err != nil {
		return items.ItemStatsCounts{}, translateLocalFetchError(err)
	}
	// counts 是本次抓取到的四项公开计数。
	counts := items.ItemStatsCounts{
		BrowseCnt: detail.BrowseCnt, WantCnt: detail.WantCnt,
		CollectCnt: detail.CollectCnt, SoldCnt: detail.SoldCnt,
	}
	return counts, nil
}

// translateLocalFetchError 把本机助手场景错误翻译为商品统计服务自有哨兵。
// 未识别的错误原样返回，由传输层按未归类错误记录诊断日志。
func translateLocalFetchError(err error) error {
	switch {
	case errors.Is(err, marketintel.ErrLocalFetchRiskControl):
		return items.ErrItemStatsRiskControl
	case errors.Is(err, marketintel.ErrLocalFetchNotLoggedIn):
		return items.ErrItemStatsNotLoggedIn
	case errors.Is(err, marketintel.ErrLocalFetchUnauthorized):
		return items.ErrItemStatsUnauthorized
	case errors.Is(err, marketintel.ErrLocalFetchUnavailable):
		return items.ErrItemStatsUnavailable
	default:
		return err
	}
}

// fetchLocalDetail 执行一次助手取详情调用并解析响应；失败统一映射为助手场景错误。
// 该方法只发起一次 HTTP 调用，不做批量与扇出：平台限流的重试语义由助手自身决定。
func (c *LocalFetchAgentClient) fetchLocalDetail(ctx context.Context, itemID string) (localFetchDetailResponse, error) {
	if c == nil {
		return localFetchDetailResponse{}, marketintel.ErrLocalFetchUnavailable
	}
	// payload 是取详情请求体；只发送 item_id，助手自行拼接商品页地址。
	payload, marshalErr := json.Marshal(map[string]string{"item_id": itemID})
	if marshalErr != nil {
		return localFetchDetailResponse{}, fmt.Errorf("构造本机抓取请求失败: %w", marshalErr)
	}
	// request 是取详情请求；err 表示请求构造失败。
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return localFetchDetailResponse{}, fmt.Errorf("构造本机抓取请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(localFetchTokenHeader, c.token)

	// response 是助手响应；doErr 表示网络层失败（助手未启动、隧道断开等）。
	response, doErr := c.httpClient.Do(request)
	if doErr != nil {
		c.logger.Warn("本机抓取助手不可达", "item_id", itemID)
		return localFetchDetailResponse{}, fmt.Errorf("%w: 连接助手失败", marketintel.ErrLocalFetchUnavailable)
	}
	defer func() { _ = response.Body.Close() }()
	// body 是响应正文。
	body, readErr := io.ReadAll(io.LimitReader(response.Body, localFetchMaxBodyBytes))
	if readErr != nil {
		return localFetchDetailResponse{}, fmt.Errorf("%w: 读取助手响应失败", marketintel.ErrLocalFetchUnavailable)
	}
	if response.StatusCode != http.StatusOK {
		return localFetchDetailResponse{}, c.mapLocalFetchFailure(itemID, response.StatusCode, body)
	}
	// detail 是解析后的详情。
	var detail localFetchDetailResponse
	if // decodeErr 是助手响应解析失败原因。
	decodeErr := json.Unmarshal(body, &detail); decodeErr != nil {
		c.logger.Warn("本机抓取助手响应解析失败", "item_id", itemID)
		return localFetchDetailResponse{}, fmt.Errorf("解析助手响应失败: %w", decodeErr)
	}
	return detail, nil
}

// mapLocalFetchFailure 把助手的失败响应映射为应用层场景错误。
// 日志只记录状态码与错误码，不记录响应正文与令牌。
func (c *LocalFetchAgentClient) mapLocalFetchFailure(itemID string, status int, body []byte) error {
	// failure 是助手返回的错误结构；解析失败时保持零值并回退到状态码判定。
	var failure localFetchErrorResponse
	_ = json.Unmarshal(body, &failure)
	c.logger.Warn("本机抓取助手返回失败", "item_id", itemID, "status", status, "code", failure.Code)
	switch failure.Code {
	case localFetchCodeNotLoggedIn:
		return marketintel.ErrLocalFetchNotLoggedIn
	case localFetchCodeRiskControl:
		return marketintel.ErrLocalFetchRiskControl
	case localFetchCodeUnauthorized:
		return marketintel.ErrLocalFetchUnauthorized
	case localFetchCodeChromeUnavailable:
		return marketintel.ErrLocalFetchUnavailable
	}
	switch status {
	case http.StatusUnauthorized:
		return marketintel.ErrLocalFetchUnauthorized
	case http.StatusServiceUnavailable:
		return marketintel.ErrLocalFetchUnavailable
	case http.StatusConflict:
		return marketintel.ErrLocalFetchRiskControl
	default:
		return fmt.Errorf("本机抓取助手返回异常状态 %d", status)
	}
}
