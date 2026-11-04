package db

import (
	"context"
	"errors"
	"testing"
)

// TestLifecycleBatchCreateAndLifecycle 覆盖批次创建校验、抢占、行状态机与收口。
func TestLifecycleBatchCreateAndLifecycle(t *testing.T) {
	// store、cleanup 保存隔离数据库和清理责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// ok、err 保存测试用户创建结果。
	ok, err := store.Users.Create(ctx, "lc-user", "lc@example.com", "password")
	if err != nil || !ok {
		t.Fatalf("创建测试用户失败 ok=%v err=%v", ok, err)
	}
	// user、err 保存测试用户查询结果。
	user, err := store.Users.GetByUsername(ctx, "lc-user")
	if err != nil {
		t.Fatal(err)
	}
	// items 是目标商品集合。
	items := []LifecycleRow{
		{CookieID: "acc-lc-1", ItemID: "item-1"},
		{CookieID: "acc-lc-1", ItemID: "item-2"},
	}
	// batchID、err 保存批次创建结果。
	batchID, err := store.PublishBatches.CreateLifecycleBatch(ctx, user.ID, LifecycleActionDownshelf, items)
	if err != nil {
		t.Fatalf("创建批次失败: %v", err)
	}
	// 非法动作必须被拒绝。
	if _, err := store.PublishBatches.CreateLifecycleBatch(ctx, user.ID, "polish", items); !errors.Is(err, ErrLifecycleInvalidAction) {
		t.Fatalf("非法动作应返回 ErrLifecycleInvalidAction，实际 %v", err)
	}
	// 超过上限必须被拒绝。
	// tooMany 构造 51 行超限输入。
	tooMany := make([]LifecycleRow, LifecycleMaxRows+1)
	// err 是本次流程后续判断的err。
	if _, err := store.PublishBatches.CreateLifecycleBatch(ctx, user.ID, LifecycleActionDelete, tooMany); !errors.Is(err, ErrLifecycleTooManyRows) {
		t.Fatalf("超限批次应返回 ErrLifecycleTooManyRows，实际 %v", err)
	}
	// 重复行必须被拒绝。
	if _, err := store.PublishBatches.CreateLifecycleBatch(ctx, user.ID, LifecycleActionDelete, []LifecycleRow{{CookieID: "a", ItemID: "x"}, {CookieID: "a", ItemID: "x"}}); !errors.Is(err, ErrLifecycleDuplicateRow) {
		t.Fatalf("重复行应返回 ErrLifecycleDuplicateRow，实际 %v", err)
	}
	// 跨用户读取必须 NotFound。
	if _, _, err := store.PublishBatches.GetLifecycleBatch(ctx, user.ID+999, batchID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨用户读取应返回 ErrNotFound，实际 %v", err)
	}
	// batch、rows、err 保存批次读取结果。
	batch, rows, err := store.PublishBatches.GetLifecycleBatch(ctx, user.ID, batchID)
	if err != nil || len(rows) != 2 || batch.Status != LifecycleBatchPending {
		t.Fatalf("读取批次失败 batch=%+v rows=%d err=%v", batch, len(rows), err)
	}
	// claimed、err 保存批次抢占结果。
	claimed, err := store.PublishBatches.ClaimLifecycleBatch(ctx, batchID, "worker-a", 2000)
	if err != nil || !claimed {
		t.Fatalf("抢占批次失败 claimed=%v err=%v", claimed, err)
	}
	// pendingRows、err 保存待处理行。
	pendingRows, err := store.PublishBatches.PendingLifecycleRows(ctx, batchID)
	if err != nil || len(pendingRows) != 2 {
		t.Fatalf("待处理行查询失败 rows=%d err=%v", len(pendingRows), err)
	}
	// err 保存行抢占结果；pending→running 只能成功一次。
	if okRow, err := store.PublishBatches.ClaimLifecycleRow(ctx, pendingRows[0].ID); err != nil || !okRow {
		t.Fatalf("抢占行失败 ok=%v err=%v", okRow, err)
	}
	// okRow 是本次流程后续判断的okRow。
	// err 是本次流程后续判断的err。
	if okRow, err := store.PublishBatches.ClaimLifecycleRow(ctx, pendingRows[0].ID); err != nil || okRow {
		t.Fatalf("重复抢占行应失败 ok=%v err=%v", okRow, err)
	}
	// err 保存首行成功标记错误。
	if err := store.PublishBatches.CompleteLifecycleRow(ctx, pendingRows[0].ID); err != nil {
		t.Fatal(err)
	}
	// err 保存次行失败标记错误。
	if err := store.PublishBatches.FailLifecycleRow(ctx, pendingRows[1].ID, "platform", "平台拒绝"); err != nil {
		t.Fatal(err)
	}
	// err 保存批次收口错误。
	if err := store.PublishBatches.RecountLifecycleBatch(ctx, batchID); err != nil {
		t.Fatal(err)
	}
	// batch、_、err 保存收口后批次读取结果。
	batch, _, err = store.PublishBatches.GetLifecycleBatch(ctx, user.ID, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != LifecycleBatchPartiallyFailed || batch.SuccessCount != 1 || batch.FailedCount != 1 {
		t.Fatalf("收口状态不符 batch=%+v", batch)
	}
	// retried、err 保存失败行重试结果。
	retried, err := store.PublishBatches.RetryLifecycleFailedRows(ctx, user.ID, batchID)
	if err != nil || retried != 1 {
		t.Fatalf("重试失败行结果不符 retried=%d err=%v", retried, err)
	}
	// batch、_、err 保存重试后批次读取结果。
	batch, _, err = store.PublishBatches.GetLifecycleBatch(ctx, user.ID, batchID)
	if err != nil || batch.Status != LifecycleBatchPending {
		t.Fatalf("重试后批次应回到 pending batch=%+v err=%v", batch, err)
	}
}

// TestLifecycleBatchCancelAndComplete 覆盖取消与全部成功收口。
func TestLifecycleBatchCancelAndComplete(t *testing.T) {
	// store、cleanup 保存隔离数据库和清理责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// ok、err 保存测试用户创建结果。
	ok, err := store.Users.Create(ctx, "lc-cancel-user", "lc-cancel@example.com", "password")
	if err != nil || !ok {
		t.Fatalf("创建测试用户失败 ok=%v err=%v", ok, err)
	}
	// user、err 保存测试用户查询结果。
	user, err := store.Users.GetByUsername(ctx, "lc-cancel-user")
	if err != nil {
		t.Fatal(err)
	}
	// batchID、err 保存批次创建结果。
	batchID, err := store.PublishBatches.CreateLifecycleBatch(ctx, user.ID, LifecycleActionDelete, []LifecycleRow{{CookieID: "acc-x", ItemID: "item-x"}})
	if err != nil {
		t.Fatal(err)
	}
	// canceled、err 保存批次取消结果。
	canceled, err := store.PublishBatches.CancelLifecycleBatch(ctx, user.ID, batchID)
	if err != nil || !canceled {
		t.Fatalf("取消批次失败 canceled=%v err=%v", canceled, err)
	}
	// 再次取消已取消批次必须失败。
	if canceled, err := store.PublishBatches.CancelLifecycleBatch(ctx, user.ID, batchID); err != nil || canceled {
		t.Fatalf("重复取消应失败 canceled=%v err=%v", canceled, err)
	}
	// 全成功路径：创建新批次、全部成功后收口为 completed。
	// batchID2、err 保存第二个批次创建结果。
	batchID2, err := store.PublishBatches.CreateLifecycleBatch(ctx, user.ID, LifecycleActionDownshelf, []LifecycleRow{{CookieID: "acc-y", ItemID: "item-y"}})
	if err != nil {
		t.Fatal(err)
	}
	// rows、err 保存待处理行。
	rows, err := store.PublishBatches.PendingLifecycleRows(ctx, batchID2)
	if err != nil || len(rows) != 1 {
		t.Fatalf("待处理行查询失败 rows=%d err=%v", len(rows), err)
	}
	// err 保存行成功标记错误。
	if err := store.PublishBatches.CompleteLifecycleRow(ctx, rows[0].ID); err != nil {
		t.Fatal(err)
	}
	// err 保存批次收口错误。
	if err := store.PublishBatches.RecountLifecycleBatch(ctx, batchID2); err != nil {
		t.Fatal(err)
	}
	// batch、_、err 保存收口后批次读取结果。
	batch, _, err := store.PublishBatches.GetLifecycleBatch(ctx, user.ID, batchID2)
	if err != nil || batch.Status != LifecycleBatchCompleted || batch.SuccessCount != 1 {
		t.Fatalf("全部成功应收口 completed batch=%+v err=%v", batch, err)
	}
}
