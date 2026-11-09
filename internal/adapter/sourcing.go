package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	sourcingapp "xianyu-go/internal/application/sourcing"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
	"xianyu-go/internal/netguard"
)

// 选品参考 AI 文案调用的固定参数。
const (
	// sourcingAIMaxTokens 限制单次文案生成的输出长度，覆盖标题加 400 字描述的需要。
	sourcingAIMaxTokens = 1200
	// sourcingAITimeout 是文案生成请求的超时时间，适应推理模型的首 token 延迟。
	sourcingAITimeout = 60 * time.Second
	// sourcingAIDefaultBaseURL 与 AI 回复引擎默认端点保持一致，未配置地址时使用。
	sourcingAIDefaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	// sourcingAIDefaultModel 与 AI 回复引擎默认模型保持一致，未配置模型时使用。
	sourcingAIDefaultModel = "qwen-plus"
)

// SourcingRepository 把 db 层选品参考仓储适配为应用层 Repository。
type SourcingRepository struct {
	// store 是数据库仓储聚合入口，仅使用 Sourcing 子仓储。
	store *db.Store
}

// NewSourcingRepository 构造选品参考持久化适配器。
func NewSourcingRepository(store *db.Store) *SourcingRepository {
	return &SourcingRepository{store: store}
}

// SaveSnapshot 写入或覆盖快照。
func (r *SourcingRepository) SaveSnapshot(ctx context.Context, snapshot sourcingapp.Snapshot) error {
	return r.store.Sourcing.SaveSnapshot(ctx, db.SourcingSnapshot{
		Kind: snapshot.Kind, SnapshotDate: snapshot.Date, PayloadJSON: snapshot.PayloadJSON,
		Status: snapshot.Status, ErrorMessage: snapshot.ErrorMessage,
	})
}

// GetSnapshot 读取快照，并把 db 层"不存在"错误转换为应用层领域错误。
func (r *SourcingRepository) GetSnapshot(ctx context.Context, kind, date string) (sourcingapp.Snapshot, error) {
	// row、err 是数据库读取结果。
	row, err := r.store.Sourcing.GetSnapshot(ctx, kind, date)
	if err != nil {
		if errors.Is(err, db.ErrSourcingSnapshotNotFound) {
			return sourcingapp.Snapshot{}, sourcingapp.ErrSnapshotNotFound
		}
		return sourcingapp.Snapshot{}, err
	}
	return sourcingapp.Snapshot{
		Kind: row.Kind, Date: row.SnapshotDate, PayloadJSON: row.PayloadJSON,
		Status: row.Status, ErrorMessage: row.ErrorMessage,
	}, nil
}

// DeleteSnapshotsBefore 清理早于截止日的快照。
func (r *SourcingRepository) DeleteSnapshotsBefore(ctx context.Context, beforeDate string) error {
	// err 是清理错误；删除条数对应用层无意义，直接丢弃。
	_, err := r.store.Sourcing.DeleteSnapshotsBefore(ctx, beforeDate)
	return err
}

// ListRecentSnapshots 按日期倒序返回某类别的最近快照，并转换为应用层视图。
func (r *SourcingRepository) ListRecentSnapshots(ctx context.Context, kind string, limit int) ([]sourcingapp.Snapshot, error) {
	// rows 是数据库快照行。
	rows, err := r.store.Sourcing.ListRecentSnapshots(ctx, kind, limit)
	if err != nil {
		return nil, err
	}
	// out 是转换后的应用层快照集合。
	out := make([]sourcingapp.Snapshot, 0, len(rows))
	// row 是当前遍历的数据库快照行。
	for _, row := range rows {
		out = append(out, sourcingapp.Snapshot{
			Kind: row.Kind, Date: row.SnapshotDate, PayloadJSON: row.PayloadJSON,
			Status: row.Status, ErrorMessage: row.ErrorMessage,
		})
	}
	return out, nil
}

// ListOrderRows 读取用户在时间窗口内的订单原始行。
func (r *SourcingRepository) ListOrderRows(ctx context.Context, userID int64, since time.Time) ([]sourcingapp.OrderRow, error) {
	// rows、err 是数据库读取结果。
	rows, err := r.store.Sourcing.ShopRankingRows(ctx, userID, since)
	if err != nil {
		return nil, err
	}
	// out 是转换后的应用层订单行。
	out := make([]sourcingapp.OrderRow, 0, len(rows))
	// row 是当前遍历到的数据库订单行。
	for _, row := range rows {
		out = append(out, sourcingapp.OrderRow{
			ItemID: row.ItemID, ItemTitle: row.ItemTitle, Quantity: row.Quantity,
			Amount: row.Amount, OrderStatus: row.OrderStatus,
		})
	}
	return out, nil
}

