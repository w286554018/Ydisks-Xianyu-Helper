// Package db: 商品生命周期批次仓库——批量下架/删除任务的持久化与行级状态机。
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// 商品生命周期批次与行的状态取值。
const (
	// LifecycleActionDownshelf 表示批次动作为平台下架。
	LifecycleActionDownshelf = "downshelf"
	// LifecycleActionDelete 表示批次动作为平台删除。
	LifecycleActionDelete = "delete"

	// LifecycleBatchPending 表示批次已创建尚未执行。
	LifecycleBatchPending = "pending"
	// LifecycleBatchRunning 表示批次正在执行。
	LifecycleBatchRunning = "running"
	// LifecycleBatchCompleted 表示批次全部成功。
	LifecycleBatchCompleted = "completed"
	// LifecycleBatchPartiallyFailed 表示批次存在失败行。
	LifecycleBatchPartiallyFailed = "partially_failed"
	// LifecycleBatchCanceled 表示批次已被用户取消。
	LifecycleBatchCanceled = "canceled"

	// LifecycleRowPending 表示行待执行。
	LifecycleRowPending = "pending"
	// LifecycleRowRunning 表示行执行中。
	LifecycleRowRunning = "running"
	// LifecycleRowSuccess 表示行执行成功。
	LifecycleRowSuccess = "success"
	// LifecycleRowFailed 表示行执行失败。
	LifecycleRowFailed = "failed"

	// LifecycleMaxRows 是单个生命周期批次的行数上限，与批量铺货保持一致。
	LifecycleMaxRows = 50
)

// 生命周期批次行数与动作校验错误哨兵。
var (
	// ErrLifecycleInvalidAction 表示传入的批次动作不被支持。
	ErrLifecycleInvalidAction = errors.New("不支持的生命周期动作")
	// ErrLifecycleTooManyRows 表示批次行数超过上限。
	ErrLifecycleTooManyRows = errors.New("生命周期批次行数超过上限")
	// ErrLifecycleDuplicateRow 表示批次内存在重复的商品行。
	ErrLifecycleDuplicateRow = errors.New("生命周期批次内存在重复商品")
	// ErrLifecycleConflict 表示乐观并发校验失败（租约被他人接管或行已被处理）。
	ErrLifecycleConflict = errors.New("生命周期批次状态已变化")
)

// LifecycleBatch 是一条生命周期批次的持久化视图。
type LifecycleBatch struct {
	// ID 是批次唯一标识。
	ID string
	// UserID 是批次归属用户。
	UserID int64
	// Action 是批次动作：downshelf 或 delete。
	Action string
	// Status 是批次状态。
	Status string
	// TotalCount 是行总数。
	TotalCount int
	// SuccessCount 是成功行数。
	SuccessCount int
	// FailedCount 是失败行数。
	FailedCount int
	// CreatedAt 是创建时间。
	CreatedAt string
	// UpdatedAt 是最近更新时间。
	UpdatedAt string
}

// LifecycleRow 是批次中的一条目标商品。
type LifecycleRow struct {
	// ID 是行自增主键。
	ID int64
	// BatchID 是所属批次。
	BatchID string
	// RowNo 是批次内序号（1 起）。
	RowNo int
	// CookieID 是商品归属账号。
	CookieID string
	// ItemID 是平台商品 ID。
	ItemID string
	// Status 是行状态。
	Status string
	// ErrorMessage 是失败原因摘要。
	ErrorMessage string
	// FailureKind 是失败分类。
	FailureKind string
}

// scanLifecycleBatch 扫描单行批次记录；调用方负责行游标生命周期。
func scanLifecycleBatch(scan func(dest ...any) error) (LifecycleBatch, error) {
	// batch 保存扫描结果。
	var batch LifecycleBatch
	// err 保存事务开启错误。
	if err := scan(&batch.ID, &batch.UserID, &batch.Action, &batch.Status,
		&batch.TotalCount, &batch.SuccessCount, &batch.FailedCount, &batch.CreatedAt, &batch.UpdatedAt); err != nil {
		return LifecycleBatch{}, err
	}
	return batch, nil
}

