package marketintel

import (
	"context"
	"fmt"
)

// AggregateView 是聚合行的对外视图。
type AggregateView struct {
	// Keyword 是关注词。
	Keyword string `json:"keyword"`
	// Date 是归属日。
	Date string `json:"date"`
	// TotalCount 是当日商品总数。
	TotalCount int `json:"total_count"`
	// AICount 是 AI 相关条数。
	AICount int `json:"ai_count"`
	// TotalWant 是想要人数总和。
	TotalWant int `json:"total_want"`
	// VerdictStatus 是判定状态：pending/done。
	VerdictStatus string `json:"verdict_status"`
}

// TrendPoint 是趋势曲线的单点。
type TrendPoint struct {
	// Date 是归属日。
	Date string `json:"date"`
	// TotalWant 是当日 want 总数。
	TotalWant int `json:"total_want"`
	// AICount 是 AI 相关条数。
	AICount int `json:"ai_count"`
	// TotalCount 是当日商品总数。
	TotalCount int `json:"total_count"`
}

// TrendView 是单词趋势视图。
type TrendView struct {
	// KeywordID 是关注词主键。
	KeywordID int64 `json:"keyword_id"`
	// Keyword 是关注词文本。
	Keyword string `json:"keyword"`
	// Days 是窗口天数。
	Days int `json:"days"`
	// Points 是按日期升序的趋势点。
	Points []TrendPoint `json:"points"`
}

// SellerBoardView 是竞品卖家榜视图。
type SellerBoardView struct {
	// Days 是统计窗口天数。
	Days int `json:"days"`
	// Sellers 是按出现次数降序的卖家统计。
	Sellers []SellerStat `json:"sellers"`
}

// queryPersistence 是查询视图需要的持久化窄接口（由 adapter 实现，与 scanPersistence 分离以便测试）。
type queryPersistence interface {
	// ListLatestAggregates 返回每个关注词最新一天的聚合。
	ListLatestAggregates(ctx context.Context) ([]AggregateView, error)
	// ListAggregates 返回某词最近 N 天的聚合（升序）。
	ListAggregates(ctx context.Context, keyword string, limitDays int) ([]AggregateView, error)
	// ListSellerStats 返回窗口天数内按卖家聚合的统计（降序）。
	ListSellerStats(ctx context.Context, days int) ([]SellerStat, error)
}

// LatestAggregates 返回各关注词最新一天的聚合摘要。
func (s *Service) LatestAggregates(ctx context.Context) ([]AggregateView, error) {
	// qp 是查询持久化边界。
	qp, ok := s.scan.(queryPersistence)
	if !ok {
		return []AggregateView{}, nil
	}
	return qp.ListLatestAggregates(ctx)
}

// KeywordTrend 返回单词的趋势曲线；词不存在时返回 ErrKeywordNotFound。
func (s *Service) KeywordTrend(ctx context.Context, keywordID int64, days int) (TrendView, error) {
	// keywords 是全部词，用于找目标词文本。
	keywords, err := s.store.ListKeywords(ctx)
	if err != nil {
		return TrendView{}, err
	}
	// keywordText 是目标词文本。
	keywordText := ""
	// keyword 遍历查找。
	for _, keyword := range keywords {
		if keyword.ID == keywordID {
			keywordText = keyword.Keyword
			break
		}
	}
	if keywordText == "" {
		return TrendView{}, ErrKeywordNotFound
	}
	// qp 是查询持久化边界。
	qp, ok := s.scan.(queryPersistence)
	if !ok {
		return TrendView{KeywordID: keywordID, Keyword: keywordText, Days: days, Points: []TrendPoint{}}, nil
	}
	// aggregates 是窗口内的聚合序列。
	// aggregates 是窗口内的聚合序列；listErr 是读取错误。
	aggregates, listErr := qp.ListAggregates(ctx, keywordText, days)
	if listErr != nil {
		return TrendView{}, listErr
	}
	// points 是趋势点集合。
	points := make([]TrendPoint, 0, len(aggregates))
	// aggregate 是当前聚合行。
	for _, aggregate := range aggregates {
		points = append(points, TrendPoint{
			Date: aggregate.Date, TotalWant: aggregate.TotalWant,
			AICount: aggregate.AICount, TotalCount: aggregate.TotalCount,
		})
	}
	return TrendView{KeywordID: keywordID, Keyword: keywordText, Days: days, Points: points}, nil
}

// SellerBoard 返回窗口天数内的竞品卖家榜。
func (s *Service) SellerBoard(ctx context.Context, days int) (SellerBoardView, error) {
	// qp 是查询持久化边界。
	qp, ok := s.scan.(queryPersistence)
	if !ok {
		return SellerBoardView{Days: days, Sellers: []SellerStat{}}, nil
	}
	// sellers 是卖家统计。
	sellers, err := qp.ListSellerStats(ctx, days)
	if err != nil {
		return SellerBoardView{}, fmt.Errorf("读取卖家统计失败: %w", err)
	}
	if sellers == nil {
		sellers = []SellerStat{}
	}
	return SellerBoardView{Days: days, Sellers: sellers}, nil
}
