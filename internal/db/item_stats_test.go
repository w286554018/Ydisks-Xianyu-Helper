// Package db: 我的商品统计仓储的读写、批量查询、有效期与错误分支焦点测试。
package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestItemStatsUpsertAndRead 验证统计的首次写入、读取、覆盖更新与唯一键约束。
func TestItemStatsUpsertAndRead(t *testing.T) {
	// store、_ 是全新数据库仓储聚合（内含一个测试账号用于外键约束）。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// 未写入前读取必须返回仓储自有的未找到错误，供调用方判断「未抓取」。
	if _, missingErr := store.ItemStats.GetItemStats(ctx, "acc", "item-1"); !errors.Is(missingErr, ErrItemStatsNotFound) {
		t.Fatalf("无统计数据应返回 ErrItemStatsNotFound, got %v", missingErr)
	}

	// first 是首次写入的统计行；抓取时间显式给出一小时前，用于验证时间被如实落库。
	first := ItemStatsRow{
		CookieID: "acc", ItemID: "item-1",
		BrowseCnt: 22, WantCnt: 2, CollectCnt: 1, SoldCnt: 0,
		FetchedAt: time.Now().UTC().Add(-time.Hour),
	}
	if // createErr 是首次写入返回的错误。
	createErr := store.ItemStats.UpsertItemStats(ctx, first); createErr != nil {
		t.Fatalf("写入商品统计失败: %v", createErr)
	}
	// loaded、loadErr 是写入后的读取结果与错误。
	loaded, loadErr := store.ItemStats.GetItemStats(ctx, "acc", "item-1")
	if loadErr != nil {
		t.Fatalf("读取商品统计失败: %v", loadErr)
	}
	if loaded.BrowseCnt != 22 || loaded.WantCnt != 2 || loaded.CollectCnt != 1 || loaded.SoldCnt != 0 {
		t.Fatalf("商品统计计数不符: %+v", loaded)
	}
	if loaded.FetchedAt.IsZero() {
		t.Fatalf("抓取时间应被解析为非零值: %+v", loaded)
	}
	// 一小时前的抓取时间在 24 小时有效期内。
	if !loaded.IsFresh(time.Now(), time.Hour*24) {
		t.Fatalf("一小时前的统计应仍在有效期内: %+v", loaded)
	}

	// 覆盖同一 (账号, 商品) 必须更新而不是新增，并且计数与抓取时间都要刷新。
	if // overwriteErr 是覆盖写入返回的错误。
	overwriteErr := store.ItemStats.UpsertItemStats(ctx, ItemStatsRow{
		CookieID: "acc", ItemID: "item-1", BrowseCnt: 30, WantCnt: 5, CollectCnt: 2, SoldCnt: 1,
	}); overwriteErr != nil {
		t.Fatalf("覆盖商品统计失败: %v", overwriteErr)
	}
	// overwritten、overwrittenErr 是覆盖后的读取结果与错误。
	overwritten, overwrittenErr := store.ItemStats.GetItemStats(ctx, "acc", "item-1")
	if overwrittenErr != nil {
		t.Fatalf("覆盖后读取失败: %v", overwrittenErr)
	}
	if overwritten.BrowseCnt != 30 || overwritten.WantCnt != 5 || overwritten.CollectCnt != 2 || overwritten.SoldCnt != 1 {
		t.Fatalf("覆盖结果不符: %+v", overwritten)
	}
	// rows 是统计表行数，用于确认 upsert 没有产生重复行。
	var rows int
	if // countErr 是行数统计返回的错误。
	countErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM item_stats`).Scan(&rows); countErr != nil || rows != 1 {
		t.Fatalf("统计表行数不符: rows=%d err=%v", rows, countErr)
	}
}

// TestItemStatsListBatch 验证批量查询的去重、空值过滤、未命中缺省与空参数短路。
func TestItemStatsListBatch(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// seeded 是两个已写入统计的商品，用于验证批量命中。
	seeded := []ItemStatsRow{
		{CookieID: "acc", ItemID: "item-a", BrowseCnt: 11},
		{CookieID: "acc", ItemID: "item-b", BrowseCnt: 12},
	}
	// row 是本次待写入的统计行。
	for _, row := range seeded {
		if // seedErr 是逐行写入返回的错误。
		seedErr := store.ItemStats.UpsertItemStats(ctx, row); seedErr != nil {
			t.Fatalf("写入商品统计失败: %v", seedErr)
		}
	}

	// found、listErr 是批量查询结果与错误；入参刻意包含重复项、空串与未命中商品。
	found, listErr := store.ItemStats.ListItemStats(ctx, "acc", []string{"item-a", "item-a", " ", "item-b", "item-missing"})
	if listErr != nil {
		t.Fatalf("批量读取商品统计失败: %v", listErr)
	}
	if len(found) != 2 || found["item-a"].BrowseCnt != 11 || found["item-b"].BrowseCnt != 12 {
		t.Fatalf("批量读取结果不符: %+v", found)
	}
	// ok 表示未抓取商品是否意外出现在结果里；出现即说明筛选逻辑失效。
	if _, ok := found["item-missing"]; ok {
		t.Fatal("未抓取的商品不应出现在批量结果中")
	}

	// 账号为空与商品列表为空都必须短路返回空映射，避免退化成全表查询。
	if // emptyAccount、emptyAccountErr 是账号为空时的结果与错误。
	emptyAccount, emptyAccountErr := store.ItemStats.ListItemStats(ctx, " ", []string{"item-a"}); emptyAccountErr != nil || len(emptyAccount) != 0 {
		t.Fatalf("账号为空应返回空结果: rows=%d err=%v", len(emptyAccount), emptyAccountErr)
	}
	if // emptyIDs、emptyIDsErr 是商品列表为空时的结果与错误。
	emptyIDs, emptyIDsErr := store.ItemStats.ListItemStats(ctx, "acc", nil); emptyIDsErr != nil || len(emptyIDs) != 0 {
		t.Fatalf("商品列表为空应返回空结果: rows=%d err=%v", len(emptyIDs), emptyIDsErr)
	}
}

// TestItemStatsIsFreshBoundaries 验证有效期判断的零值与边界行为。
func TestItemStatsIsFreshBoundaries(t *testing.T) {
	// now 是判定基准时刻。
	now := time.Now()
	// cases 是抓取时间与期望有效性的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// fetchedAt 是统计行的抓取时间。
		fetchedAt time.Time
		// wantFresh 是期望是否仍在有效期内。
		wantFresh bool
	}{
		{name: "零值无效", fetchedAt: time.Time{}, wantFresh: false},
		{name: "一小时前有效", fetchedAt: now.Add(-time.Hour), wantFresh: true},
		{name: "二十三小时前有效", fetchedAt: now.Add(-23 * time.Hour), wantFresh: true},
		{name: "二十五小时前过期", fetchedAt: now.Add(-25 * time.Hour), wantFresh: false},
	}
	// testCase 是本次断言的有效期用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// row 是待判定的统计行。
			row := ItemStatsRow{ItemID: "item-1", FetchedAt: testCase.fetchedAt}
			if // got 是本次调用的实际返回值。
			got := row.IsFresh(now, 24*time.Hour); got != testCase.wantFresh {
				t.Fatalf("有效期判断不符: got=%v want=%v", got, testCase.wantFresh)
			}
		})
	}
}

// TestItemStatsDialectBranches 验证三方言写入分支各自的写入行为。
func TestItemStatsDialectBranches(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// MySQL 分支的 ON DUPLICATE KEY 语法在 SQLite 上不可执行，必须返回包裹错误而不是静默失败。
	mysqlRepo := &ItemStats{DB: store.ItemStats.DB, Dialect: DialectMySQL}
	if // mysqlErr 是 MySQL 方言分支返回的错误。
	mysqlErr := mysqlRepo.UpsertItemStats(ctx, ItemStatsRow{CookieID: "acc", ItemID: "mysql-item"}); mysqlErr == nil {
		t.Fatal("MySQL 方言分支应返回错误")
	}

	// Postgres 与 SQLite 分支的 ON CONFLICT 语法在 SQLite 上均可执行，写入必须生效且可覆盖。
	for _, dialect := range []Dialect{DialectPostgres, DialectSQLite} {
		// repo 是绑定目标方言的仓储。
		repo := &ItemStats{DB: store.ItemStats.DB, Dialect: dialect}
		// itemID 是本次方言用例的商品标识。
		itemID := "item-" + string(dialect)
		if // saveErr 是本次写入返回的错误。
		saveErr := repo.UpsertItemStats(ctx, ItemStatsRow{CookieID: "acc", ItemID: itemID, BrowseCnt: 7}); saveErr != nil {
			t.Fatalf("方言 %v 写入失败: %v", dialect, saveErr)
		}
		if // againErr 是覆盖同一行的第二次写入错误。
		againErr := repo.UpsertItemStats(ctx, ItemStatsRow{CookieID: "acc", ItemID: itemID, BrowseCnt: 9}); againErr != nil {
			t.Fatalf("方言 %v 覆盖失败: %v", dialect, againErr)
		}
		// loaded、loadErr 是写入后的读取结果与错误。
		loaded, loadErr := repo.GetItemStats(ctx, "acc", itemID)
		if loadErr != nil || loaded.BrowseCnt != 9 {
			t.Fatalf("方言 %v 写入未生效: %+v err=%v", dialect, loaded, loadErr)
		}
	}
}

// TestItemStatsRepositoryErrors 验证参数非法、数据库不可用与零值仓储下的错误分支。
func TestItemStatsRepositoryErrors(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// 缺少账号或商品标识时无法定位记录，必须直接返回参数错误而不是写坏数据。
	if // missingKeyErr 是缺少唯一键时的写入错误。
	missingKeyErr := store.ItemStats.UpsertItemStats(ctx, ItemStatsRow{ItemID: "item-1"}); missingKeyErr == nil {
		t.Fatal("缺少账号标识时写入应报错")
	}
	// 空键读取按未找到处理，不触发数据库查询。
	if _, blankKeyErr := store.ItemStats.GetItemStats(ctx, " ", "item-1"); !errors.Is(blankKeyErr, ErrItemStatsNotFound) {
		t.Fatalf("空账号读取应返回未找到: %v", blankKeyErr)
	}

	// 关闭底层连接，模拟数据库不可用。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatalf("关闭测试数据库失败: %v", closeErr)
	}
	// 关闭后读取必须返回底层错误而不是未找到，否则会被误判成「尚未抓取」而重复抓取。
	if _, readErr := store.ItemStats.GetItemStats(ctx, "acc", "item-1"); readErr == nil || errors.Is(readErr, ErrItemStatsNotFound) {
		t.Fatalf("数据库不可用应返回底层错误: %v", readErr)
	}
	// batchErr 是数据库不可用时批量读取返回的错误。
	if _, batchErr := store.ItemStats.ListItemStats(ctx, "acc", []string{"item-1"}); batchErr == nil {
		t.Fatal("数据库不可用时批量读取应返回错误")
	}
	if // writeErr 是数据库不可用时的写入错误。
	writeErr := store.ItemStats.UpsertItemStats(ctx, ItemStatsRow{CookieID: "acc", ItemID: "item-1"}); writeErr == nil {
		t.Fatal("数据库不可用时写入应返回错误")
	}

	// nilRepo 是未注入连接的零值仓储，所有方法都必须返回错误而不是 panic。
	var nilRepo *ItemStats
	// nilReadErr 是零值仓储读取统计返回的错误。
	if _, nilReadErr := nilRepo.GetItemStats(ctx, "acc", "item-1"); nilReadErr == nil {
		t.Fatal("未初始化仓储读取统计应报错")
	}
	// nilListErr 是零值仓储批量读取返回的错误。
	if _, nilListErr := nilRepo.ListItemStats(ctx, "acc", []string{"item-1"}); nilListErr == nil {
		t.Fatal("未初始化仓储批量读取应报错")
	}
	// nilWriteErr 是零值仓储写入统计返回的错误。
	if nilWriteErr := nilRepo.UpsertItemStats(ctx, ItemStatsRow{CookieID: "acc", ItemID: "item-1"}); nilWriteErr == nil {
		t.Fatal("未初始化仓储写入统计应报错")
	}
}
