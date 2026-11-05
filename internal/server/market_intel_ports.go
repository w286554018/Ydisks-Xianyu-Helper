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
}

// marketIntelApplication 返回当前 Server 绑定的市场情报用例端口；未装配时返回 nil。
func (s *Server) marketIntelApplication() MarketIntelPort {
	return s.applicationServiceSet().marketIntel
}

// marketIntelKeywordProjector 把具体 Service 投影为 MarketIntelPort；Service 天然满足接口。
type marketIntelKeywordProjector struct {
	// service 是组合根装配的市场情报应用服务。
	service *marketintel.Service
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

// MarketIntelTransportPort 把具体 Service 投影为 transport 端口；nil 服务返回 nil 接口。
func MarketIntelTransportPort(service *marketintel.Service) MarketIntelPort {
	if service == nil {
		return nil
	}
	return marketIntelKeywordProjector{service: service}
}
