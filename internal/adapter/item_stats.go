// Package adapter: 我的商品统计仓储适配器——把持久层模型转换为应用层自有模型。
package adapter

import (
	"context"
	"errors"
	"strings"

	"xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// ItemStatsStore 把我的商品统计仓储适配为应用层端口；向上只暴露应用自有模型。
type ItemStatsStore struct {
	// store 是数据库仓储聚合；缺失时所有方法返回未初始化错误而不是 panic。
	store *db.Store
}

// NewItemStatsStore 构造商品统计仓储适配器。
func NewItemStatsStore(store *db.Store) *ItemStatsStore {
	return &ItemStatsStore{store: store}
}

// validate 校验适配器依赖在构造期已注入。
func (s *ItemStatsStore) validate() error {
	if s == nil || s.store == nil || s.store.ItemStats == nil {
		return errors.New("商品统计仓储适配器未初始化")
	}
	return nil
}

// ListItemStats 读取一批商品的统计并转换为应用层模型；未抓取的商品不出现在结果中。
func (s *ItemStatsStore) ListItemStats(ctx context.Context, cookieID string, itemIDs []string) (map[string]items.ItemStats, error) {
	if // validateErr 是适配器依赖校验错误。
	validateErr := s.validate(); validateErr != nil {
		return nil, validateErr
	}
	// rows、err 是持久层查询结果与错误。
	rows, err := s.store.ItemStats.ListItemStats(ctx, cookieID, itemIDs)
	if err != nil {
		return nil, err
	}
	// out 是转换后的应用层统计集合。
	out := make(map[string]items.ItemStats, len(rows))
	// itemID 是当前待转换的商品标识；row 是它的持久层统计行。
	for itemID, row := range rows {
		out[itemID] = itemStatsFromRow(row)
	}
	return out, nil
}

// UpsertItemStats 把应用层统计转换为持久层模型并写入。
func (s *ItemStatsStore) UpsertItemStats(ctx context.Context, stats items.ItemStats) error {
	if // validateErr 是适配器依赖校验错误。
	validateErr := s.validate(); validateErr != nil {
		return validateErr
	}
	return s.store.ItemStats.UpsertItemStats(ctx, db.ItemStatsRow{
		CookieID: strings.TrimSpace(stats.CookieID), ItemID: strings.TrimSpace(stats.ItemID),
		BrowseCnt: stats.BrowseCnt, WantCnt: stats.WantCnt, CollectCnt: stats.CollectCnt, SoldCnt: stats.SoldCnt,
		FetchedAt: stats.FetchedAt,
	})
}

// itemStatsFromRow 把持久层统计行转换为应用层模型；Fresh 由应用层按当前时间判定，这里保持零值。
func itemStatsFromRow(row db.ItemStatsRow) items.ItemStats {
	return items.ItemStats{
		CookieID: row.CookieID, ItemID: row.ItemID,
		BrowseCnt: row.BrowseCnt, WantCnt: row.WantCnt, CollectCnt: row.CollectCnt, SoldCnt: row.SoldCnt,
		FetchedAt: row.FetchedAt,
	}
}
