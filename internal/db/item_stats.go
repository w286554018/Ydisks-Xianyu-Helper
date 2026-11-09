// Package db: 我的商品统计仓储——按 (账号, 商品) 保存平台公开的浏览/想要/收藏/已售计数。
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrItemStatsNotFound 表示该 (账号, 商品) 尚无统计数据；调用方应按「未抓取」处理，而不是当作故障。
var ErrItemStatsNotFound = errors.New("商品统计不存在")

// ItemStatsRow 是一条我的商品统计行；只保存详情页公开计数，不含任何平台凭证。
type ItemStatsRow struct {
	// CookieID 是统计所属账号标识，用于区分同一商品在不同账号下的归属。
	CookieID string
	// ItemID 是平台商品标识。
	ItemID string
	// BrowseCnt 是详情返回的浏览量；平台未提供该字段时按 0 处理。
	BrowseCnt int64
	// WantCnt 是详情返回的想要人数；平台未提供该字段时按 0 处理。
	WantCnt int64
	// CollectCnt 是详情返回的收藏数；平台未提供该字段时按 0 处理。
	CollectCnt int64
	// SoldCnt 是详情返回的已售数；平台未提供该字段时按 0 处理。
	SoldCnt int64
	// FetchedAt 是最近一次成功抓取时间；零值表示从未抓取或时间解析失败，两种情形都按过期处理。
	FetchedAt time.Time
}

// IsFresh 判断统计是否仍在 ttl 有效期内；从未抓取（零值时间）一律视为不新鲜。
func (r ItemStatsRow) IsFresh(now time.Time, ttl time.Duration) bool {
	if r.FetchedAt.IsZero() {
		return false
	}
	return now.Sub(r.FetchedAt) < ttl
}

// ItemStats 保存我的商品统计仓储；按 (cookie_id, item_id) 唯一，支持跨库 upsert。
type ItemStats struct {
	// DB 是仓储使用的数据库连接池，由组合根注入。
	DB *sql.DB
	// Dialect 标识当前数据库方言，用于 upsert 语法分支。
	Dialect Dialect
}

// GetItemStats 读取单个商品统计；不存在返回 ErrItemStatsNotFound，调用方可据此走首次抓取。
func (s *ItemStats) GetItemStats(ctx context.Context, cookieID, itemID string) (ItemStatsRow, error) {
	if s == nil || s.DB == nil {
		return ItemStatsRow{}, errors.New("商品统计仓储未初始化")
	}
	// trimmedCookieID、trimmedItemID 是去空白后的查询键，避免前后空格导致查不到已存在的记录。
	trimmedCookieID, trimmedItemID := strings.TrimSpace(cookieID), strings.TrimSpace(itemID)
	if trimmedCookieID == "" || trimmedItemID == "" {
		return ItemStatsRow{}, ErrItemStatsNotFound
	}
	// row 是查询命中的统计行。
	var row ItemStatsRow
	// fetchedAt 是抓取时间文本；跨驱动读取时按可空文本接收，再统一解析。
	var fetchedAt sql.NullString
	// err 是本次查询错误；sql.ErrNoRows 需要转换成仓储自有错误语义。
	err := s.DB.QueryRowContext(ctx,
		`SELECT cookie_id, item_id, browse_cnt, want_cnt, collect_cnt, sold_cnt, fetched_at
		 FROM item_stats WHERE cookie_id=? AND item_id=?`, trimmedCookieID, trimmedItemID).
		Scan(&row.CookieID, &row.ItemID, &row.BrowseCnt, &row.WantCnt, &row.CollectCnt, &row.SoldCnt, &fetchedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ItemStatsRow{}, ErrItemStatsNotFound
		}
		return ItemStatsRow{}, fmt.Errorf("读取商品统计失败: %w", err)
	}
	if fetchedAt.Valid {
		row.FetchedAt = parseMarketIntelTime(fetchedAt.String)
	}
	return row, nil
}

