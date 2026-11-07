// Package db: 市场情报仓储的 CRUD、联查与错误分支焦点测试。
package db

import (
	"context"
	"errors"
	"testing"
)

// marketIntelSeedKeyword 是测试用的关注词文本。
const marketIntelSeedKeyword = "AI课程"

// TestMarketIntelKeywordCRUD 验证关注词的写入、更新、启用过滤、统计与删除。
func TestMarketIntelKeywordCRUD(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// firstID、firstErr 是首个关注词的写入结果。
	firstID, firstErr := store.MarketIntel.CreateKeyword(ctx, marketIntelSeedKeyword, true, 1)
	if firstErr != nil || firstID == 0 {
		t.Fatalf("新增关注词失败 id=%d err=%v", firstID, firstErr)
	}
	// 重复写入必须返回稳定错误，供应用层映射为 400。
	if _, dupErr := store.MarketIntel.CreateKeyword(ctx, marketIntelSeedKeyword, false, 2); !errors.Is(dupErr, ErrMarketIntelKeywordDuplicate) {
		t.Fatalf("重复关注词应返回 ErrMarketIntelKeywordDuplicate, got %v", dupErr)
	}
	// secondID 是第二个关注词主键；它保持停用以验证启用过滤。
	secondID, secondErr := store.MarketIntel.CreateKeyword(ctx, "AI绘画", false, 2)
	if secondErr != nil || secondID == 0 {
		t.Fatalf("新增第二个关注词失败 id=%d err=%v", secondID, secondErr)
	}

	// keywords、listErr 是词表读取结果。
	keywords, listErr := store.MarketIntel.ListKeywords(ctx)
	if listErr != nil || len(keywords) != 2 || keywords[0].Keyword != marketIntelSeedKeyword || !keywords[0].Enabled || keywords[0].CreatedAt.IsZero() {
		t.Fatalf("词表读取不符: %+v err=%v", keywords, listErr)
	}
	// enabled、enabledErr 是启用词读取结果。
	enabled, enabledErr := store.MarketIntel.ListEnabledKeywords(ctx)
	if enabledErr != nil || len(enabled) != 1 || enabled[0].ID != firstID {
		t.Fatalf("启用词读取不符: %+v err=%v", enabled, enabledErr)
	}
	// total、enabledCount、countErr 是关注词统计结果。
	total, enabledCount, countErr := store.MarketIntel.CountKeywords(ctx)
	if countErr != nil || total != 2 || enabledCount != 1 {
		t.Fatalf("关注词统计不符 total=%d enabled=%d err=%v", total, enabledCount, countErr)
	}

	// 更新首个词：改名并停用。
	if updateErr := store.MarketIntel.UpdateKeyword(ctx, firstID, "AI课程代做", false, 3); updateErr != nil {
		t.Fatalf("更新关注词失败: %v", updateErr)
	}
	// refreshed、refreshedErr 是更新后的词表。
	refreshed, refreshedErr := store.MarketIntel.ListKeywords(ctx)
	if refreshedErr != nil || len(refreshed) != 2 {
		t.Fatalf("更新后词表读取不符: %+v err=%v", refreshed, refreshedErr)
	}
	// 改名到已存在的文本必须返回重复错误。
	if updateErr := store.MarketIntel.UpdateKeyword(ctx, firstID, "AI绘画", false, 4); !errors.Is(updateErr, ErrMarketIntelKeywordDuplicate) {
		t.Fatalf("改名冲突应返回 ErrMarketIntelKeywordDuplicate, got %v", updateErr)
	}
	// 更新不存在的词必须返回未找到。
	if updateErr := store.MarketIntel.UpdateKeyword(ctx, 999999, "不存在", false, 9); !errors.Is(updateErr, ErrMarketIntelKeywordNotFound) {
		t.Fatalf("更新不存在词应返回 ErrMarketIntelKeywordNotFound, got %v", updateErr)
	}
	// 删除不存在的词必须返回未找到。
	if deleteErr := store.MarketIntel.DeleteKeyword(ctx, 999999); !errors.Is(deleteErr, ErrMarketIntelKeywordNotFound) {
		t.Fatalf("删除不存在词应返回 ErrMarketIntelKeywordNotFound, got %v", deleteErr)
	}
	// 删除存在的词必须成功，并让统计回落。
	if deleteErr := store.MarketIntel.DeleteKeyword(ctx, secondID); deleteErr != nil {
		t.Fatalf("删除关注词失败: %v", deleteErr)
	}
	// remaining、remainingErr 是删除后的关注词统计结果。
	remaining, remainingEnabled, remainingErr := store.MarketIntel.CountKeywords(ctx)
	if remainingErr != nil || remaining != 1 || remainingEnabled != 0 {
		t.Fatalf("删除后统计不符 total=%d enabled=%d err=%v", remaining, remainingEnabled, remainingErr)
	}
}