// SourcingCredentialReader 只读取当前用户自己名下账号的解密 cookie；明文只返回给浏览器注入路径。
type SourcingCredentialReader struct {
	// store 是数据库仓储聚合入口。
	store *db.Store
}

// NewSourcingCredentialReader 构造选品参考凭证读取适配器。
func NewSourcingCredentialReader(store *db.Store) *SourcingCredentialReader {
	return &SourcingCredentialReader{store: store}
}

// ReadCookie 校验账号归属后返回解密 cookie；账号不属于当前用户时返回不含凭证的错误。
func (r *SourcingCredentialReader) ReadCookie(ctx context.Context, userID int64, cookieID string) (string, error) {
	// trimmedID 是去空白后的账号标识。
	trimmedID := strings.TrimSpace(cookieID)
	if trimmedID == "" {
		return "", sourcingapp.ErrAccountNotOwned
	}
	// value、err 是按所有者过滤后的解密 cookie；错误信息不包含任何凭证片段。
	value, err := r.store.Cookies.GetValueOwned(ctx, userID, trimmedID)
	if err != nil || strings.TrimSpace(value) == "" {
		return "", sourcingapp.ErrAccountNotOwned
	}
	return value, nil
}

// SourcingBrowserGateway 把浏览器管理器适配为应用层 BrowserGateway，并转换领域错误。
type SourcingBrowserGateway struct {
	// manager 是进程唯一的 Chromium 生命周期拥有者。
	manager *browser.Manager
}

// NewSourcingBrowserGateway 构造浏览器采集适配器；manager 为空时返回 nil，调用方据此降级。
func NewSourcingBrowserGateway(manager *browser.Manager) *SourcingBrowserGateway {
	if manager == nil {
		return nil
	}
	return &SourcingBrowserGateway{manager: manager}
}

// FetchHotKeywords 以游客身份采集热词。
func (g *SourcingBrowserGateway) FetchHotKeywords(ctx context.Context) ([]string, error) {
	// keywords、err 是浏览器采集结果。
	keywords, err := g.manager.FetchHotKeywords(ctx)
	return keywords, mapSourcingBrowserError(err)
}

// SearchItems 以账号登录态执行单次关键词查询。
func (g *SourcingBrowserGateway) SearchItems(ctx context.Context, cookieID, cookieStr, keyword string) ([]sourcingapp.SearchItem, error) {
	// items、err 是浏览器查询结果。
	items, err := g.manager.SearchItems(ctx, cookieID, cookieStr, keyword)
	if err != nil {
		return nil, mapSourcingBrowserError(err)
	}
	// out 是转换后的应用层商品摘要。
	out := make([]sourcingapp.SearchItem, 0, len(items))
	// item 是当前遍历到的浏览器商品摘要。
	for _, item := range items {
		out = append(out, sourcingapp.SearchItem{
			ItemID: item.ItemID, Title: item.Title, Price: item.Price,
			WantCount: item.WantCount, Location: item.Location,
			SellerNickname: item.SellerNickname, PublishTime: item.PublishTime,
			DetailURL: item.DetailURL,
		})
	}
	return out, nil
}

// mapSourcingBrowserError 把浏览器层错误转换为应用层领域错误；未知错误原样返回。
func mapSourcingBrowserError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, browser.ErrSourcingRiskControl):
		return sourcingapp.ErrRiskControl
	case errors.Is(err, browser.ErrSourcingSessionExpired):
		return sourcingapp.ErrSessionExpired
	case errors.Is(err, browser.ErrSourcingNoResult):
		return sourcingapp.ErrNoResult
	case errors.Is(err, browser.ErrSourcingHotKeywordUnavailable):
		return sourcingapp.ErrHotKeywordUnavailable
	default:
		return err
	}
}

// sourcingAISettingsReader 是 AI 文案客户端读取配置所需的最小能力，便于测试替换。
type sourcingAISettingsReader interface {
	// ReadAPIKey 在写入访问审计后读取用户可用的 AI 密钥。
	ReadAPIKey(ctx context.Context, userID int64) (string, error)
	// ReadSetting 读取非敏感系统设置。
	ReadSetting(ctx context.Context, key string) (string, error)
}

// storeAISettingsReader 基于 db.Store 实现 AI 配置读取，密钥读取必经访问审计。
type storeAISettingsReader struct {
	// store 是数据库仓储聚合入口。
	store *db.Store
}

// ReadAPIKey 读取 AI 密钥并写入访问审计。
func (r storeAISettingsReader) ReadAPIKey(ctx context.Context, userID int64) (string, error) {
	return r.store.ReadSensitiveSetting(ctx, userID, "ai_api_key", "settings.use", "sourcing_copywriting")
}

// ReadSetting 读取非敏感系统设置。
func (r storeAISettingsReader) ReadSetting(ctx context.Context, key string) (string, error) {
	return r.store.Settings.Get(ctx, key)
}

