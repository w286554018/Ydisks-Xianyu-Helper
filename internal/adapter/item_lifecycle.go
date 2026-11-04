// Package adapter: 商品生命周期平台端口——把 mtop 下架/删除调用包装成应用层窄接口。
package adapter

import (
	"context"
	"errors"
	"fmt"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// ItemLifecyclePlatformPort 是生命周期应用端口的平台实现；凭证读取与会话回写遵循批量发布端口模式。
type ItemLifecyclePlatformPort struct {
	// store 提供平台凭证读取与运行中 Cookie 更新能力。
	store *db.Store
	// client 返回当前平台客户端，允许测试替换实现。
	client func() mtop.Client
	// updateRunningCookie 将平台返回的新 Cookie 同步到运行中的账号实例。
	updateRunningCookie func(context.Context, string, string)
}

// NewItemLifecyclePlatformPort 构造生命周期平台端口；依赖缺失返回错误。
func NewItemLifecyclePlatformPort(store *db.Store, client func() mtop.Client, updateRunningCookie func(context.Context, string, string)) (*ItemLifecyclePlatformPort, error) {
	if store == nil || client == nil {
		return nil, errors.New("生命周期平台端口依赖不完整")
	}
	return &ItemLifecyclePlatformPort{store: store, client: client, updateRunningCookie: updateRunningCookie}, nil
}

// DownshelfItem 下架平台商品；凭证快照语义与批量发布一致。
func (p *ItemLifecyclePlatformPort) DownshelfItem(ctx context.Context, cookieID, itemID string) (bool, []string, error) {
	return p.invoke(ctx, cookieID, itemID, "下架商品", func(mtopCtx context.Context, cookies string) (bool, []string, string, error) {
		return p.client().DownshelfItemContext(mtopCtx, cookies, itemID)
	})
}

// DeleteItem 删除平台商品；凭证快照语义与下架一致。
func (p *ItemLifecyclePlatformPort) DeleteItem(ctx context.Context, cookieID, itemID string) (bool, []string, error) {
	return p.invoke(ctx, cookieID, itemID, "删除商品", func(mtopCtx context.Context, cookies string) (bool, []string, string, error) {
		return p.client().DeleteItemContext(mtopCtx, cookies, itemID)
	})
}

// invoke 是两个动作共用的凭证读取与调用路径。
func (p *ItemLifecyclePlatformPort) invoke(ctx context.Context, cookieID, itemID, label string, call func(context.Context, string) (bool, []string, string, error)) (bool, []string, error) {
	// unlock、latest、loadErr 保存凭证快照读取及其锁释放。
	unlock := p.store.LockAccountCredentials(cookieID)
	// latest 保存加锁读取的平台凭证视图。
	latest, loadErr := p.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if loadErr != nil || latest.UserID == 0 || latest.Value == "" {
		unlock()
		return false, nil, fmt.Errorf("%s失败: 账号凭证不可用", label)
	}
	// mtopCtx、cookieSession 挂载本次调用使用的 Cookie 会话。
	mtopCtx, cookieSession := withCookieSnapshot(ctx, latest)
	// 在远端 I/O 前释放锁，避免凭证锁覆盖慢速平台请求。
	unlock()
	// ok、ret、updated、err 保存平台返回。
	ok, ret, updated, err := call(mtopCtx, latest.Value)
	// persistErr 保存平台会话（Cookie Jar）变化写回错误；写回失败只记录，不改变平台结果判定。
	if persistErr := p.persistSession(ctx, cookieID, latest, cookieSession); persistErr != nil {
		_ = persistErr
	}
	if updated != "" && updated != latest.Value && p.updateRunningCookie != nil {
		p.updateRunningCookie(ctx, cookieID, updated)
	}
	return ok, ret, err
}

// persistSession 把平台调用引起的 Cookie 会话变化写回持久层；凭证一致性由加锁复核保证。
func (p *ItemLifecyclePlatformPort) persistSession(ctx context.Context, cookieID string, detail db.CookiePlatformRuntimeData, session *mtop.CookieSession) error {
	// unlock 保护凭证复核与持久化。
	unlock := p.store.LockAccountCredentials(cookieID)
	defer unlock()
	// latest、loadErr 保存调用完成后的最新平台凭证视图；发生变化说明并发更新，放弃写回。
	latest, loadErr := p.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if loadErr != nil || latest.Value != detail.Value || latest.MetadataJSON != detail.MetadataJSON {
		return nil
	}
	// _, _, _, persistErr 保存会话持久化结果；是否生效不影响本次调用结果判定。
	_, _, _, persistErr := persistBatchCookieSession(ctx, p.store, latest, session)
	return persistErr
}

// NewItemLifecycleOwnershipAdapter 构造生命周期归属校验适配器，复用既有 Cookie 归属查询。
func NewItemLifecycleOwnershipAdapter(previewPort *ItemBatchPreviewPort) *ItemLifecycleOwnershipAdapter {
	return &ItemLifecycleOwnershipAdapter{previewPort: previewPort}
}

// ItemLifecycleOwnershipAdapter 把批量预检端口的 Cookie 归属查询暴露给生命周期服务。
type ItemLifecycleOwnershipAdapter struct {
	// previewPort 是既有预检端口，其 CookieOwned 已覆盖归属与存在性校验。
	previewPort *ItemBatchPreviewPort
}

// CookieOwned 判断账号是否属于指定用户。
func (a *ItemLifecycleOwnershipAdapter) CookieOwned(ctx context.Context, userID int64, cookieID string) (bool, error) {
	return a.previewPort.CookieOwned(ctx, userID, cookieID)
}

// ClassifyLifecyclePlatformError 把 mtop 错误分类映射为生命周期失败类别字符串。
// 返回 "risk"/"session" 时批次立即中止；其余按可重试的平台失败处理。
func ClassifyLifecyclePlatformError(err error) string {
	switch {
	case mtop.IsRiskVerificationErr(err):
		return "risk"
	case mtop.IsSessionExpiredErr(err):
		return "session"
	default:
		return "platform"
	}
}

// translateLifecycleStoreError 把持久层哨兵错误转换为应用层哨兵，保持上层不依赖 db 包。
func translateLifecycleStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, db.ErrNotFound):
		return itemapp.ErrLifecycleBatchNotFound
	case errors.Is(err, db.ErrLifecycleInvalidAction):
		return itemapp.ErrLifecycleInvalidAction
	case errors.Is(err, db.ErrLifecycleTooManyRows):
		return itemapp.ErrLifecycleTooManyRows
	case errors.Is(err, db.ErrLifecycleDuplicateRow):
		return itemapp.ErrLifecycleDuplicateRow
	default:
		return err
	}
}

