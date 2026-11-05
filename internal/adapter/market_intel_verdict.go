// Package adapter: 市场情报 AI 判定端口——独立模型配置的批量结构化判定。
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	marketintel "xianyu-go/internal/application/marketintel"
	"xianyu-go/internal/db"
	"xianyu-go/internal/netguard"
)

// 市场情报 AI 判定的固定参数。
const (
	// marketIntelAIDefaultBaseURL 是未配置时的默认 OpenAI 兼容端点。
	marketIntelAIDefaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	// marketIntelAIDefaultModel 是未配置时的默认判定模型。
	marketIntelAIDefaultModel = "qwen-plus"
	// marketIntelAITimeout 是单次批量判定的超时。
	marketIntelAITimeout = 120 * time.Second
	// marketIntelAIMaxTokens 是批量判定的输出上限；30 条 JSON 结果需要足够空间。
	marketIntelAIMaxTokens = 4096
	// marketIntelAIResponseLimit 是响应正文大小上限。
	marketIntelAIResponseLimit = 4 << 20
)

// 市场情报判定模型的系统设置键。
const (
	// SettingMarketIntelAPIURL 是判定模型的服务地址键。
	SettingMarketIntelAPIURL = "market_intel_api_url"
	// SettingMarketIntelModel 是判定模型名称键。
	SettingMarketIntelModel = "market_intel_model"
	// SettingMarketIntelAPIKey 是判定模型的敏感密钥键。
	SettingMarketIntelAPIKey = "market_intel_api_key"
)

// MarketIntelVerdictPort 用独立模型配置执行批量商品判定；失败不重试（由应用层降级）。
type MarketIntelVerdictPort struct {
	// store 提供判定模型配置读取（密钥读取必经访问审计）。
	store *db.Store
	// userID 是密钥审计归属用户（系统任务使用管理员）。
	userID int64
	// newHTTPClient 按配置端点构造受限 HTTP 客户端，测试可替换。
	newHTTPClient func(baseURL string) (*http.Client, error)
}

// NewMarketIntelVerdictPort 构造市场情报判定端口。
func NewMarketIntelVerdictPort(store *db.Store, userID int64) *MarketIntelVerdictPort {
	return &MarketIntelVerdictPort{
		store:  store,
		userID: userID,
		newHTTPClient: func(baseURL string) (*http.Client, error) {
			return netguard.ConfiguredEndpointHTTPClient(baseURL, marketIntelAITimeout)
		},
	}
}

// marketIntelChatRequest 是 OpenAI 兼容的判定请求体。
type marketIntelChatRequest struct {
	// Model 是使用的判定模型名称。
	Model string `json:"model"`
	// Messages 是系统提示与用户提示。
	Messages []marketIntelChatMessage `json:"messages"`
	// MaxTokens 限制输出长度。
	MaxTokens int `json:"max_tokens"`
	// Temperature 降低随机性，保证结构化输出稳定。
	Temperature float64 `json:"temperature"`
}

// marketIntelChatMessage 是单条对话消息。
type marketIntelChatMessage struct {
	// Role 是消息角色。
	Role string `json:"role"`
	// Content 是消息内容。
	Content string `json:"content"`
}

