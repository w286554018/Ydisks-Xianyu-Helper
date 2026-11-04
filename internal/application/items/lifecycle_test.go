package items

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeLifecycleStore 是内存版生命周期仓储替身；记录关键调用序列供断言。
type fakeLifecycleStore struct {
	// mu 保护全部可变状态。
	mu sync.Mutex
	// batches 以批次 ID 保存批次。
	batches map[string]LifecycleBatch
	// rows 以批次 ID 保存行集合。
	rows map[string][]LifecycleBatchRow
	// statuses 记录 UpdateStatus 调用。
	statuses [][2]string
	// deleted 记录 Delete 调用。
	deleted [][2]string
	// runningByUser 控制 LifecycleBatchRunningByUser 返回值。
	runningByUser bool
}

// newFakeLifecycleStore 构造空仓储替身。
func newFakeLifecycleStore() *fakeLifecycleStore {
	return &fakeLifecycleStore{batches: map[string]LifecycleBatch{}, rows: map[string][]LifecycleBatchRow{}}
}

// CreateLifecycleBatch 创建批次。
func (f *fakeLifecycleStore) CreateLifecycleBatch(ctx context.Context, userID int64, action string, items []LifecycleTargetItem) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if action != LifecycleActionDownshelf && action != LifecycleActionDelete {
		return "", errors.New("bad action")
	}
	if len(items) == 0 || len(items) > 50 {
		return "", errors.New("bad rows")
	}
	// id 生成自增批次 ID。
	id := "batch-" + string(rune('A'+len(f.batches)))
	f.batches[id] = LifecycleBatch{ID: id, Action: action, Status: "pending", TotalCount: len(items)}
	// out 是写入的行集合。
	out := make([]LifecycleBatchRow, 0, len(items))
	// item 是本次流程后续判断的item。
	// i 是本次流程后续判断的i。
	for i, item := range items {
		out = append(out, LifecycleBatchRow{ID: int64(i + 1), RowNo: i + 1, CookieID: item.CookieID, ItemID: item.ItemID, Status: "pending"})
	}
	f.rows[id] = out
	return id, nil
}

// GetLifecycleBatch 读取批次与行。
func (f *fakeLifecycleStore) GetLifecycleBatch(ctx context.Context, userID int64, batchID string) (LifecycleBatch, []LifecycleBatchRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// ok 是本次流程后续判断的ok。
	// batch 是本次流程后续判断的batch。
	batch, ok := f.batches[batchID]
	if !ok {
		return LifecycleBatch{}, nil, errors.New("not found")
	}
	return batch, append([]LifecycleBatchRow(nil), f.rows[batchID]...), nil
}

// ListLifecycleBatches 列出批次。
func (f *fakeLifecycleStore) ListLifecycleBatches(ctx context.Context, userID int64, limit int) ([]LifecycleBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// out 是本次流程后续判断的out。
	out := make([]LifecycleBatch, 0, len(f.batches))
	// b 是本次流程后续判断的b。
	for _, b := range f.batches {
		out = append(out, b)
	}
	return out, nil
}

// LifecycleBatchRunningByUser 返回预置的进行中状态。
func (f *fakeLifecycleStore) LifecycleBatchRunningByUser(ctx context.Context, userID int64) (bool, error) {
	return f.runningByUser, nil
}

// UpdateStatus 记录状态写入。
func (f *fakeLifecycleStore) UpdateStatus(ctx context.Context, cookieID, itemID, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = append(f.statuses, [2]string{itemID, status})
	return nil
}

// Delete 记录软删除调用。
func (f *fakeLifecycleStore) Delete(ctx context.Context, cookieID, itemID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, [2]string{cookieID, itemID})
	return nil
}

// ClaimLifecycleBatch 标记批次 running。
func (f *fakeLifecycleStore) ClaimLifecycleBatch(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// ok 是本次流程后续判断的ok。
	// batch 是本次流程后续判断的batch。
	batch, ok := f.batches[batchID]
	if !ok || batch.Status != "pending" {
		return false, nil
	}
	batch.Status = "running"
	f.batches[batchID] = batch
	return true, nil
}

// RenewLifecycleBatchLease 始终成功。
func (f *fakeLifecycleStore) RenewLifecycleBatchLease(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	return true, nil
}

