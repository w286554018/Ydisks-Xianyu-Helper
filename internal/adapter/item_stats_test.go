// Package adapter: 商品统计仓储适配器的模型转换、批量读取与错误分支焦点测试。
package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// newItemStatsAdapterStore 打开全新测试数据库并写入一个用户与一个账号，返回仓储与账号标识。
func newItemStatsAdapterStore(t *testing.T) (*db.Store, string) {
	t.Helper()
	// store、cleanup 是测试数据库与关闭函数。
	store, cleanup := newDeliveryTemplateAdapterStore(t)
	t.Cleanup(cleanup)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// result、userErr 是测试用户写入结果与错误；item_stats 的账号外键依赖用户。
	result, userErr := store.DB.ExecContext(ctx, `INSERT INTO users (username, email, password_hash) VALUES ('stats-user','stats@example.test','x')`)
	if userErr != nil {
		t.Fatalf("写入测试用户失败: %v", userErr)
	}
	// userID 是新建用户主键。
	userID, _ := result.LastInsertId()
	// cookieID 是本次测试使用的账号标识。
	cookieID := "stats-account"
	if // saveErr 是测试账号写入失败原因。
	saveErr := store.Cookies.Save(ctx, cookieID, "unb=1", userID); saveErr != nil {
		t.Fatalf("写入测试账号失败: %v", saveErr)
	}
	return store, cookieID
}

