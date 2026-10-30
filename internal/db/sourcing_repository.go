package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 选品参考快照的类别标识；热词与本店畅销榜共用一张表，靠该字段区分。
const (
	// SourcingKindHotKeywords 表示平台搜索热词快照。
	SourcingKindHotKeywords = "hot_keywords"
	// SourcingKindShopRanking 表示本店畅销榜快照。
	SourcingKindShopRanking = "shop_ranking"
	// SourcingSnapshotStatusOK 表示该日快照采集成功。
	SourcingSnapshotStatusOK = "ok"
	// SourcingSnapshotStatusFailed 表示该日快照采集失败，失败原因在 error_message 中。
	SourcingSnapshotStatusFailed = "failed"
	// sourcingSnapshotRetentionDays 是快照保留天数，超出部分在写入后按日清理。
	sourcingSnapshotRetentionDays = 30
)

// ErrSourcingSnapshotNotFound 表示指定类别与日期的快照尚不存在。
var ErrSourcingSnapshotNotFound = errors.New("选品参考快照不存在")

// SourcingSnapshot 是选品参考的单日快照记录，不含任何凭证或敏感字段。
type SourcingSnapshot struct {
	// Kind 是快照类别，取 SourcingKindHotKeywords 或 SourcingKindShopRanking。
	Kind string
	// SnapshotDate 是快照归属日，格式 YYYY-MM-DD，按北京时间划分。
	SnapshotDate string
	// PayloadJSON 是快照的结构化结果，由应用层序列化后写入。
	PayloadJSON string
	// Status 是采集结果，取 SourcingSnapshotStatusOK 或 SourcingSnapshotStatusFailed。
	Status string
	// ErrorMessage 是失败原因的脱敏摘要，成功时为空字符串。
	ErrorMessage string
	// CreatedAt 是快照写入时间，用于运维排查，不参与业务判断。
	CreatedAt time.Time
}

// SourcingOrderRow 是畅销榜聚合前的原始订单行；聚合在应用层完成以保持三种方言口径一致。
type SourcingOrderRow struct {
	// ItemID 是平台商品标识，可能为空（历史数据缺失时）。
	ItemID string
	// ItemTitle 是本地商品标题，商品记录缺失时为空。
	ItemTitle string
	// Quantity 是订单数量的原始文本，可能不是合法数字，聚合时按 0 处理。
	Quantity string
	// Amount 是订单金额的原始文本，可能不是合法数字，聚合时按 0 处理。
	Amount string
	// OrderStatus 是订单状态文本，应用层据此过滤未付款订单。
	OrderStatus string
}

// Sourcing 是选品参考快照与本店畅销榜的持久化仓储。
type Sourcing struct {
	// DB 是仓储使用的数据库连接池，由组合根注入。
	DB *sql.DB
	// Dialect 标识当前数据库方言，用于 upsert 语法分支。
	Dialect Dialect
}

// SaveSnapshot 写入或覆盖某类别某日的快照；同一 (kind, snapshot_date) 只保留一条。
func (s *Sourcing) SaveSnapshot(ctx context.Context, snapshot SourcingSnapshot) error {
	if s == nil || s.DB == nil {
		return errors.New("选品参考仓储未初始化")
	}
	if strings.TrimSpace(snapshot.Kind) == "" || strings.TrimSpace(snapshot.SnapshotDate) == "" {
		return errors.New("选品参考快照缺少类别或日期")
	}
	// status 归一化：未知状态按失败处理，避免把异常结果当成成功快照。
	status := snapshot.Status
	if status != SourcingSnapshotStatusOK && status != SourcingSnapshotStatusFailed {
		status = SourcingSnapshotStatusFailed
	}
	// createdAt 是当前写入的 Unix 秒时间戳，供运维排查使用。
	createdAt := snapshot.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	// query 是方言相关的 upsert 语句；三方言统一使用 ? 占位符，由驱动层负责重写。
	var query string
	switch s.Dialect {
	case DialectMySQL:
		query = `INSERT INTO sourcing_snapshots (kind, snapshot_date, payload_json, status, error_message, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE payload_json=VALUES(payload_json), status=VALUES(status), error_message=VALUES(error_message), created_at=VALUES(created_at)`
	default:
		query = `INSERT INTO sourcing_snapshots (kind, snapshot_date, payload_json, status, error_message, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(kind, snapshot_date) DO UPDATE SET payload_json=excluded.payload_json, status=excluded.status, error_message=excluded.error_message, created_at=excluded.created_at`
	}
	// err 保存快照写入失败的错误；调用方负责记录并降级，不阻断其它功能。
	if _, err := s.DB.ExecContext(ctx, query,
		snapshot.Kind, snapshot.SnapshotDate, snapshot.PayloadJSON, status, snapshot.ErrorMessage, createdAt.Unix(),
	); err != nil {
		return fmt.Errorf("写入选品参考快照失败: %w", err)
	}
	return nil
}

