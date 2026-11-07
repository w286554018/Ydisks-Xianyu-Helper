// Package browser: 搜索页卡片解析与合并逻辑的确定性焦点测试（不需要真实 Chromium）。
package browser

import (
	"errors"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// fakeSourcingPage 是只替换 Evaluate 的 playwright.Page 替身；其余方法保持未实现即可。
type fakeSourcingPage struct {
	// Page 是被嵌入的真实接口类型，仅用于满足接口方法集。
	playwright.Page
	// evaluate 是被替换的页面脚本执行实现。
	evaluate func(expression string, arg ...any) (any, error)
}

// Evaluate 委托给测试注入的实现。
func (f *fakeSourcingPage) Evaluate(expression string, arg ...any) (any, error) {
	return f.evaluate(expression, arg...)
}

// TestBuildSearchItems 验证卡片文本解析：标题截断、价格与想要人数提取、详情链接拼接与无效卡片跳过。
func TestBuildSearchItems(t *testing.T) {
	// cards 是页面脚本返回的卡片原始文本集合。
	cards := []sourcingSearchCard{
		{ItemID: "1001", Text: "华为HUAWEI FreeBuds SE2蓝牙耳机 原装正品 ¥ 19 485人想要 广东 回复超快"},
		{ItemID: "1002", Text: "    "},
		{ItemID: "", Text: "只有文本没有标识"},
		{ItemID: "1003", Text: ""},
	}
	// items 是解析结果。
	items := buildSearchItems(cards)
	if len(items) != 2 {
		t.Fatalf("应保留 2 条有效卡片: %+v", items)
	}
	if items[0].ItemID != "1001" || items[0].Price != "19" || items[0].WantCount != "485" || items[0].Location != "广东" {
		t.Fatalf("首条卡片解析不符: %+v", items[0])
	}
	if items[0].Title != "华为HUAWEI FreeBuds SE2蓝牙耳机 原装正品" || items[0].DetailURL != "https://www.goofish.com/item?id=1001" {
		t.Fatalf("标题或详情链接不符: %+v", items[0])
	}
	// 无标识但有文本的卡片保留文本，且不拼接详情链接。
	if items[1].ItemID != "" || items[1].DetailURL != "" || items[1].Title == "" {
		t.Fatalf("无标识卡片处理不符: %+v", items[1])
	}
	// 空卡片集合必须返回空集合而不是 nil，避免前端判空歧义。
	if empty := buildSearchItems(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("空输入应返回空集合: %+v", empty)
	}
}

// TestExtractSearchItemsFromPage 验证页面脚本返回结构正常、异常与失败三类结果。
func TestExtractSearchItemsFromPage(t *testing.T) {
	// 正常返回：脚本结构可解析，卡片被转换为商品摘要。
	normal := &fakeSourcingPage{evaluate: func(string, ...any) (any, error) {
		return map[string]any{"cards": []any{map[string]any{
			"itemId": "2001",
			"text":   "二手显示器 ¥ 128 66人想要 浙江",
		}}}, nil
	}}
	// items 是正常路径的解析结果。
	items := extractSearchItems(normal)
	if len(items) != 1 || items[0].ItemID != "2001" || items[0].Price != "128" {
		t.Fatalf("正常路径解析不符: %+v", items)
	}

	// 脚本执行失败：页面尚未渲染，返回 nil。
	failed := &fakeSourcingPage{evaluate: func(string, ...any) (any, error) { return nil, errors.New("页面未就绪") }}
	// failedItems 是脚本执行失败时的解析结果。
	if failedItems := extractSearchItems(failed); failedItems != nil {
		t.Fatalf("脚本失败应返回 nil: %+v", failedItems)
	}

	// 返回值无法编码为 JSON：视为页面未渲染，返回 nil。
	unencodable := &fakeSourcingPage{evaluate: func(string, ...any) (any, error) { return make(chan int), nil }}
	// unencodableItems 是返回值无法编码时的解析结果。
	if unencodableItems := extractSearchItems(unencodable); unencodableItems != nil {
		t.Fatalf("无法编码应返回 nil: %+v", unencodableItems)
	}

	// 返回值结构不匹配：视为页面未渲染，返回 nil。
	mismatched := &fakeSourcingPage{evaluate: func(string, ...any) (any, error) { return "not-a-payload", nil }}
	// mismatchedItems 是返回值结构不匹配时的解析结果。
	if mismatchedItems := extractSearchItems(mismatched); mismatchedItems != nil {
		t.Fatalf("结构不匹配应返回 nil: %+v", mismatchedItems)
	}
}

// TestMergeSearchItems 验证接口数据优先、缺省字段按商品标识用 DOM 值补齐。
func TestMergeSearchItems(t *testing.T) {
	// apiItems 是搜索接口结构化数据，其中第二条缺标题与价格。
	apiItems := []SearchItem{
		{ItemID: "1", Title: "接口标题", Price: "10", WantCount: "3"},
		{ItemID: "2", Title: "", Price: "", WantCount: ""},
	}
	// domItems 是页面 DOM 兜底数据。
	domItems := []SearchItem{
		{ItemID: "1", Title: "DOM标题", Price: "99"},
		{ItemID: "2", Title: "兜底标题", Price: "20", WantCount: "8"},
		{ItemID: "3", Title: "仅 DOM 商品"},
	}
	// merged 是合并结果。
	merged := mergeSearchItems(apiItems, domItems)
	if len(merged) != 2 {
		t.Fatalf("接口优先时不应引入额外商品: %+v", merged)
	}
	if merged[0].Title != "接口标题" || merged[0].Price != "10" {
		t.Fatalf("接口已有字段不应被覆盖: %+v", merged[0])
	}
	if merged[1].Title != "兜底标题" || merged[1].Price != "20" || merged[1].WantCount != "8" {
		t.Fatalf("缺省字段应被补齐: %+v", merged[1])
	}

	// 接口无数据时必须原样使用 DOM 结果，保证兜底链路可用。
	if domOnly := mergeSearchItems(nil, domItems); len(domOnly) != 3 {
		t.Fatalf("接口无数据应返回 DOM 结果: %+v", domOnly)
	}

	// 接口有数据但商品未出现在 DOM 时，缺省字段保持为空且不报错。
	// unmatched 是同标识不存在的接口数据。
	unmatched := mergeSearchItems([]SearchItem{{ItemID: "9"}}, domItems)
	if len(unmatched) != 1 || unmatched[0].Title != "" || unmatched[0].Price != "" {
		t.Fatalf("无 DOM 匹配时应保留原值: %+v", unmatched)
	}
}