// TestItemStatsStoreRoundTrip 验证应用层统计模型与持久层的往返转换、覆盖写入与批量读取。
func TestItemStatsStoreRoundTrip(t *testing.T) {
	// store、cookieID 是含一个账号的测试数据库与账号标识。
	store, cookieID := newItemStatsAdapterStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// repository 是被测统计仓储适配器。
	repository := NewItemStatsStore(store)
	// fetchedAt 是显式给出的抓取时间，用于验证时间往返。
	fetchedAt := time.Date(2026, 11, 20, 8, 30, 0, 0, time.UTC)

	// 首次写入。
	first := itemapp.ItemStats{CookieID: cookieID, ItemID: "item-1", BrowseCnt: 22, WantCnt: 2, CollectCnt: 1, SoldCnt: 0, FetchedAt: fetchedAt}
	if // saveErr 是首次写入返回的错误。
	saveErr := repository.UpsertItemStats(ctx, first); saveErr != nil {
		t.Fatalf("写入商品统计失败: %v", saveErr)
	}
	// rows、listErr 是批量读取结果与错误。
	rows, listErr := repository.ListItemStats(ctx, cookieID, []string{"item-1", "item-missing"})
	if listErr != nil {
		t.Fatalf("批量读取商品统计失败: %v", listErr)
	}
	// loaded 是读取到的统计；未抓取的商品不得出现在结果里。
	loaded, ok := rows["item-1"]
	if !ok || len(rows) != 1 {
		t.Fatalf("批量读取结果不符: %+v", rows)
	}
	if loaded.CookieID != cookieID || loaded.ItemID != "item-1" || loaded.BrowseCnt != 22 || loaded.WantCnt != 2 ||
		loaded.CollectCnt != 1 || loaded.SoldCnt != 0 {
		t.Fatalf("统计字段转换不符: %+v", loaded)
	}
	if loaded.FetchedAt.IsZero() || loaded.FetchedAt.UTC().Year() != 2026 {
		t.Fatalf("抓取时间未正确往返: %+v", loaded.FetchedAt)
	}
	// Fresh 由应用层按当前时间判定，适配器必须保持零值而不是替应用层做判断。
	if loaded.Fresh {
		t.Fatalf("适配器不得代替应用层判定新鲜度: %+v", loaded)
	}

	// 覆盖写入同一 (账号, 商品) 必须更新而不是新增。
	if // overwriteErr 是覆盖写入返回的错误。
	overwriteErr := repository.UpsertItemStats(ctx, itemapp.ItemStats{CookieID: cookieID, ItemID: "item-1", BrowseCnt: 30, WantCnt: 5}); overwriteErr != nil {
		t.Fatalf("覆盖商品统计失败: %v", overwriteErr)
	}
	// updated、updatedErr 是覆盖后的读取结果与错误。
	updated, updatedErr := repository.ListItemStats(ctx, cookieID, []string{"item-1"})
	if updatedErr != nil || updated["item-1"].BrowseCnt != 30 || updated["item-1"].WantCnt != 5 {
		t.Fatalf("覆盖结果不符: %+v err=%v", updated, updatedErr)
	}
	// count 是统计表行数，用于确认没有产生重复行。
	var count int
	if // countErr 是行数统计返回的错误。
	countErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM item_stats`).Scan(&count); countErr != nil || count != 1 {
		t.Fatalf("统计表行数不符: count=%d err=%v", count, countErr)
	}
}

// TestItemStatsStoreEmptyInputs 验证空账号与空商品集合的短路行为。
func TestItemStatsStoreEmptyInputs(t *testing.T) {
	// store、_ 是含一个账号的测试数据库。
	store, _ := newItemStatsAdapterStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// repository 是被测统计仓储适配器。
	repository := NewItemStatsStore(store)

	// 空账号必须返回空结果而不是跨账号数据。
	if // blankAccount、blankAccountErr 是空账号查询的结果与错误。
	blankAccount, blankAccountErr := repository.ListItemStats(ctx, "  ", []string{"item-1"}); blankAccountErr != nil || len(blankAccount) != 0 {
		t.Fatalf("空账号应返回空结果: rows=%d err=%v", len(blankAccount), blankAccountErr)
	}
	// 空商品集合必须返回空结果。
	if // emptyItems、emptyItemsErr 是空商品集合查询的结果与错误。
	emptyItems, emptyItemsErr := repository.ListItemStats(ctx, "stats-account", nil); emptyItemsErr != nil || len(emptyItems) != 0 {
		t.Fatalf("空商品集合应返回空结果: rows=%d err=%v", len(emptyItems), emptyItemsErr)
	}
}

// TestItemStatsStoreErrors 验证依赖缺失、唯一键缺失与数据库不可用时的错误分支。
func TestItemStatsStoreErrors(t *testing.T) {
	// store、cookieID 是含一个账号的测试数据库与账号标识。
	store, cookieID := newItemStatsAdapterStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// 未注入仓储的适配器必须返回初始化错误而不是 panic。
	// nilStore 是未注入数据库聚合的适配器。
	nilStore := NewItemStatsStore(nil)
	// nilListErr 是未初始化适配器批量读取返回的错误。
	if _, nilListErr := nilStore.ListItemStats(ctx, cookieID, []string{"item-1"}); nilListErr == nil {
		t.Fatal("未初始化适配器批量读取应报错")
	}
	// nilWriteErr 是未初始化适配器写入返回的错误。
	if nilWriteErr := nilStore.UpsertItemStats(ctx, itemapp.ItemStats{CookieID: cookieID, ItemID: "item-1"}); nilWriteErr == nil {
		t.Fatal("未初始化适配器写入应报错")
	}
	// 数据库聚合存在但统计仓储缺失时同样必须报错。
	// missingRepository 是缺少统计仓储的适配器。
	missingRepository := NewItemStatsStore(&db.Store{DB: store.DB, Dialect: store.Dialect})
	// missingErr 是缺少统计仓储时读取返回的错误。
	if _, missingErr := missingRepository.ListItemStats(ctx, cookieID, []string{"item-1"}); missingErr == nil {
		t.Fatal("缺少统计仓储时读取应报错")
	}

	// 唯一键缺失由持久层拒绝，适配器必须原样返回该错误。
	// repository 是被测统计仓储适配器。
	repository := NewItemStatsStore(store)
	if // missingKeyErr 是缺少商品标识时的写入错误。
	missingKeyErr := repository.UpsertItemStats(ctx, itemapp.ItemStats{CookieID: cookieID}); missingKeyErr == nil {
		t.Fatal("缺少商品标识时写入应报错")
	}

	// 数据库不可用时读写都必须显式报错。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatalf("关闭测试数据库失败: %v", closeErr)
	}
	// closedListErr 是数据库不可用时批量读取返回的错误。
	if _, closedListErr := repository.ListItemStats(ctx, cookieID, []string{"item-1"}); closedListErr == nil {
		t.Fatal("数据库不可用时批量读取应报错")
	}
	// closedWriteErr 是数据库不可用时的写入错误。
	closedWriteErr := repository.UpsertItemStats(ctx, itemapp.ItemStats{CookieID: cookieID, ItemID: "item-1"})
	if closedWriteErr == nil || errors.Is(closedWriteErr, itemapp.ErrItemStatsNoItems) {
		t.Fatalf("数据库不可用时写入应返回底层错误: %v", closedWriteErr)
	}
}
