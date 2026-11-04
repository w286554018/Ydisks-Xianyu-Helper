package db

import (
	"context"
	"errors"
	"testing"
)

// TestItemStatusDefaultsAndLifecycle 覆盖商品生命周期状态的默认值、合法状态切换、
// 非法状态拒绝与不存在商品 NotFound 行为。
func TestItemStatusDefaultsAndLifecycle(t *testing.T) {
	// store、cleanup 保存隔离数据库和清理责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// ok、err 保存测试用户创建结果。
	ok, err := store.Users.Create(ctx, "item-status-user", "item-status@example.com", "password")
	if err != nil || !ok {
		t.Fatalf("创建测试用户失败 ok=%v err=%v", ok, err)
	}
	// user、err 保存测试用户查询结果。
	user, err := store.Users.GetByUsername(ctx, "item-status-user")
	if err != nil {
		t.Fatal(err)
	}
	// err 保存测试账号写入错误；商品表通过该账号满足外键约束。
	if err := store.Cookies.Save(ctx, "acc-status", "unb=1", user.ID); err != nil {
		t.Fatal(err)
	}
	// err 保存初始商品写入错误；未显式指定 status，应落库为默认 active。
	if err := store.Items.Upsert(ctx, &ItemInfoRow{CookieID: "acc-status", ItemID: "item-1", ItemTitle: "状态商品"}); err != nil {
		t.Fatal(err)
	}
	// rows、err 保存账号商品列表查询结果。
	rows, err := store.Items.AllForCookie(ctx, "acc-status")
	if err != nil || len(rows) != 1 {
		t.Fatalf("查询账号商品失败 rows=%d err=%v", len(rows), err)
	}
	if rows[0].Status != ItemStatusActive {
		t.Fatalf("新商品默认状态应为 active，实际 %q", rows[0].Status)
	}
	// err 保存下架状态写入错误。
	if err := store.Items.UpdateStatus(ctx, "acc-status", "item-1", ItemStatusDown); err != nil {
		t.Fatalf("下架状态写入失败: %v", err)
	}
	// rows、err 保存下架后商品列表查询结果。
	rows, err = store.Items.AllForCookie(ctx, "acc-status")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Status != ItemStatusDown {
		t.Fatalf("下架后状态应为 down，实际 %q", rows[0].Status)
	}
	// err 保存恢复在售写入错误，验证状态可回写。
	if err := store.Items.UpdateStatus(ctx, "acc-status", "item-1", ItemStatusActive); err != nil {
		t.Fatalf("恢复在售写入失败: %v", err)
	}
	// row、err 保存恢复后商品详情查询结果。
	row, err := store.Items.GetByCookieItem(ctx, "acc-status", "item-1")
	if err != nil || row.Status != ItemStatusActive {
		t.Fatalf("恢复后状态应为 active row=%+v err=%v", row, err)
	}
	// err 保存非法状态写入错误；非法值必须被拒绝而不是静默归一化。
	if err := store.Items.UpdateStatus(ctx, "acc-status", "item-1", "gone"); err == nil {
		t.Fatal("非法状态值应返回错误")
	}
	// err 保存不存在商品的状态写入错误。
	if err := store.Items.UpdateStatus(ctx, "acc-status", "missing-item", ItemStatusDown); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在商品应返回 ErrNotFound，实际 %v", err)
	}
}

// TestItemUpsertPreservesStatus 覆盖平台全量同步 Upsert 不重置生命周期状态的约束：
// 同步只覆盖内容字段，已下架商品的 status 必须保留，否则全量同步会复活下架展示状态。
func TestItemUpsertPreservesStatus(t *testing.T) {
	// store、cleanup 保存隔离数据库和清理责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// ok、err 保存测试用户创建结果。
	ok, err := store.Users.Create(ctx, "item-sync-status-user", "item-sync-status@example.com", "password")
	if err != nil || !ok {
		t.Fatalf("创建测试用户失败 ok=%v err=%v", ok, err)
	}
	// user、err 保存测试用户查询结果。
	user, err := store.Users.GetByUsername(ctx, "item-sync-status-user")
	if err != nil {
		t.Fatal(err)
	}
	// err 保存测试账号写入错误。
	if err := store.Cookies.Save(ctx, "acc-sync", "unb=1", user.ID); err != nil {
		t.Fatal(err)
	}
	// err 保存初始商品写入错误。
	if err := store.Items.Upsert(ctx, &ItemInfoRow{CookieID: "acc-sync", ItemID: "item-sync", ItemTitle: "同步商品"}); err != nil {
		t.Fatal(err)
	}
	// err 保存下架状态写入错误。
	if err := store.Items.UpdateStatus(ctx, "acc-sync", "item-sync", ItemStatusDown); err != nil {
		t.Fatal(err)
	}
	// result、err 保存一次全量同步结果；同步内容更新标题但不得触碰 status。
	result, err := store.Items.SyncFromRemote(ctx, "acc-sync", []ItemInfoRow{{ItemID: "item-sync", ItemTitle: "同步商品-新标题"}})
	if err != nil || result.Saved != 1 {
		t.Fatalf("全量同步失败 result=%+v err=%v", result, err)
	}
	// row、err 保存同步后商品详情查询结果。
	row, err := store.Items.GetByCookieItem(ctx, "acc-sync", "item-sync")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != ItemStatusDown {
		t.Fatalf("同步后已下架状态应保留为 down，实际 %q", row.Status)
	}
	if row.ItemTitle != "同步商品-新标题" {
		t.Fatalf("同步应更新标题，实际 %q", row.ItemTitle)
	}
}
