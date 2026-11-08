// Package marketintel: 竞品详情查看、AI 改写与卖点分析。
package marketintel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// CompetitorDetailCacheTTL 是竞品详情缓存有效期。
const CompetitorDetailCacheTTL = 7 * 24 * time.Hour

// CompetitorDetail 是竞品商品详情视图。
type CompetitorDetail struct {
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// Price 是价格文本。
	Price string `json:"price"`
	// DescText 是商品描述纯文本。
	DescText string `json:"desc_text"`
	// Images 是商品图片 URL 列表。
	Images []string `json:"images"`
	// SellerNickname 是卖家昵称。
	SellerNickname string `json:"seller_nickname"`
	// SellerID 是卖家 userId。
	SellerID string `json:"seller_id"`
	// WantCountText 是想要人数文本。
	WantCountText string `json:"want_count_text"`
	// Cached 表示本次是否来自缓存。
	Cached bool `json:"cached"`
}

// DetailFetcher 是竞品详情抓取边界；实现方保证单次调用只产生 1 次详情页访问。
// 该接口导出，便于组合根明确区分「已装配」与「未装配」（typed-nil 会被应用层的 nil 判断漏掉）。
type DetailFetcher interface {
	// FetchCompetitorDetail 抓取指定商品的详情（标题/描述/图片/卖家）。
	FetchCompetitorDetail(ctx context.Context, itemID string) (CompetitorDetail, error)
}

// detailCache 是竞品详情缓存边界。
type detailCache interface {
	// GetCachedDetail 读取缓存详情；不存在返回 ErrCompetitorDetailNotFound。
	GetCachedDetail(ctx context.Context, itemID string) (CompetitorDetail, error)
	// SaveDetail 写入详情缓存。
	SaveDetail(ctx context.Context, detail CompetitorDetail) error
	// IsFresh 判断缓存行是否仍有效（实现方持有抓取时间与 TTL）。
	IsFresh(ctx context.Context, itemID string) (bool, error)
}

// aiRewriter 是 AI 文案改写与分析边界。
type aiRewriter interface {
	// RewriteCopy 把竞品标题与描述改写为不同表达（保留卖点）。
	RewriteCopy(ctx context.Context, title, desc string) (RewrittenCopy, error)
	// AnalyzeSellingPoints 分析竞品商品的卖点与文案套路。
	AnalyzeSellingPoints(ctx context.Context, title, desc, price, wantCount string) (string, error)
}

// RewrittenCopy 是 AI 改写后的文案。
type RewrittenCopy struct {
	// Title 是改写后的标题。
	Title string `json:"title"`
	// Desc 是改写后的描述。
	Desc string `json:"desc"`
}

// DetailCacheRowAlias 是缓存行的应用层公开表示（由 adapter 转换）。
type DetailCacheRowAlias = detailCacheRow

// detailCacheRow 是缓存行的内部表示（由 adapter 转换）。
type detailCacheRow struct {
	// ItemID 是平台商品标识。
	ItemID string
	// Title 是商品标题。
	Title string
	// Price 是价格文本。
	Price string
	// DescText 是描述纯文本。
	DescText string
	// ImagesJSON 是图片 URL 列表 JSON。
	ImagesJSON string
	// SellerNickname 是卖家昵称。
	SellerNickname string
	// SellerID 是卖家 userId。
	SellerID string
	// WantCountText 是想要人数文本。
	WantCountText string
	// FetchedAt 是抓取时间。
	FetchedAt time.Time
}

// detailCacheStore 是缓存持久化边界（由 adapter 实现行与视图的转换）。
type detailCacheStore interface {
	// LoadDetailRow 读取缓存行；不存在返回 not-found 错误。
	LoadDetailRow(ctx context.Context, itemID string) (detailCacheRow, error)
	// SaveDetailRow 写入缓存行。
	SaveDetailRow(ctx context.Context, row detailCacheRow) error
}