// CreateBatch 创建生命周期批次并写入全部明细行；动作与行数在入库前校验。
func (b *ItemPublishBatches) CreateLifecycleBatch(ctx context.Context, userID int64, action string, items []LifecycleRow) (string, error) {
	if action != LifecycleActionDownshelf && action != LifecycleActionDelete {
		return "", ErrLifecycleInvalidAction
	}
	if len(items) == 0 || len(items) > LifecycleMaxRows {
		return "", ErrLifecycleTooManyRows
	}
	// seen 用于批次内去重；同一 (账号, 商品) 重复提交属于调用方逻辑错误。
	seen := make(map[string]struct{}, len(items))
	// i 是当前遍历的行下标。
	for i := range items {
		items[i].CookieID = strings.TrimSpace(items[i].CookieID)
		items[i].ItemID = strings.TrimSpace(items[i].ItemID)
		if items[i].CookieID == "" || items[i].ItemID == "" {
			return "", fmt.Errorf("第 %d 行缺少账号或商品 ID", i+1)
		}
		// key 是账号与商品组合的去重键。
		key := items[i].CookieID + "/" + items[i].ItemID
		// dup 标记当前键是否已出现过。
		if _, dup := seen[key]; dup {
			return "", ErrLifecycleDuplicateRow
		}
		seen[key] = struct{}{}
	}
	// batchID 由服务端生成，避免客户端注入。
	batchID := uuid.NewString()
	// tx 保证批次头与全部明细行原子落库。
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	// err 保存批次头写入错误。
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO item_lifecycle_batches (id, user_id, action, status, total_count) VALUES (?,?,?,?,?)`,
		batchID, userID, action, LifecycleBatchPending, len(items)); err != nil {
		return "", err
	}
	// err 保存明细行写入错误。
	for i := range items {
		// i 是当前写入行的序号偏移。
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item_lifecycle_batch_rows (batch_id, row_no, cookie_id, item_id, status) VALUES (?,?,?,?,?)`,
			batchID, i+1, items[i].CookieID, items[i].ItemID, LifecycleRowPending); err != nil {
			return "", err
		}
	}
	// err 保存明细行查询错误。
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return batchID, nil
}

