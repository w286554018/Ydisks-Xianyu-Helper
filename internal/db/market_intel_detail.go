// Package db: 竞品商品详情缓存仓储——按需抓取的详情 7 天内复用。
package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CompetitorDetailCacheTTL 是竞品详情的缓存有效期；7 天内不重复抓取。
const CompetitorDetailCacheTTL = 7 * 24 * time.Hour

// ErrCompetitorDetailNotFound 表示该商品尚无缓存详情。
var ErrCompetitorDetailNotFound = errors.New("竞品详情不存在")

// MarketIntelItemDetailRow 是一条竞品商品详情缓存行。
type MarketIntelItemDetailRow struct {
	// ItemID 是平台商品标识。
	ItemID string
	// Title 是商品标题。
	Title string
	// Price 是价格文本。
	Price string
	// DescText 是商品描述纯文本。
	DescText string
	// ImagesJSON 是商品图片 URL 列表 JSON。
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

// GetMarketIntelItemDetail 读取商品详情缓存；不存在返回 ErrCompetitorDetailNotFound。
func (m *MarketIntel) GetItemDetail(ctx context.Context, itemID string) (MarketIntelItemDetailRow, error) {
	if m == nil || m.DB == nil {
		return MarketIntelItemDetailRow{}, errors.New("市场情报仓储未初始化")
	}
	// row 是查询结果行。
	var row MarketIntelItemDetailRow
	// fetchedAt 是抓取时间文本。
	var fetchedAt sql.NullString
	// err 是查询错误。
	err := m.DB.QueryRowContext(ctx,
		`SELECT item_id, title, price, desc_text, images_json, seller_nickname, seller_id, want_count_text, fetched_at
		 FROM market_intel_item_details WHERE item_id=?`, itemID).
		Scan(&row.ItemID, &row.Title, &row.Price, &row.DescText, &row.ImagesJSON,
			&row.SellerNickname, &row.SellerID, &row.WantCountText, &fetchedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MarketIntelItemDetailRow{}, ErrCompetitorDetailNotFound
		}
		return MarketIntelItemDetailRow{}, err
	}
	if fetchedAt.Valid {
		row.FetchedAt = parseMarketIntelTime(fetchedAt.String)
	}
	return row, nil
}

// IsFresh 判断缓存行是否仍在 7 天有效期内。
func (r MarketIntelItemDetailRow) IsFresh(now time.Time) bool {
	if r.FetchedAt.IsZero() {
		return false
	}
	return now.Sub(r.FetchedAt) < CompetitorDetailCacheTTL
}

// SaveItemDetail 写入或覆盖商品详情缓存。
func (m *MarketIntel) SaveItemDetail(ctx context.Context, row MarketIntelItemDetailRow) error {
	if m == nil || m.DB == nil {
		return errors.New("市场情报仓储未初始化")
	}
	switch m.Dialect {
	case DialectMySQL:
		// execErr 是 MySQL 分支的写入错误。
		if _, execErr := m.DB.ExecContext(ctx,
			`INSERT INTO market_intel_item_details (item_id, title, price, desc_text, images_json, seller_nickname, seller_id, want_count_text, fetched_at)
			 VALUES (?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)
			 ON DUPLICATE KEY UPDATE title=VALUES(title), price=VALUES(price), desc_text=VALUES(desc_text), images_json=VALUES(images_json), seller_nickname=VALUES(seller_nickname), seller_id=VALUES(seller_id), want_count_text=VALUES(want_count_text), fetched_at=CURRENT_TIMESTAMP`,
			row.ItemID, row.Title, row.Price, row.DescText, row.ImagesJSON, row.SellerNickname, row.SellerID, row.WantCountText); execErr != nil {
			return execErr
		}
	case DialectPostgres:
		// execErr 是 Postgres 分支的写入错误。
		if _, execErr := m.DB.ExecContext(ctx,
			`INSERT INTO market_intel_item_details (item_id, title, price, desc_text, images_json, seller_nickname, seller_id, want_count_text, fetched_at)
			 VALUES (?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)
			 ON CONFLICT (item_id) DO UPDATE SET title=EXCLUDED.title, price=EXCLUDED.price, desc_text=EXCLUDED.desc_text, images_json=EXCLUDED.images_json, seller_nickname=EXCLUDED.seller_nickname, seller_id=EXCLUDED.seller_id, want_count_text=EXCLUDED.want_count_text, fetched_at=CURRENT_TIMESTAMP`,
			row.ItemID, row.Title, row.Price, row.DescText, row.ImagesJSON, row.SellerNickname, row.SellerID, row.WantCountText); execErr != nil {
			return execErr
		}
	default:
		// execErr 是 SQLite 分支的写入错误。
		if _, execErr := m.DB.ExecContext(ctx,
			`INSERT INTO market_intel_item_details (item_id, title, price, desc_text, images_json, seller_nickname, seller_id, want_count_text, fetched_at)
			 VALUES (?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)
			 ON CONFLICT(item_id) DO UPDATE SET title=excluded.title, price=excluded.price, desc_text=excluded.desc_text, images_json=excluded.images_json, seller_nickname=excluded.seller_nickname, seller_id=excluded.seller_id, want_count_text=excluded.want_count_text, fetched_at=CURRENT_TIMESTAMP`,
			row.ItemID, row.Title, row.Price, row.DescText, row.ImagesJSON, row.SellerNickname, row.SellerID, row.WantCountText); execErr != nil {
			return execErr
		}
	}
	return nil
}
