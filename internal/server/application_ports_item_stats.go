// Package server: 我的商品统计端口定义与访问器。
package server

import (
	"context"

	itemapp "xianyu-go/internal/application/items"
)

// ItemStatsPort 定义我的商品统计刷新与读取用例。
type ItemStatsPort interface {
	// RefreshItemStats 强制刷新单个商品的统计（忽略缓存）。
	RefreshItemStats(ctx context.Context, userID int64, cookieID, itemID string) (itemapp.ItemStats, error)
	// RefreshStatsBatch 执行一轮限量批量刷新；上限由应用层强制。
	RefreshStatsBatch(ctx context.Context, userID int64, cookieID string, itemIDs []string, limit int) (itemapp.StatsBatchResult, error)
	// ListItemStatsForItems 按账号分组读取统计，供商品列表附带展示。
	ListItemStatsForItems(ctx context.Context, refs []itemapp.ItemStatsRef) (map[string]itemapp.ItemStats, error)
}

// itemStatsApplication 返回我的商品统计用例。
func (server *Server) itemStatsApplication() ItemStatsPort {
	return server.applicationServiceSet().itemStats
}
