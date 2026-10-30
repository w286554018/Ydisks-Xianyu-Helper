package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// newSourcingTestStore 创建用于选品参考仓储测试的 SQLite 库与仓储实例。
func newSourcingTestStore(t *testing.T) (*Sourcing, context.Context) {
	t.Helper()
	// ctx 是测试期间的默认上下文，不设超时以便定位阻塞问题。
	ctx := context.Background()
	// database 是本测试独占的临时 SQLite 连接池，Open 会同时执行全部迁移。
	database, dialect, err := Open(ctx, filepath.Join(t.TempDir(), "sourcing.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return &Sourcing{DB: database, Dialect: dialect}, ctx
}

// TestSourcingSnapshotRoundTrip 验证快照写入、同日覆盖与读取。
func TestSourcingSnapshotRoundTrip(t *testing.T) {
	// store 是临时 SQLite 上已完成迁移的被测仓储；ctx 是测试上下文。
	store, ctx := newSourcingTestStore(t)

	// first 是首次写入的热词快照。
	first := SourcingSnapshot{
		Kind:         SourcingKindHotKeywords,
		SnapshotDate: "2026-10-29",
		PayloadJSON:  `{"keywords":["耳机"]}`,
		Status:       SourcingSnapshotStatusOK,
	}
	// err 是调用 store.SaveSnapshot 的结果，按本用例期望断言成功或特定领域错误。
	if err := store.SaveSnapshot(ctx, first); err != nil {
		t.Fatalf("写入快照失败: %v", err)
	}

	// loaded 是读回的首次快照。
	loaded, err := store.GetSnapshot(ctx, SourcingKindHotKeywords, "2026-10-29")
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if loaded.PayloadJSON != first.PayloadJSON || loaded.Status != SourcingSnapshotStatusOK {
		t.Fatalf("快照内容不符: %+v", loaded)
	}

	// second 是同一天再次写入的快照，应覆盖而非新增。
	second := SourcingSnapshot{
		Kind:         SourcingKindHotKeywords,
		SnapshotDate: "2026-10-29",
		PayloadJSON:  `{"keywords":["电动车"]}`,
		Status:       SourcingSnapshotStatusOK,
	}
	// err 是调用 store.SaveSnapshot 的结果，按本用例期望断言成功或特定领域错误。
	if err := store.SaveSnapshot(ctx, second); err != nil {
		t.Fatalf("覆盖快照失败: %v", err)
	}
	// overwritten 是覆盖后读回的记录。
	overwritten, err := store.GetSnapshot(ctx, SourcingKindHotKeywords, "2026-10-29")
	if err != nil {
		t.Fatalf("覆盖后读取快照失败: %v", err)
	}
	if overwritten.PayloadJSON != second.PayloadJSON {
		t.Fatalf("同日快照未覆盖: %s", overwritten.PayloadJSON)
	}

	// list 应只有一条，证明 upsert 生效。
	list, err := store.ListRecentSnapshots(ctx, SourcingKindHotKeywords, 10)
	if err != nil {
		t.Fatalf("读取快照列表失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("同日快照出现重复, got %d", len(list))
	}
}

// TestSourcingSnapshotFailedStatus 验证采集失败会被记录且原因可见。
func TestSourcingSnapshotFailedStatus(t *testing.T) {
	// store 是临时 SQLite 上已完成迁移的被测仓储；ctx 是测试上下文。
	store, ctx := newSourcingTestStore(t)

	// failed 是采集失败时写入的快照。
	failed := SourcingSnapshot{
		Kind:         SourcingKindHotKeywords,
		SnapshotDate: "2026-10-28",
		PayloadJSON:  "{}",
		Status:       SourcingSnapshotStatusFailed,
		ErrorMessage: "热词接口未返回预期结构",
	}
	// err 是调用 store.SaveSnapshot 的结果，按本用例期望断言成功或特定领域错误。
	if err := store.SaveSnapshot(ctx, failed); err != nil {
		t.Fatalf("写入失败快照出错: %v", err)
	}

	// loaded 是读回的失败快照。
	loaded, err := store.GetSnapshot(ctx, SourcingKindHotKeywords, "2026-10-28")
	if err != nil {
		t.Fatalf("读取失败快照出错: %v", err)
	}
	if loaded.Status != SourcingSnapshotStatusFailed || loaded.ErrorMessage == "" {
		t.Fatalf("失败快照未保留原因: %+v", loaded)
	}
}

// TestSourcingSnapshotUnknownStatusBecomesFailed 验证未知状态不会被当成成功。
func TestSourcingSnapshotUnknownStatusBecomesFailed(t *testing.T) {
	// store 是临时 SQLite 上已完成迁移的被测仓储；ctx 是测试上下文。
	store, ctx := newSourcingTestStore(t)

	// err 是写入未知状态快照的结果；该状态应被归一化为失败。
	err := store.SaveSnapshot(ctx, SourcingSnapshot{
		Kind:         SourcingKindShopRanking,
		SnapshotDate: "2026-10-27",
		PayloadJSON:  "{}",
		Status:       "weird",
	})
	if err != nil {
		t.Fatalf("写入快照失败: %v", err)
	}
	// loaded 是读回的快照。
	loaded, err := store.GetSnapshot(ctx, SourcingKindShopRanking, "2026-10-27")
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if loaded.Status != SourcingSnapshotStatusFailed {
		t.Fatalf("未知状态应归一化为失败, got %s", loaded.Status)
	}
}

// TestSourcingGetSnapshotMissing 验证不存在的快照返回领域错误。
func TestSourcingGetSnapshotMissing(t *testing.T) {
	// store 是临时 SQLite 上已完成迁移的被测仓储；ctx 是测试上下文。
	store, ctx := newSourcingTestStore(t)

	// err 是读取不存在快照的错误，必须是 ErrSourcingSnapshotNotFound。
	_, err := store.GetSnapshot(ctx, SourcingKindHotKeywords, "2000-01-01")
	if err == nil || !isSourcingNotFound(err) {
		t.Fatalf("缺少快照时应返回 ErrSourcingSnapshotNotFound, got %v", err)
	}
}

// isSourcingNotFound 判断错误是否为快照不存在，避免测试直接依赖 errors.Is 的包装细节。
func isSourcingNotFound(err error) bool {
	return err == ErrSourcingSnapshotNotFound
}

// TestSourcingDeleteSnapshotsBefore 验证 30 天保留策略的清理边界。
func TestSourcingDeleteSnapshotsBefore(t *testing.T) {
	// store 是临时 SQLite 上已完成迁移的被测仓储；ctx 是测试上下文。
	store, ctx := newSourcingTestStore(t)

	// dates 是待写入的快照日期集合，涵盖清理边界两侧。
	dates := []string{"2026-09-01", "2026-09-29", "2026-10-29"}
	// date 是待写入快照的归属日。
	for _, date := range dates {
		// err 是调用 store.SaveSnapshot 的结果，按本用例期望断言成功或特定领域错误。
		if err := store.SaveSnapshot(ctx, SourcingSnapshot{
			Kind: SourcingKindHotKeywords, SnapshotDate: date, PayloadJSON: "{}", Status: SourcingSnapshotStatusOK,
		}); err != nil {
			t.Fatalf("写入快照 %s 失败: %v", date, err)
		}
	}

	// affected 是删除条数；严格早于 2026-09-29 的只有一条（09-01）。
	affected, err := store.DeleteSnapshotsBefore(ctx, "2026-09-29")
	if err != nil {
		t.Fatalf("清理快照失败: %v", err)
	}
	if affected != 1 {
		t.Fatalf("清理条数不符, got %d", affected)
	}

	// remaining 是清理后剩余的日期列表。
	remaining, err := store.ListRecentSnapshots(ctx, SourcingKindHotKeywords, 10)
	if err != nil {
		t.Fatalf("清理后读取快照失败: %v", err)
	}
	if len(remaining) != 2 || remaining[0].SnapshotDate != "2026-10-29" || remaining[1].SnapshotDate != "2026-09-29" {
		t.Fatalf("清理结果不符: %+v", remaining)
	}
}

// TestSourcingShopRankingRows 验证畅销榜只读取当前用户、未软删除且落在时间窗口内的订单。
func TestSourcingShopRankingRows(t *testing.T) {
	// store 是临时 SQLite 上已完成迁移的被测仓储；ctx 是测试上下文。
	store, ctx := newSourcingTestStore(t)

	// setup 用原始 SQL 构造用户、账号、商品与订单数据，覆盖各种过滤条件。
	setup := []string{
		`INSERT INTO users (id, username, email, password_hash) VALUES (1,'u1','u1@example.com','x')`,
		`INSERT INTO users (id, username, email, password_hash) VALUES (2,'u2','u2@example.com','x')`,
		`INSERT INTO cookies (id, value, user_id) VALUES ('acc1','v',1)`,
		`INSERT INTO cookies (id, value, user_id) VALUES ('acc2','v',2)`,
		`INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','item-a','商品甲')`,
		`INSERT INTO orders (order_id, item_id, cookie_id, quantity, amount, order_status, created_at) VALUES ('o1','item-a','acc1','2','19.90','已完成','2026-10-28 10:00:00')`,
		`INSERT INTO orders (order_id, item_id, cookie_id, quantity, amount, order_status, created_at) VALUES ('o2','item-a','acc1','很多','abc','已完成','2026-10-28 11:00:00')`,
		`INSERT INTO orders (order_id, item_id, cookie_id, quantity, amount, order_status, created_at) VALUES ('o3','item-b','acc1','1','5.00','已完成','2026-08-01 10:00:00')`,
		`INSERT INTO orders (order_id, item_id, cookie_id, quantity, amount, order_status, created_at) VALUES ('o4','item-c','acc2','9','99.00','已完成','2026-10-28 10:00:00')`,
		`INSERT INTO orders (order_id, item_id, cookie_id, quantity, amount, order_status, created_at, deleted_at) VALUES ('o5','item-a','acc1','7','70.00','已完成','2026-10-28 12:00:00','2026-10-29 09:00:00')`,
	}
	// statement 是一条准备测试数据的 SQL。
	for _, statement := range setup {
		// err 是调用 store.DB.ExecContext 的结果，按本用例期望断言成功或特定领域错误。
		if _, err := store.DB.ExecContext(ctx, statement); err != nil {
			t.Fatalf("准备测试数据失败: %v\nSQL: %s", err, statement)
		}
	}

	// since 是 7 天窗口起点，用于排除 8 月的订单 o3。
	since := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	// rows 是用户 1 在窗口内的订单原始行。
	rows, err := store.ShopRankingRows(ctx, 1, since)
	if err != nil {
		t.Fatalf("读取畅销榜订单失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("订单过滤结果不符, want 2 got %d: %+v", len(rows), rows)
	}
	// row 是按用户与时间窗口过滤后返回的订单行。
	for _, row := range rows {
		if row.ItemID != "item-a" {
			t.Fatalf("出现非目标商品: %+v", row)
		}
		if row.ItemTitle != "商品甲" {
			t.Fatalf("商品标题未关联: %+v", row)
		}
	}
}

// TestParseSourcingDecimal 验证文本金额与数量的安全转换。
func TestParseSourcingDecimal(t *testing.T) {
	// cases 覆盖空值、合法数字、负数与非法文本。
	cases := []struct {
		raw  string
		want float64
	}{
		{"", 0},
		{"  ", 0},
		{"12.5", 12.5},
		{" 7 ", 7},
		{"abc", 0},
		{"很多", 0},
		{"-3", 0},
	}
	// c 是当前用例的输入与期望结果。
	for _, c := range cases {
		// got 是 ParseSourcingDecimal 对当前用例输入的实际结果。
		if got := ParseSourcingDecimal(c.raw); got != c.want {
			t.Fatalf("ParseSourcingDecimal(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

// TestSourcingSnapshotCutoffDate 验证 30 天保留窗口的起始日计算。
func TestSourcingSnapshotCutoffDate(t *testing.T) {
	// location 是快照归属使用的时区。
	location := time.FixedZone("CST", 8*3600)
	// now 是参考时间点。
	now := time.Date(2026, 10, 30, 9, 0, 0, 0, location)
	// got 是保留窗口起始日；含当天共 30 天，因此起点为 10-01。
	got := SourcingSnapshotCutoffDate(now, location)
	if got != "2026-10-01" {
		t.Fatalf("保留窗口起始日不符, got %s", got)
	}
}