// TestMarketIntelSnapshotAndVerdictQueries 验证快照写入、判定存在性、范围联查、清理与最新快照日。
func TestMarketIntelSnapshotAndVerdictQueries(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// saved、saveErr 是快照写入行数与错误。
	saved, saveErr := store.MarketIntel.SaveSnapshots(ctx, []MarketIntelSnapshotRow{
		{Keyword: marketIntelSeedKeyword, SnapshotDate: "2026-11-01", ItemID: "old-1", Title: "旧快照", Price: "10", WantCount: 5, WantCountText: "5人想要", SellerNickname: "甲", PublishTime: "2026-11-01 10:00", RawJSON: `{"a":1}`},
		{Keyword: marketIntelSeedKeyword, SnapshotDate: "2026-11-05", ItemID: "new-1", Title: "新快照", Price: "99", WantCount: 120, WantCountText: "120人想要", SellerNickname: "乙", PublishTime: "2026-11-05 10:00", RawJSON: `{"b":2}`},
		{Keyword: "AI绘画", SnapshotDate: "2026-11-05", ItemID: "other-1", Title: "其它词快照", Price: "20", WantCount: 8, WantCountText: "8人想要"},
	})
	if saveErr != nil || saved != 3 {
		t.Fatalf("快照写入不符 saved=%d err=%v", saved, saveErr)
	}

	// snapshots、verdicts、joinErr 是单日联查结果；尚无判定时 verdicts 为空。
	snapshots, verdicts, joinErr := store.MarketIntel.ListSnapshotsWithVerdicts(ctx, marketIntelSeedKeyword, "2026-11-05")
	if joinErr != nil || len(snapshots) != 1 || len(verdicts) != 0 {
		t.Fatalf("单日联查不符: snapshots=%+v verdicts=%+v err=%v", snapshots, verdicts, joinErr)
	}
	if snapshots[0].ItemID != "new-1" || snapshots[0].WantCount != 120 || snapshots[0].CreatedAt.IsZero() {
		t.Fatalf("快照字段不符: %+v", snapshots[0])
	}

	// exists、existsErr 是无判定时的存在性检查结果。
	exists, existsErr := store.MarketIntel.SnapshotVerdictExists(ctx, snapshots[0].ID)
	if existsErr != nil || exists {
		t.Fatalf("无判定时应返回 false: exists=%v err=%v", exists, existsErr)
	}
	// 写入判定后存在性检查必须为真，并出现在联查结果中。
	if verdictErr := store.MarketIntel.SaveVerdicts(ctx, []MarketIntelVerdictRow{{SnapshotID: snapshots[0].ID, IsAIRelated: true, Category: "AI代做", Reason: "AI"}}); verdictErr != nil {
		t.Fatalf("写入判定失败: %v", verdictErr)
	}
	// existsAfter、existsAfterErr 是写入判定后的存在性检查结果。
	existsAfter, existsAfterErr := store.MarketIntel.SnapshotVerdictExists(ctx, snapshots[0].ID)
	if existsAfterErr != nil || !existsAfter {
		t.Fatalf("有判定时应返回 true: exists=%v err=%v", existsAfter, existsAfterErr)
	}
	// joined、joinedVerdicts、joinedErr 是含判定的联查结果。
	joined, joinedVerdicts, joinedErr := store.MarketIntel.ListSnapshotsWithVerdicts(ctx, marketIntelSeedKeyword, "2026-11-05")
	if joinedErr != nil || len(joined) != 1 || len(joinedVerdicts) != 1 || !joinedVerdicts[0].IsAIRelated || joinedVerdicts[0].Category != "AI代做" || joinedVerdicts[0].CreatedAt.IsZero() {
		t.Fatalf("含判定联查不符: snapshots=%+v verdicts=%+v err=%v", joined, joinedVerdicts, joinedErr)
	}

	// rangeSnapshots、rangeVerdicts、rangeErr 是跨词跨日范围联查结果。
	rangeSnapshots, rangeVerdicts, rangeErr := store.MarketIntel.ListSnapshotsInRangeWithVerdicts(ctx, "2026-11-01", "2026-11-05")
	if rangeErr != nil || len(rangeSnapshots) != 3 || len(rangeVerdicts) != 1 {
		t.Fatalf("范围联查不符: snapshots=%d verdicts=%d err=%v", len(rangeSnapshots), len(rangeVerdicts), rangeErr)
	}
	// narrowed、narrowedVerdicts、narrowedErr 是收窄日期后的范围联查结果。
	narrowed, narrowedVerdicts, narrowedErr := store.MarketIntel.ListSnapshotsInRangeWithVerdicts(ctx, "2026-11-05", "2026-11-05")
	if narrowedErr != nil || len(narrowed) != 2 || len(narrowedVerdicts) != 1 {
		t.Fatalf("收窄范围联查不符: snapshots=%d verdicts=%d err=%v", len(narrowed), len(narrowedVerdicts), narrowedErr)
	}

	// latest、latestErr 是最新快照日查询结果。
	latest, latestErr := store.MarketIntel.LatestSnapshotDate(ctx, marketIntelSeedKeyword)
	if latestErr != nil || latest != "2026-11-05" {
		t.Fatalf("最新快照日不符: %q err=%v", latest, latestErr)
	}
	// emptyLatest、emptyLatestErr 是未知词的最新快照日查询结果。
	emptyLatest, emptyLatestErr := store.MarketIntel.LatestSnapshotDate(ctx, "不存在的词")
	if emptyLatestErr != nil || emptyLatest != "" {
		t.Fatalf("未知词应返回空串: %q err=%v", emptyLatest, emptyLatestErr)
	}

	// removed、removedErr 是清理 2026-11-05 之前快照的结果；判定随快照级联删除。
	removed, removedErr := store.MarketIntel.DeleteSnapshotsBefore(ctx, "2026-11-05")
	if removedErr != nil || removed != 1 {
		t.Fatalf("清理历史快照不符 removed=%d err=%v", removed, removedErr)
	}
	// afterClean、afterVerdicts、afterCleanErr 是清理后的范围联查结果。
	afterClean, afterVerdicts, afterCleanErr := store.MarketIntel.ListSnapshotsInRangeWithVerdicts(ctx, "2026-11-01", "2026-11-05")
	if afterCleanErr != nil || len(afterClean) != 2 || len(afterVerdicts) != 1 {
		t.Fatalf("清理后联查不符: snapshots=%d verdicts=%d err=%v", len(afterClean), len(afterVerdicts), afterCleanErr)
	}
}

