// Package server: 商品平台同步下架/删除的 HTTP 端点与 DTO。
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/auth"
)

// lifecycleCreateRequest 是创建生命周期批次的请求体。
type lifecycleCreateRequest struct {
	// Action 是批次动作：downshelf 或 delete。
	Action string `json:"action"`
	// Items 是目标商品集合。
	Items []lifecycleTargetDTO `json:"items"`
}

// lifecycleTargetDTO 是批次目标商品。
type lifecycleTargetDTO struct {
	// CookieID 是商品归属账号。
	CookieID string `json:"cookie_id"`
	// ItemID 是平台商品 ID。
	ItemID string `json:"item_id"`
}

// lifecycleBatchResponse 是批次响应。
type lifecycleBatchResponse struct {
	// ID 是批次唯一标识。
	ID string `json:"id"`
	// Action 是批次动作。
	Action string `json:"action"`
	// Status 是批次状态。
	Status string `json:"status"`
	// TotalCount/SuccessCount/FailedCount 分别是总数、成功数、失败数。
	TotalCount   int `json:"total_count"`
	SuccessCount int `json:"success_count"`
	FailedCount  int `json:"failed_count"`
	// CreatedAt/UpdatedAt 是创建与更新时间。
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// lifecycleRowResponse 是批次行响应。
type lifecycleRowResponse struct {
	// ID 是行主键。
	ID int64 `json:"id"`
	// RowNo 是批次内序号。
	RowNo int `json:"row_no"`
	// CookieID 是账号。
	CookieID string `json:"cookie_id"`
	// ItemID 是平台商品 ID。
	ItemID string `json:"item_id"`
	// Status 是行状态。
	Status string `json:"status"`
	// ErrorMessage 是失败原因摘要。
	ErrorMessage string `json:"error_message,omitempty"`
	// FailureKind 是失败分类。
	FailureKind string `json:"failure_kind,omitempty"`
}

// lifecycleBatchDetailResponse 是批次详情（含行明细）。
type lifecycleBatchDetailResponse struct {
	// Batch 是批次头。
	Batch lifecycleBatchResponse `json:"batch"`
	// Rows 是行明细。
	Rows []lifecycleRowResponse `json:"rows"`
}

// lifecycleCreateResponse 是批次创建响应。
type lifecycleCreateResponse struct {
	// BatchID 是新批次 ID。
	BatchID string `json:"batch_id"`
}

// lifecycleRetryResponse 是失败行重试响应。
type lifecycleRetryResponse struct {
	// ResetCount 是重置的行数。
	ResetCount int64 `json:"reset_count"`
}

// lifecycleStatusResponse 是取消/重试等动作的通用状态响应。
type lifecycleStatusResponse struct {
	// Accepted 表示动作是否生效。
	Accepted bool `json:"accepted"`
}

// createItemLifecycleBatch 创建生命周期批次并立即在后台启动执行。
func (s *Server) createItemLifecycleBatch(w http.ResponseWriter, r *http.Request) {
	// svc 是生命周期用例；未装配返回 503。
	svc := s.itemLifecycleApplication()
	if svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "商品生命周期功能未启用")
		return
	}
	// session 是当前登录会话。
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// req 解析请求体。
	var req lifecycleCreateRequest
	// err 是本次流程后续判断的err。
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// input 是应用层输入。
	input := itemapp.CreateLifecycleBatchInput{Action: req.Action}
	// item 是本次流程后续判断的item。
	for _, item := range req.Items {
		input.Items = append(input.Items, itemapp.LifecycleTargetItem{CookieID: strings.TrimSpace(item.CookieID), ItemID: strings.TrimSpace(item.ItemID)})
	}
	// batchID、err 保存批次创建结果。
	batchID, err := svc.CreateBatch(r.Context(), session.UserID, input)
	if err != nil {
		s.writeLifecycleCreateError(w, err)
		return
	}
	// 后台执行批次；使用独立 context 避免请求结束取消任务。
	go func() {
		// runCtx 由服务生命周期控制；这里以 Background 派生保证批次不随请求终止。
		runCtx := context.Background()
		// runErr 是本次流程后续判断的runErr。
		if runErr := svc.RunLifecycleBatch(runCtx, session.UserID, batchID); runErr != nil {
			slog.Warn("生命周期批次执行结束", "batch", batchID, "err", runErr)
		}
	}()
	writeJSON(w, http.StatusCreated, lifecycleCreateResponse{BatchID: batchID})
}

// writeLifecycleCreateError 把批次创建错误映射为统一错误信封。
func (s *Server) writeLifecycleCreateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, itemapp.ErrLifecycleBatchBusy):
		writeErr(w, http.StatusConflict, "已有进行中的下架/删除任务，请等待完成")
	case errors.Is(err, itemapp.ErrLifecycleInvalidAction):
		writeErr(w, http.StatusBadRequest, "不支持的生命周期动作")
	case errors.Is(err, itemapp.ErrLifecycleTooManyRows):
		writeErr(w, http.StatusBadRequest, "批次行数必须为 1-50")
	case errors.Is(err, itemapp.ErrLifecycleDuplicateRow):
		writeErr(w, http.StatusBadRequest, "批次内存在重复商品")
	case strings.Contains(err.Error(), "缺少账号或商品"):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "创建生命周期批次失败")
	}
}

