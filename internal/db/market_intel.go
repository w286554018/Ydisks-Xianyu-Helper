package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 市场情报聚合的判定状态取值。
const (
	// MarketIntelVerdictPending 表示该词当日尚无 AI 判定结果。
	MarketIntelVerdictPending = "pending"
	// MarketIntelVerdictDone 表示该词当日已完成 AI 判定并聚合。
	MarketIntelVerdictDone = "done"
)

// 市场情报领域错误。
var (
	// ErrMarketIntelKeywordNotFound 表示关注词不存在。
	ErrMarketIntelKeywordNotFound = errors.New("关注词不存在")
	// ErrMarketIntelKeywordDuplicate 表示关注词已存在。
	ErrMarketIntelKeywordDuplicate = errors.New("关注词已存在")
)

// MarketIntelKeyword 是一条关注词记录。
type MarketIntelKeyword struct {
	// ID 是关注词主键。
	ID int64
	// Keyword 是搜索词文本。
	Keyword string
	// Enabled 表示该词是否参与每日扫描。
	Enabled bool
	// SortOrder 是展示排序值，越小越靠前。
	SortOrder int
	// CreatedAt 是创建时间。
	CreatedAt time.Time
}

// MarketIntelSnapshotRow 是一条商品快照行。
type MarketIntelSnapshotRow struct {
	// ID 是快照主键。
	ID int64
	// Keyword 是采集该快照的关注词。
	Keyword string
	// SnapshotDate 是快照归属日，格式 YYYY-MM-DD。
	SnapshotDate string
	// ItemID 是平台商品标识。
	ItemID string
	// Title 是商品标题。
	Title string
	// Price 是价格文本。
	Price string
	// WantCount 是解析后的想要人数；解析失败为 0。
	WantCount int
	// WantCountText 是页面原始想要人数文本；解析失败时保留原文。
	WantCountText string
	// SellerNickname 是卖家昵称。
	SellerNickname string
	// PublishTime 是发布时间文本。
	PublishTime string
	// RawJSON 是搜索结果原始 JSON 片段，供回溯。
	RawJSON string
	// CreatedAt 是写入时间。
	CreatedAt time.Time
}

// MarketIntelVerdictRow 是一条 AI 判定行。
type MarketIntelVerdictRow struct {
	// SnapshotID 是对应的快照主键。
	SnapshotID int64
	// IsAIRelated 表示该商品是否为 AI 相关产品。
	IsAIRelated bool
	// Category 是细分类目；非 AI 商品为空。
	Category string
	// Reason 是判定理由摘要。
	Reason string
	// CreatedAt 是写入时间。
	CreatedAt time.Time
}

// MarketIntelAggregateRow 是某词某天的聚合统计行。
type MarketIntelAggregateRow struct {
	// ID 是聚合主键。
	ID int64
	// Keyword 是关注词。
	Keyword string
	// SnapshotDate 是归属日。
	SnapshotDate string
	// TotalCount 是当日快照商品总数。
	TotalCount int
	// AICount 是 AI 相关商品条数。
	AICount int
	// TotalWant 是当日全部商品 want 数总和。
	TotalWant int
	// CategoryJSON 是 AI 相关商品的类目分布 JSON。
	CategoryJSON string
	// TopSellersJSON 是头部卖家统计 JSON。
	TopSellersJSON string
	// VerdictStatus 是判定状态：pending 或 done。
	VerdictStatus string
	// CreatedAt 是写入时间。
	CreatedAt time.Time
}

// MarketIntel 是市场情报四张表的持久化仓储。
type MarketIntel struct {
	// DB 是仓储使用的数据库连接池，由组合根注入。
	DB *sql.DB
	// Dialect 标识当前数据库方言，用于 upsert 语法分支。
	Dialect Dialect
}