// TestMarketIntelAggregates 验证聚合写入覆盖、词内趋势序列与每词最新聚合。
func TestMarketIntelAggregates(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()

	// rows 是三个词日聚合行，用于覆盖排序与最新日挑选逻辑。
	rows := []MarketIntelAggregateRow{
		{Keyword: marketIntelSeedKeyword, SnapshotDate: "2026-11-03", TotalCount: 30, AICount: 12, TotalWant: 400, CategoryJSON: `{"AI代做":12}`, TopSellersJSON: `[{"nickname":"甲"}]`, VerdictStatus: MarketIntelVerdictDone},
		{Keyword: marketIntelSeedKeyword, SnapshotDate: "2026-11-04", TotalCount: 31, AICount: 13, TotalWant: 500, VerdictStatus: MarketIntelVerdictPending},
		{Keyword: "AI绘画", SnapshotDate: "2026-11-04", TotalCount: 20, AICount: 5, TotalWant: 900, VerdictStatus: MarketIntelVerdictDone},
	}
	// row 是当前待写入聚合行。
	for _, row := range rows {
		// saveErr 是本次写入返回的错误。
		if saveErr := store.MarketIntel.SaveAggregate(ctx, row); saveErr != nil {
			t.Fatalf("写入聚合失败: %v", saveErr)
		}
	}
	// 同词同日再次写入必须覆盖而不是新增。
	if overwriteErr := store.MarketIntel.SaveAggregate(ctx, MarketIntelAggregateRow{
		Keyword: marketIntelSeedKeyword, SnapshotDate: "2026-11-04", TotalCount: 44, AICount: 21, TotalWant: 777,
		CategoryJSON: `{"AI代做":21}`, TopSellersJSON: `[{"nickname":"乙"}]`, VerdictStatus: MarketIntelVerdictDone,
	}); overwriteErr != nil {
		t.Fatalf("覆盖聚合失败: %v", overwriteErr)
	}

	// series、seriesErr 是词内趋势序列；必须按日期升序且已覆盖。
	series, seriesErr := store.MarketIntel.ListAggregates(ctx, marketIntelSeedKeyword, 30)
	if seriesErr != nil || len(series) != 2 {
		t.Fatalf("趋势序列不符: %+v err=%v", series, seriesErr)
	}
	if series[0].SnapshotDate != "2026-11-03" || series[1].TotalWant != 777 || series[1].VerdictStatus != MarketIntelVerdictDone || series[1].CreatedAt.IsZero() {
		t.Fatalf("趋势序列内容不符: %+v", series)
	}
	// limited、limitedErr 是限制天数后的趋势序列；只保留最新一天。
	limited, limitedErr := store.MarketIntel.ListAggregates(ctx, marketIntelSeedKeyword, 1)
	if limitedErr != nil || len(limited) != 1 || limited[0].SnapshotDate != "2026-11-04" {
		t.Fatalf("限制天数序列不符: %+v err=%v", limited, limitedErr)
	}

	// latest、latestErr 是每词最新聚合；按 total_want 倒序，AI绘画领先。
	latest, latestErr := store.MarketIntel.ListLatestAggregates(ctx)
	if latestErr != nil || len(latest) != 2 {
		t.Fatalf("最新聚合不符: %+v err=%v", latest, latestErr)
	}
	if latest[0].Keyword != "AI绘画" || latest[1].Keyword != marketIntelSeedKeyword || latest[1].TotalWant != 777 {
		t.Fatalf("最新聚合顺序或内容不符: %+v", latest)
	}
}

