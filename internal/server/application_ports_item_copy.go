package server

import (
	"context"

	itemapp "xianyu-go/internal/application/items"
)

// ItemCopyPublishPort 定义跨账号复制发布的预览与任务创建能力。
type ItemCopyPublishPort interface {
	// PreviewCopy 组装配制预览与目标账号预检，全程不发平台请求。
	PreviewCopy(ctx context.Context, userID int64, cookieID, itemID string, targetCookieIDs []string) (itemapp.CopyPreview, error)
	// CreateCopyTask 创建复制批次，返回批次标识供既有批次进度接口轮询。
	CreateCopyTask(ctx context.Context, input itemapp.CopyTaskInput) (itemapp.CopyTaskResult, error)
}

// itemCopyPublishApplication 返回跨账号复制发布用例。
func (server *Server) itemCopyPublishApplication() ItemCopyPublishPort {
	return server.applicationServiceSet().itemCopyPublish
}