// GetSnapshot 读取某类别某日的最新快照；不存在时返回 ErrSourcingSnapshotNotFound。
func (s *Sourcing) GetSnapshot(ctx context.Context, kind, snapshotDate string) (*SourcingSnapshot, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("选品参考仓储未初始化")
	}
	// row 保存单行查询结果，扫描失败时由下列判断转换错误。
	row := s.DB.QueryRowContext(ctx,
		`SELECT kind, snapshot_date, payload_json, status, error_message, created_at FROM sourcing_snapshots WHERE kind=? AND snapshot_date=?`,
		kind, snapshotDate)
	// snapshot 保存解码后的快照记录（CreatedAt 在扫描后按 Unix 秒还原）。
	snapshot := &SourcingSnapshot{}
	// createdAtUnix 保存数据库中的 Unix 秒时间戳，只在扫描阶段使用。
	var createdAtUnix int64
	// err 保存扫描错误；sql.ErrNoRows 转换为领域错误便于上层区分"没有快照"与"读取失败"。
	if err := row.Scan(&snapshot.Kind, &snapshot.SnapshotDate, &snapshot.PayloadJSON, &snapshot.Status, &snapshot.ErrorMessage, &createdAtUnix); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSourcingSnapshotNotFound
		}
		return nil, fmt.Errorf("读取选品参考快照失败: %w", err)
	}
	snapshot.CreatedAt = time.Unix(createdAtUnix, 0)
	return snapshot, nil
}

// ListRecentSnapshots 按日期倒序返回指定类别最近的若干条快照，用于趋势展示。
func (s *Sourcing) ListRecentSnapshots(ctx context.Context, kind string, limit int) ([]SourcingSnapshot, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("选品参考仓储未初始化")
	}
	if limit <= 0 {
		limit = sourcingSnapshotRetentionDays
	}
	// rows 是快照查询结果集，必须由调用方关闭。
	rows, err := s.DB.QueryContext(ctx,
		`SELECT kind, snapshot_date, payload_json, status, error_message, created_at FROM sourcing_snapshots WHERE kind=? ORDER BY snapshot_date DESC LIMIT ?`,
		kind, limit)
	if err != nil {
		return nil, fmt.Errorf("读取选品参考快照列表失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// out 汇总按日期倒序的快照记录。
	out := make([]SourcingSnapshot, 0, limit)
	for rows.Next() {
		// item 是当前遍历到的快照记录。
		var item SourcingSnapshot
		// createdAtUnix 是当前行的时间戳。
		var createdAtUnix int64
		// scanErr 表示当前快照行字段类型与表结构不一致。
		if scanErr := rows.Scan(&item.Kind, &item.SnapshotDate, &item.PayloadJSON, &item.Status, &item.ErrorMessage, &createdAtUnix); scanErr != nil {
			return nil, fmt.Errorf("读取选品参考快照列表失败: %w", scanErr)
		}
		item.CreatedAt = time.Unix(createdAtUnix, 0)
		out = append(out, item)
	}
	// rowsErr 是遍历结果集过程中发生的驱动或连接错误。
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("读取选品参考快照列表失败: %w", rowsErr)
	}
	return out, nil
}