// GetCompetitorDetail 返回竞品详情：7 天内缓存直接复用，未命中实时抓取并落库。
func (s *Service) GetCompetitorDetail(ctx context.Context, itemID string) (CompetitorDetail, error) {
	// cacheStore 断言缓存能力。
	cacheStore, cacheOK := s.scan.(detailCacheStore)
	if cacheOK {
		// row 是缓存行。
		row, err := cacheStore.LoadDetailRow(ctx, itemID)
		if err == nil && time.Since(row.FetchedAt) < CompetitorDetailCacheTTL {
			// detail 是缓存命中的详情。
			detail := CompetitorDetail{
				ItemID: row.ItemID, Title: row.Title, Price: row.Price,
				DescText: row.DescText, SellerNickname: row.SellerNickname,
				SellerID: row.SellerID, WantCountText: row.WantCountText, Cached: true,
			}
			// images 是图片列表。
			if err := json.Unmarshal([]byte(row.ImagesJSON), &detail.Images); err != nil {
				detail.Images = []string{}
			}
			return detail, nil
		}
	}
	// 无详情抓取能力时明确报错。
	if s.detail == nil {
		return CompetitorDetail{}, errors.New("浏览器不可用，无法查看竞品详情")
	}
	// detail 是实时抓取结果。
	detail, err := s.detail.FetchCompetitorDetail(ctx, itemID)
	if err != nil {
		return CompetitorDetail{}, err
	}
	detail.Cached = false
	// 落库缓存（失败只记日志，不影响返回）。
	if cacheOK {
		// imagesJSON 是图片列表 JSON。
		imagesJSON, _ := json.Marshal(detail.Images)
		// saveErr 是缓存写入错误。
		if saveErr := cacheStore.SaveDetailRow(ctx, detailCacheRow{
			ItemID: detail.ItemID, Title: detail.Title, Price: detail.Price,
			DescText: detail.DescText, ImagesJSON: string(imagesJSON),
			SellerNickname: detail.SellerNickname, SellerID: detail.SellerID,
			WantCountText: detail.WantCountText, FetchedAt: time.Now(),
		}); saveErr != nil {
			s.logger.Warn("竞品详情缓存写入失败", "item_id", itemID, "err", saveErr)
		}
	}
	return detail, nil
}

// RewriteCompetitorCopy 用市场情报模型改写竞品文案。
func (s *Service) RewriteCompetitorCopy(ctx context.Context, title, desc string) (RewrittenCopy, error) {
	// rewriter 断言 AI 能力。
	rewriter, ok := s.verdict.(aiRewriter)
	if !ok {
		return RewrittenCopy{}, ErrAINotConfigured
	}
	return rewriter.RewriteCopy(ctx, title, desc)
}

// AnalyzeCompetitorSellingPoints 用市场情报模型分析竞品卖点。
func (s *Service) AnalyzeCompetitorSellingPoints(ctx context.Context, title, desc, price, wantCount string) (string, error) {
	// rewriter 断言 AI 能力。
	rewriter, ok := s.verdict.(aiRewriter)
	if !ok {
		return "", ErrAINotConfigured
	}
	return rewriter.AnalyzeSellingPoints(ctx, title, desc, price, wantCount)
}

// ErrCompetitorDetailRiskControl 表示查看竞品详情时平台要求人工完成验证。
// 详情场景要求用户立即在闲鱼网页完成验证，因此不能复用「今日扫描已中止，请明天再试」的扫描文案。
var ErrCompetitorDetailRiskControl = errors.New("闲鱼返回风控验证要求，请先在闲鱼网页手动完成验证后再试")

// ErrCompetitorDetailSessionExpired 表示查看竞品详情时账号登录态已失效。
var ErrCompetitorDetailSessionExpired = errors.New("闲鱼登录态已失效，请重新登录该账号")

// ErrLocalFetchUnavailable 表示本机抓取助手不可用（家用机休眠、隧道断开或助手未启动）。
// 该错误必须立即返回：服务器侧浏览器抓取已被平台确定性判罚，回退只会浪费时间并加重风控。
var ErrLocalFetchUnavailable = errors.New("本机抓取助手未连接，请确认家用机上的抓取助手与隧道已启动")

// ErrLocalFetchUnauthorized 表示服务器与助手之间的共享令牌不一致。
var ErrLocalFetchUnauthorized = errors.New("本机抓取助手令牌不正确，请核对两侧配置")

// ErrLocalFetchNotLoggedIn 表示助手浏览器尚未登录闲鱼。
var ErrLocalFetchNotLoggedIn = errors.New("本机抓取助手的浏览器尚未登录闲鱼，请在该浏览器登录后重试")

// ErrLocalFetchRiskControl 表示助手抓取时平台仍要求人机验证。
var ErrLocalFetchRiskControl = errors.New("闲鱼要求完成人机验证，请在本机抓取助手的浏览器中完成验证后重试")

// ErrAINotConfigured 表示市场情报模型未配置。
var ErrAINotConfigured = errors.New("市场情报模型未配置，请先在设置页填写 API Key")

// trimCopyFields 去除文案字段首尾空白。
func trimCopyFields(title, desc string) (string, string) {
	return strings.TrimSpace(title), strings.TrimSpace(desc)
}
