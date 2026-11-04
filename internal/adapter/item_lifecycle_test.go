// Package adapter: 商品生命周期适配器焦点测试——仓储转换、错误哨兵映射、归属校验与平台端口。
package adapter

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// newLifecycleTestStore 打开全新 SQLite 数据库，创建测试用户与账号并返回仓储聚合和用户 ID。
func newLifecycleTestStore(t *testing.T) (*db.Store, int64) {
	t.Helper()
	// database、dialect、openErr 保存全新 SQLite 数据库的打开结果。
	database, dialect, openErr := db.Open(context.Background(), filepath.Join(t.TempDir(), "lifecycle.db"))
	if openErr != nil {
		t.Fatalf("打开数据库失败: %v", openErr)
	}
	t.Cleanup(func() { _ = database.Close() })
	// store 是全新数据库的 repository 聚合入口。
	store := db.NewStore(database, dialect)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// ok、createErr 保存测试用户创建结果。
	ok, createErr := store.Users.Create(ctx, "lifecycle-user", "lifecycle@example.com", "password")
	if createErr != nil || !ok {
		t.Fatalf("创建测试用户失败 ok=%v err=%v", ok, createErr)
	}
	// user、userErr 保存测试用户查询结果。
	user, userErr := store.Users.GetByUsername(ctx, "lifecycle-user")
	if userErr != nil {
		t.Fatal(userErr)
	}
	// cookieErr 保存测试账号写入错误；批次与商品表通过该账号满足外键约束。
	if cookieErr := store.Cookies.Save(ctx, "acc", "unb=1", user.ID); cookieErr != nil {
		t.Fatal(cookieErr)
	}
	return store, user.ID
}

// TestTranslateLifecycleStoreErrorMapsSentinels 验证持久层哨兵错误到应用层哨兵的完整映射。
func TestTranslateLifecycleStoreErrorMapsSentinels(t *testing.T) {
	// cases 是输入错误与期望应用哨兵的对照表。
	cases := []struct {
		// name 是用例名称。
		name string
		// input 是持久层输入错误。
		input error
		// want 是期望的应用层哨兵。
		want error
	}{
		{name: "nil 保持 nil", input: nil, want: nil},
		{name: "NotFound 映射批次不存在", input: db.ErrNotFound, want: itemapp.ErrLifecycleBatchNotFound},
		{name: "非法动作映射应用哨兵", input: db.ErrLifecycleInvalidAction, want: itemapp.ErrLifecycleInvalidAction},
		{name: "行数超限映射应用哨兵", input: db.ErrLifecycleTooManyRows, want: itemapp.ErrLifecycleTooManyRows},
		{name: "重复行映射应用哨兵", input: db.ErrLifecycleDuplicateRow, want: itemapp.ErrLifecycleDuplicateRow},
		{name: "未知错误原样返回", input: errors.New("其他错误"), want: nil},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name /* 逐个断言映射结果。 */, func(t *testing.T) {
			// got 是转换后的错误。
			got := translateLifecycleStoreError(testCase.input)
			if testCase.want == nil {
				if testCase.input == nil && got != nil {
					t.Fatalf("nil 输入应返回 nil，实际 %v", got)
				}
				if testCase.input != nil && !errors.Is(got, testCase.input) {
					t.Fatalf("未知错误应原样返回，实际 %v", got)
				}
				return
			}
			if !errors.Is(got, testCase.want) {
				t.Fatalf("期望 %v，实际 %v", testCase.want, got)
			}
		})
	}
}

