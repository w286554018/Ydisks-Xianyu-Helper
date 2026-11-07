// Package browser: 竞品详情解析与守卫分支的确定性焦点测试（不需要真实 Chromium）。
package browser

import (
	"context"
	"errors"
	"testing"
)

// competitorDetailSample 是详情接口响应的真实结构样本（字段名与线上响应一致）。
const competitorDetailSample = `{
  "ret": ["SUCCESS::调用成功"],
  "data": {
    "itemDO": {
      "title": " AI绘画接单 ",
      "desc": "<div>专业<b>代画</b></div><p>价格面议</p>",
      "price": [{"text": "¥"}, {"text": "88"}, {"text": ".00"}],
      "imageInfos": [{"url": "https://img.example.com/a.jpg"}, {"url": "  "}, {"url": "https://img.example.com/b.jpg"}]
    },
    "sellerDO": {"sellerId": " 2200 ", "nickName": " 卖家甲 "}
  }
}`

// TestParseCompetitorDetailPayload 验证详情响应解析的合法与非法输入。
func TestParseCompetitorDetailPayload(t *testing.T) {
	// payload、usable 是合法响应样本的解析结果。
	payload, usable := parseCompetitorDetailPayload([]byte(competitorDetailSample))
	if !usable {
		t.Fatal("合法 JSON 响应应解析成功")
	}
	if len(payload.Ret) != 1 || payload.Data.ItemDO.Title == "" || payload.Data.SellerDO.SellerID == "" {
		t.Fatalf("解析字段不符: %+v", payload)
	}
	// 非 JSON 响应（例如被重定向到登录页）必须返回不可用而不是 panic。
	if _, htmlUsable := parseCompetitorDetailPayload([]byte("<html>登录</html>")); htmlUsable {
		t.Fatal("非 JSON 响应应判定为不可用")
	}
	// 空正文同样不可用。
	if _, emptyUsable := parseCompetitorDetailPayload(nil); emptyUsable {
		t.Fatal("空正文应判定为不可用")
	}
}

// TestBuildCompetitorDetail 验证详情视图转换：价格分段拼接、图片去空白与描述去标签。
func TestBuildCompetitorDetail(t *testing.T) {
	// payload 是样本响应解析结果。
	payload, usable := parseCompetitorDetailPayload([]byte(competitorDetailSample))
	if !usable {
		t.Fatal("样本响应应解析成功")
	}
	// detail 是转换后的详情视图。
	detail := buildCompetitorDetail("item-9", payload)
	if detail.ItemID != "item-9" || detail.Title != "AI绘画接单" {
		t.Fatalf("标题或商品标识不符: %+v", detail)
	}
	if detail.Price != "¥88.00" {
		t.Fatalf("价格分段拼接不符: %q", detail.Price)
	}
	if detail.SellerID != "2200" || detail.SellerNickname != "卖家甲" {
		t.Fatalf("卖家字段不符: %+v", detail)
	}
	if len(detail.Images) != 2 || detail.Images[0] != "https://img.example.com/a.jpg" || detail.Images[1] != "https://img.example.com/b.jpg" {
		t.Fatalf("图片收集去空白失败: %+v", detail.Images)
	}
	if detail.DescText != "专业 代画 价格面议" {
		t.Fatalf("描述去标签不符: %q", detail.DescText)
	}
}

// TestStripHTMLTags 验证 HTML 标签移除与空白压缩。
func TestStripHTMLTags(t *testing.T) {
	// cases 是原始文本与期望结果的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// raw 是原始描述文本。
		raw string
		// want 是期望的纯文本。
		want string
	}{
		{name: "换行与标签", raw: "<div>第一行</div>\n\n<div>第二行</div>", want: "第一行 第二行"},
		{name: "纯文本原样", raw: "  没有标签  ", want: "没有标签"},
		{name: "空文本", raw: "", want: ""},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// got 是本次调用的实际返回值。
			if got := stripHTMLTags(testCase.raw); got != testCase.want {
				t.Fatalf("去标签不符: got=%q want=%q", got, testCase.want)
			}
		})
	}
}

// TestCompetitorItemDetailRejectsInvalidInput 验证参数守卫与浏览器不可用时的错误分支。
func TestCompetitorItemDetailRejectsInvalidInput(t *testing.T) {
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// nilManager 是未初始化的管理器。
	var nilManager *Manager
	// err 是本次调用的返回错误。
	if _, err := nilManager.CompetitorItemDetail(ctx, "acc", "cookie", "item-1"); err == nil {
		t.Fatal("未初始化管理器应报错")
	}
	// emptyManager 是参数守卫阶段即可返回错误的管理器；守卫先于浏览器调用生效。
	emptyManager := &Manager{}
	// err 是本次调用的返回错误。
	if _, err := emptyManager.CompetitorItemDetail(ctx, "acc", "cookie", "  "); err == nil {
		t.Fatal("空商品标识应报错")
	}
	// err 是本次调用的返回错误。
	if _, err := emptyManager.CompetitorItemDetail(ctx, "", "cookie", "item-1"); err == nil {
		t.Fatal("空账号应报错")
	}
	// brokenManager 的浏览器安装阶段必然失败，用于覆盖「浏览器不可用」分支；参数必须合法才会走到该阶段。
	brokenManager := &Manager{installFn: func(context.Context) error { return errors.New("测试用安装失败") }}
	// err 是本次调用的返回错误。
	if _, err := brokenManager.CompetitorItemDetail(ctx, "acc", "cookie", "item-1"); err == nil {
		t.Fatal("浏览器不可用时应报错")
	}
}
