// Package sourcing 提供选品参考用例：平台热词、关键词查货、本店畅销榜与 AI 文案。
package sourcing

import (
	"context"
	"errors"
	"time"
)

// 快照类别与状态的领域常量；取值与持久化层保持一致。
const (
	// KindHotKeywords 表示平台搜索热词快照。
	KindHotKeywords = "hot_keywords"
	// StatusOK 表示该日快照采集成功。
	StatusOK = "ok"
	// StatusFailed 表示该日快照采集失败。
	StatusFailed = "failed"
	// SnapshotRetentionDays 是快照保留天数。
	SnapshotRetentionDays = 30
	// SearchCooldown 是同一账号同一关键词的最短查询间隔，用于防止界面连点造成高频搜索。
	SearchCooldown = 10 * time.Second
	// RankingWindow7Days 是畅销榜的近 7 天窗口标识。
	RankingWindow7Days = "7d"
	// RankingWindow30Days 是畅销榜的近 30 天窗口标识。
	RankingWindow30Days = "30d"
	// CopywritingModeListing 表示生成可直接上架的标题与描述。
	CopywritingModeListing = "listing"
	// CopywritingModeAnalysis 表示生成品类卖点与定价分析。
	CopywritingModeAnalysis = "analysis"
	// rankingLimit 是畅销榜返回条数上限。
	rankingLimit = 20
)

// 应用层领域错误，供传输层映射为明确的状态码与提示。
var (
	// ErrSnapshotNotFound 表示指定类别与日期尚无可展示快照。
	ErrSnapshotNotFound = errors.New("选品参考快照不存在")
	// ErrSearchCooling 表示同一账号同一关键词仍在冷却期内。
	ErrSearchCooling = errors.New("该关键词刚刚查询过，请稍后再试")
	// ErrRiskControl 表示平台要求风控验证，必须人工处理。
	ErrRiskControl = errors.New("闲鱼返回风控验证要求，请先在闲鱼网页手动完成验证后再试")
	// ErrSessionExpired 表示账号登录态失效。
	ErrSessionExpired = errors.New("闲鱼登录态已失效，请重新登录该账号")
	// ErrNoResult 表示本次查询没有取到商品。
	ErrNoResult = errors.New("本次查询没有取到商品")
	// ErrHotKeywordUnavailable 表示本次没有取到平台热词。
	ErrHotKeywordUnavailable = errors.New("本次没有取到平台热词")
	// ErrAIConfigMissing 表示系统尚未配置可用的 AI 接口。
	ErrAIConfigMissing = errors.New("尚未配置 AI 接口，请先在系统设置中完成 AI 配置")
	// ErrAIEmptyContent 表示 AI 返回了空内容。
	ErrAIEmptyContent = errors.New("AI 未返回可用内容，请稍后重试")
	// ErrInvalidCopywritingMode 表示文案模式参数不受支持。
	ErrInvalidCopywritingMode = errors.New("不支持的文案模式")
	// ErrEmptyKeyword 表示查询关键词为空，不发出任何平台请求。
	ErrEmptyKeyword = errors.New("查询关键词不能为空")
	// ErrAccountNotOwned 表示账号不存在或不属于当前用户，不能用于查询。
	ErrAccountNotOwned = errors.New("账号不存在或无权使用该账号")
	// ErrBrowserUnavailable 表示浏览器能力不可用，相关功能必须明确降级。
	ErrBrowserUnavailable = errors.New("浏览器能力不可用，无法执行该查询")
)

// Snapshot 是选品参考的单日快照，不含任何凭证。
type Snapshot struct {
	// Kind 是快照类别。
	Kind string
	// Date 是快照归属日，格式 YYYY-MM-DD。
	Date string
	// PayloadJSON 是快照结构化结果。
	PayloadJSON string
	// Status 是采集结果。
	Status string
	// ErrorMessage 是失败原因的脱敏摘要。
	ErrorMessage string
}

// HotKeywordSnapshotPayload 是热词快照的持久化内容。
type HotKeywordSnapshotPayload struct {
	// Keywords 是采集到的热词词条。
	Keywords []string `json:"keywords"`
}

// RankingEntry 是畅销榜的单条商品统计。
type RankingEntry struct {
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是本地商品标题，缺失时为空。
	ItemTitle string `json:"item_title"`
	// SoldQuantity 是窗口内售出数量。
	SoldQuantity int `json:"sold_quantity"`
	// Revenue 是窗口内营收合计。
	Revenue float64 `json:"revenue"`
	// OrderCount 是窗口内订单数。
	OrderCount int `json:"order_count"`
}