// ListKeywords 返回全部关注词，按 sort_order、id 排序。
func (m *MarketIntel) ListKeywords(ctx context.Context) ([]MarketIntelKeyword, error) {
	if m == nil || m.DB == nil {
		return nil, errors.New("市场情报仓储未初始化")
	}
	// rows 是查询结果集，必须由调用方关闭。
	rows, err := m.DB.QueryContext(ctx,
		`SELECT id, keyword, enabled, sort_order, created_at FROM market_intel_keywords ORDER BY sort_order, id`)
	if err != nil {
		return nil, fmt.Errorf("读取关注词失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// out 是关注词集合。
	out := make([]MarketIntelKeyword, 0)
	for rows.Next() {
		// item 是当前行。
		var item MarketIntelKeyword
		// enabledInt 是启用标记整数。
		var enabledInt int
		// createdAt 是创建时间文本。
		var createdAt sql.NullString
		// err 是本次数据库操作错误。
		if err := rows.Scan(&item.ID, &item.Keyword, &enabledInt, &item.SortOrder, &createdAt); err != nil {
			return nil, fmt.Errorf("读取关注词失败: %w", err)
		}
		item.Enabled = enabledInt == 1
		if createdAt.Valid {
			item.CreatedAt = parseMarketIntelTime(createdAt.String)
		}
		out = append(out, item)
	}
	// err 是本次数据库操作错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取关注词失败: %w", err)
	}
	return out, nil
}

// CreateKeyword 新增关注词；重复时返回 ErrMarketIntelKeywordDuplicate。
func (m *MarketIntel) CreateKeyword(ctx context.Context, keyword string, enabled bool, sortOrder int) (int64, error) {
	if m == nil || m.DB == nil {
		return 0, errors.New("市场情报仓储未初始化")
	}
	// enabledInt 是布尔转整数。
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	// id 与 err 是写入结果；Postgres 走 RETURNING id，SQLite/MySQL 走 LastInsertId。
	id, err := insertReturningID(ctx, m.DB, m.Dialect,
		`INSERT INTO market_intel_keywords (keyword, enabled, sort_order) VALUES (?, ?, ?)`,
		keyword, enabledInt, sortOrder)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrMarketIntelKeywordDuplicate
		}
		return 0, fmt.Errorf("新增关注词失败: %w", err)
	}
	return id, nil
}

// UpdateKeyword 更新关注词的文本、启用状态与排序；0 行更新返回 ErrMarketIntelKeywordNotFound。
func (m *MarketIntel) UpdateKeyword(ctx context.Context, id int64, keyword string, enabled bool, sortOrder int) error {
	if m == nil || m.DB == nil {
		return errors.New("市场情报仓储未初始化")
	}
	// enabledInt 是布尔转整数。
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	// result 是更新结果。
	result, err := m.DB.ExecContext(ctx,
		`UPDATE market_intel_keywords SET keyword=?, enabled=?, sort_order=? WHERE id=?`,
		keyword, enabledInt, sortOrder, id)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrMarketIntelKeywordDuplicate
		}
		return fmt.Errorf("更新关注词失败: %w", err)
	}
	// affected 是更新行数。
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取更新行数失败: %w", err)
	}
	if affected == 0 {
		return ErrMarketIntelKeywordNotFound
	}
	return nil
}

