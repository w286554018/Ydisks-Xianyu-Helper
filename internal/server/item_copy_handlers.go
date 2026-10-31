package server

import (
	"errors"
	"net/http"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/auth"
)

// copyPreviewRequest 是复制发布预览的请求体。
type copyPreviewRequest struct {
	// CookieID 是源账号标识。
	CookieID string `json:"cookie_id"`
	// ItemID 是源商品标识。
	ItemID string `json:"item_id"`
	// TargetCookieIDs 是候选目标账号列表；可为空表示仅返回全部账号预检。
	TargetCookieIDs []string `json:"target_cookie_ids"`
}

// copyTaskRequest 是创建复制任务的请求体。
type copyTaskRequest struct {
	// CookieID 是源账号标识。
	CookieID string `json:"cookie_id"`
	// ItemID 是源商品标识。
	ItemID string `json:"item_id"`
	// TargetCookieIDs 是目标账号列表。
	TargetCookieIDs []string `json:"target_cookie_ids"`
	// TitleEdit 为空时沿用源标题。
	TitleEdit string `json:"title_edit"`
	// DescriptionEdit 为空时沿用源描述。
	DescriptionEdit string `json:"description_edit"`
	// PriceEdit 为空时沿用源价格。
	PriceEdit string `json:"price_edit"`
	// PublishIntervalSeconds 是相邻发布的最小间隔秒数；非正数使用系统默认。
	PublishIntervalSeconds int `json:"publish_interval_seconds"`
}

// copyAccountResponse 是预检中单个目标账号的传输视图（非敏感字段）。
type copyAccountResponse struct {
	// CookieID 是账号稳定标识。
	CookieID string `json:"cookie_id"`
	// Name 是展示名（备注优先，昵称兜底）。
	Name string `json:"name"`
	// Usable 表示该账号当前是否可作为复制目标。
	Usable bool `json:"usable"`
	// Reason 是不可用原因说明；可用时为空。
	Reason string `json:"reason,omitempty"`
}

// copyPreviewResponse 是复制预览的响应 DTO。
type copyPreviewResponse struct {
	// SourceCookieID 是源账号标识。
	SourceCookieID string `json:"source_cookie_id"`
	// SourceItemID 是源商品标识。
	SourceItemID string `json:"source_item_id"`
	// Title 是预填标题（可编辑）。
	Title string `json:"title"`
	// Description 是预填描述（可编辑）。
	Description string `json:"description"`
	// Price 是预填售价文本（可编辑）。
	Price string `json:"price"`
	// ImageURLs 是源商品平台图片地址列表（展示用）。
	ImageURLs []string `json:"image_urls"`
	// Targets 是目标账号预检结果。
	Targets []copyAccountResponse `json:"targets"`
}

// copyTaskResultResponse 是创建复制任务的响应 DTO。
type copyTaskResultResponse struct {
	// BatchID 是生成的批量铺货批次标识，前端用既有批次进度接口轮询。
	BatchID string `json:"batch_id"`
	// RowCount 是批次行数（目标账号数）。
	RowCount int `json:"row_count"`
}

// copyPreviewResponseFrom 把应用预览模型映射为传输 DTO；图片列表 nil 转空数组。
func copyPreviewResponseFrom(preview itemapp.CopyPreview) copyPreviewResponse {
	// images 保证 JSON 输出为数组而不是 null。
	images := make([]string, 0, len(preview.ImageURLs))
	images = append(images, preview.ImageURLs...)
	// targets 保证 JSON 输出为数组而不是 null。
	targets := make([]copyAccountResponse, 0, len(preview.Targets))
	// target 是当前遍历的预检账号。
	for _, target := range preview.Targets {
		targets = append(targets, copyAccountResponse{
			CookieID: target.CookieID, Name: target.Name,
			Usable: target.Usable, Reason: target.Reason,
		})
	}
	return copyPreviewResponse{
		SourceCookieID: preview.SourceCookieID, SourceItemID: preview.SourceItemID,
		Title: preview.Title, Description: preview.Description, Price: preview.Price,
		ImageURLs: images, Targets: targets,
	}
}

// previewItemCopy 处理 POST /api/v1/items/copy-preview。
// 语义：400 缺参或请求格式错误、404 源商品不存在、409 多规格或无图片、200 预览成功。
func (s *Server) previewItemCopy(w http.ResponseWriter, r *http.Request) {
	// svc 是复制发布用例；未装配返回 503。
	svc := s.itemCopyPublishApplication()
	if svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "复制发布功能未启用")
		return
	}
	// req 解析请求体。
	var req copyPreviewRequest
	// err 表示请求体不是合法的预览 JSON。
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// 必填校验。
	if req.CookieID == "" || req.ItemID == "" {
		writeErr(w, http.StatusBadRequest, "cookie_id 与 item_id 不能为空")
		return
	}
	// sess 是当前请求的认证会话，预检只覆盖该用户名下账号。
	sess := auth.SessionFromContext(r.Context())
	// preview 是预览结果。
	preview, err := svc.PreviewCopy(r.Context(), sess.UserID, req.CookieID, req.ItemID, req.TargetCookieIDs)
	if err != nil {
		writeCopyServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, copyPreviewResponseFrom(preview))
}

// createItemCopyTask 处理 POST /api/v1/items/copy-tasks。
// 语义：400 缺参或参数非法、403 归属失败、404 源商品不存在、409 多规格或无图片、201 任务创建成功。
func (s *Server) createItemCopyTask(w http.ResponseWriter, r *http.Request) {
	// svc 是复制发布用例；未装配返回 503。
	svc := s.itemCopyPublishApplication()
	if svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "复制发布功能未启用")
		return
	}
	// req 解析请求体。
	var req copyTaskRequest
	// err 表示请求体不是合法的任务 JSON。
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// 必填校验。
	if req.CookieID == "" || req.ItemID == "" {
		writeErr(w, http.StatusBadRequest, "cookie_id 与 item_id 不能为空")
		return
	}
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// result 是任务创建结果。
	result, err := svc.CreateCopyTask(r.Context(), itemapp.CopyTaskInput{
		UserID: sess.UserID, CookieID: req.CookieID, ItemID: req.ItemID,
		TargetCookieIDs: req.TargetCookieIDs,
		TitleEdit:       req.TitleEdit, DescriptionEdit: req.DescriptionEdit, PriceEdit: req.PriceEdit,
		PublishIntervalSeconds: req.PublishIntervalSeconds,
	})
	if err != nil {
		writeCopyServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, copyTaskResultResponse{BatchID: result.BatchID, RowCount: result.RowCount})
}

// writeCopyServiceError 把复制服务错误映射为统一错误响应。
func writeCopyServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, itemapp.ErrCopySourceMissing):
		writeErr(w, http.StatusNotFound, "源商品不存在或快照数据不足")
	case errors.Is(err, itemapp.ErrCopyNotOwned):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, itemapp.ErrCopyMultiSpecUnsupported), errors.Is(err, itemapp.ErrCopyNoImages):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, itemapp.ErrCopyNoTargets), errors.Is(err, itemapp.ErrCopyTooManyTargets):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "复制任务创建失败")
	}
}