// OrderRow 是畅销榜聚合前的订单原始行。
type OrderRow struct {
	// ItemID 是平台商品标识。
	ItemID string
	// ItemTitle 是本地商品标题。
	ItemTitle string
	// Quantity 是数量原始文本。
	Quantity string
	// Amount 是金额原始文本。
	Amount string
	// OrderStatus 是订单状态文本。
	OrderStatus string
}

// SearchItem 是关键词查货返回的单条商品摘要。
type SearchItem struct {
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// Price 是价格文本。
	Price string `json:"price"`
	// WantCount 是"多少人想要"文本。
	WantCount string `json:"want_count"`
	// Location 是卖家地区文本。
	Location string `json:"location"`
	// DetailURL 是商品详情页地址。
	DetailURL string `json:"detail_url"`
}

// HotKeywordView 是热词接口的返回视图，包含采集状态以便界面区分"没有热词"与"采集失败"。
type HotKeywordView struct {
	// Date 是快照归属日。
	Date string `json:"date"`
	// Keywords 是热词词条。
	Keywords []string `json:"keywords"`
	// Status 是采集状态。
	Status string `json:"status"`
	// ErrorMessage 是失败原因摘要。
	ErrorMessage string `json:"error_message"`
}

// ShopRankingView 是畅销榜接口的返回视图。
type ShopRankingView struct {
	// Date 是快照归属日。
	Date string `json:"date"`
	// Window 是统计窗口标识。
	Window string `json:"window"`
	// Entries 是排行条目。
	Entries []RankingEntry `json:"entries"`
	// Status 是快照状态。
	Status string `json:"status"`
}

// CopywritingRequest 是 AI 文案生成请求。
type CopywritingRequest struct {
	// Mode 是文案模式，取 CopywritingModeListing 或 CopywritingModeAnalysis。
	Mode string
	// Topic 是主题词或商品标题。
	Topic string
	// Reference 是可选参考信息，例如搜索结果中的价格与想要人数。
	Reference string
}

// Copywriting 是 AI 文案生成结果。
type Copywriting struct {
	// Mode 是本次使用的文案模式。
	Mode string `json:"mode"`
	// Content 是生成的正文内容。
	Content string `json:"content"`
}

// Repository 是选品参考的持久化边界，由 db 层实现并在组合根注入。
type Repository interface {
	// SaveSnapshot 写入或覆盖某类别某日的快照。
	SaveSnapshot(ctx context.Context, snapshot Snapshot) error
	// GetSnapshot 读取某类别某日快照；不存在时返回 ErrSnapshotNotFound。
	GetSnapshot(ctx context.Context, kind, date string) (Snapshot, error)
	// DeleteSnapshotsBefore 删除早于指定日期的快照。
	DeleteSnapshotsBefore(ctx context.Context, beforeDate string) error
	// ListRecentSnapshots 按日期倒序返回某类别的最近快照；用于热词涨幅的时间序列对比。
	ListRecentSnapshots(ctx context.Context, kind string, limit int) ([]Snapshot, error)
	// ListOrderRows 返回指定用户在时间窗口内的订单原始行。
	ListOrderRows(ctx context.Context, userID int64, since time.Time) ([]OrderRow, error)
}

// BrowserGateway 是浏览器态数据采集边界，由 browser 层适配实现。
type BrowserGateway interface {
	// FetchHotKeywords 以游客身份采集平台热词，不携带账号凭证。
	FetchHotKeywords(ctx context.Context) ([]string, error)
	// SearchItems 以账号登录态执行单次关键词查询。
	SearchItems(ctx context.Context, cookieID, cookieStr, keyword string) ([]SearchItem, error)
}

// CredentialReader 提供账号凭证解密读取；实现方必须保证不在日志或响应中暴露明文。
type CredentialReader interface {
	// ReadCookie 返回指定账号的解密 cookie 文本，仅供浏览器登录态注入使用。
	ReadCookie(ctx context.Context, userID int64, cookieID string) (string, error)
}

// ChatClient 是 AI 对话补全边界，由 adapter 层适配现有 AI 配置实现。
type ChatClient interface {
	// Complete 发送系统提示与用户提示并返回补全文本。
	Complete(ctx context.Context, userID int64, systemPrompt, userPrompt string) (string, error)
}