// DeleteSnapshotsBefore 删除早于指定日期的全部快照，返回删除条数；用于 30 天保留策略。
func (s *Sourcing) DeleteSnapshotsBefore(ctx context.Context, beforeDate string) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("选品参考仓储未初始化")
	}
	if strings.TrimSpace(beforeDate) == "" {
		return 0, errors.New("清理选品参考快照需要截止日期")
	}
	// result 保存删除操作结果，用于返回受影响行数。
	result, err := s.DB.ExecContext(ctx, `DELETE FROM sourcing_snapshots WHERE snapshot_date < ?`, beforeDate)
	if err != nil {
		return 0, fmt.Errorf("清理选品参考快照失败: %w", err)
	}
	// affected 是本次删除的条数；驱动不支持时按 0 返回，不影响清理语义。
	affected, affectedErr := result.RowsAffected()
	if affectedErr != nil {
		return 0, nil
	}
	return affected, nil
}

// ShopRankingRows 返回指定用户名下账号在给定时间点之后的订单原始行，用于应用层聚合畅销榜。
// 该查询只读本地数据库，不产生任何平台出站请求。
func (s *Sourcing) ShopRankingRows(ctx context.Context, userID int64, since time.Time) ([]SourcingOrderRow, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("选品参考仓储未初始化")
	}
	// query 按账号归属与创建时间过滤订单，并左连本地商品表补标题；软删除订单与隐私字段不参与。
	// 时间参数使用项目统一的 UTC 文本格式，与 analytics 的 DateBoundary 口径一致，避免方言差异。
	query := `SELECT o.item_id, COALESCE(i.item_title,''), COALESCE(o.quantity,''), COALESCE(o.amount,''), COALESCE(o.order_status,'')
FROM orders o
JOIN cookies c ON c.id = o.cookie_id
LEFT JOIN item_info i ON i.cookie_id = o.cookie_id AND i.item_id = o.item_id
WHERE c.user_id = ? AND o.deleted_at IS NULL AND o.created_at >= ?`
	// sinceText 是数据库可直接比较的 UTC 时间文本；订单 created_at 由 CURRENT_TIMESTAMP 以 UTC 写入。
	sinceText := since.UTC().Format("2006-01-02 15:04:05")
	// rows 是订单原始行结果集，必须由调用方关闭。
	rows, err := s.DB.QueryContext(ctx, query, userID, sinceText)
	if err != nil {
		return nil, fmt.Errorf("读取本店订单失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// out 汇总订单原始行。
	out := make([]SourcingOrderRow, 0, 64)
	for rows.Next() {
		// row 是当前遍历到的订单原始行。
		var row SourcingOrderRow
		// scanErr 表示当前订单行字段无法读取。
		if scanErr := rows.Scan(&row.ItemID, &row.ItemTitle, &row.Quantity, &row.Amount, &row.OrderStatus); scanErr != nil {
			return nil, fmt.Errorf("读取本店订单失败: %w", scanErr)
		}
		out = append(out, row)
	}
	// rowsErr 是遍历订单结果集过程中发生的驱动或连接错误。
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("读取本店订单失败: %w", rowsErr)
	}
	return out, nil
}

// ParseSourcingDecimal 把订单中的文本金额或数量转换为浮点数；非数字内容按 0 处理。
// 该函数在应用层聚合时使用，避免在三种方言里分别实现不安全的 CAST。
func ParseSourcingDecimal(raw string) float64 {
	// trimmed 是去空白后的原始文本，空值直接按 0 处理。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	// value 是解析后的浮点值；解析失败说明文本不是数字，按 0 计入聚合。
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// SourcingSnapshotCutoffDate 返回保留策略允许的最早快照日期（含当天共保留 30 天）。
func SourcingSnapshotCutoffDate(now time.Time, location *time.Location) string {
	if location == nil {
		location = time.UTC
	}
	// cutoff 是保留窗口的起始日，早于该日期的快照会被清理。
	cutoff := now.In(location).AddDate(0, 0, -(sourcingSnapshotRetentionDays - 1))
	return cutoff.Format("2006-01-02")
}
