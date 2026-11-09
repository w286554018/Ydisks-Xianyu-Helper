package adapter

import "encoding/json"

// unwrapChatResponseEnvelope 兼容会被额外包一层的 OpenAI 兼容网关。
//
// 部分自建网关（例如 sub2api）把标准 chat completion 响应整体放进 `data` 字段：
// `{"data":{"choices":[...]}}`。按顶层读取 `choices` 会得到空候选，从而把
// 「模型已正常回答」误判成「模型返回空结果」。本函数在解包前做一次形状归一：
// 顶层已有 `choices` 时原样返回；否则若 `data` 是对象且其内部含 `choices`，
// 返回该内层对象的原始 JSON；其余形状（含非 JSON）一律原样返回，交由调用方按原逻辑报错。
func unwrapChatResponseEnvelope(raw []byte) []byte {
	// probe 是仅用于探测形状的最小结构。
	var probe struct {
		// Choices 是顶层候选列表；非空表示已是标准形状。
		Choices json.RawMessage `json:"choices"`
		// Data 是可能承载真实响应的包装对象。
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return raw
	}
	if len(probe.Choices) > 0 {
		return raw
	}
	// inner 是包装对象内部的候选列表。
	var inner struct {
		// Choices 是内层候选列表。
		Choices json.RawMessage `json:"choices"`
	}
	if len(probe.Data) == 0 || json.Unmarshal(probe.Data, &inner) != nil || len(inner.Choices) == 0 {
		return raw
	}
	return probe.Data
}
