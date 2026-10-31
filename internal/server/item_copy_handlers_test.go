package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// fakeItemCopyPublishPort 是跨账号复制发布用例替身，记录调用参数并返回预置结果，避免测试访问真实平台。
type fakeItemCopyPublishPort struct {
	// previewErr 是预览返回的错误，用于覆盖错误映射。
	previewErr error
	// createErr 是创建任务返回的错误。
	createErr error
	// lastUserID 是最近一次调用传入的用户标识，用于验证数据隔离。
	lastUserID int64
	// lastSourceCookieID 是最近一次调用的源账号标识。
	lastSourceCookieID string
	// lastSourceItemID 是最近一次调用的源商品标识。
	lastSourceItemID string
	// lastTargets 是最近一次创建任务的目标账号列表。
	lastTargets []string
	// lastInterval 是最近一次创建任务的发布间隔秒数。
	lastInterval int
}

// PreviewCopy 返回固定预览或预置错误，并记录用户与源商品。
func (f *fakeItemCopyPublishPort) PreviewCopy(_ context.Context, userID int64, cookieID, itemID string, _ []string) (itemapp.CopyPreview, error) {
	f.lastUserID, f.lastSourceCookieID, f.lastSourceItemID = userID, cookieID, itemID
	if f.previewErr != nil {
		return itemapp.CopyPreview{}, f.previewErr
	}
	return itemapp.CopyPreview{
		SourceCookieID: cookieID, SourceItemID: itemID,
		Title: "源商品标题", Description: "源商品描述", Price: "19.9",
		ImageURLs: []string{"https://img.example.com/1.jpg"},
		Targets: []itemapp.CopyAccount{
			{CookieID: "t1", Name: "目标一号", Usable: true},
			{CookieID: "t2", Name: "目标二号", Usable: false, Reason: "账号处于暂停期"},
		},
	}, nil
}

// CreateCopyTask 返回固定批次结果或预置错误，并记录输入。
func (f *fakeItemCopyPublishPort) CreateCopyTask(_ context.Context, input itemapp.CopyTaskInput) (itemapp.CopyTaskResult, error) {
	f.lastUserID, f.lastSourceCookieID, f.lastSourceItemID = input.UserID, input.CookieID, input.ItemID
	f.lastTargets, f.lastInterval = input.TargetCookieIDs, input.PublishIntervalSeconds
	if f.createErr != nil {
		return itemapp.CopyTaskResult{}, f.createErr
	}
	return itemapp.CopyTaskResult{BatchID: "copy-batch-1", RowCount: len(input.TargetCookieIDs)}, nil
}