// ItemLifecycleStoreAdapter 把生命周期批次仓储与商品状态收口包装成应用层窄接口。
type ItemLifecycleStoreAdapter struct {
	// batches 提供生命周期批次持久化。
	batches *db.ItemPublishBatches
	// items 提供商品状态更新与软删除。
	items *db.Items
}

// NewItemLifecycleStoreAdapter 构造生命周期仓储适配器。
func NewItemLifecycleStoreAdapter(store *db.Store) *ItemLifecycleStoreAdapter {
	return &ItemLifecycleStoreAdapter{batches: store.PublishBatches, items: store.Items}
}

// CreateLifecycleBatch 创建批次并写入明细；应用目标行转换为持久层模型。
func (a *ItemLifecycleStoreAdapter) CreateLifecycleBatch(ctx context.Context, userID int64, action string, items []itemapp.LifecycleTargetItem) (string, error) {
	// rows 是转换后的持久层行。
	rows := make([]db.LifecycleRow, 0, len(items))
	// item 是当前遍历的应用目标行。
	for _, item := range items {
		rows = append(rows, db.LifecycleRow{CookieID: item.CookieID, ItemID: item.ItemID})
	}
	// batchID、err 保存批次创建结果并把持久层错误归一为应用哨兵。
	batchID, err := a.batches.CreateLifecycleBatch(ctx, userID, action, rows)
	return batchID, translateLifecycleStoreError(err)
}

// GetLifecycleBatch 读取批次与明细并转换为应用视图。
func (a *ItemLifecycleStoreAdapter) GetLifecycleBatch(ctx context.Context, userID int64, batchID string) (itemapp.LifecycleBatch, []itemapp.LifecycleBatchRow, error) {
	// batch、rows、err 保存持久层查询结果。
	batch, rows, err := a.batches.GetLifecycleBatch(ctx, userID, batchID)
	if err != nil {
		return itemapp.LifecycleBatch{}, nil, translateLifecycleStoreError(err)
	}
	return lifecycleBatchView(batch), lifecycleRowViews(rows), nil
}