// marketIntelChatResponse 是 OpenAI 兼容响应的最小解析结构。
type marketIntelChatResponse struct {
	// Choices 是候选列表。
	Choices []struct {
		// Message 是首个候选的完整消息。
		Message struct {
			// Content 是模型正文。
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// marketIntelVerdictItem 是模型输出的单条判定。
type marketIntelVerdictItem struct {
	// Index 是输入列表中的下标（0 起）。
	Index int `json:"index"`
	// IsAIRelated 表示是否 AI 相关产品。
	IsAIRelated bool `json:"is_ai_related"`
	// Category 是细分类目。
	Category string `json:"category"`
	// Reason 是判定理由摘要。
	Reason string `json:"reason"`
}

// verdictSystemPrompt 是批量判定的系统提示。
const verdictSystemPrompt = `你是闲鱼市场的商品分类助手。用户会给出一个搜索关键词和一批商品（含标题、价格、想要人数）。
对每条商品判断它是否属于「AI 相关产品」——即商品本身涉及人工智能技术、AI 工具、AI 课程教学、AI 代做服务、AI 绘画/写作/视频接单、AI 账号或提示词等。
注意：普通软件课程、普通设计服务、普通电子产品不算 AI 相关；标题不含 AI 但实质是 AI 服务（如机器学习代做、AI 绘画接单）算 AI 相关。
只输出 JSON 数组，不要输出任何其他文字。每个元素格式：{"index":<输入下标>,"is_ai_related":true|false,"category":"<AI课程|AI代做|AI绘画|AI写作|AI视频|AI工具账号|AI其他|>","reason":"<20字内理由>"}。
非 AI 商品 category 为空字符串。`

// JudgeBatch 对一批快照执行单次批量判定；解析失败一次性返回错误，不重试。
func (p *MarketIntelVerdictPort) JudgeBatch(ctx context.Context, keyword string, items []marketintel.VerdictInput) ([]marketintel.VerdictResult, error) {
	if len(items) == 0 {
		return nil, nil
	}
	// cfg 是判定模型配置；未配置时返回明确错误。
	cfg, err := p.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	// userPrompt 是构建的批量输入。
	userPrompt := buildVerdictUserPrompt(keyword, items)
	// client 是受出站策略约束的 HTTP 客户端。
	client, clientErr := p.newHTTPClient(cfg.baseURL)
	if clientErr != nil {
		return nil, fmt.Errorf("市场情报模型地址无效: %w", clientErr)
	}
	// body 是请求正文。
	body, marshalErr := json.Marshal(marketIntelChatRequest{
		Model: cfg.model,
		Messages: []marketIntelChatMessage{
			{Role: "system", Content: verdictSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens:   marketIntelAIMaxTokens,
		Temperature: 0.1,
	})
	if marshalErr != nil {
		return nil, fmt.Errorf("构造判定请求失败: %w", marshalErr)
	}
	// request 是绑定调用方取消信号的请求。
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, cfg.baseURL+"/chat/completions", bytes.NewReader(body))
	if requestErr != nil {
		return nil, requestErr
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.apiKey))
	// response 是远端响应。
	response, doErr := client.Do(request)
	if doErr != nil {
		if errors.Is(doErr, context.DeadlineExceeded) {
			return nil, errors.New("市场情报模型响应超时")
		}
		return nil, errors.New("连接市场情报模型失败")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("市场情报模型认证失败 (HTTP %d)", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("市场情报模型请求失败 (HTTP %d)", response.StatusCode)
	}
	// limited 是受大小限制的响应正文。
	limited := io.LimitReader(response.Body, marketIntelAIResponseLimit)
	// raw 是响应正文。
	raw, readErr := io.ReadAll(limited)
	if readErr != nil {
		return nil, fmt.Errorf("读取模型响应失败: %w", readErr)
	}
	// payload 是解析后的兼容响应。
	var payload marketIntelChatResponse
	// decodeErr 表示成功响应不是可解析的 OpenAI 兼容 JSON。
	if decodeErr := json.Unmarshal(raw, &payload); decodeErr != nil {
		return nil, errors.New("市场情报模型返回了无法解析的响应")
	}
	if len(payload.Choices) == 0 {
		return nil, errors.New("市场情报模型返回空结果")
	}
	// results 是解析出的判定结果。
	return parseVerdictContent(payload.Choices[0].Message.Content, items)
}

// marketIntelModelConfig 是判定模型的三项配置。
type marketIntelModelConfig struct {
	// baseURL 是服务地址。
	baseURL string
	// model 是模型名称。
	model string
	// apiKey 是敏感密钥。
	apiKey string
}

// loadConfig 读取判定模型配置；密钥读取写入访问审计；未配置密钥时返回错误。
func (p *MarketIntelVerdictPort) loadConfig(ctx context.Context) (marketIntelModelConfig, error) {
	if p == nil || p.store == nil {
		return marketIntelModelConfig{}, errors.New("市场情报模型存储未装配")
	}
	// baseURL 是服务地址。
	baseURL, _ := p.store.Settings.Get(ctx, SettingMarketIntelAPIURL)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = marketIntelAIDefaultBaseURL
	}
	// model 是模型名称。
	model, _ := p.store.Settings.Get(ctx, SettingMarketIntelModel)
	model = strings.TrimSpace(model)
	if model == "" {
		model = marketIntelAIDefaultModel
	}
	// apiKey 是受控读取的密钥；读取失败或为空都视为未配置。
	apiKey, keyErr := p.store.ReadSensitiveSetting(ctx, p.userID, SettingMarketIntelAPIKey, "settings.use", "market_intel_verdict")
	if keyErr != nil || strings.TrimSpace(apiKey) == "" {
		return marketIntelModelConfig{}, errors.New("市场情报模型未配置，请在设置页填写 API Key")
	}
	return marketIntelModelConfig{baseURL: baseURL, model: model, apiKey: apiKey}, nil
}

// buildVerdictUserPrompt 构造批量判定的用户提示。
func buildVerdictUserPrompt(keyword string, items []marketintel.VerdictInput) string {
	// builder 是提示构造器。
	var builder strings.Builder
	builder.WriteString("搜索关键词：")
	builder.WriteString(keyword)
	builder.WriteString("\n商品列表：\n")
	// index 遍历商品。
	for index, item := range items {
		builder.WriteString(fmt.Sprintf("%d. 标题：%s | 价格：%s | 想要：%s\n", index, item.Title, item.Price, item.WantCountText))
	}
	return builder.String()
}

// parseVerdictContent 解析模型正文中的 JSON 数组并映射回快照主键。
func parseVerdictContent(content string, items []marketintel.VerdictInput) ([]marketintel.VerdictResult, error) {
	// trimmed 是去掉 markdown 代码栅栏后的正文。
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	// parsed 是模型输出的判定数组。
	var parsed []marketIntelVerdictItem
	// unmarshalErr 是判定 JSON 解析错误；解析失败按当天降级处理。
	if unmarshalErr := json.Unmarshal([]byte(trimmed), &parsed); unmarshalErr != nil {
		return nil, fmt.Errorf("判定结果不是合法 JSON: %w", unmarshalErr)
	}
	// out 是映射回快照主键的结果。
	out := make([]marketintel.VerdictResult, 0, len(items))
	// item 遍历输出。
	for _, verdictItem := range parsed {
		if verdictItem.Index < 0 || verdictItem.Index >= len(items) {
			continue
		}
		// input 是对应输入。
		input := items[verdictItem.Index]
		out = append(out, marketintel.VerdictResult{
			SnapshotID: input.SnapshotID, IsAIRelated: verdictItem.IsAIRelated,
			Category: strings.TrimSpace(verdictItem.Category), Reason: strings.TrimSpace(verdictItem.Reason),
		})
	}
	if len(out) == 0 {
		return nil, errors.New("判定结果与输入不匹配")
	}
	return out, nil
}