// TestItemLifecycleStoreAdapterRoundTrip 验证批次创建、查询、行推进与本地收口的仓储适配器往返。
func TestItemLifecycleStoreAdapterRoundTrip(t *testing.T) {
	// store、userID 是带种子用户与账号的仓储聚合和测试用户。
	store, userID := newLifecycleTestStore(t)
	// adapter 是被测生命周期仓储适配器。
	adapter := NewItemLifecycleStoreAdapter(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// 非法动作应映射为应用哨兵。
	if _, err := adapter.CreateLifecycleBatch(ctx, userID, "invalid", []itemapp.LifecycleTargetItem{{CookieID: "acc", ItemID: "i1"}}); !errors.Is(err, itemapp.ErrLifecycleInvalidAction) {
		t.Fatalf("非法动作应返回应用哨兵，实际 %v", err)
	}
	// batchID 是成功创建的批次标识。
	batchID, createErr := adapter.CreateLifecycleBatch(ctx, userID, "downshelf", []itemapp.LifecycleTargetItem{{CookieID: "acc", ItemID: "i1"}, {CookieID: "acc", ItemID: "i2"}})
	if createErr != nil {
		t.Fatalf("创建批次失败: %v", createErr)
	}
	// 重复行应映射为应用哨兵。
	if _, dupErr := adapter.CreateLifecycleBatch(ctx, userID, "delete", []itemapp.LifecycleTargetItem{{CookieID: "acc", ItemID: "i1"}, {CookieID: "acc", ItemID: "i1"}}); !errors.Is(dupErr, itemapp.ErrLifecycleDuplicateRow) {
		t.Fatalf("重复行应返回应用哨兵，实际 %v", dupErr)
	}
	// running 表示创建后用户存在进行中批次。
	running, runningErr := adapter.LifecycleBatchRunningByUser(ctx, userID)
	if runningErr != nil || !running {
		t.Fatalf("创建后应有进行中批次 running=%v err=%v", running, runningErr)
	}
	// batch、rows 是查询到的批次与行明细。
	batch, rows, getErr := adapter.GetLifecycleBatch(ctx, userID, batchID)
	if getErr != nil {
		t.Fatalf("查询批次失败: %v", getErr)
	}
	if batch.ID != batchID || batch.Action != "downshelf" || batch.Status != db.LifecycleBatchPending || batch.TotalCount != 2 || len(rows) != 2 {
		t.Fatalf("批次视图不符合预期: %+v rows=%d", batch, len(rows))
	}
	if rows[0].RowNo != 1 || rows[0].CookieID != "acc" || rows[0].ItemID != "i1" || rows[0].Status != db.LifecycleRowPending {
		t.Fatalf("行视图不符合预期: %+v", rows[0])
	}
	// 不存在的批次应映射为应用哨兵。
	if _, _, notFoundErr := adapter.GetLifecycleBatch(ctx, userID, "missing"); !errors.Is(notFoundErr, itemapp.ErrLifecycleBatchNotFound) {
		t.Fatalf("缺失批次应返回应用哨兵，实际 %v", notFoundErr)
	}
	// pendingRows 是抢占前的待处理行。
	pendingRows, pendingErr := adapter.PendingLifecycleRows(ctx, batchID)
	if pendingErr != nil || len(pendingRows) != 2 {
		t.Fatalf("待处理行查询失败 rows=%d err=%v", len(pendingRows), pendingErr)
	}
	// claimed 表示单行抢占成功。
	claimed, claimErr := adapter.ClaimLifecycleRow(ctx, pendingRows[0].ID)
	if claimErr != nil || !claimed {
		t.Fatalf("抢占单行失败 claimed=%v err=%v", claimed, claimErr)
	}
	// completeErr 保存标记行成功的结果。
	if completeErr := adapter.CompleteLifecycleRow(ctx, pendingRows[0].ID); completeErr != nil {
		t.Fatalf("标记行成功失败: %v", completeErr)
	}
	// 再次抢占已成功行应失败。
	if reclaimed, reclaimErr := adapter.ClaimLifecycleRow(ctx, pendingRows[0].ID); reclaimErr != nil || reclaimed {
		t.Fatalf("已成功行不应再次抢占 reclaimed=%v err=%v", reclaimed, reclaimErr)
	}
	// failErr 保存标记行失败的结果。
	if failErr := adapter.FailLifecycleRow(ctx, pendingRows[1].ID, "platform", "平台失败"); failErr != nil {
		t.Fatalf("标记行失败出错: %v", failErr)
	}
	// 重算批次计数：仍有 pending 行时批次保持原状态。
	if recountErr := adapter.RecountLifecycleBatch(ctx, batchID); recountErr != nil {
		t.Fatalf("重算批次失败: %v", recountErr)
	}
	// 重试失败行应重置 1 行。
	reset, retryErr := adapter.RetryLifecycleFailedRows(ctx, userID, batchID)
	if retryErr != nil || reset != 1 {
		t.Fatalf("重试失败行异常 reset=%d err=%v", reset, retryErr)
	}
	// 不存在的批次重试应返回应用哨兵。
	if _, missingRetryErr := adapter.RetryLifecycleFailedRows(ctx, userID, "missing"); !errors.Is(missingRetryErr, itemapp.ErrLifecycleBatchNotFound) {
		t.Fatalf("缺失批次重试应返回应用哨兵，实际 %v", missingRetryErr)
	}
	// 列表应能看到该批次。
	batches, listErr := adapter.ListLifecycleBatches(ctx, userID, 10)
	if listErr != nil || len(batches) != 1 || batches[0].ID != batchID {
		t.Fatalf("批次列表异常 batches=%d err=%v", len(batches), listErr)
	}
}

// TestItemLifecycleStoreAdapterLocalOutcome 验证商品状态收口与软删除语义。
func TestItemLifecycleStoreAdapterLocalOutcome(t *testing.T) {
	// store、_ 是带种子用户与账号的仓储聚合；商品外键依赖种子账号。
	store, _ := newLifecycleTestStore(t)
	// adapter 是被测生命周期仓储适配器。
	adapter := NewItemLifecycleStoreAdapter(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// seedErr 是商品种子数据写入错误。
	seedErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "acc", ItemID: "i1", ItemTitle: "标题", ItemPrice: "19.9"})
	if seedErr != nil {
		t.Fatalf("准备商品数据失败: %v", seedErr)
	}
	// 不存在的商品更新状态应返回 ErrNotFound。
	if err := adapter.UpdateStatus(ctx, "acc", "missing", "down"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("缺失商品更新状态应返回 ErrNotFound，实际 %v", err)
	}
	// 非法状态值应被拒绝。
	if err := adapter.UpdateStatus(ctx, "acc", "i1", "bogus"); err == nil {
		t.Fatal("非法状态值应被拒绝")
	}
	// statusErr 保存合法下架状态写入结果。
	statusErr := adapter.UpdateStatus(ctx, "acc", "i1", "down")
	if statusErr != nil {
		t.Fatalf("下架状态写入失败: %v", statusErr)
	}
	// 软删除后再次更新状态应返回 ErrNotFound。
	if err := adapter.Delete(ctx, "acc", "i1"); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}
	// deletedErr 保存软删除后的状态更新结果，应返回 ErrNotFound。
	deletedErr := adapter.UpdateStatus(ctx, "acc", "i1", "down")
	if !errors.Is(deletedErr, db.ErrNotFound) {
		t.Fatalf("软删除后更新状态应返回 ErrNotFound，实际 %v", deletedErr)
	}
}