// ListItemStats 一次查出某账号下一批商品的统计，返回以商品标识索引的映射；
// 未命中的商品不会出现在结果里，调用方据此判断「未抓取」。
func (s *ItemStats) ListItemStats(ctx context.Context, cookieID string, itemIDs []string) (map[string]ItemStatsRow, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("商品统计仓储未初始化")
	}
	// result 是按商品标识索引的结果映射，保证调用方判空无需再遍历切片。
	result := make(map[string]ItemStatsRow)
	// trimmedCookieID 是去空白后的账号标识；为空时直接返回空结果，避免退化成跨账号全表查询。
	trimmedCookieID := strings.TrimSpace(cookieID)
	if trimmedCookieID == "" {
		return result, nil
	}
	// uniqueIDs 是去重后的商品标识集合，保证 IN 子句既不重复绑定也不因空值污染查询。
	uniqueIDs := make([]string, 0, len(itemIDs))
	// seen 记录已收集的商品标识，用于 O(1) 去重。
	seen := make(map[string]struct{}, len(itemIDs))
	// itemID 是本次待归一的商品标识，可能重复或为空串。
	for _, itemID := range itemIDs {
		// trimmed 是去空白后的商品标识。
		trimmed := strings.TrimSpace(itemID)
		if trimmed == "" {
			continue
		}
		// ok 表示该商品标识此前是否已收集；重复时必须跳过，避免 IN 子句重复绑定参数。
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		uniqueIDs = append(uniqueIDs, trimmed)
	}
	if len(uniqueIDs) == 0 {
		return result, nil
	}
	// placeholders 是 IN 子句的占位符串；本项目在三种方言下统一使用 ? 占位符（Postgres 由驱动改写）。
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(uniqueIDs)), ",")
	// args 是绑定参数，顺序与 placeholders 一致：账号在前，商品标识在后。
	args := make([]any, 0, len(uniqueIDs)+1)
	args = append(args, trimmedCookieID)
	// itemID 是本次绑定的商品标识，遍历顺序必须与 placeholders 一致。
	for _, itemID := range uniqueIDs {
		args = append(args, itemID)
	}
	// rows 是查询结果集，必须由调用方关闭。
	rows, err := s.DB.QueryContext(ctx,
		`SELECT cookie_id, item_id, browse_cnt, want_cnt, collect_cnt, sold_cnt, fetched_at
		 FROM item_stats WHERE cookie_id=? AND item_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("读取商品统计失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		// row 是当前统计行。
		var row ItemStatsRow
		// fetchedAt 是当前行的抓取时间文本。
		var fetchedAt sql.NullString
		if // scanErr 表示当前行字段类型与表结构不一致。
		scanErr := rows.Scan(&row.CookieID, &row.ItemID, &row.BrowseCnt, &row.WantCnt, &row.CollectCnt, &row.SoldCnt, &fetchedAt); scanErr != nil {
			return nil, fmt.Errorf("读取商品统计失败: %w", scanErr)
		}
		if fetchedAt.Valid {
			row.FetchedAt = parseMarketIntelTime(fetchedAt.String)
		}
		result[row.ItemID] = row
	}
	if // rowsErr 表示遍历结果集过程中发生的错误，必须显式暴露。
	rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("读取商品统计失败: %w", rowsErr)
	}
	return result, nil
}

// UpsertItemStats 按 (cookie_id, item_id) 写入或覆盖统计；成功抓取后必须调用，包括计数全为 0 的情况。
func (s *ItemStats) UpsertItemStats(ctx context.Context, row ItemStatsRow) error {
	if s == nil || s.DB == nil {
		return errors.New("商品统计仓储未初始化")
	}
	// cookieID、itemID 是去空白后的唯一键组成部分；任一为空都无法定位记录。
	cookieID, itemID := strings.TrimSpace(row.CookieID), strings.TrimSpace(row.ItemID)
	if cookieID == "" || itemID == "" {
		return errors.New("商品统计缺少账号或商品标识")
	}
	// fetchedAt 是写库用的抓取时间文本；跨库统一存成本地无时区文本，便于三种方言一致解析。
	fetchedAt := row.FetchedAt.UTC().Format("2006-01-02 15:04:05")
	if row.FetchedAt.IsZero() {
		fetchedAt = time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	switch s.Dialect {
	case DialectMySQL:
		// execErr 是 MySQL 分支的写入错误。
		if _, execErr := s.DB.ExecContext(ctx,
			`INSERT INTO item_stats (cookie_id, item_id, browse_cnt, want_cnt, collect_cnt, sold_cnt, fetched_at)
			 VALUES (?,?,?,?,?,?,?)
			 ON DUPLICATE KEY UPDATE browse_cnt=VALUES(browse_cnt), want_cnt=VALUES(want_cnt),
			 collect_cnt=VALUES(collect_cnt), sold_cnt=VALUES(sold_cnt), fetched_at=VALUES(fetched_at),
			 updated_at=CURRENT_TIMESTAMP`,
			cookieID, itemID, row.BrowseCnt, row.WantCnt, row.CollectCnt, row.SoldCnt, fetchedAt); execErr != nil {
			return fmt.Errorf("写入商品统计失败: %w", execErr)
		}
	case DialectPostgres:
		// execErr 是 Postgres 分支的写入错误。
		if _, execErr := s.DB.ExecContext(ctx,
			`INSERT INTO item_stats (cookie_id, item_id, browse_cnt, want_cnt, collect_cnt, sold_cnt, fetched_at)
			 VALUES (?,?,?,?,?,?,?)
			 ON CONFLICT (cookie_id, item_id) DO UPDATE SET browse_cnt=EXCLUDED.browse_cnt,
			 want_cnt=EXCLUDED.want_cnt, collect_cnt=EXCLUDED.collect_cnt, sold_cnt=EXCLUDED.sold_cnt,
			 fetched_at=EXCLUDED.fetched_at, updated_at=CURRENT_TIMESTAMP`,
			cookieID, itemID, row.BrowseCnt, row.WantCnt, row.CollectCnt, row.SoldCnt, fetchedAt); execErr != nil {
			return fmt.Errorf("写入商品统计失败: %w", execErr)
		}
	default:
		// execErr 是 SQLite 分支的写入错误。
		if _, execErr := s.DB.ExecContext(ctx,
			`INSERT INTO item_stats (cookie_id, item_id, browse_cnt, want_cnt, collect_cnt, sold_cnt, fetched_at)
			 VALUES (?,?,?,?,?,?,?)
			 ON CONFLICT(cookie_id, item_id) DO UPDATE SET browse_cnt=excluded.browse_cnt,
			 want_cnt=excluded.want_cnt, collect_cnt=excluded.collect_cnt, sold_cnt=excluded.sold_cnt,
			 fetched_at=excluded.fetched_at, updated_at=CURRENT_TIMESTAMP`,
			cookieID, itemID, row.BrowseCnt, row.WantCnt, row.CollectCnt, row.SoldCnt, fetchedAt); execErr != nil {
			return fmt.Errorf("写入商品统计失败: %w", execErr)
		}
	}
	return nil
}
