package server

import (
	"context"

	sourcingapp "xianyu-go/internal/application/sourcing"
)

// SourcingPort 定义选品参考的热词、关键词查货、本店畅销榜与 AI 文案能力。
type SourcingPort interface {
	// HotKeywords 返回当日平台热词快照。
	HotKeywords(context.Context) (sourcingapp.HotKeywordView, error)
	// RefreshHotKeywords 手动触发一次热词采集。
	RefreshHotKeywords(context.Context) error
	// SearchItems 以账号登录态执行单次关键词查询。
	SearchItems(context.Context, int64, string, string) ([]sourcingapp.SearchItem, error)
	// ShopRanking 按当前用户实时返回本店畅销榜。
	ShopRanking(context.Context, int64, string) (sourcingapp.ShopRankingView, error)
	// GenerateCopywriting 生成指定模式的 AI 文案。
	GenerateCopywriting(context.Context, int64, sourcingapp.CopywritingRequest) (sourcingapp.Copywriting, error)
	// Momentum 返回热词涨幅榜（窗口内名次变化对比）。
	Momentum(context.Context) (sourcingapp.MomentumSummary, error)
}
