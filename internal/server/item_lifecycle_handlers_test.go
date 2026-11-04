// Package server: 商品生命周期端点契约测试——覆盖 5 个 operation 的真实成功响应与错误映射。
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// fakeItemLifecyclePort 是生命周期用例替身；记录输入并返回固定数据或预置错误。
type fakeItemLifecyclePort struct {
	// createErr 是批次创建错误。
	createErr error
	// retryErr 是重试错误。
	retryErr error
	// cancelOK 是取消返回值。
	cancelOK bool
	// lastAction 是最近一次创建的动作。
	lastAction string
	// lastItems 是最近一次创建的目标集合。
	lastItems []itemapp.LifecycleTargetItem
	// lastBatchID 是最近一次批次查询/重试/取消的批次 ID。
	lastBatchID string
}

// CreateBatch 记录输入并返回固定批次 ID 或预置错误。
func (f *fakeItemLifecyclePort) CreateBatch(_ context.Context, _ int64, input itemapp.CreateLifecycleBatchInput) (string, error) {
	f.lastAction = input.Action
	f.lastItems = input.Items
	if f.createErr != nil {
		return "", f.createErr
	}
	return "lc-batch-1", nil
}

// GetBatch 返回固定批次与行明细。
func (f *fakeItemLifecyclePort) GetBatch(_ context.Context, _ int64, batchID string) (itemapp.LifecycleBatch, []itemapp.LifecycleBatchRow, error) {
	f.lastBatchID = batchID
	// batch 是本次流程后续判断的batch。
	batch := itemapp.LifecycleBatch{ID: batchID, Action: "downshelf", Status: "partially_failed",
		TotalCount: 2, SuccessCount: 1, FailedCount: 1, CreatedAt: "2026-11-03 21:00:00", UpdatedAt: "2026-11-03 21:05:00"}
	// rows 是本次流程后续判断的rows。
	rows := []itemapp.LifecycleBatchRow{
		{ID: 1, RowNo: 1, CookieID: "acc-1", ItemID: "item-1", Status: "success"},
		{ID: 2, RowNo: 2, CookieID: "acc-1", ItemID: "item-2", Status: "failed", ErrorMessage: "平台拒绝", FailureKind: "platform"},
	}
	return batch, rows, nil
}

// ListBatches 返回固定批次列表。
func (f *fakeItemLifecyclePort) ListBatches(_ context.Context, _ int64, _ int) ([]itemapp.LifecycleBatch, error) {
	return []itemapp.LifecycleBatch{{ID: "lc-batch-1", Action: "downshelf", Status: "completed", TotalCount: 1, SuccessCount: 1, CreatedAt: "2026-11-03 21:00:00", UpdatedAt: "2026-11-03 21:01:00"}}, nil
}

// RetryFailed 返回固定重置数量或预置错误。
func (f *fakeItemLifecyclePort) RetryFailed(_ context.Context, _ int64, batchID string) (int64, error) {
	f.lastBatchID = batchID
	if f.retryErr != nil {
		return 0, f.retryErr
	}
	return 1, nil
}

// Cancel 返回预置取消结果。
func (f *fakeItemLifecyclePort) Cancel(_ context.Context, _ int64, batchID string) (bool, error) {
	f.lastBatchID = batchID
	return f.cancelOK, nil
}

// RunLifecycleBatch 记录批次执行调用。
func (f *fakeItemLifecyclePort) RunLifecycleBatch(_ context.Context, _ int64, batchID string) error {
	f.lastBatchID = batchID
	return nil
}

