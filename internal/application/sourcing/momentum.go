package sourcing

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// momentumWindowDays 是热词涨幅对比的时间窗口：取最近 N 天快照做序列对比。
const momentumWindowDays = 14

// momentumRiseThreshold 是判定「排名显著上升」的最小名次提升数。
const momentumRiseThreshold = 3

// 热词涨幅状态取值。
const (
	// MomentumNew 表示该词首次进入热词榜。
	MomentumNew = "new"
	// MomentumRising 表示该词排名较前一日显著上升。
	MomentumRising = "rising"
	// MomentumSteady 表示该词连续多日稳定在榜。
	MomentumSteady = "steady"
	// MomentumFalling 表示该词排名下降或跌出榜单。
	MomentumFalling = "falling"
)

// KeywordMomentum 是单个热词的涨幅视图。
type KeywordMomentum struct {
	// Keyword 是热词文本。
	Keyword string `json:"keyword"`
	// Status 是涨幅状态：new/rising/steady/falling。
	Status string `json:"status"`
	// Position 是该词在最新快照中的名次（1 起）；已跌出时为 0。
	Position int `json:"position"`
	// PreviousPosition 是该词在前一日快照中的名次；此前不在榜时为 0。
	PreviousPosition int `json:"previous_position"`
	// DaysOnList 是该词在窗口内出现的总天数。
	DaysOnList int `json:"days_on_list"`
}

// MomentumSummary 是热词涨幅榜视图。
type MomentumSummary struct {
	// WindowDays 是对比窗口天数。
	WindowDays int `json:"window_days"`
	// LatestDate 是最新快照归属日；无快照时为空。
	LatestDate string `json:"latest_date"`
	// Items 是按「新冒头优先、再按名次」排序的涨幅词条。
	Items []KeywordMomentum `json:"items"`
}

// Momentum 计算热词涨幅榜：读窗口内各日热词快照，对比名次变化。
// 无任何成功快照时返回空词条列表，不视为错误（界面提示尚未采集）。
func (s *Service) Momentum(ctx context.Context) (MomentumSummary, error) {
	// snapshots 是窗口内的每日热词快照，按日期倒序。
	snapshots, err := s.repository.ListRecentSnapshots(ctx, KindHotKeywords, momentumWindowDays)
	if err != nil {
		return MomentumSummary{}, fmt.Errorf("读取热词快照序列失败: %w", err)
	}
	// latest 是最新一天的词条序列；无成功快照时返回空视图。
	latest, latestDate := momentumDayKeywords(snapshots, 0)
	if len(snapshots) == 0 || latestDate == "" {
		return MomentumSummary{WindowDays: momentumWindowDays, Items: []KeywordMomentum{}}, nil
	}
	// previous 是前一日的词条序列（可能不存在）。
	previous, _ := momentumDayKeywords(snapshots, 1)
	// daysOnList 统计每个词在窗口内出现的总天数。
	daysOnList := momentumDaysOnList(snapshots)
	// items 是涨幅视图集合。
	items := make([]KeywordMomentum, 0, len(latest))
	// keyword、position 遍历最新榜单。
	for keyword, position := range latest {
		// entry 是单个词的涨幅视图。
		entry := KeywordMomentum{Keyword: keyword, Position: position, DaysOnList: daysOnList[keyword]}
		// previousPosition 是该词前一日名次；不在榜时为 0。
		previousPosition, wasOnList := previous[keyword]
		if wasOnList {
			entry.PreviousPosition = previousPosition
		}
		entry.Status = momentumStatus(entry, wasOnList)
		items = append(items, entry)
	}
	sort.Slice(items /* 新冒头优先，其次按最新名次升序。 */, func(i, j int) bool {
		// rankNew 把新冒头排最前。
		rankNew := func(status string) int {
			if status == MomentumNew {
				return 0
			}
			if status == MomentumRising {
				return 1
			}
			return 2
		}
		// aRank、bRank 是两条目的排序权重。
		aRank, bRank := rankNew(items[i].Status), rankNew(items[j].Status)
		if aRank != bRank {
			return aRank < bRank
		}
		return items[i].Position < items[j].Position
	})
	return MomentumSummary{WindowDays: momentumWindowDays, LatestDate: latestDate, Items: items}, nil
}

// momentumDayKeywords 解析窗口内第 offset 天（0=最新）的热词名次表；该日缺失或失败时返回空表。
func momentumDayKeywords(snapshots []Snapshot, offset int) (map[string]int, string) {
	if offset >= len(snapshots) {
		return map[string]int{}, ""
	}
	// snapshot 是目标日快照。
	snapshot := snapshots[offset]
	if snapshot.Status != StatusOK {
		return map[string]int{}, ""
	}
	// payload 是热词快照载荷。
	var payload HotKeywordSnapshotPayload
	// unmarshalErr 是载荷解析错误；解析失败按空榜单处理。
	if unmarshalErr := json.Unmarshal([]byte(snapshot.PayloadJSON), &payload); unmarshalErr != nil {
		return map[string]int{}, ""
	}
	// positions 是词到名次（1 起）的映射；重复词保留最靠前名次。
	positions := make(map[string]int, len(payload.Keywords))
	// index 是词条下标。
	for index, keyword := range payload.Keywords {
		// trimmed 是去空白后的词条。
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" {
			continue
		}
		// exists 表示该词是否已有更靠前名次。
		if _, exists := positions[trimmed]; !exists {
			positions[trimmed] = index + 1
		}
	}
	return positions, snapshot.Date
}

// momentumDaysOnList 统计窗口内每个词出现在成功快照中的天数。
func momentumDaysOnList(snapshots []Snapshot) map[string]int {
	// counts 是词到出现天数的映射。
	counts := make(map[string]int)
	// snapshot 是窗口内某日快照。
	for _, snapshot := range snapshots {
		if snapshot.Status != StatusOK {
			continue
		}
		// payload 是该日热词载荷。
		var payload HotKeywordSnapshotPayload
		// unmarshalErr 是载荷解析错误；解析失败跳过该日。
		if unmarshalErr := json.Unmarshal([]byte(snapshot.PayloadJSON), &payload); unmarshalErr != nil {
			continue
		}
		// seen 防止同日重复词重复计数。
		seen := make(map[string]struct{}, len(payload.Keywords))
		// keyword 是当前词条。
		for _, keyword := range payload.Keywords {
			// trimmed 是去空白后的词条。
			trimmed := strings.TrimSpace(keyword)
			if trimmed == "" {
				continue
			}
			// exists 表示该词当日已计数。
			if _, exists := seen[trimmed]; exists {
				continue
			}
			seen[trimmed] = struct{}{}
			counts[trimmed]++
		}
	}
	return counts
}

// momentumStatus 依据最新名次、前一日名次与在榜天数判定涨幅状态。
func momentumStatus(entry KeywordMomentum, wasOnList bool) string {
	switch {
	case !wasOnList:
		return MomentumNew
	case entry.PreviousPosition-entry.Position >= momentumRiseThreshold:
		return MomentumRising
	case entry.Position-entry.PreviousPosition >= momentumRiseThreshold:
		return MomentumFalling
	default:
		return MomentumSteady
	}
}
