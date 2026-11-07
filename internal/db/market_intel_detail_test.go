// Package db: 竞品详情缓存仓储的读写、有效期与错误分支焦点测试。
package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestMarketIntelItemDetailRoundTrip 验证详情缓存的写入、覆盖、读取与有效期判断。
func TestMarketIntelItemDetailRoundTrip(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// 未写入前读取必须返回稳定的未找到错误。
	if _, missingErr := store.MarketIntel.GetItemDetail(ctx, "item-1"); !errors.Is(missingErr, ErrCompetitorDetailNotFound) {
		t.Fatalf("无缓存应返回 ErrCompetitorDetailNotFound, got %v", missingErr)
	}

	// first 是首次写入的详情行。
	first := MarketIntelItemDetailRow{
		ItemID: "item-1", Title: "标题", Price: "99", DescText: "描述",
		ImagesJSON: `["https://example.com/a.jpg"]`, SellerNickname: "卖家甲", SellerID: "u1", WantCountText: "10人想要",
	}
	// saveErr 是本次写入返回的错误。
	if saveErr := store.MarketIntel.SaveItemDetail(ctx, first); saveErr != nil {
		t.Fatalf("写入详情失败: %v", saveErr)
	}
	// loaded、loadErr 是读取结果与错误。
	loaded, loadErr := store.MarketIntel.GetItemDetail(ctx, "item-1")
	if loadErr != nil {
		t.Fatalf("读取详情失败: %v", loadErr)
	}
	if loaded.Title != "标题" || loaded.SellerID != "u1" || loaded.ImagesJSON != first.ImagesJSON || loaded.FetchedAt.IsZero() {
		t.Fatalf("详情字段不符: %+v", loaded)
	}
	// 刚写入的行必须在 7 天有效期内。
	if !loaded.IsFresh(time.Now()) {
		t.Fatalf("刚写入的缓存应有效: %+v", loaded)
	}

	// 覆盖同一商品必须更新而不是新增。
	if overwriteErr := store.MarketIntel.SaveItemDetail(ctx, MarketIntelItemDetailRow{
		ItemID: "item-1", Title: "新标题", Price: "88", DescText: "新描述", ImagesJSON: `[]`,
	}); overwriteErr != nil {
		t.Fatalf("覆盖详情失败: %v", overwriteErr)
	}
	// overwritten、overwrittenErr 是覆盖后的读取结果。
	overwritten, overwrittenErr := store.MarketIntel.GetItemDetail(ctx, "item-1")
	if overwrittenErr != nil || overwritten.Title != "新标题" || overwritten.Price != "88" || overwritten.SellerNickname != "" {
		t.Fatalf("覆盖结果不符: %+v err=%v", overwritten, overwrittenErr)
	}
	// 覆盖后仍应只有一行。
	// rows 是详情表行数。
	var rows int
	// countErr 是行数统计返回的错误。
	if countErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM market_intel_item_details`).Scan(&rows); countErr != nil || rows != 1 {
		t.Fatalf("详情表行数不符: rows=%d err=%v", rows, countErr)
	}
}

// TestMarketIntelItemDetailFreshness 验证有效期判断的边界与零值行为。
func TestMarketIntelItemDetailFreshness(t *testing.T) {
	// now 是判定基准时刻。
	now := time.Now()
	// cases 是抓取时间与期望有效性的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// fetchedAt 是缓存行的抓取时间。
		fetchedAt time.Time
		// wantFresh 是期望是否仍有效。
		wantFresh bool
	}{
		{name: "零值无效", fetchedAt: time.Time{}, wantFresh: false},
		{name: "一小时前有效", fetchedAt: now.Add(-time.Hour), wantFresh: true},
		{name: "六天前有效", fetchedAt: now.Add(-6 * 24 * time.Hour), wantFresh: true},
		{name: "八天前过期", fetchedAt: now.Add(-8 * 24 * time.Hour), wantFresh: false},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// row 是待判定的缓存行。
			row := MarketIntelItemDetailRow{ItemID: "item-1", FetchedAt: testCase.fetchedAt}
			// got 是本次调用的实际返回值。
			if got := row.IsFresh(now); got != testCase.wantFresh {
				t.Fatalf("有效期判断不符: got=%v want=%v", got, testCase.wantFresh)
			}
		})
	}
}

// TestMarketIntelItemDetailDialectBranches 验证三方言写入分支各自的写入行为。
func TestMarketIntelItemDetailDialectBranches(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// MySQL 分支的 ON DUPLICATE KEY 语法在 SQLite 上不可执行，必须返回包裹错误而不是静默失败。
	mysqlRepo := &MarketIntel{DB: store.MarketIntel.DB, Dialect: DialectMySQL}
	// mysqlErr 是MySQL 方言分支返回的错误。
	if mysqlErr := mysqlRepo.SaveItemDetail(ctx, MarketIntelItemDetailRow{ItemID: "mysql-item", Title: "t"}); mysqlErr == nil {
		t.Fatal("MySQL 方言分支应返回错误")
	}

	// Postgres 与 SQLite 分支的 ON CONFLICT 语法在 SQLite 上均可执行，写入必须生效。
	for _, dialect := range []Dialect{DialectPostgres, DialectSQLite} {
		// repo 是绑定目标方言的仓储。
		repo := &MarketIntel{DB: store.MarketIntel.DB, Dialect: dialect}
		// itemID 是本次方言用例的商品标识。
		itemID := "item-" + string(dialect)
		// saveErr 是本次写入返回的错误。
		if saveErr := repo.SaveItemDetail(ctx, MarketIntelItemDetailRow{ItemID: itemID, Title: "方言标题", ImagesJSON: `[]`}); saveErr != nil {
			t.Fatalf("方言 %v 写入失败: %v", dialect, saveErr)
		}
		// loaded、loadErr 是写入后的读取结果。
		loaded, loadErr := repo.GetItemDetail(ctx, itemID)
		if loadErr != nil || loaded.Title != "方言标题" {
			t.Fatalf("方言 %v 写入未生效: %+v err=%v", dialect, loaded, loadErr)
		}
	}
}

// TestMarketIntelItemDetailRepositoryErrors 验证数据库不可用与零值仓储下的错误分支。
func TestMarketIntelItemDetailRepositoryErrors(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 关闭底层连接，模拟数据库不可用。
	if closeErr := store.MarketIntel.DB.Close(); closeErr != nil {
		t.Fatalf("关闭测试数据库失败: %v", closeErr)
	}
	// 关闭后读取必须返回错误而不是未找到。
	if _, readErr := store.MarketIntel.GetItemDetail(ctx, "item-1"); readErr == nil || errors.Is(readErr, ErrCompetitorDetailNotFound) {
		t.Fatalf("数据库不可用应返回底层错误: %v", readErr)
	}
	// 关闭后写入必须返回错误。
	if writeErr := store.MarketIntel.SaveItemDetail(ctx, MarketIntelItemDetailRow{ItemID: "item-1"}); writeErr == nil {
		t.Fatal("数据库不可用时写入应返回错误")
	}

	// nilRepo 是未注入连接的零值仓储。
	var nilRepo *MarketIntel
	// nilReadErr 是未初始化仓储的读取错误。
	if _, nilReadErr := nilRepo.GetItemDetail(ctx, "item-1"); nilReadErr == nil {
		t.Fatal("未初始化仓储读取详情应报错")
	}
	// nilWriteErr 是未初始化仓储的写入错误。
	if nilWriteErr := nilRepo.SaveItemDetail(ctx, MarketIntelItemDetailRow{}); nilWriteErr == nil {
		t.Fatal("未初始化仓储写入详情应报错")
	}
}