// ListLifecycleBatches 列出最近批次并转换为应用视图。
func (a *ItemLifecycleStoreAdapter) ListLifecycleBatches(ctx context.Context, userID int64, limit int) ([]itemapp.LifecycleBatch, error) {
	// batches、err 保存持久层查询结果。
	batches, err := a.batches.ListLifecycleBatches(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	// out 是应用视图集合。
	out := make([]itemapp.LifecycleBatch, 0, len(batches))
	// batch 是当前遍历的持久层批次。
	for _, batch := range batches {
		out = append(out, lifecycleBatchView(batch))
	}
	return out, nil
}

// lifecycleBatchView 转换批次持久层模型到应用视图。
func lifecycleBatchView(batch db.LifecycleBatch) itemapp.LifecycleBatch {
	return itemapp.LifecycleBatch{ID: batch.ID, Action: batch.Action, Status: batch.Status,
		TotalCount: batch.TotalCount, SuccessCount: batch.SuccessCount, FailedCount: batch.FailedCount,
		CreatedAt: batch.CreatedAt, UpdatedAt: batch.UpdatedAt}
}

// lifecycleRowViews 转换行持久层模型切片到应用视图。
func lifecycleRowViews(rows []db.LifecycleRow) []itemapp.LifecycleBatchRow {
	// out 是应用视图集合。
	out := make([]itemapp.LifecycleBatchRow, 0, len(rows))
	// row 是当前遍历的持久层行。
	for _, row := range rows {
		out = append(out, itemapp.LifecycleBatchRow{ID: row.ID, RowNo: row.RowNo, CookieID: row.CookieID,
			ItemID: row.ItemID, Status: row.Status, ErrorMessage: row.ErrorMessage, FailureKind: row.FailureKind})
	}
	return out
}

// LifecycleBatchRunningByUser 判断是否有进行中批次。
func (a *ItemLifecycleStoreAdapter) LifecycleBatchRunningByUser(ctx context.Context, userID int64) (bool, error) {
	return a.batches.LifecycleBatchRunningByUser(ctx, userID)
}

// UpdateStatus 更新商品生命周期状态。
func (a *ItemLifecycleStoreAdapter) UpdateStatus(ctx context.Context, cookieID, itemID, status string) error {
	return a.items.UpdateStatus(ctx, cookieID, itemID, status)
}

// Delete 软删除本地商品。
func (a *ItemLifecycleStoreAdapter) Delete(ctx context.Context, cookieID, itemID string) error {
	return a.items.Delete(ctx, cookieID, itemID)
}

// ClaimLifecycleBatch 抢占批次执行权。
func (a *ItemLifecycleStoreAdapter) ClaimLifecycleBatch(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	return a.batches.ClaimLifecycleBatch(ctx, batchID, workerToken, leaseExpiresAt)
}

// RenewLifecycleBatchLease 延长批次租约。
func (a *ItemLifecycleStoreAdapter) RenewLifecycleBatchLease(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	return a.batches.RenewLifecycleBatchLease(ctx, batchID, workerToken, leaseExpiresAt)
}

// PendingLifecycleRows 查询待处理行并转换为应用视图。
func (a *ItemLifecycleStoreAdapter) PendingLifecycleRows(ctx context.Context, batchID string) ([]itemapp.LifecycleBatchRow, error) {
	// rows、err 保存持久层查询结果。
	rows, err := a.batches.PendingLifecycleRows(ctx, batchID)
	if err != nil {
		return nil, err
	}
	return lifecycleRowViews(rows), nil
}

// ClaimLifecycleRow 抢占单行。
func (a *ItemLifecycleStoreAdapter) ClaimLifecycleRow(ctx context.Context, rowID int64) (bool, error) {
	return a.batches.ClaimLifecycleRow(ctx, rowID)
}

// CompleteLifecycleRow 标记行成功。
func (a *ItemLifecycleStoreAdapter) CompleteLifecycleRow(ctx context.Context, rowID int64) error {
	return a.batches.CompleteLifecycleRow(ctx, rowID)
}

// FailLifecycleRow 标记行失败。
func (a *ItemLifecycleStoreAdapter) FailLifecycleRow(ctx context.Context, rowID int64, failureKind, message string) error {
	return a.batches.FailLifecycleRow(ctx, rowID, failureKind, message)
}

// RecountLifecycleBatch 重算批次计数并收口。
func (a *ItemLifecycleStoreAdapter) RecountLifecycleBatch(ctx context.Context, batchID string) error {
	return a.batches.RecountLifecycleBatch(ctx, batchID)
}

// FailLifecycleBatch 批次整体置为部分失败。
func (a *ItemLifecycleStoreAdapter) FailLifecycleBatch(ctx context.Context, batchID, workerToken string) (bool, error) {
	return a.batches.FailLifecycleBatch(ctx, batchID, workerToken)
}

// RetryLifecycleFailedRows 重置失败行为待处理。
func (a *ItemLifecycleStoreAdapter) RetryLifecycleFailedRows(ctx context.Context, userID int64, batchID string) (int64, error) {
	// reset、err 保存重置结果并把持久层错误归一为应用哨兵。
	reset, err := a.batches.RetryLifecycleFailedRows(ctx, userID, batchID)
	return reset, translateLifecycleStoreError(err)
}

// CancelLifecycleBatch 取消批次。
func (a *ItemLifecycleStoreAdapter) CancelLifecycleBatch(ctx context.Context, userID int64, batchID string) (bool, error) {
	return a.batches.CancelLifecycleBatch(ctx, userID, batchID)
}