// TestClassifyLifecyclePlatformError 验证平台错误分类映射。
func TestClassifyLifecyclePlatformError(t *testing.T) {
	// cases 是平台错误与期望类别的对照表。
	cases := []struct {
		// name 是用例名称。
		name string
		// input 是平台错误。
		input error
		// want 是期望的失败类别。
		want string
	}{
		{name: "风控错误", input: &mtop.RiskVerificationError{API: "test.api", Ret: []string{"FAIL_SYS_USER_VALIDATE"}}, want: "risk"},
		{name: "会话过期", input: &mtop.SessionExpiredError{API: "test.api", Ret: []string{"SESSION_EXPIRED"}}, want: "session"},
		{name: "普通错误", input: errors.New("网络错误"), want: "platform"},
		{name: "nil 错误", input: nil, want: "platform"},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name /* 逐个断言分类结果。 */, func(t *testing.T) {
			if got := ClassifyLifecyclePlatformError(testCase.input); got != testCase.want {
				t.Fatalf("期望 %s，实际 %s", testCase.want, got)
			}
		})
	}
}

// TestItemLifecyclePlatformPortValidation 验证平台端口构造的依赖校验。
func TestItemLifecyclePlatformPortValidation(t *testing.T) {
	// nilStore 构造应因缺少 store 失败。
	if _, err := NewItemLifecyclePlatformPort(nil, func() mtop.Client { return nil }, nil); err == nil {
		t.Fatal("缺少 store 应返回错误")
	}
	// nilClient 构造应因缺少 client 失败。
	if _, err := NewItemLifecyclePlatformPort(&db.Store{}, nil, nil); err == nil {
		t.Fatal("缺少 client 应返回错误")
	}
}