// GetLifecycleBatch 按用户读取批次与全部明细行；跨用户读取返回 ErrNotFound。
func (b *ItemPublishBatches) GetLifecycleBatch(ctx context.Context, userID int64, batchID string) (LifecycleBatch, []LifecycleRow, error) {
	// batch 保存批次头查询结果。
	batch, err := scanLifecycleBatch(func(dest ...any) error {
		return b.DB.QueryRowContext(ctx,
			`SELECT id, user_id, action, status, total_count, success_count, failed_count, created_at, updated_at
			 FROM item_lifecycle_batches WHERE id=? AND user_id=?`, batchID, userID).Scan(dest...)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return LifecycleBatch{}, nil, ErrNotFound
	}
	if err != nil {
		return LifecycleBatch{}, nil, err
	}
	// rows、err 保存明细行查询结果。
	rows, err := b.DB.QueryContext(ctx,
		`SELECT id, batch_id, row_no, cookie_id, item_id, status, error_message, failure_kind
		 FROM item_lifecycle_batch_rows WHERE batch_id=? ORDER BY row_no`, batchID)
	if err != nil {
		return LifecycleBatch{}, nil, err
	}
	defer rows.Close()
	// out 是全部明细行。
	var out []LifecycleRow
	for rows.Next() {
		// r 是当前扫描到的行。
		var r LifecycleRow
		// err 保存按用户列表查询错误。
		if err := rows.Scan(&r.ID, &r.BatchID, &r.RowNo, &r.CookieID, &r.ItemID, &r.Status, &r.ErrorMessage, &r.FailureKind); err != nil {
			return LifecycleBatch{}, nil, err
		}
		out = append(out, r)
	}
	return batch, out, rows.Err()
}

// ListLifecycleBatches 按用户倒序列出最近批次。
func (b *ItemPublishBatches) ListLifecycleBatches(ctx context.Context, userID int64, limit int) ([]LifecycleBatch, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	// rows、err 保存批次列表查询结果。
	rows, err := b.DB.QueryContext(ctx,
		`SELECT id, user_id, action, status, total_count, success_count, failed_count, created_at, updated_at
		 FROM item_lifecycle_batches WHERE user_id=? ORDER BY created_at DESC, id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 是结果集合。
	var out []LifecycleBatch
	for rows.Next() {
		// batch 是当前扫描到的批次。
		batch, err := scanLifecycleBatch(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, batch)
	}
	return out, rows.Err()
}

// ClaimLifecycleBatch 以乐观租约抢占批次执行权；只有 pending 批次可被抢占。
func (b *ItemPublishBatches) ClaimLifecycleBatch(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	// res、err 保存条件更新结果。
	res, err := b.DB.ExecContext(ctx,
		`UPDATE item_lifecycle_batches SET status=?, worker_token=?, lease_expires_at=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND status=? AND (worker_token='' OR lease_expires_at < ?)`,
		LifecycleBatchRunning, workerToken, leaseExpiresAt, batchID, LifecycleBatchPending, time.Now().UTC().Unix())
	if err != nil {
		return false, err
	}
	// n、nErr 保存条件更新影响行数与读取错误。
	n, nErr := res.RowsAffected()
	return n > 0 && nErr == nil, nil
}

// RenewLifecycleBatchLease 延长当前 worker 的批次租约；token 不匹配或批次已结束返回 false。
func (b *ItemPublishBatches) RenewLifecycleBatchLease(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	// res、err 保存条件更新结果。
	res, err := b.DB.ExecContext(ctx,
		`UPDATE item_lifecycle_batches SET lease_expires_at=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND worker_token=? AND status IN (?,?)`,
		leaseExpiresAt, batchID, workerToken, LifecycleBatchRunning, LifecycleBatchPending)
	if err != nil {
		return false, err
	}
	// n、nErr 保存条件更新影响行数与读取错误。
	n, nErr := res.RowsAffected()
	return n > 0 && nErr == nil, nil
}

// ClaimLifecycleRow 原子抢占一条 pending 行；返回是否抢占成功。
func (b *ItemPublishBatches) ClaimLifecycleRow(ctx context.Context, rowID int64) (bool, error) {
	// res、err 保存条件更新结果。
	res, err := b.DB.ExecContext(ctx,
		`UPDATE item_lifecycle_batch_rows SET status=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND status=?`, LifecycleRowRunning, rowID, LifecycleRowPending)
	if err != nil {
		return false, err
	}
	// n、nErr 保存条件更新影响行数与读取错误。
	n, nErr := res.RowsAffected()
	return n > 0 && nErr == nil, nil
}

// PendingLifecycleRows 查询批次内仍待处理的行。
func (b *ItemPublishBatches) PendingLifecycleRows(ctx context.Context, batchID string) ([]LifecycleRow, error) {
	return b.lifecycleRowsByStatus(ctx, batchID, LifecycleRowPending)
}

// lifecycleRowsByStatus 按状态查询批次明细行。
func (b *ItemPublishBatches) lifecycleRowsByStatus(ctx context.Context, batchID, status string) ([]LifecycleRow, error) {
	// rows、err 保存明细行查询结果。
	rows, err := b.DB.QueryContext(ctx,
		`SELECT id, batch_id, row_no, cookie_id, item_id, status, error_message, failure_kind
		 FROM item_lifecycle_batch_rows WHERE batch_id=? AND status=? ORDER BY row_no`, batchID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 是结果集合。
	var out []LifecycleRow
	for rows.Next() {
		// r 是当前扫描到的行。
		var r LifecycleRow
		// err 保存收口条件更新错误。
		if err := rows.Scan(&r.ID, &r.BatchID, &r.RowNo, &r.CookieID, &r.ItemID, &r.Status, &r.ErrorMessage, &r.FailureKind); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CompleteLifecycleRow 把行标记为成功。
func (b *ItemPublishBatches) CompleteLifecycleRow(ctx context.Context, rowID int64) error {
	// err 保存失败标记条件更新错误。
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_lifecycle_batch_rows SET status=?, error_message='', failure_kind='', updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		LifecycleRowSuccess, rowID)
	return err
}

// FailLifecycleRow 把行标记为失败并记录原因摘要与分类。
func (b *ItemPublishBatches) FailLifecycleRow(ctx context.Context, rowID int64, failureKind, message string) error {
	if len(message) > 500 {
		message = message[:500]
	}
	// err 保存取消计数查询错误。
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_lifecycle_batch_rows SET status=?, error_message=?, failure_kind=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		LifecycleRowFailed, message, failureKind, rowID)
	return err
}

// RecountLifecycleBatch 重算批次成功/失败计数；没有待处理行时收口为 completed 或 partially_failed。
func (b *ItemPublishBatches) RecountLifecycleBatch(ctx context.Context, batchID string) error {
	// stats 扫描 pending/success/failed 计数。
	var pending, success, failed int
	// err 保存取消条件更新错误。
	if err := b.DB.QueryRowContext(ctx,
		`SELECT SUM(CASE WHEN status=? THEN 1 ELSE 0 END),
		        SUM(CASE WHEN status=? THEN 1 ELSE 0 END),
		        SUM(CASE WHEN status=? THEN 1 ELSE 0 END)
		 FROM item_lifecycle_batch_rows WHERE batch_id=?`,
		LifecycleRowPending, LifecycleRowSuccess, LifecycleRowFailed, batchID).Scan(&pending, &success, &failed); err != nil {
		return err
	}
	// finalStatus 在无待处理行时给出终态。
	finalStatus := LifecycleBatchRunning
	if pending == 0 {
		if failed > 0 {
			finalStatus = LifecycleBatchPartiallyFailed
		} else {
			finalStatus = LifecycleBatchCompleted
		}
	}
	// err 保存失败行重置查询错误。
	_, err := b.DB.ExecContext(ctx,
		`UPDATE item_lifecycle_batches SET total_count=?, success_count=?, failed_count=?, status=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`, success+failed+pending, success, failed, finalStatus, batchID)
	return err
}

// FailLifecycleBatch 把批次整体置为 failed（风控/会话失效中止时使用）。
func (b *ItemPublishBatches) FailLifecycleBatch(ctx context.Context, batchID, workerToken string) (bool, error) {
	// res、err 保存条件更新结果。
	res, err := b.DB.ExecContext(ctx,
		`UPDATE item_lifecycle_batches SET status=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND worker_token=? AND status=?`,
		LifecycleBatchPartiallyFailed, batchID, workerToken, LifecycleBatchRunning)
	if err != nil {
		return false, err
	}
	// n、nErr 保存受影响行数与读取错误。
	n, nErr := res.RowsAffected()
	return n > 0 && nErr == nil, nil
}

// CancelLifecycleBatch 把批次置为 canceled 并把未完成行重置为 pending 保持事实可追溯。
func (b *ItemPublishBatches) CancelLifecycleBatch(ctx context.Context, userID int64, batchID string) (bool, error) {
	// res、err 保存条件更新结果；只有未结束的批次可取消。
	res, err := b.DB.ExecContext(ctx,
		`UPDATE item_lifecycle_batches SET status=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND user_id=? AND status IN (?,?)`,
		LifecycleBatchCanceled, batchID, userID, LifecycleBatchPending, LifecycleBatchRunning)
	if err != nil {
		return false, err
	}
	// n、nErr 保存受影响行数与读取错误。
	n, nErr := res.RowsAffected()
	return n > 0 && nErr == nil, nil
}

// RetryLifecycleFailedRows 把批次内失败行重置为 pending 以便重试；返回重置数量。
func (b *ItemPublishBatches) RetryLifecycleFailedRows(ctx context.Context, userID int64, batchID string) (int64, error) {
	// owned、err 保存批次归属校验结果。
	var owned int
	// err 保存批次状态重置错误。
	if err := b.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item_lifecycle_batches WHERE id=? AND user_id=? AND status IN (?,?)`,
		batchID, userID, LifecycleBatchCompleted, LifecycleBatchPartiallyFailed).Scan(&owned); err != nil {
		return 0, err
	}
	if owned == 0 {
		return 0, ErrNotFound
	}
	// res、err 保存失败行重置结果。
	res, err := b.DB.ExecContext(ctx,
		`UPDATE item_lifecycle_batch_rows SET status=?, error_message='', failure_kind='', updated_at=CURRENT_TIMESTAMP
		 WHERE batch_id=? AND status=?`, LifecycleRowPending, batchID, LifecycleRowFailed)
	if err != nil {
		return 0, err
	}
	// affected 是重置的行数。
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	// 重试使批次回到 pending 等待重新认领。
	if _, err := b.DB.ExecContext(ctx,
		`UPDATE item_lifecycle_batches SET status=?, worker_token='', lease_expires_at=0, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		LifecycleBatchPending, batchID); err != nil {
		return 0, err
	}
	return affected, nil
}

// LifecycleBatchRunningByUser 判断指定用户是否有未完成的生命周期批次（并发限流用）。
func (b *ItemPublishBatches) LifecycleBatchRunningByUser(ctx context.Context, userID int64) (bool, error) {
	// running 保存进行中批次数。
	var running int
	// running 保存进行中批次数。
	if err := b.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item_lifecycle_batches WHERE user_id=? AND status IN (?,?)`,
		userID, LifecycleBatchPending, LifecycleBatchRunning).Scan(&running); err != nil {
		return false, err
	}
	return running > 0, nil
}
