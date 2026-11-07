// Package server: 市场情报 transport 端口——消费者侧窄接口定义与组合根投影。
package server

import (
	"context"

	marketintel "xianyu-go/internal/application/marketintel"
)

// MarketIntelPort 定义市场情报的词表管理、扫描触发与查询视图能力。
// 消费者接口只暴露 HTTP 端点实际调用的方法，由组合根投影具体 Service 实现。
type MarketIntelPort interface {
	// ListKeywords 返回全部关注词。
	ListKeywords(ctx context.Context) ([]marketintel.WatchedKeyword, error)
	// CreateKeyword 新增关注词；启用词达到上限时返回 ErrKeywordLimitReached。
	CreateKeyword(ctx context.Context, keyword string, enabled bool) (marketintel.WatchedKeyword, error)
	// UpdateKeyword 更新关注词文本与启用状态。
	UpdateKeyword(ctx context.Context, id int64, keyword string, enabled bool) error
	// DeleteKeyword 删除关注词。
	DeleteKeyword(ctx context.Context, id int64) error
	// RunDailyScan 执行每日扫描（幂等）。
	RunDailyScan(ctx context.Context) (marketintel.ScanResult, error)
	// LatestAggregates 返回各词最新聚合摘要。
	LatestAggregates(ctx context.Context) ([]marketintel.AggregateView, error)
	// KeywordTrend 返回单词趋势曲线。
	KeywordTrend(ctx context.Context, keywordID int64, days int) (marketintel.TrendView, error)
	// SellerBoard 返回竞品卖家榜。
	SellerBoard(ctx context.Context, days int) (marketintel.SellerBoardView, error)
	// EnsureSeedKeywords 在词表为空时写入种子词（幂等）。
	EnsureSeedKeywords(ctx context.Context) error
	// GetCompetitorDetail 返回竞品详情（7 天缓存，未命中实时抓取）。
	GetCompetitorDetail(ctx context.Context, itemID string) (marketintel.CompetitorDetail, error)
	// RewriteCompetitorCopy 用 AI 改写竞品文案。
	RewriteCompetitorCopy(ctx context.Context, title, desc string) (marketintel.RewrittenCopy, error)
	// AnalyzeCompetitorSellingPoints 用 AI 分析竞品卖点。
	AnalyzeCompetitorSellingPoints(ctx context.Context, title, desc, price, wantCount string) (string, error)
	// CarryCompetitorItem 执行一键搬运：文案改写 + 图片入资源库，返回预填数据。
	CarryCompetitorItem(ctx context.Context, userID int64, itemID string) (marketintel.CarryResult, error)
	// ListKeywordItems 返回某词最新一天的快照商品列表。
	ListKeywordItems(ctx context.Context, keyword string) ([]marketintel.ItemSummary, error)
}

// marketIntelApplication 返回当前 Server 绑定的市场情报用例端口；未装配时返回 nil。
func (s *Server) marketIntelApplication() MarketIntelPort {
	return s.applicationServiceSet().marketIntel
}

// marketIntelKeywordProjector 把具体 Service 投影为 MarketIntelPort；Service 天然满足接口。
type marketIntelKeywordProjector struct {
	// service 是组合根装配的市场情报应用服务。
	service *marketintel.Service
	// carry 是组合根装配的竞品一键搬运用例服务；未装配时搬运端点明确降级。
	carry *marketintel.CarryService
}

// ListKeywords 返回全部关注词。
func (p marketIntelKeywordProjector) ListKeywords(ctx context.Context) ([]marketintel.WatchedKeyword, error) {
	return p.service.ListKeywords(ctx)
}

// CreateKeyword 新增关注词。
func (p marketIntelKeywordProjector) CreateKeyword(ctx context.Context, keyword string, enabled bool) (marketintel.WatchedKeyword, error) {
	return p.service.CreateKeyword(ctx, keyword, enabled)
}

// UpdateKeyword 更新关注词。
func (p marketIntelKeywordProjector) UpdateKeyword(ctx context.Context, id int64, keyword string, enabled bool) error {
	return p.service.UpdateKeyword(ctx, id, keyword, enabled)
}

// DeleteKeyword 删除关注词。
func (p marketIntelKeywordProjector) DeleteKeyword(ctx context.Context, id int64) error {
	return p.service.DeleteKeyword(ctx, id)
}

// RunDailyScan 执行每日扫描。
func (p marketIntelKeywordProjector) RunDailyScan(ctx context.Context) (marketintel.ScanResult, error) {
	return p.service.RunDailyScan(ctx)
}

// LatestAggregates 返回各词最新聚合摘要。
func (p marketIntelKeywordProjector) LatestAggregates(ctx context.Context) ([]marketintel.AggregateView, error) {
	return p.service.LatestAggregates(ctx)
}

// KeywordTrend 返回单词趋势曲线。
func (p marketIntelKeywordProjector) KeywordTrend(ctx context.Context, keywordID int64, days int) (marketintel.TrendView, error) {
	return p.service.KeywordTrend(ctx, keywordID, days)
}

// SellerBoard 返回竞品卖家榜。
func (p marketIntelKeywordProjector) SellerBoard(ctx context.Context, days int) (marketintel.SellerBoardView, error) {
	return p.service.SellerBoard(ctx, days)
}

// EnsureSeedKeywords 在词表为空时写入种子词。
func (p marketIntelKeywordProjector) EnsureSeedKeywords(ctx context.Context) error {
	return p.service.EnsureSeedKeywords(ctx)
}

// GetCompetitorDetail 返回竞品详情。
func (p marketIntelKeywordProjector) GetCompetitorDetail(ctx context.Context, itemID string) (marketintel.CompetitorDetail, error) {
	return p.service.GetCompetitorDetail(ctx, itemID)
}

// RewriteCompetitorCopy 用 AI 改写竞品文案。
func (p marketIntelKeywordProjector) RewriteCompetitorCopy(ctx context.Context, title, desc string) (marketintel.RewrittenCopy, error) {
	return p.service.RewriteCompetitorCopy(ctx, title, desc)
}

// AnalyzeCompetitorSellingPoints 用 AI 分析竞品卖点。
func (p marketIntelKeywordProjector) AnalyzeCompetitorSellingPoints(ctx context.Context, title, desc, price, wantCount string) (string, error) {
	return p.service.AnalyzeCompetitorSellingPoints(ctx, title, desc, price, wantCount)
}

// CarryCompetitorItem 执行一键搬运；搬运用例未装配时返回 ErrImageLibraryUnavailable。
func (p marketIntelKeywordProjector) CarryCompetitorItem(ctx context.Context, userID int64, itemID string) (marketintel.CarryResult, error) {
	if p.carry == nil {
		return marketintel.CarryResult{}, marketintel.ErrImageLibraryUnavailable
	}
	return p.carry.CarryCompetitorItem(ctx, userID, itemID)
}

// ListKeywordItems 返回某词最新一天的快照商品列表。
func (p marketIntelKeywordProjector) ListKeywordItems(ctx context.Context, keyword string) ([]marketintel.ItemSummary, error) {
	return p.service.ListKeywordItems(ctx, keyword)
}

// MarketIntelTransportPort 把具体 Service 与搬运用例投影为 transport 端口；nil 服务返回 nil 接口。
func MarketIntelTransportPort(service *marketintel.Service, carry *marketintel.CarryService) MarketIntelPort {
	if service == nil {
		return nil
	}
	return marketIntelKeywordProjector{service: service, carry: carry}
}