// TestMarketIntelDialectBranches 验证 MySQL/Postgres 方言分支的 SQL 分支确实被执行到。
func TestMarketIntelDialectBranches(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 先落一条快照作为判定与聚合的引用目标。
	if _, err := store.MarketIntel.SaveSnapshots(ctx, []MarketIntelSnapshotRow{{Keyword: marketIntelSeedKeyword, SnapshotDate: "2026-11-05", ItemID: "i1"}}); err != nil {
		t.Fatalf("写入快照失败: %v", err)
	}
	// MySQL 分支在 SQLite 上必然是不可执行的 SQL，因此断言「分支被走到并返回包裹错误」。
	mysqlRepo := &MarketIntel{DB: store.MarketIntel.DB, Dialect: DialectMySQL}
	// mysqlVerdictErr 是 MySQL 判定分支的返回错误。
	if mysqlVerdictErr := mysqlRepo.SaveVerdicts(ctx, []MarketIntelVerdictRow{{SnapshotID: 1, IsAIRelated: true}}); mysqlVerdictErr == nil {
		t.Fatal("MySQL 判定分支应返回错误")
	}
	// mysqlAggregateErr 是 MySQL 聚合分支的返回错误。
	if mysqlAggregateErr := mysqlRepo.SaveAggregate(ctx, MarketIntelAggregateRow{Keyword: marketIntelSeedKeyword, SnapshotDate: "2026-11-05"}); mysqlAggregateErr == nil {
		t.Fatal("MySQL 聚合分支应返回错误")
	}

	// Postgres 分支的 ON CONFLICT 语法在 SQLite 上同样可执行，因此断言写入确实生效。
	postgresRepo := &MarketIntel{DB: store.MarketIntel.DB, Dialect: DialectPostgres}
	// postgresVerdictErr 是Postgres 方言判定分支返回的错误。
	if postgresVerdictErr := postgresRepo.SaveVerdicts(ctx, []MarketIntelVerdictRow{{SnapshotID: 1, IsAIRelated: true, Category: "pg", Reason: "pg"}}); postgresVerdictErr != nil {
		t.Fatalf("Postgres 判定分支写入失败: %v", postgresVerdictErr)
	}
	// postgresAggregateErr 是Postgres 方言聚合分支返回的错误。
	if postgresAggregateErr := postgresRepo.SaveAggregate(ctx, MarketIntelAggregateRow{
		Keyword: marketIntelSeedKeyword, SnapshotDate: "2026-11-05", TotalCount: 3, AICount: 1, TotalWant: 30, VerdictStatus: MarketIntelVerdictDone,
	}); postgresAggregateErr != nil {
		t.Fatalf("Postgres 聚合分支写入失败: %v", postgresAggregateErr)
	}
	// snapshots、verdicts、err 是 Postgres 分支写入后的联查结果。
	snapshots, verdicts, err := store.MarketIntel.ListSnapshotsWithVerdicts(ctx, marketIntelSeedKeyword, "2026-11-05")
	if err != nil || len(snapshots) != 1 || len(verdicts) != 1 || verdicts[0].Category != "pg" {
		t.Fatalf("Postgres 分支写入未生效: snapshots=%+v verdicts=%+v err=%v", snapshots, verdicts, err)
	}
	// aggregates、aggregateErr 是 Postgres 分支写入后的聚合行。
	aggregates, aggregateErr := store.MarketIntel.ListAggregates(ctx, marketIntelSeedKeyword, 1)
	if aggregateErr != nil || len(aggregates) != 1 || aggregates[0].TotalWant != 30 {
		t.Fatalf("Postgres 聚合分支写入未生效: %+v err=%v", aggregates, aggregateErr)
	}
}