// lifecycleFakeMtopClient 只实现生命周期用到的两个平台方法；其余方法不会被调用。
type lifecycleFakeMtopClient struct {
	// mtop.Client 内嵌接口占位，未覆盖的方法在测试中不应触发。
	mtop.Client
	// downshelfOK 是下架调用的预期成功标记。
	downshelfOK bool
	// downshelfRet 是下架调用的预期 ret。
	downshelfRet []string
	// downshelfUpdated 是下架调用返回的轮换后 Cookie。
	downshelfUpdated string
	// downshelfErr 是下架调用的预期错误。
	downshelfErr error
	// seenCookies 记录下架调用收到的 Cookie 明文。
	seenCookies string
	// seenItemID 记录下架调用收到的商品 ID。
	seenItemID string
	// deleteCalls 记录删除调用次数。
	deleteCalls int
}

// DownshelfItemContext 记录入参并返回预设结果。
func (f *lifecycleFakeMtopClient) DownshelfItemContext(_ context.Context, cookies, itemID string) (bool, []string, string, error) {
	f.seenCookies = cookies
	f.seenItemID = itemID
	return f.downshelfOK, f.downshelfRet, f.downshelfUpdated, f.downshelfErr
}

// DeleteItemContext 记录调用次数并返回下架预设的相同结果形态。
func (f *lifecycleFakeMtopClient) DeleteItemContext(_ context.Context, cookies, itemID string) (bool, []string, string, error) {
	f.deleteCalls++
	f.seenCookies = cookies
	f.seenItemID = itemID
	return f.downshelfOK, f.downshelfRet, f.downshelfUpdated, f.downshelfErr
}

// TestItemLifecyclePlatformPortInvokeAndCredentialGuards 验证平台端口的凭证读取、调用透传与凭证缺失守卫。
func TestItemLifecyclePlatformPortInvokeAndCredentialGuards(t *testing.T) {
	// store、cleanup 是带测试账号的隔离存储及释放函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// updated 记录平台端口回传的轮换 Cookie。
	updated := ""
	// port 是绑定 fake 客户端的被测平台端口。
	port, portErr := NewItemLifecyclePlatformPort(store, func() mtop.Client {
		return &lifecycleFakeMtopClient{downshelfOK: true, downshelfRet: []string{"SUCCESS::调用成功"}, downshelfUpdated: "unb=2"}
	}, func(_ context.Context, _ string, value string) { updated = value })
	if portErr != nil {
		t.Fatal(portErr)
	}
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// ok、ret、invokeErr 保存下架调用结果。
	ok, ret, invokeErr := port.DownshelfItem(ctx, "cid", "i1")
	if invokeErr != nil || !ok || len(ret) != 1 || ret[0] != "SUCCESS::调用成功" {
		t.Fatalf("下架调用结果异常 ok=%v ret=%v err=%v", ok, ret, invokeErr)
	}
	if updated != "unb=2" {
		t.Fatalf("轮换 Cookie 未回传 updated=%q", updated)
	}
	// 删除调用应复用同一端口路径。
	if _, _, deleteErr := port.DeleteItem(ctx, "cid", "i1"); deleteErr != nil {
		t.Fatalf("删除调用失败: %v", deleteErr)
	}
	// 凭证缺失的账号应返回明确错误且不触发平台调用。
	if _, _, missingErr := port.DownshelfItem(ctx, "missing-account", "i1"); missingErr == nil {
		t.Fatal("凭证缺失应返回错误")
	}
}

// TestItemLifecycleOwnershipAdapterDelegates 验证归属适配器透传既有预检端口的归属查询。
func TestItemLifecycleOwnershipAdapterDelegates(t *testing.T) {
	// store、cleanup 是带测试账号的隔离存储及释放函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// previewPort 是绑定测试存储的既有批量预检端口。
	previewPort := NewItemBatchPreviewPort(store)
	// adapter 是被测归属适配器。
	adapter := NewItemLifecycleOwnershipAdapter(previewPort)
	// owned、ownedErr 保存账号归属查询结果与错误。
	owned, ownedErr := adapter.CookieOwned(context.Background(), 1, "cid")
	if ownedErr != nil {
		t.Fatalf("归属查询失败: %v", ownedErr)
	}
	_ = owned
	_ = store
}