// DeleteKeyword 删除关注词；0 行删除返回 ErrMarketIntelKeywordNotFound。
func (m *MarketIntel) DeleteKeyword(ctx context.Context, id int64) error {
	if m == nil || m.DB == nil {
		return errors.New("市场情报仓储未初始化")
	}
	// result 是删除结果。
	result, err := m.DB.ExecContext(ctx, `DELETE FROM market_intel_keywords WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("删除关注词失败: %w", err)
	}
	// affected 是删除行数。
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取删除行数失败: %w", err)
	}
	if affected == 0 {
		return ErrMarketIntelKeywordNotFound
	}
	return nil
}

// ListEnabledKeywords 返回全部启用中的关注词，按 sort_order、id 排序。
func (m *MarketIntel) ListEnabledKeywords(ctx context.Context) ([]MarketIntelKeyword, error) {
	if m == nil || m.DB == nil {
		return nil, errors.New("市场情报仓储未初始化")
	}
	// rows 是查询结果集，必须由调用方关闭。
	rows, err := m.DB.QueryContext(ctx,
		`SELECT id, keyword, enabled, sort_order, created_at FROM market_intel_keywords WHERE enabled=1 ORDER BY sort_order, id`)
	if err != nil {
		return nil, fmt.Errorf("读取启用关注词失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// out 是关注词集合。
	out := make([]MarketIntelKeyword, 0)
	for rows.Next() {
		// item 是当前行。
		var item MarketIntelKeyword
		// enabledInt 是启用标记整数。
		var enabledInt int
		// createdAt 是创建时间文本。
		var createdAt sql.NullString
		// err 是本次数据库操作错误。
		if err := rows.Scan(&item.ID, &item.Keyword, &enabledInt, &item.SortOrder, &createdAt); err != nil {
			return nil, fmt.Errorf("读取启用关注词失败: %w", err)
		}
		item.Enabled = enabledInt == 1
		if createdAt.Valid {
			item.CreatedAt = parseMarketIntelTime(createdAt.String)
		}
		out = append(out, item)
	}
	// err 是本次数据库操作错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取启用关注词失败: %w", err)
	}
	return out, nil
}

// SaveSnapshots 批量写入商品快照，返回写入行数。
func (m *MarketIntel) SaveSnapshots(ctx context.Context, rows []MarketIntelSnapshotRow) (int64, error) {
	if m == nil || m.DB == nil {
		return 0, errors.New("市场情报仓储未初始化")
	}
	// saved 累计写入行数。
	var saved int64
	// row 是当前待写入的快照行。
	for _, row := range rows {
		// result 是单行写入结果。
		result, err := m.DB.ExecContext(ctx,
			`INSERT INTO market_intel_snapshots (keyword, snapshot_date, item_id, title, price, want_count, want_count_text, seller_nickname, publish_time, raw_json) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			row.Keyword, row.SnapshotDate, row.ItemID, row.Title, row.Price, row.WantCount, row.WantCountText, row.SellerNickname, row.PublishTime, row.RawJSON)
		if err != nil {
			return saved, fmt.Errorf("写入商品快照失败: %w", err)
		}
		// affected 是本行写入行数。
		affected, err := result.RowsAffected()
		if err != nil {
			return saved, fmt.Errorf("读取快照写入行数失败: %w", err)
		}
		saved += affected
	}
	return saved, nil
}

// SaveVerdicts 批量写入判定结果，按快照主键 upsert。
func (m *MarketIntel) SaveVerdicts(ctx context.Context, rows []MarketIntelVerdictRow) error {
	if m == nil || m.DB == nil {
		return errors.New("市场情报仓储未初始化")
	}
	// row 是当前待写入的判定行。
	for _, row := range rows {
		// aiInt 是布尔转整数。
		aiInt := 0
		if row.IsAIRelated {
			aiInt = 1
		}
		// err 是单行 upsert 结果。
		if err := m.upsertVerdict(ctx, row.SnapshotID, aiInt, row.Category, row.Reason); err != nil {
			return err
		}
	}
	return nil
}

// upsertVerdict 按方言分支写入单条判定；重复快照主键时覆盖。
func (m *MarketIntel) upsertVerdict(ctx context.Context, snapshotID int64, aiInt int, category, reason string) error {
	switch m.Dialect {
	case DialectMySQL:
		// execErr 是单条写入错误。
		if _, execErr := m.DB.ExecContext(ctx,
			`INSERT INTO market_intel_verdicts (snapshot_id, is_ai_related, category, reason) VALUES (?,?,?,?)
			 ON DUPLICATE KEY UPDATE is_ai_related=VALUES(is_ai_related), category=VALUES(category), reason=VALUES(reason)`,
			snapshotID, aiInt, category, reason); execErr != nil {
			return fmt.Errorf("写入判定失败: %w", execErr)
		}
	case DialectPostgres:
		// execErr 是单条写入错误。
		if _, execErr := m.DB.ExecContext(ctx,
			`INSERT INTO market_intel_verdicts (snapshot_id, is_ai_related, category, reason) VALUES (?,?,?,?)
			 ON CONFLICT (snapshot_id) DO UPDATE SET is_ai_related=EXCLUDED.is_ai_related, category=EXCLUDED.category, reason=EXCLUDED.reason`,
			snapshotID, aiInt, category, reason); execErr != nil {
			return fmt.Errorf("写入判定失败: %w", execErr)
		}
	default:
		// execErr 是单条写入错误。
		if _, execErr := m.DB.ExecContext(ctx,
			`INSERT INTO market_intel_verdicts (snapshot_id, is_ai_related, category, reason) VALUES (?,?,?,?)
			 ON CONFLICT(snapshot_id) DO UPDATE SET is_ai_related=excluded.is_ai_related, category=excluded.category, reason=excluded.reason`,
			snapshotID, aiInt, category, reason); execErr != nil {
			return fmt.Errorf("写入判定失败: %w", execErr)
		}
	}
	return nil
}

