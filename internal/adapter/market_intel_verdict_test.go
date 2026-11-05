// Package adapter: 市场情报判定端口焦点测试——JSON 解析、栅栏剥离与下标映射。
package adapter

import (
	"context"
	"strings"
	"testing"

	marketintel "xianyu-go/internal/application/marketintel"
)

// TestParseVerdictContent 验证 JSON 解析、代码栅栏剥离与非法下标跳过。
func TestParseVerdictContent(t *testing.T) {
	// items 是输入列表。
	items := []marketintel.VerdictInput{
		{SnapshotID: 11, Title: "AI课程代做"},
		{SnapshotID: 12, Title: "普通蓝牙耳机"},
	}
	// cases 是正文与期望结果的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// content 是模型正文。
		content string
		// wantLen 是期望结果条数。
		wantLen int
		// wantFirstID 是首条映射的快照主键。
		wantFirstID int64
		// wantFirstAI 是首条是否 AI 相关。
		wantFirstAI bool
		// wantErr 表示是否期望解析失败。
		wantErr bool
	}{
		{name: "裸 JSON 数组", content: `[{"index":0,"is_ai_related":true,"category":"AI课程","reason":"标题含AI"}]`, wantLen: 1, wantFirstID: 11, wantFirstAI: true},
		{name: "markdown 栅栏包裹", content: "```json\n[{\"index\":1,\"is_ai_related\":false,\"category\":\"\",\"reason\":\"普通商品\"}]\n```", wantLen: 1, wantFirstID: 12, wantFirstAI: false},
		{name: "非法下标跳过", content: `[{"index":9,"is_ai_related":true,"category":"AI课程","reason":"越界"}]`, wantLen: 0, wantErr: true},
		{name: "非法 JSON", content: `这不是JSON`, wantErr: true},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// results、err 是解析结果与错误。
			results, err := parseVerdictContent(testCase.content, items)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("期望解析失败，实际成功: %+v", results)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != testCase.wantLen {
				t.Fatalf("期望 %d 条，实际 %d", testCase.wantLen, len(results))
			}
			if results[0].SnapshotID != testCase.wantFirstID || results[0].IsAIRelated != testCase.wantFirstAI {
				t.Fatalf("首条结果不符: %+v", results[0])
			}
		})
	}
}

// TestJudgeBatchNilStore 验证存储未装配时返回配置缺失错误而非 panic。
func TestJudgeBatchNilStore(t *testing.T) {
	// port 是未绑定存储的判定端口。
	port := &MarketIntelVerdictPort{}
	// 未装配存储时应返回明确错误。
	_, err := port.JudgeBatch(context.Background(), "AI课程", []marketintel.VerdictInput{{SnapshotID: 1, Title: "AI课程"}})
	if err == nil {
		t.Fatal("未装配存储应返回错误")
	}
}

// TestParseWantCount 验证想要人数文本解析。
func TestParseWantCount(t *testing.T) {
	// cases 是文本与期望数值的对照表。
	cases := []struct {
		// text 是原始文本。
		text string
		// want 是期望数值。
		want int
	}{
		{text: "120人想要", want: 120},
		{text: "1.2万人想要", want: 12000},
		{text: "想要", want: 0},
		{text: "", want: 0},
		{text: "3,000人想要", want: 3000},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.text, func(t *testing.T) {
			// got 是实际解析数值。
			if got := parseWantCount(testCase.text); got != testCase.want {
				t.Fatalf("期望 %d，实际 %d", testCase.want, got)
			}
		})
	}
}

// TestBuildVerdictUserPrompt 验证批量判定提示的组装格式。
func TestBuildVerdictUserPrompt(t *testing.T) {
	// items 是输入列表。
	items := []marketintel.VerdictInput{
		{SnapshotID: 1, Title: "AI课程代做", Price: "99", WantCountText: "120人想要"},
		{SnapshotID: 2, Title: "蓝牙耳机", Price: "19.9", WantCountText: "5人想要"},
	}
	// prompt 是组装后的用户提示。
	prompt := buildVerdictUserPrompt("AI课程", items)
	// 提示必须包含关键词、下标、标题与想要人数。
	if !containsAllStrings(prompt, []string{"AI课程", "0.", "AI课程代做", "120人想要", "1.", "蓝牙耳机"}) {
		t.Fatalf("判定提示格式不符: %q", prompt)
	}
}

// containsAllStrings 判断文本是否包含全部子串。
func containsAllStrings(text string, substrings []string) bool {
	// substring 遍历子串。
	for _, substring := range substrings {
		if !strings.Contains(text, substring) {
			return false
		}
	}
	return true
}