// TestItemCopyEndpointsContract 验证复制发布两个端点的成功契约、错误映射、鉴权与未启用降级。
func TestItemCopyEndpointsContract(t *testing.T) {
	// srv、cleanup 是测试 Server 与清理函数；默认测试装配未启用复制发布。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是真实路由；cookie 是管理员会话。
	handler := srv.Router()
	// cookie 是管理员登录后获得的会话 Cookie。
	cookie := loginHelper(t, handler)

	// 未装配复制发布时两个端点都必须返回 503 而不是空指针崩溃。
	disabled, _ := sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/copy-preview", `{"cookie_id":"a","item_id":"b"}`)
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用预览应返回 503, got %d", disabled.Code)
	}
	disabled, _ = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/copy-tasks", `{"cookie_id":"a","item_id":"b","target_cookie_ids":["t1"]}`)
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用创建任务应返回 503, got %d", disabled.Code)
	}

	// fake 是注入的复制发布用例替身。
	fake := &fakeItemCopyPublishPort{}
	srv.applications.itemCopyPublish = fake

	// 未登录访问必须被拒绝。
	anonymous, _ := sourcingRequest(handler, nil, http.MethodPost, "/api/v1/items/copy-preview", `{"cookie_id":"a","item_id":"b"}`)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应返回 401, got %d", anonymous.Code)
	}

	// 预览成功场景：断言 OpenAPI 契约与响应字段。
	recorder, request := sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/copy-preview", `{"cookie_id":"src","item_id":"i1","target_cookie_ids":["t1","t2"]}`)
	assertOpenAPISuccessResponse(t, request, recorder)
	// preview 是解析后的预览响应。
	var preview copyPreviewResponse
	// err 是解析预览响应 JSON 的错误，期望为空。
	if err := json.Unmarshal(recorder.Body.Bytes(), &preview); err != nil {
		t.Fatalf("预览响应解析失败: %v", err)
	}
	if preview.SourceCookieID != "src" || preview.SourceItemID != "i1" || preview.Title != "源商品标题" || len(preview.ImageURLs) != 1 || len(preview.Targets) != 2 {
		t.Fatalf("预览响应不符: %+v", preview)
	}
	if !preview.Targets[0].Usable || preview.Targets[1].Usable || preview.Targets[1].Reason == "" {
		t.Fatalf("目标预检状态不符: %+v", preview.Targets)
	}
	if fake.lastUserID <= 0 {
		t.Fatalf("预览应传入当前登录用户, got %d", fake.lastUserID)
	}

	// 创建任务成功场景：断言契约与请求透传；创建端点的成功状态是 201。
	recorder, request = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/copy-tasks", `{"cookie_id":"src","item_id":"i1","target_cookie_ids":["t1","t2"],"title_edit":"新标题","publish_interval_seconds":300}`)
	assertOpenAPIExpectedStatusResponse(t, request, recorder, http.StatusCreated)
	// result 是解析后的任务创建响应。
	var result copyTaskResultResponse
	// err 是解析创建响应 JSON 的错误，期望为空。
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.BatchID != "copy-batch-1" || result.RowCount != 2 {
		t.Fatalf("创建任务响应不符: %+v err=%v", result, err)
	}
	if fake.lastSourceCookieID != "src" || fake.lastSourceItemID != "i1" || len(fake.lastTargets) != 2 || fake.lastInterval != 300 {
		t.Fatalf("创建任务输入透传不符: src=%s item=%s targets=%v interval=%d", fake.lastSourceCookieID, fake.lastSourceItemID, fake.lastTargets, fake.lastInterval)
	}

	// 缺参必须返回 400。
	missing, _ := sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/copy-preview", `{}`)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("缺参预览应返回 400, got %d", missing.Code)
	}
	// badBody 记录非法请求体的响应。
	badBody, _ := sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/copy-preview", `not-json`)
	if badBody.Code != http.StatusBadRequest {
		t.Fatalf("非法请求体应返回 400, got %d", badBody.Code)
	}

	// 应用层错误到 HTTP 状态的映射。
	mappings := []struct {
		// name 是场景名称。
		name string
		// err 是替身返回的应用层错误。
		err error
		// want 是期望的 HTTP 状态码。
		want int
	}{
		{name: "source-missing", err: itemapp.ErrCopySourceMissing, want: http.StatusNotFound},
		{name: "not-owned", err: itemapp.ErrCopyNotOwned, want: http.StatusForbidden},
		{name: "multi-spec", err: itemapp.ErrCopyMultiSpecUnsupported, want: http.StatusConflict},
		{name: "no-images", err: itemapp.ErrCopyNoImages, want: http.StatusConflict},
		{name: "no-targets", err: itemapp.ErrCopyNoTargets, want: http.StatusBadRequest},
		{name: "too-many-targets", err: itemapp.ErrCopyTooManyTargets, want: http.StatusBadRequest},
		{name: "internal", err: errors.New("其他错误"), want: http.StatusInternalServerError},
	}
	for _, mapping := range mappings { // mapping 是当前验证的错误映射样例。
		fake.createErr = mapping.err
		fake.previewErr = mapping.err
		recorder, _ = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/copy-tasks", `{"cookie_id":"src","item_id":"i1","target_cookie_ids":["t1"]}`)
		if recorder.Code != mapping.want {
			t.Fatalf("%s 应返回 %d, got %d", mapping.name, mapping.want, recorder.Code)
		}
		recorder, _ = sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/copy-preview", `{"cookie_id":"src","item_id":"i1"}`)
		if recorder.Code != mapping.want {
			t.Fatalf("预览 %s 应返回 %d, got %d", mapping.name, mapping.want, recorder.Code)
		}
		fake.createErr, fake.previewErr = nil, nil
	}

	// 路由注册必须命中复制 handler 而不是通配的 createItem。
	probe, _ := sourcingRequest(handler, cookie, http.MethodPost, "/api/v1/items/copy-preview", `{"cookie_id":"x","item_id":"y"}`)
	if !strings.Contains(probe.Body.String(), "source_cookie_id") {
		t.Fatalf("copy-preview 未命中复制 handler: %s", probe.Body.String())
	}
}