// SaveAggregate 写入或覆盖某词某日的聚合行。
func (m *MarketIntel) SaveAggregate(ctx context.Context, row MarketIntelAggregateRow) error {
	if m == nil || m.DB == nil {
		return errors.New("市场情报仓储未初始化")
	}
	switch m.Dialect {
	case DialectMySQL:
		// execErr 是单条写入错误。
		if _, execErr := m.DB.ExecContext(ctx,
			`INSERT INTO market_intel_aggregates (keyword, snapshot_date, total_count, ai_count, total_want, category_json, top_sellers_json, verdict_status) VALUES (?,?,?,?,?,?,?,?)
			 ON DUPLICATE KEY UPDATE total_count=VALUES(total_count), ai_count=VALUES(ai_count), total_want=VALUES(total_want), category_json=VALUES(category_json), top_sellers_json=VALUES(top_sellers_json), verdict_status=VALUES(verdict_status)`,
			row.Keyword, row.SnapshotDate, row.TotalCount, row.AICount, row.TotalWant, row.CategoryJSON, row.TopSellersJSON, row.VerdictStatus); execErr != nil {
			return fmt.Errorf("写入聚合失败: %w", execErr)
		}
	case DialectPostgres:
		// execErr 是单条写入错误。
		if _, execErr := m.DB.ExecContext(ctx,
			`INSERT INTO market_intel_aggregates (keyword, snapshot_date, total_count, ai_count, total_want, category_json, top_sellers_json, verdict_status) VALUES (?,?,?,?,?,?,?,?)
			 ON CONFLICT (keyword, snapshot_date) DO UPDATE SET total_count=EXCLUDED.total_count, ai_count=EXCLUDED.ai_count, total_want=EXCLUDED.total_want, category_json=EXCLUDED.category_json, top_sellers_json=EXCLUDED.top_sellers_json, verdict_status=EXCLUDED.verdict_status`,
			row.Keyword, row.SnapshotDate, row.TotalCount, row.AICount, row.TotalWant, row.CategoryJSON, row.TopSellersJSON, row.VerdictStatus); execErr != nil {
			return fmt.Errorf("写入聚合失败: %w", execErr)
		}
	default:
		// execErr 是单条写入错误。
		if _, execErr := m.DB.ExecContext(ctx,
			`INSERT INTO market_intel_aggregates (keyword, snapshot_date, total_count, ai_count, total_want, category_json, top_sellers_json, verdict_status) VALUES (?,?,?,?,?,?,?,?)
			 ON CONFLICT(keyword, snapshot_date) DO UPDATE SET total_count=excluded.total_count, ai_count=excluded.ai_count, total_want=excluded.total_want, category_json=excluded.category_json, top_sellers_json=excluded.top_sellers_json, verdict_status=excluded.verdict_status`,
			row.Keyword, row.SnapshotDate, row.TotalCount, row.AICount, row.TotalWant, row.CategoryJSON, row.TopSellersJSON, row.VerdictStatus); execErr != nil {
			return fmt.Errorf("写入聚合失败: %w", execErr)
		}
	}
	return nil
}

