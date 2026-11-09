// Package aicompat: 兼容网关响应形状归一的确定性测试。
package aicompat

import (
	"encoding/json"
	"testing"
)

// TestUnwrapChatResponseEnvelope 验证响应形状归一：
// 标准形状原样返回；被 data 包一层时返回内层对象；其余形状原样返回。
func TestUnwrapChatResponseEnvelope(t *testing.T) {
	// standard 是标准 OpenAI 兼容响应。
	standard := `{"choices":[{"message":{"content":"标准"}}]}`
	// wrapped 是被网关包一层 data 的响应（线上实测形状）。
	wrapped := `{"data":{"choices":[{"message":{"content":"包装"}}],"id":"x"},"extra":1}`
	// cases 是各类输入与期望的候选数量。
	cases := []struct {
		// name 是用例名。
		name string
		// raw 是输入响应。
		raw string
		// wantChoiceCount 是归一后解析到的候选数量。
		wantChoiceCount int
	}{
		{name: "标准形状原样返回", raw: standard, wantChoiceCount: 1},
		{name: "data 包装时解出内层", raw: wrapped, wantChoiceCount: 1},
		{name: "非 JSON 原样返回", raw: `not-json`, wantChoiceCount: 0},
		{name: "data 内没有 choices 时原样返回", raw: `{"data":{"id":"x"}}`, wantChoiceCount: 0},
		{name: "data 非对象时原样返回", raw: `{"data":"text"}`, wantChoiceCount: 0},
		{name: "choices 为空数组时不误判包装", raw: `{"choices":[]}`, wantChoiceCount: 0},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// normalized 是归一后的响应字节。
			normalized := UnwrapChatResponseEnvelope([]byte(tc.raw))
			// payload 是用于统计候选数量的最小结构。
			var payload struct {
				// Choices 是候选列表。
				Choices []json.RawMessage `json:"choices"`
			}
			if // err 是归一结果的 JSON 解析错误；非 JSON 输入会被原样返回，此时按原样处理。
			err := json.Unmarshal(normalized, &payload); err != nil {
				if string(normalized) == tc.raw {
					return
				}
				t.Fatalf("归一结果不是合法 JSON: %v", err)
			}
			if len(payload.Choices) != tc.wantChoiceCount {
				t.Fatalf("候选数量期望 %d，实际 %d（归一结果：%s）", tc.wantChoiceCount, len(payload.Choices), normalized)
			}
		})
	}
}
