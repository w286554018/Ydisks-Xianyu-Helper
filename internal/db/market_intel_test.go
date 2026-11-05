// Package db: 市场情报仓储焦点测试——判定 upsert 与时间文本解析。
package db

import (
	"context"
	"path/filepath"
	"testing"
)

// newMarketIntelTestStore 打开全新 SQLite 数据库并创建测试账号，返回仓储聚合与用户 ID。
func newMarketIntelTestStore(t *testing.T) (*Store, int64) {
	t.Helper()
	// database、dialect、openErr 保存全新 SQLite 数据库的打开结果。
	database, dialect, openErr := Open(context.Background(), filepath.Join(t.TempDir(), "market-intel.db"))
	if openErr != nil {
		t.Fatalf("打开数据库失败: %v", openErr)
	}
	t.Cleanup(func() { _ = database.Close() })
	// store 是全新数据库的 repository 聚合入口。
	store := NewStore(database, dialect)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// ok、createErr 保存测试用户创建结果。
	ok, createErr := store.Users.Create(ctx, "mi-user", "mi@example.com", "password")
	if createErr != nil || !ok {
		t.Fatalf("创建测试用户失败 ok=%v err=%v", ok, createErr)
	}
	// user、userErr 保存测试用户查询结果。
	user, userErr := store.Users.GetByUsername(ctx, "mi-user")
	if userErr != nil {
		t.Fatal(userErr)
	}
	// cookieErr 保存测试账号写入错误。
	if cookieErr := store.Cookies.Save(ctx, "acc", "unb=1", user.ID); cookieErr != nil {
		t.Fatal(cookieErr)
	}
	return store, user.ID
}

// TestMarketIntelUpsertVerdictAndTimeParsing 验证判定 upsert 与时间文本解析。
func TestMarketIntelUpsertVerdictAndTimeParsing(t *testing.T) {
	// store、_ 是全新数据库仓储聚合。
	store, _ := newMarketIntelTestStore(t)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 先落一条快照供判定引用。
	if _, err := store.MarketIntel.SaveSnapshots(ctx, []MarketIntelSnapshotRow{{
		Keyword: "AI课程", SnapshotDate: "2026-11-05", ItemID: "1", Title: "AI课程代做", Price: "99", WantCount: 120, WantCountText: "120人想要",
	}}); err != nil {
		t.Fatal(err)
	}
	// 首次写入判定。
	if err := store.MarketIntel.SaveVerdicts(ctx, []MarketIntelVerdictRow{{SnapshotID: 1, IsAIRelated: true, Category: "AI代做", Reason: "AI"}}); err != nil {
		t.Fatalf("首次写入判定失败: %v", err)
	}
	// upsert 覆盖为非 AI。
	if err := store.MarketIntel.SaveVerdicts(ctx, []MarketIntelVerdictRow{{SnapshotID: 1, IsAIRelated: false, Category: "", Reason: "普通"}}); err != nil {
		t.Fatalf("覆盖判定失败: %v", err)
	}
	// snapshots、verdicts 是联查结果；判定应为覆盖后的非 AI。
	snapshots, verdicts, err := store.MarketIntel.ListSnapshotsWithVerdicts(ctx, "AI课程", "2026-11-05")
	if err != nil || len(snapshots) != 1 || len(verdicts) != 1 {
		t.Fatalf("联查不符: %+v %+v err=%v", snapshots, verdicts, err)
	}
	if verdicts[0].IsAIRelated {
		t.Fatalf("判定应已被覆盖为非 AI: %+v", verdicts[0])
	}
	// 时间解析：合法格式与非法输入。
	// parsed 是合法时间解析结果。
	if parsed := parseMarketIntelTime("2026-11-05 10:00:00"); parsed.IsZero() {
		t.Fatal("合法时间应解析成功")
	}
	// emptyParsed 是空串解析结果。
	if emptyParsed := parseMarketIntelTime(""); !emptyParsed.IsZero() {
		t.Fatal("空串应返回零值")
	}
	// invalidParsed 是非法时间解析结果。
	if invalidParsed := parseMarketIntelTime("not-a-time"); !invalidParsed.IsZero() {
		t.Fatal("非法时间应返回零值")
	}
	// trimSnapshotDate 应去空白。
	if got := trimSnapshotDate(" 2026-11-05 "); got != "2026-11-05" {
		t.Fatalf("日期去空白不符: %q", got)
	}
}
