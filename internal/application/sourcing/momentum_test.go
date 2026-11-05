package sourcing

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// momentumSnapshot 构造一天的成功热词快照。
func momentumSnapshot(t *testing.T, date string, keywords []string) Snapshot {
	t.Helper()
	// payloadJSON 是序列化后的热词载荷。
	payloadJSON, err := json.Marshal(HotKeywordSnapshotPayload{Keywords: keywords})
	if err != nil {
		t.Fatal(err)
	}
	return Snapshot{Kind: KindHotKeywords, Date: date, PayloadJSON: string(payloadJSON), Status: StatusOK}
}

// TestMomentumStatusClassification 验证新冒头、上升、回落与稳定四类状态判定。
func TestMomentumStatusClassification(t *testing.T) {
	// service、repository 是被测服务与内存仓储。
	service, repository, _, _ := newTestService(t)
	// 前日榜单：A 第 1、C 第 2、B 第 5；今日榜单：B 升到第 1（上升 4 名）、C 跌到第 4（回落 2 名不足阈值则稳定，改为第 6 回落 4 名）、A 稳定、D 新冒头。
	repository.snapshots[KindHotKeywords+"|2026-10-28"] = momentumSnapshot(t, "2026-10-28", []string{"A", "C", "X1", "X2", "B"})
	repository.snapshots[KindHotKeywords+"|2026-10-29"] = momentumSnapshot(t, "2026-10-29", []string{"B", "A", "D", "X3", "X4", "C"})
	// summary 是涨幅计算结果。
	summary, err := service.Momentum(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// statuses 是期望的词到状态映射；X3/X4 是前日不存在的填充词，今日在榜即新冒头。
	statuses := map[string]string{
		"D":  MomentumNew,
		"B":  MomentumRising,
		"A":  MomentumSteady,
		"C":  MomentumFalling,
		"X3": MomentumNew,
		"X4": MomentumNew,
	}
	// entry 遍历断言每个词的状态与名次。
	for _, entry := range summary.Items {
		// wantStatus 是期望状态。
		wantStatus, exists := statuses[entry.Keyword]
		if !exists {
			t.Fatalf("出现意外词条 %q", entry.Keyword)
		}
		if entry.Status != wantStatus {
			t.Fatalf("词 %q 期望状态 %s，实际 %s", entry.Keyword, wantStatus, entry.Status)
		}
		delete(statuses, entry.Keyword)
	}
	if len(statuses) != 0 {
		t.Fatalf("缺少词条: %v", statuses)
	}
	// 新冒头词条必须排最前。
	if summary.Items[0].Keyword != "D" || summary.Items[0].Status != MomentumNew {
		t.Fatalf("新冒头应排最前，实际首条 %q", summary.Items[0].Keyword)
	}
	if summary.LatestDate != "2026-10-29" || summary.WindowDays != momentumWindowDays {
		t.Fatalf("窗口信息不符: %+v", summary)
	}
	// 在榜天数：A/B/C 各 2 天，D 1 天；填充词 X 系列只出现在单日，不参与断言。
	for _, entry := range summary.Items {
		// wantDays 是期望在榜天数；未知填充词跳过。
		wantDays, known := map[string]int{"A": 2, "B": 2, "C": 2, "D": 1, "X3": 1, "X4": 1}[entry.Keyword]
		if !known {
			continue
		}
		if entry.DaysOnList != wantDays {
			t.Fatalf("词 %q 期望在榜 %d 天，实际 %d", entry.Keyword, wantDays, entry.DaysOnList)
		}
	}
}

// TestMomentumSkipsFailedSnapshots 验证失败快照不参与对比，也不视为错误。
func TestMomentumSkipsFailedSnapshots(t *testing.T) {
	// service、repository 是被测服务与内存仓储。
	service, repository, _, _ := newTestService(t)
	// 最新一天成功，前一天采集失败。
	repository.snapshots[KindHotKeywords+"|2026-10-28"] = Snapshot{Kind: KindHotKeywords, Date: "2026-10-28", Status: StatusFailed, ErrorMessage: "浏览器不可用"}
	repository.snapshots[KindHotKeywords+"|2026-10-29"] = momentumSnapshot(t, "2026-10-29", []string{"A", "B"})
	// summary 是涨幅计算结果；前日失败时全部词都是新冒头。
	summary, err := service.Momentum(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// entry 遍历断言全部为新冒头。
	for _, entry := range summary.Items {
		if entry.Status != MomentumNew {
			t.Fatalf("前日失败时词 %q 应为新冒头，实际 %s", entry.Keyword, entry.Status)
		}
	}
}

// TestMomentumEmptyWhenNoSnapshots 验证无快照时返回空视图而非错误。
func TestMomentumEmptyWhenNoSnapshots(t *testing.T) {
	// service 是被测服务（内存仓储无任何快照）。
	service, _, _, _ := newTestService(t)
	// summary 是涨幅计算结果。
	summary, err := service.Momentum(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.LatestDate != "" || len(summary.Items) != 0 {
		t.Fatalf("无快照应返回空视图: %+v", summary)
	}
	_ = time.Now
}