// getItemLifecycleBatch 查询批次详情。
func (s *Server) getItemLifecycleBatch(w http.ResponseWriter, r *http.Request) {
	// svc 是生命周期用例。
	svc := s.itemLifecycleApplication()
	if svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "商品生命周期功能未启用")
		return
	}
	// session 是当前登录会话。
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// batchID 是路径参数。
	batchID := chi.URLParam(r, "batch_id")
	// batch、rows、err 保存批次查询结果。
	batch, rows, err := svc.GetBatch(r.Context(), session.UserID, batchID)
	if errors.Is(err, itemapp.ErrLifecycleBatchNotFound) {
		writeErr(w, http.StatusNotFound, "批次不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询生命周期批次失败")
		return
	}
	writeJSON(w, http.StatusOK, lifecycleBatchDetailResponse{Batch: lifecycleBatchDTO(batch), Rows: lifecycleRowDTOs(rows)})
}

// listItemLifecycleBatches 列出最近批次。
func (s *Server) listItemLifecycleBatches(w http.ResponseWriter, r *http.Request) {
	// svc 是生命周期用例。
	svc := s.itemLifecycleApplication()
	if svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "商品生命周期功能未启用")
		return
	}
	// session 是当前登录会话。
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// batches、err 保存批次列表。
	batches, err := svc.ListBatches(r.Context(), session.UserID, 50)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询生命周期批次列表失败")
		return
	}
	// out 是响应集合。
	out := make([]lifecycleBatchResponse, 0, len(batches))
	// batch 是本次流程后续判断的batch。
	for _, batch := range batches {
		out = append(out, lifecycleBatchDTO(batch))
	}
	writeJSON(w, http.StatusOK, out)
}

// retryItemLifecycleBatch 重试批次内失败行并重新启动执行。
func (s *Server) retryItemLifecycleBatch(w http.ResponseWriter, r *http.Request) {
	// svc 是生命周期用例。
	svc := s.itemLifecycleApplication()
	if svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "商品生命周期功能未启用")
		return
	}
	// session 是当前登录会话。
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// batchID 是路径参数。
	batchID := chi.URLParam(r, "batch_id")
	// reset、err 保存失败行重置结果。
	reset, err := svc.RetryFailed(r.Context(), session.UserID, batchID)
	if errors.Is(err, itemapp.ErrLifecycleBatchNotFound) {
		writeErr(w, http.StatusNotFound, "批次不存在或状态不支持重试")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "重试生命周期批次失败")
		return
	}
	if reset > 0 {
		go func() {
			// runCtx 以 Background 派生保证批次不随请求终止。
			if runErr := svc.RunLifecycleBatch(context.Background(), session.UserID, batchID); runErr != nil {
				slog.Warn("生命周期批次重试执行结束", "batch", batchID, "err", runErr)
			}
		}()
	}
	writeJSON(w, http.StatusOK, lifecycleRetryResponse{ResetCount: reset})
}

// cancelItemLifecycleBatch 取消批次。
func (s *Server) cancelItemLifecycleBatch(w http.ResponseWriter, r *http.Request) {
	// svc 是生命周期用例。
	svc := s.itemLifecycleApplication()
	if svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "商品生命周期功能未启用")
		return
	}
	// session 是当前登录会话。
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// batchID 是路径参数。
	batchID := chi.URLParam(r, "batch_id")
	// accepted、err 保存取消结果。
	accepted, err := svc.Cancel(r.Context(), session.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "取消生命周期批次失败")
		return
	}
	if !accepted {
		writeErr(w, http.StatusConflict, "批次已结束或不存在，无法取消")
		return
	}
	writeJSON(w, http.StatusOK, lifecycleStatusResponse{Accepted: true})
}

// lifecycleBatchDTO 转换批次应用视图为响应 DTO。
func lifecycleBatchDTO(batch itemapp.LifecycleBatch) lifecycleBatchResponse {
	return lifecycleBatchResponse{ID: batch.ID, Action: batch.Action, Status: batch.Status,
		TotalCount: batch.TotalCount, SuccessCount: batch.SuccessCount, FailedCount: batch.FailedCount,
		CreatedAt: batch.CreatedAt, UpdatedAt: batch.UpdatedAt}
}

// lifecycleRowDTOs 转换行应用视图切片为响应 DTO。
func lifecycleRowDTOs(rows []itemapp.LifecycleBatchRow) []lifecycleRowResponse {
	// out 是响应集合。
	out := make([]lifecycleRowResponse, 0, len(rows))
	// row 是本次流程后续判断的row。
	for _, row := range rows {
		out = append(out, lifecycleRowResponse{ID: row.ID, RowNo: row.RowNo, CookieID: row.CookieID,
			ItemID: row.ItemID, Status: row.Status, ErrorMessage: row.ErrorMessage, FailureKind: row.FailureKind})
	}
	return out
}