// PendingLifecycleRows 返回 pending 行。
func (f *fakeLifecycleStore) PendingLifecycleRows(ctx context.Context, batchID string) ([]LifecycleBatchRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// out 是本次流程后续判断的out。
	out := make([]LifecycleBatchRow, 0)
	// row 是本次流程后续判断的row。
	for _, row := range f.rows[batchID] {
		if row.Status == "pending" {
			out = append(out, row)
		}
	}
	return out, nil
}

// ClaimLifecycleRow 原子抢占行；与真实仓储一致按 pending 状态判定，重试后可再次抢占。
func (f *fakeLifecycleStore) ClaimLifecycleRow(ctx context.Context, rowID int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// batchID 是本次流程后续判断的batchID。
	for batchID := range f.rows {
		// i 是本次流程后续判断的i。
		for i := range f.rows[batchID] {
			if f.rows[batchID][i].ID == rowID {
				if f.rows[batchID][i].Status != "pending" {
					return false, nil
				}
				f.rows[batchID][i].Status = "running"
				return true, nil
			}
		}
	}
	return false, nil
}

// CompleteLifecycleRow 标记行成功。
func (f *fakeLifecycleStore) CompleteLifecycleRow(ctx context.Context, rowID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// batchID 是本次流程后续判断的batchID。
	for batchID := range f.rows {
		// i 是本次流程后续判断的i。
		for i := range f.rows[batchID] {
			if f.rows[batchID][i].ID == rowID {
				f.rows[batchID][i].Status = "success"
			}
		}
	}
	return nil
}

// FailLifecycleRow 标记行失败。
func (f *fakeLifecycleStore) FailLifecycleRow(ctx context.Context, rowID int64, failureKind, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// batchID 是本次流程后续判断的batchID。
	for batchID := range f.rows {
		// i 是本次流程后续判断的i。
		for i := range f.rows[batchID] {
			if f.rows[batchID][i].ID == rowID {
				f.rows[batchID][i].Status = "failed"
				f.rows[batchID][i].FailureKind = failureKind
				f.rows[batchID][i].ErrorMessage = message
			}
		}
	}
	return nil
}

// RecountLifecycleBatch 收口批次状态。
func (f *fakeLifecycleStore) RecountLifecycleBatch(ctx context.Context, batchID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// pending、success、failed 统计行状态。
	var pending, success, failed int
	// row 是本次流程后续判断的row。
	for _, row := range f.rows[batchID] {
		switch row.Status {
		case "pending":
			pending++
		case "success":
			success++
		case "failed":
			failed++
		}
	}
	// batch 更新计数与终态。
	batch := f.batches[batchID]
	batch.SuccessCount, batch.FailedCount = success, failed
	if pending == 0 {
		if failed > 0 {
			batch.Status = "partially_failed"
		} else {
			batch.Status = "completed"
		}
	}
	f.batches[batchID] = batch
	return nil
}

// FailLifecycleBatch 置批次为部分失败。
func (f *fakeLifecycleStore) FailLifecycleBatch(ctx context.Context, batchID, workerToken string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// ok 是本次流程后续判断的ok。
	// batch 是本次流程后续判断的batch。
	batch, ok := f.batches[batchID]
	if !ok {
		return false, nil
	}
	batch.Status = "partially_failed"
	f.batches[batchID] = batch
	return true, nil
}

// RetryLifecycleFailedRows 重置失败行。
func (f *fakeLifecycleStore) RetryLifecycleFailedRows(ctx context.Context, userID int64, batchID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// ok 是本次流程后续判断的ok。
	// batch 是本次流程后续判断的batch。
	batch, ok := f.batches[batchID]
	if !ok {
		return 0, errors.New("not found")
	}
	// count 统计重置数量。
	var count int64
	// i 是本次流程后续判断的i。
	for i := range f.rows[batchID] {
		if f.rows[batchID][i].Status == "failed" {
			f.rows[batchID][i].Status = "pending"
			count++
		}
	}
	batch.Status = "pending"
	f.batches[batchID] = batch
	return count, nil
}