// TestMarketIntelRepositoryErrors 验证数据库不可用时各仓储方法返回包裹错误而非 panic。
func TestMarketIntelRepositoryErrors(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 关闭底层连接，模拟数据库不可用。
	if closeErr := store.MarketIntel.DB.Close(); closeErr != nil {
		t.Fatalf("关闭测试数据库失败: %v", closeErr)
	}
	// cases 是「调用 → 期望失败」的对照表，覆盖每个查询与写入方法的错误分支。
	cases := []struct {
		// name 是用例名。
		name string
		// call 是被测仓储调用。
		call func() error
	}{
		// err 是本次调用的返回错误。
		{name: "词表读取", call: func() error { _, err := store.MarketIntel.ListKeywords(ctx); return err }},
		// err 是本次调用的返回错误。
		{name: "启用词读取", call: func() error { _, err := store.MarketIntel.ListEnabledKeywords(ctx); return err }},
		// err 是本次调用的返回错误。
		{name: "新增词", call: func() error { _, err := store.MarketIntel.CreateKeyword(ctx, "x", true, 1); return err }},
		{name: "更新词", call: func() error { return store.MarketIntel.UpdateKeyword(ctx, 1, "x", true, 1) }},
		{name: "删除词", call: func() error { return store.MarketIntel.DeleteKeyword(ctx, 1) }},
		// err 是本次调用的返回错误。
		{name: "词统计", call: func() error { _, _, err := store.MarketIntel.CountKeywords(ctx); return err }},
		{name: "快照写入", call: func() error {
			// err 是本次调用的返回错误。
			_, err := store.MarketIntel.SaveSnapshots(ctx, []MarketIntelSnapshotRow{{Keyword: "x"}})
			return err
		}},
		{name: "判定写入", call: func() error { return store.MarketIntel.SaveVerdicts(ctx, []MarketIntelVerdictRow{{SnapshotID: 1}}) }},
		// err 是本次调用的返回错误。
		{name: "判定存在性", call: func() error { _, err := store.MarketIntel.SnapshotVerdictExists(ctx, 1); return err }},
		{name: "单日联查", call: func() error {
			// err 是本次调用的返回错误。
			_, _, err := store.MarketIntel.ListSnapshotsWithVerdicts(ctx, "x", "2026-11-05")
			return err
		}},
		{name: "范围联查", call: func() error {
			// err 是本次调用的返回错误。
			_, _, err := store.MarketIntel.ListSnapshotsInRangeWithVerdicts(ctx, "2026-11-01", "2026-11-05")
			return err
		}},
		// err 是本次调用的返回错误。
		{name: "最新快照日", call: func() error { _, err := store.MarketIntel.LatestSnapshotDate(ctx, "x"); return err }},
		// err 是本次调用的返回错误。
		{name: "清理快照", call: func() error { _, err := store.MarketIntel.DeleteSnapshotsBefore(ctx, "2026-11-05"); return err }},
		{name: "聚合写入", call: func() error { return store.MarketIntel.SaveAggregate(ctx, MarketIntelAggregateRow{Keyword: "x"}) }},
		// err 是本次调用的返回错误。
		{name: "趋势序列", call: func() error { _, err := store.MarketIntel.ListAggregates(ctx, "x", 30); return err }},
		// err 是本次调用的返回错误。
		{name: "最新聚合", call: func() error { _, err := store.MarketIntel.ListLatestAggregates(ctx); return err }},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// err 是本次调用的返回错误。
			if err := testCase.call(); err == nil {
				t.Fatal("数据库不可用时必须返回错误")
			}
		})
	}
}