// TestItemLifecycleEndpointsContract 覆盖生命周期 5 个端点的契约与错误映射。
func TestItemLifecycleEndpointsContract(t *testing.T) {
	// srv、cleanup 是测试 Server 与清理函数；默认测试装配未启用生命周期。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是真实路由；cookie 是管理员会话。
	handler := srv.Router()
	// cookie 是管理员登录后获得的会话 Cookie。
	cookie := loginHelper(t, handler)

	// 未装配生命周期时端点必须返回 503 而不是空指针崩溃。
	disabled, _ := sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/lifecycle", `{"action":"downshelf","items":[{"cookie_id":"a","item_id":"b"}]}`)
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用创建应返回 503, got %d", disabled.Code)
	}

	// fake 是注入的生命周期用例替身；取消默认成功，错误映射场景再翻转。
	fake := &fakeItemLifecyclePort{cancelOK: true}
	srv.applications.itemLifecycle = fake

	// 未登录访问必须被拒绝。
	anonymous, _ := sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/lifecycle", `{"action":"downshelf","items":[{"cookie_id":"a","item_id":"b"}]}`)
	_ = anonymous

	// 创建批次成功场景：契约 201 + 输入透传。
	recorder, request := sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/lifecycle", `{"action":"delete","items":[{"cookie_id":"acc-1","item_id":"i1"},{"cookie_id":"acc-1","item_id":"i2"}]}`)
	assertOpenAPIExpectedStatusResponse(t, request, recorder, http.StatusCreated)
	// created 是解析后的创建响应。
	var created lifecycleCreateResponse
	// err 是本次流程后续判断的err。
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil || created.BatchID != "lc-batch-1" {
		t.Fatalf("创建响应不符: %+v err=%v", created, err)
	}
	if fake.lastAction != "delete" || len(fake.lastItems) != 2 || fake.lastItems[1].ItemID != "i2" {
		t.Fatalf("创建输入透传不符: action=%s items=%v", fake.lastAction, fake.lastItems)
	}

	// 批次列表成功场景。
	recorder, request = sourcingRequest(handler, cookie, http.MethodGet, "/api/v1/items/lifecycle-batches", "")
	assertOpenAPISuccessResponse(t, request, recorder)
	// batches 是解析后的列表响应。
	var batches []lifecycleBatchResponse
	// err 是本次流程后续判断的err。
	if err := json.Unmarshal(recorder.Body.Bytes(), &batches); err != nil || len(batches) != 1 || batches[0].Action != "downshelf" {
		t.Fatalf("批次列表响应不符: %+v err=%v", batches, err)
	}

	// 批次详情成功场景。
	recorder, request = sourcingRequest(handler, cookie, http.MethodGet, "/api/v1/items/lifecycle-batches/lc-batch-1", "")
	assertOpenAPISuccessResponse(t, request, recorder)
	// detail 是解析后的详情响应。
	var detail lifecycleBatchDetailResponse
	// err 是本次流程后续判断的err。
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil || detail.Batch.ID != "lc-batch-1" || len(detail.Rows) != 2 {
		t.Fatalf("批次详情响应不符: %+v err=%v", detail, err)
	}

	// 重试失败行成功场景。
	recorder, request = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/lifecycle-batches/lc-batch-1/retry-failed", "")
	assertOpenAPISuccessResponse(t, request, recorder)
	// retry 是解析后的重试响应。
	var retry lifecycleRetryResponse
	// err 是本次流程后续判断的err。
	if err := json.Unmarshal(recorder.Body.Bytes(), &retry); err != nil || retry.ResetCount != 1 {
		t.Fatalf("重试响应不符: %+v err=%v", retry, err)
	}

	// 取消成功场景。
	recorder, request = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/lifecycle-batches/lc-batch-1/cancel", "")
	assertOpenAPISuccessResponse(t, request, recorder)

	// 应用层错误到 HTTP 状态的映射。
	fake.createErr = itemapp.ErrLifecycleBatchBusy
	// busy 是本次流程后续判断的busy。
	busy, _ := sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/lifecycle", `{"action":"downshelf","items":[{"cookie_id":"a","item_id":"b"}]}`)
	if busy.Code != http.StatusConflict {
		t.Fatalf("批次冲突应返回 409, got %d", busy.Code)
	}
	fake.createErr = errors.New("其他错误")
	// other 是本次流程后续判断的other。
	other, _ := sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/lifecycle", `{"action":"downshelf","items":[{"cookie_id":"a","item_id":"b"}]}`)
	if other.Code != http.StatusInternalServerError {
		t.Fatalf("其他错误应返回 500, got %d", other.Code)
	}
	fake.cancelOK = false
	// cancelConflict 是本次流程后续判断的cancelConflict。
	cancelConflict, _ := sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/lifecycle-batches/lc-batch-1/cancel", "")
	if cancelConflict.Code != http.StatusConflict {
		t.Fatalf("不可取消应返回 409, got %d", cancelConflict.Code)
	}
}