// CancelLifecycleBatch 取消批次。
func (f *fakeLifecycleStore) CancelLifecycleBatch(ctx context.Context, userID int64, batchID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// ok 是本次流程后续判断的ok。
	// batch 是本次流程后续判断的batch。
	batch, ok := f.batches[batchID]
	if !ok || (batch.Status != "pending" && batch.Status != "running") {
		return false, nil
	}
	batch.Status = "canceled"
	f.batches[batchID] = batch
	return true, nil
}

// fakeLifecyclePlatform 是平台替身；按脚本逐行返回。
type fakeLifecyclePlatform struct {
	// mu 保护调用记录。
	mu sync.Mutex
	// calls 依次记录 (action,itemID)。
	calls []string
	// script 是每次调用的 (ok, ret, err) 队列。
	script []fakePlatformOutcome
}

// fakePlatformOutcome 是一次平台调用的预定结果。
type fakePlatformOutcome struct {
	// ok 表示是否成功。
	ok bool
	// ret 是平台返回。
	ret []string
	// err 是平台错误。
	err error
}

// DownshelfItem 记录并弹出脚本结果。
func (f *fakeLifecyclePlatform) DownshelfItem(ctx context.Context, cookieID, itemID string) (bool, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "downshelf/"+itemID)
	if len(f.script) == 0 {
		return true, []string{"SUCCESS::ok"}, nil
	}
	// out 是本次流程后续判断的out。
	out := f.script[0]
	f.script = f.script[1:]
	return out.ok, out.ret, out.err
}

// DeleteItem 记录并弹出脚本结果。
func (f *fakeLifecyclePlatform) DeleteItem(ctx context.Context, cookieID, itemID string) (bool, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "delete/"+itemID)
	if len(f.script) == 0 {
		return true, []string{"SUCCESS::ok"}, nil
	}
	// out 是本次流程后续判断的out。
	out := f.script[0]
	f.script = f.script[1:]
	return out.ok, out.ret, out.err
}

// fakeLifecycleOwnership 是归属校验替身。
type fakeLifecycleOwnership struct {
	// owned 预置归属结果。
	owned bool
}

// CookieOwned 返回预置归属。
func (f *fakeLifecycleOwnership) CookieOwned(ctx context.Context, userID int64, cookieID string) (bool, error) {
	return f.owned, nil
}

// TestLifecycleSingleDownshelfAndDelete 覆盖单商品下架/删除的编排与本地收口。
func TestLifecycleSingleDownshelfAndDelete(t *testing.T) {
	// store、platform、ownership 是替身三件套。
	store := newFakeLifecycleStore()
	// platform 是本次流程后续判断的platform。
	platform := &fakeLifecyclePlatform{}
	// service 构造被测服务。
	service, err := NewItemLifecycleService(store, platform, &fakeLifecycleOwnership{owned: true}, func(err error) string { return "platform" })
	if err != nil {
		t.Fatal(err)
	}
	// ctx 保存测试上下文。
	ctx := context.Background()
	// result、err 保存下架结果。
	result, err := service.Downshelf(ctx, 1, "acc-1", "item-1")
	if err != nil || !result.OK {
		t.Fatalf("下架失败 result=%+v err=%v", result, err)
	}
	if len(store.statuses) != 1 || store.statuses[0][0] != "item-1" || store.statuses[0][1] != "down" {
		t.Fatalf("下架应写 status=down statuses=%v", store.statuses)
	}
	// result、err 保存删除结果。
	result, err = service.Delete(ctx, 1, "acc-1", "item-2")
	if err != nil || !result.OK {
		t.Fatalf("删除失败 result=%+v err=%v", result, err)
	}
	if len(store.deleted) != 1 || store.deleted[0][1] != "item-2" {
		t.Fatalf("删除应收口软删除 deleted=%v", store.deleted)
	}
	// 归属失败必须拒绝且不调用平台。
	// service2 构造归属为假的第二服务。
	service2, _ := NewItemLifecycleService(store, platform, &fakeLifecycleOwnership{owned: false}, func(err error) string { return "platform" })
	// err 是本次流程后续判断的err。
	if _, err := service2.Downshelf(ctx, 1, "acc-x", "item-x"); !errors.Is(err, ErrLifecycleNotOwned) {
		t.Fatalf("归属失败应返回 ErrLifecycleNotOwned，实际 %v", err)
	}
	// 平台失败必须返回错误且不落本地状态。
	platform.script = []fakePlatformOutcome{{ok: false, ret: []string{"FAIL_BIZ_X::拒绝"}}}
	// err 是本次流程后续判断的err。
	if _, err := service.Downshelf(ctx, 1, "acc-1", "item-9"); !errors.Is(err, ErrLifecyclePlatformFailed) {
		t.Fatalf("平台失败应返回 ErrLifecyclePlatformFailed，实际 %v", err)
	}
}