// ListAggregates 返回某词最近 N 天的聚合行，按日期升序，供趋势曲线使用。
func (m *MarketIntel) ListAggregates(ctx context.Context, keyword string, limitDays int) ([]MarketIntelAggregateRow, error) {
	if m == nil || m.DB == nil {
		return nil, errors.New("市场情报仓储未初始化")
	}
	// rows 是查询结果集，必须由调用方关闭。
	rows, err := m.DB.QueryContext(ctx,
		`SELECT id, keyword, snapshot_date, total_count, ai_count, total_want, category_json, top_sellers_json, verdict_status, created_at
		 FROM (SELECT * FROM market_intel_aggregates WHERE keyword=? ORDER BY snapshot_date DESC LIMIT ?) sub
		 ORDER BY snapshot_date ASC`, keyword, limitDays)
	if err != nil {
		return nil, fmt.Errorf("读取聚合序列失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// out 是聚合行集合。
	out := make([]MarketIntelAggregateRow, 0)
	for rows.Next() {
		// item 是当前行。
		var item MarketIntelAggregateRow
		// createdAt 是创建时间文本。
		var createdAt sql.NullString
		// err 是本次数据库操作错误。
		if err := rows.Scan(&item.ID, &item.Keyword, &item.SnapshotDate, &item.TotalCount, &item.AICount, &item.TotalWant, &item.CategoryJSON, &item.TopSellersJSON, &item.VerdictStatus, &createdAt); err != nil {
			return nil, fmt.Errorf("读取聚合序列失败: %w", err)
		}
		if createdAt.Valid {
			item.CreatedAt = parseMarketIntelTime(createdAt.String)
		}
		out = append(out, item)
	}
	// err 是本次数据库操作错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取聚合序列失败: %w", err)
	}
	return out, nil
}

// ListLatestAggregates 返回每个关注词最新一天的聚合行，供概览使用。
func (m *MarketIntel) ListLatestAggregates(ctx context.Context) ([]MarketIntelAggregateRow, error) {
	if m == nil || m.DB == nil {
		return nil, errors.New("市场情报仓储未初始化")
	}
	// rows 是查询结果集，必须由调用方关闭。
	rows, err := m.DB.QueryContext(ctx,
		`SELECT a.id, a.keyword, a.snapshot_date, a.total_count, a.ai_count, a.total_want, a.category_json, a.top_sellers_json, a.verdict_status, a.created_at
		 FROM market_intel_aggregates a
		 JOIN (SELECT keyword, MAX(snapshot_date) AS max_date FROM market_intel_aggregates GROUP BY keyword) latest
		   ON a.keyword=latest.keyword AND a.snapshot_date=latest.max_date
		 ORDER BY a.total_want DESC`)
	if err != nil {
		return nil, fmt.Errorf("读取最新聚合失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// out 是聚合行集合。
	out := make([]MarketIntelAggregateRow, 0)
	for rows.Next() {
		// item 是当前行。
		var item MarketIntelAggregateRow
		// createdAt 是创建时间文本。
		var createdAt sql.NullString
		// err 是本次数据库操作错误。
		if err := rows.Scan(&item.ID, &item.Keyword, &item.SnapshotDate, &item.TotalCount, &item.AICount, &item.TotalWant, &item.CategoryJSON, &item.TopSellersJSON, &item.VerdictStatus, &createdAt); err != nil {
			return nil, fmt.Errorf("读取最新聚合失败: %w", err)
		}
		if createdAt.Valid {
			item.CreatedAt = parseMarketIntelTime(createdAt.String)
		}
		out = append(out, item)
	}
	// err 是本次数据库操作错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取最新聚合失败: %w", err)
	}
	return out, nil
}

// ListSnapshotsWithVerdicts 返回某词某天的快照与判定（LEFT JOIN），供卖家榜聚合使用。
func (m *MarketIntel) ListSnapshotsWithVerdicts(ctx context.Context, keyword, snapshotDate string) ([]MarketIntelSnapshotRow, []MarketIntelVerdictRow, error) {
	if m == nil || m.DB == nil {
		return nil, nil, errors.New("市场情报仓储未初始化")
	}
	// rows 是联查结果集，必须由调用方关闭。
	rows, err := m.DB.QueryContext(ctx,
		`SELECT s.id, s.keyword, s.snapshot_date, s.item_id, s.title, s.price, s.want_count, s.want_count_text, s.seller_nickname, s.publish_time, s.raw_json, s.created_at,
		        v.is_ai_related, v.category, v.reason, v.created_at
		 FROM market_intel_snapshots s
		 LEFT JOIN market_intel_verdicts v ON v.snapshot_id=s.id
		 WHERE s.keyword=? AND s.snapshot_date=?
		 ORDER BY s.want_count DESC`, keyword, snapshotDate)
	if err != nil {
		return nil, nil, fmt.Errorf("读取快照与判定失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// snapshots 与 verdicts 分别收集快照与判定。
	snapshots := make([]MarketIntelSnapshotRow, 0)
	// verdicts 是判定行集合。
	verdicts := make([]MarketIntelVerdictRow, 0)
	for rows.Next() {
		// snapshot 是当前快照行。
		var snapshot MarketIntelSnapshotRow
		// aiInt 是判定标记；NULL 表示无判定。
		var aiInt sql.NullInt64
		// category、reason 是判定文本字段。
		var category, reason sql.NullString
		// createdAt 是快照写入时间文本。
		var createdAt sql.NullString
		// verdictCreatedAt 是判定写入时间文本。
		var verdictCreatedAt sql.NullString
		// err 是本次数据库操作错误。
		if err := rows.Scan(&snapshot.ID, &snapshot.Keyword, &snapshot.SnapshotDate, &snapshot.ItemID, &snapshot.Title, &snapshot.Price, &snapshot.WantCount, &snapshot.WantCountText, &snapshot.SellerNickname, &snapshot.PublishTime, &snapshot.RawJSON, &createdAt,
			&aiInt, &category, &reason, &verdictCreatedAt); err != nil {
			return nil, nil, fmt.Errorf("读取快照与判定失败: %w", err)
		}
		if createdAt.Valid {
			snapshot.CreatedAt = parseMarketIntelTime(createdAt.String)
		}
		snapshots = append(snapshots, snapshot)
		if aiInt.Valid {
			// verdict 是当前判定行。
			verdict := MarketIntelVerdictRow{SnapshotID: snapshot.ID, Category: category.String, Reason: reason.String}
			verdict.IsAIRelated = aiInt.Int64 == 1
			if verdictCreatedAt.Valid {
				verdict.CreatedAt = parseMarketIntelTime(verdictCreatedAt.String)
			}
			verdicts = append(verdicts, verdict)
		}
	}
	// err 是本次数据库操作错误。
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("读取快照与判定失败: %w", err)
	}
	return snapshots, verdicts, nil
}

// SnapshotVerdictExists 判断某快照是否已有判定结果，供补判定幂等检查。
func (m *MarketIntel) SnapshotVerdictExists(ctx context.Context, snapshotID int64) (bool, error) {
	if m == nil || m.DB == nil {
		return false, errors.New("市场情报仓储未初始化")
	}
	// count 是判定行数。
	var count int
	// err 是本次数据库操作错误。
	if err := m.DB.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM market_intel_verdicts WHERE snapshot_id=?`, snapshotID).Scan(&count); err != nil {
		return false, fmt.Errorf("查询判定存在性失败: %w", err)
	}
	return count > 0, nil
}

// DeleteSnapshotsBefore 删除早于指定日期的商品快照（判定级联删除），返回删除行数。
func (m *MarketIntel) DeleteSnapshotsBefore(ctx context.Context, beforeDate string) (int64, error) {
	if m == nil || m.DB == nil {
		return 0, errors.New("市场情报仓储未初始化")
	}
	// result 是删除结果。
	result, err := m.DB.ExecContext(ctx,
		`DELETE FROM market_intel_snapshots WHERE snapshot_date < ?`, beforeDate)
	if err != nil {
		return 0, fmt.Errorf("清理历史快照失败: %w", err)
	}
	// affected 是删除行数。
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("读取清理行数失败: %w", err)
	}
	return affected, nil
}

// CountKeywords 返回关注词总数与启用数，供上限校验。
func (m *MarketIntel) CountKeywords(ctx context.Context) (total int, enabled int, err error) {
	if m == nil || m.DB == nil {
		return 0, 0, errors.New("市场情报仓储未初始化")
	}
	// totalInt、enabledInt 是计数结果。
	var totalInt, enabledInt int
	// err 是本次数据库操作错误。
	if err := m.DB.QueryRowContext(ctx,
		`SELECT COUNT(1), COALESCE(SUM(enabled),0) FROM market_intel_keywords`).Scan(&totalInt, &enabledInt); err != nil {
		return 0, 0, fmt.Errorf("统计关注词失败: %w", err)
	}
	return totalInt, enabledInt, nil
}

// parseMarketIntelTime 把数据库时间文本解析为 time.Time；空串与非法格式返回零值。
func parseMarketIntelTime(raw string) time.Time {
	// trimmed 是去空白后的时间文本。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}
	}
	// layouts 是 SQLite TIMESTAMP 的常见存储格式。
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04:05.999999999", time.RFC3339}
	// layout 遍历尝试解析。
	for _, layout := range layouts {
		// parsed 是解析结果。
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// trimSnapshotDate 校验日期格式，防止脏日期进入索引。
func trimSnapshotDate(date string) string {
	return strings.TrimSpace(date)
}

// ListSnapshotsInRangeWithVerdicts 返回日期范围内全部快照与判定（跨词），供卖家榜聚合。
func (m *MarketIntel) ListSnapshotsInRangeWithVerdicts(ctx context.Context, startDate, endDate string) ([]MarketIntelSnapshotRow, []MarketIntelVerdictRow, error) {
	if m == nil || m.DB == nil {
		return nil, nil, errors.New("市场情报仓储未初始化")
	}
	// rows 是联查结果集，必须由调用方关闭。
	rows, err := m.DB.QueryContext(ctx,
		`SELECT s.id, s.keyword, s.snapshot_date, s.item_id, s.title, s.price, s.want_count, s.want_count_text, s.seller_nickname, s.publish_time, s.raw_json, s.created_at,
		        v.is_ai_related, v.category, v.reason, v.created_at
		 FROM market_intel_snapshots s
		 LEFT JOIN market_intel_verdicts v ON v.snapshot_id=s.id
		 WHERE s.snapshot_date >= ? AND s.snapshot_date <= ?`, startDate, endDate)
	if err != nil {
		return nil, nil, fmt.Errorf("读取范围快照失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// snapshots 与 verdicts 是结果集合。
	snapshots := make([]MarketIntelSnapshotRow, 0)
	// verdicts 是判定行集合。
	verdicts := make([]MarketIntelVerdictRow, 0)
	for rows.Next() {
		// snapshot 是当前快照行。
		var snapshot MarketIntelSnapshotRow
		// aiInt 是判定标记；NULL 表示无判定。
		var aiInt sql.NullInt64
		// category、reason 是判定文本字段。
		var category, reason sql.NullString
		// createdAt 是快照写入时间文本。
		var createdAt sql.NullString
		// verdictCreatedAt 是判定写入时间文本。
		var verdictCreatedAt sql.NullString
		// scanErr 表示当前行字段类型与表结构不一致。
		if scanErr := rows.Scan(&snapshot.ID, &snapshot.Keyword, &snapshot.SnapshotDate, &snapshot.ItemID, &snapshot.Title, &snapshot.Price, &snapshot.WantCount, &snapshot.WantCountText, &snapshot.SellerNickname, &snapshot.PublishTime, &snapshot.RawJSON, &createdAt,
			&aiInt, &category, &reason, &verdictCreatedAt); scanErr != nil {
			return nil, nil, fmt.Errorf("读取范围快照失败: %w", scanErr)
		}
		if createdAt.Valid {
			snapshot.CreatedAt = parseMarketIntelTime(createdAt.String)
		}
		snapshots = append(snapshots, snapshot)
		if aiInt.Valid {
			// verdict 是当前判定行。
			verdict := MarketIntelVerdictRow{SnapshotID: snapshot.ID, Category: category.String, Reason: reason.String}
			verdict.IsAIRelated = aiInt.Int64 == 1
			if verdictCreatedAt.Valid {
				verdict.CreatedAt = parseMarketIntelTime(verdictCreatedAt.String)
			}
			verdicts = append(verdicts, verdict)
		}
	}
	// rowsErr 是遍历结果集错误。
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, nil, fmt.Errorf("读取范围快照失败: %w", rowsErr)
	}
	return snapshots, verdicts, nil
}

// LatestSnapshotDate 返回某词最新快照日；无快照返回空串。
func (m *MarketIntel) LatestSnapshotDate(ctx context.Context, keyword string) (string, error) {
	if m == nil || m.DB == nil {
		return "", errors.New("市场情报仓储未初始化")
	}
	// date 是最新快照日。
	var date sql.NullString
	// err 是查询错误。
	if err := m.DB.QueryRowContext(ctx,
		`SELECT MAX(snapshot_date) FROM market_intel_snapshots WHERE keyword=?`, keyword).Scan(&date); err != nil {
		return "", fmt.Errorf("读取最新快照日失败: %w", err)
	}
	return date.String, nil
}