// SourcingChatClient 复用系统 AI 配置调用 OpenAI 兼容 chat completions 接口生成文案。
type SourcingChatClient struct {
	// settings 读取 AI 地址、模型与密钥。
	settings sourcingAISettingsReader
	// newHTTPClient 按管理员配置端点构造受限 HTTP 客户端，测试可替换。
	newHTTPClient func(baseURL string) (*http.Client, error)
}

// NewSourcingChatClient 构造选品参考 AI 文案客户端。
func NewSourcingChatClient(store *db.Store) *SourcingChatClient {
	return &SourcingChatClient{
		settings: storeAISettingsReader{store: store},
		newHTTPClient: func(baseURL string) (*http.Client, error) {
			return netguard.ConfiguredEndpointHTTPClient(baseURL, sourcingAITimeout)
		},
	}
}

// sourcingChatRequest 是 OpenAI 兼容的文案生成请求体。
type sourcingChatRequest struct {
	// Model 是使用的模型名称。
	Model string `json:"model"`
	// Messages 是系统提示与用户提示。
	Messages []aiConnectionTestMessage `json:"messages"`
	// MaxTokens 限制输出长度。
	MaxTokens int `json:"max_tokens"`
}

// Complete 发送系统提示与用户提示并返回模型正文；API 密钥只出现在请求头，不写日志也不进入错误信息。
func (c *SourcingChatClient) Complete(ctx context.Context, userID int64, systemPrompt, userPrompt string) (string, error) {
	// apiKey、keyErr 是审计后读取的 AI 密钥。
	apiKey, keyErr := c.settings.ReadAPIKey(ctx, userID)
	if keyErr != nil {
		return "", fmt.Errorf("读取 AI 配置失败: %w", keyErr)
	}
	if strings.TrimSpace(apiKey) == "" {
		return "", sourcingapp.ErrAIConfigMissing
	}
	// baseURL 优先读取 ai_api_url，兼容旧键 ai_base_url，均为空时使用默认端点。
	baseURL, _ := c.settings.ReadSetting(ctx, "ai_api_url")
	if strings.TrimSpace(baseURL) == "" {
		baseURL, _ = c.settings.ReadSetting(ctx, "ai_base_url")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = sourcingAIDefaultBaseURL
	}
	// model 是配置的模型名称，空值使用默认模型。
	model, _ := c.settings.ReadSetting(ctx, "ai_model")
	model = strings.TrimSpace(model)
	if model == "" {
		model = sourcingAIDefaultModel
	}
	// client、clientErr 是受出站策略约束的 HTTP 客户端。
	client, clientErr := c.newHTTPClient(baseURL)
	if clientErr != nil {
		return "", fmt.Errorf("AI API 地址无效: %w", clientErr)
	}
	// body、marshalErr 是请求正文。
	body, marshalErr := json.Marshal(sourcingChatRequest{
		Model: model,
		Messages: []aiConnectionTestMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens: sourcingAIMaxTokens,
	})
	if marshalErr != nil {
		return "", fmt.Errorf("构造 AI 请求失败: %w", marshalErr)
	}
	// request、requestErr 是绑定调用方取消信号的请求。
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if requestErr != nil {
		return "", requestErr
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	// response、doErr 是远端响应。
	response, doErr := client.Do(request)
	if doErr != nil {
		if errors.Is(doErr, context.DeadlineExceeded) {
			return "", errors.New("AI 服务响应超时，请稍后重试")
		}
		return "", errors.New("连接 AI 服务失败")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("AI 认证失败：API Key 无效或权限不足 (HTTP %d)", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("AI 服务请求失败 (HTTP %d)", response.StatusCode)
	}
	// raw、readErr 是受大小限制的响应正文。
	raw, readErr := ReadAIModelsBody(response.Body)
	if readErr != nil {
		return "", fmt.Errorf("读取 AI 服务响应失败: %w", readErr)
	}
	// payload 是解析后的兼容响应。
	var payload aiChatCompletionResponse
	// decodeErr 表示成功响应不是可解析的 OpenAI 兼容 JSON。
	if // decodeErr 表示成功响应不是可解析的 OpenAI 兼容 JSON（含被网关包一层的情况）。
	decodeErr := json.Unmarshal(unwrapChatResponseEnvelope(raw), &payload); decodeErr != nil {
		return "", errors.New("AI 服务返回了无法解析的响应")
	}
	if len(payload.Choices) == 0 {
		return "", sourcingapp.ErrAIEmptyContent
	}
	// content 是首个候选的可见正文；不回退到推理字段，避免把思考过程当文案。
	content := extractAIMessageText(payload.Choices[0].Message.Content)
	if content == "" {
		content = strings.TrimSpace(payload.Choices[0].Text)
	}
	return content, nil
}