// TestLifecycleBatchRunWithRiskStop 覆盖批次串行执行、抖动路径与风控即停。
func TestLifecycleBatchRunWithRiskStop(t *testing.T) {
	// store、platform、ownership 是替身三件套。
	store := newFakeLifecycleStore()
	// platform 是本次流程后续判断的platform。
	platform := &fakeLifecyclePlatform{}
	// service 构造被测服务。
	service, err := NewItemLifecycleService(store, platform, &fakeLifecycleOwnership{owned: true}, func(err error) string { return "risk" })
	if err != nil {
		t.Fatal(err)
	}
	// 测试把抖动压缩到 0-1 秒，避免用例等待真实 5-10 秒间隔。
	service.jitterMin, service.jitterMax = 0, 1
	// batchID、err 保存三行批次创建结果。
	batchID, err := service.CreateBatch(context.Background(), 1, CreateLifecycleBatchInput{
		Action: LifecycleActionDownshelf,
		Items:  []LifecycleTargetItem{{CookieID: "a", ItemID: "i1"}, {CookieID: "a", ItemID: "i2"}, {CookieID: "a", ItemID: "i3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// busy 状态下创建新批次必须被拒绝。
	store.runningByUser = true
	// err 是本次流程后续判断的err。
	if _, err := service.CreateBatch(context.Background(), 1, CreateLifecycleBatchInput{Action: LifecycleActionDownshelf, Items: []LifecycleTargetItem{{CookieID: "a", ItemID: "i4"}}}); !errors.Is(err, ErrLifecycleBatchBusy) {
		t.Fatalf("进行中批次应拒绝新建，实际 %v", err)
	}
	store.runningByUser = false
	// 平台在第二行返回风控错误：第一行成功、批次中止、第三行保持 pending。
	platform.script = []fakePlatformOutcome{
		{ok: true, ret: []string{"SUCCESS::ok"}},
		{ok: false, ret: []string{"FAIL_SYS_USER_VALIDATE"}, err: errors.New("风控")},
	}
	// runErr 保存批次执行返回错误。
	runErr := service.RunLifecycleBatch(context.Background(), 1, batchID)
	if runErr == nil || !strings.Contains(runErr.Error(), "风控") {
		t.Fatalf("风控应中止批次并返回错误，实际 %v", runErr)
	}
	// batch、rows、err 保存中止后批次状态。
	batch, rows, err := service.GetBatch(context.Background(), 1, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.calls) != 2 {
		t.Fatalf("风控即停：平台调用应恰好 2 次，实际 %v", platform.calls)
	}
	if batch.Status != "partially_failed" {
		t.Fatalf("风控中止后批次应保持 partially_failed，实际 %s", batch.Status)
	}
	// 行状态：i1 success、i2 failed(risk)、i3 pending。
	statuses := map[string]string{}
	// row 是本次流程后续判断的row。
	for _, row := range rows {
		statuses[row.ItemID] = row.Status
	}
	if statuses["i1"] != "success" || statuses["i2"] != "failed" || statuses["i3"] != "pending" {
		t.Fatalf("行状态不符 %v", statuses)
	}
	// 重试失败行后重新执行应完成批次。
	if _, err := service.RetryFailed(context.Background(), 1, batchID); err != nil {
		t.Fatal(err)
	}
	// err 是本次流程后续判断的err。
	if err := service.RunLifecycleBatch(context.Background(), 1, batchID); err != nil {
		t.Fatalf("重试执行应成功 err=%v", err)
	}
	// batch、rows、err 保存最终批次状态。
	batch, _, err = service.GetBatch(context.Background(), 1, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != "completed" {
		t.Fatalf("重试后批次应收口 completed，实际 %s", batch.Status)
	}
}