// TestMarketIntelNilRepository 验证零值仓储在未注入连接时返回稳定错误。
func TestMarketIntelNilRepository(t *testing.T) {
	// nilRepo 是未注入连接的零值仓储。
	var nilRepo *MarketIntel
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 每种方法都必须在未初始化时快速失败，避免空指针。
	if _, err := nilRepo.ListKeywords(ctx); err == nil {
		t.Fatal("未初始化仓储读取词表应报错")
	}
	// err 是本次调用的返回错误。
	if _, err := nilRepo.ListEnabledKeywords(ctx); err == nil {
		t.Fatal("未初始化仓储读取启用词应报错")
	}
	// err 是本次调用的返回错误。
	if _, err := nilRepo.CreateKeyword(ctx, "x", true, 1); err == nil {
		t.Fatal("未初始化仓储新增词应报错")
	}
	// err 是本次调用的返回错误。
	if err := nilRepo.UpdateKeyword(ctx, 1, "x", true, 1); err == nil {
		t.Fatal("未初始化仓储更新词应报错")
	}
	// err 是本次调用的返回错误。
	if err := nilRepo.DeleteKeyword(ctx, 1); err == nil {
		t.Fatal("未初始化仓储删除词应报错")
	}
	// err 是本次调用的返回错误。
	if _, _, err := nilRepo.CountKeywords(ctx); err == nil {
		t.Fatal("未初始化仓储统计词应报错")
	}
	// err 是本次调用的返回错误。
	if _, err := nilRepo.SaveSnapshots(ctx, nil); err == nil {
		t.Fatal("未初始化仓储写入快照应报错")
	}
	// err 是本次调用的返回错误。
	if err := nilRepo.SaveVerdicts(ctx, nil); err == nil {
		t.Fatal("未初始化仓储写入判定应报错")
	}
	// err 是本次调用的返回错误。
	if _, err := nilRepo.SnapshotVerdictExists(ctx, 1); err == nil {
		t.Fatal("未初始化仓储查询判定存在性应报错")
	}
	// err 是本次调用的返回错误。
	if _, _, err := nilRepo.ListSnapshotsWithVerdicts(ctx, "x", "2026-11-05"); err == nil {
		t.Fatal("未初始化仓储单日联查应报错")
	}
	// err 是本次调用的返回错误。
	if _, _, err := nilRepo.ListSnapshotsInRangeWithVerdicts(ctx, "2026-11-01", "2026-11-05"); err == nil {
		t.Fatal("未初始化仓储范围联查应报错")
	}
	// err 是本次调用的返回错误。
	if _, err := nilRepo.LatestSnapshotDate(ctx, "x"); err == nil {
		t.Fatal("未初始化仓储查询最新快照日应报错")
	}
	// err 是本次调用的返回错误。
	if _, err := nilRepo.DeleteSnapshotsBefore(ctx, "2026-11-05"); err == nil {
		t.Fatal("未初始化仓储清理快照应报错")
	}
	// err 是本次调用的返回错误。
	if err := nilRepo.SaveAggregate(ctx, MarketIntelAggregateRow{}); err == nil {
		t.Fatal("未初始化仓储写入聚合应报错")
	}
	// err 是本次调用的返回错误。
	if _, err := nilRepo.ListAggregates(ctx, "x", 30); err == nil {
		t.Fatal("未初始化仓储趋势序列应报错")
	}
	// err 是本次调用的返回错误。
	if _, err := nilRepo.ListLatestAggregates(ctx); err == nil {
		t.Fatal("未初始化仓储最新聚合应报错")
	}
}
