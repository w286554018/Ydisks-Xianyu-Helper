// Package server: 商品生命周期端口定义与访问器。
package server

import (
	"context"

	itemapp "xianyu-go/internal/application/items"
)

// ItemLifecyclePort 定义商品平台下架/删除与批次管理用例。
type ItemLifecyclePort interface {
	// CreateBatch 创建生命周期批次（单商品操作也是一行批次）。
	CreateBatch(ctx context.Context, userID int64, input itemapp.CreateLifecycleBatchInput) (string, error)
	// GetBatch 读取批次与明细。
	GetBatch(ctx context.Context, userID int64, batchID string) (itemapp.LifecycleBatch, []itemapp.LifecycleBatchRow, error)
	// ListBatches 列出最近批次。
	ListBatches(ctx context.Context, userID int64, limit int) ([]itemapp.LifecycleBatch, error)
	// RetryFailed 重试批次内失败行。
	RetryFailed(ctx context.Context, userID int64, batchID string) (int64, error)
	// Cancel 取消批次。
	Cancel(ctx context.Context, userID int64, batchID string) (bool, error)
	// RunLifecycleBatch 执行批次（后台调用）。
	RunLifecycleBatch(ctx context.Context, userID int64, batchID string) error
}

// itemLifecycleApplication 返回商品生命周期用例。
func (server *Server) itemLifecycleApplication() ItemLifecyclePort {
	return server.applicationServiceSet().itemLifecycle
}
