// Package browser: 竞品详情解析与守卫分支的确定性焦点测试（不需要真实 Chromium）。
package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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
	if _, _, err := nilManager.CompetitorItemDetail(ctx, "acc", "cookie", "item-1"); err == nil {
		t.Fatal("未初始化管理器应报错")
	}
	// emptyManager 是参数守卫阶段即可返回错误的管理器；守卫先于浏览器调用生效。
	emptyManager := &Manager{}
	// err 是本次调用的返回错误。
	if _, _, err := emptyManager.CompetitorItemDetail(ctx, "acc", "cookie", "  "); err == nil {
		t.Fatal("空商品标识应报错")
	}
	// err 是本次调用的返回错误。
	if _, _, err := emptyManager.CompetitorItemDetail(ctx, "", "cookie", "item-1"); err == nil {
		t.Fatal("空账号应报错")
	}
	// brokenManager 的浏览器安装阶段必然失败，用于覆盖「浏览器不可用」分支；参数必须合法才会走到该阶段。
	brokenManager := &Manager{installFn: func(context.Context) error { return errors.New("测试用安装失败") }}
	// err 是本次调用的返回错误。
	if _, _, err := brokenManager.CompetitorItemDetail(ctx, "acc", "cookie", "item-1"); err == nil {
		t.Fatal("浏览器不可用时应报错")
	}
}

// TestSanitizeDiagnosticURL 验证诊断地址去除 query 与 fragment，避免把令牌写进日志。
func TestSanitizeDiagnosticURL(t *testing.T) {
	// cases 是原始地址与期望结果的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// raw 是原始地址。
		raw string
		// want 是去除 query 与 fragment 后的地址。
		want string
	}{
		{name: "含 query", raw: "https://h5api.m.goofish.com/h5/x5sec.htm?token=secret&b=2", want: "https://h5api.m.goofish.com/h5/x5sec.htm"},
		{name: "含 fragment", raw: "https://www.goofish.com/item?id=1#top", want: "https://www.goofish.com/item"},
		{name: "干净地址", raw: "  https://www.goofish.com/punish.htm  ", want: "https://www.goofish.com/punish.htm"},
		{name: "空值", raw: "   ", want: ""},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// got 是实际提取到的候选地址集合。
			if got := sanitizeDiagnosticURL(testCase.raw); got != testCase.want {
				t.Fatalf("地址清理不符: got=%q want=%q", got, testCase.want)
			}
		})
	}
}

// TestFindVerificationURLs 验证从响应正文提取疑似验证或处罚地址并限制条数。
func TestFindVerificationURLs(t *testing.T) {
	// body 是含多个地址的模拟处罚响应正文。
	body := []byte(`{"ret":["RGV587_ERROR::SM::哎哟喂,被挤爆啦"],"data":{"punishUrl":"https://h5api.m.goofish.com/h5/x5sec.htm?token=abc","other":"https://www.goofish.com/normal.htm","captcha":"https://captcha.alibaba.com/verify?t=1","third":"https://x.example.com/verification/entry","fourth":"https://y.example.com/captcha/another"}}`)
	// urls 是提取结果。
	urls := findVerificationURLs(body)
	if len(urls) != competitorDetailURLCandidateLimit {
		t.Fatalf("应限制候选地址条数: %+v", urls)
	}
	if urls[0] != "https://h5api.m.goofish.com/h5/x5sec.htm" {
		t.Fatalf("首个候选地址应去除 query: %+v", urls)
	}
	// 普通地址不应被当作验证地址。
	// plain 是不含验证关键字的正文。
	plain := []byte(`{"data":{"url":"https://www.goofish.com/item?id=1"}}`)
	// got 是普通地址正文的提取结果。
	if got := findVerificationURLs(plain); len(got) != 0 {
		t.Fatalf("普通地址不应入选: %+v", got)
	}
	// 无地址正文返回空切片。
	if got := findVerificationURLs([]byte("no url here")); len(got) != 0 {
		t.Fatalf("无地址应返回空: %+v", got)
	}
}

// TestBuildCompetitorDetailDiagnostic 验证诊断结构保留状态、地址与返回码。
func TestBuildCompetitorDetailDiagnostic(t *testing.T) {
	// diagnostic 是组装结果。
	diagnostic := buildCompetitorDetailDiagnostic(200, "https://h5api.m.goofish.com/h5/x5sec.htm?token=abc",
		[]byte(`{"punishUrl":"https://h5api.m.goofish.com/h5/x5sec.htm?token=abc"}`), []string{"RGV587_ERROR::SM::哎哟喂"})
	if diagnostic.Status != 200 || diagnostic.URL != "https://h5api.m.goofish.com/h5/x5sec.htm" {
		t.Fatalf("状态或地址不符: %+v", diagnostic)
	}
	if len(diagnostic.Rets) != 1 || len(diagnostic.CandidateURLs) != 1 {
		t.Fatalf("返回码或候选地址不符: %+v", diagnostic)
	}
}

// TestFindCompetitorDetailPunishURL 验证从详情响应正文提取平台处罚地址并保留 query。
func TestFindCompetitorDetailPunishURL(t *testing.T) {
	// body 是含处罚地址的模拟响应正文；地址中的验证参数必须原样保留。
	body := `{"ret":["FAIL_SYS_USER_VALIDATE::"],"data":{"url":"https:\/\/h5api.m.goofish.com:443//h5/mtop.taobao.idle.pc.detail/1.0/_____tmd_____/punish?x5secdata=abc&action=captcha"}}`
	// got 是提取到的处罚地址。
	got := findCompetitorDetailPunishURL(body)
	if !strings.Contains(got, competitorDetailPunishMarker) || !strings.Contains(got, "x5secdata=abc") {
		t.Fatalf("处罚地址提取不符: %q", got)
	}
	// 无处罚地址的正文必须返回空串。
	if plain := findCompetitorDetailPunishURL(`{"data":{"url":"https://www.goofish.com/item?id=1"}}`); plain != "" {
		t.Fatalf("普通正文不应提取处罚地址: %q", plain)
	}
	// 完全无地址的正文必须返回空串。
	if none := findCompetitorDetailPunishURL("no url"); none != "" {
		t.Fatalf("无地址正文应返回空串: %q", none)
	}
}

// TestCaptchaRecoverySuppression 验证自动验证失败的抑制窗口：失败后抑制、成功后清除、窗口过期恢复。
func TestCaptchaRecoverySuppression(t *testing.T) {
	// manager 是被测浏览器管理器；仅使用内存抑制状态，不启动浏览器。
	manager := &Manager{}
	if manager.captchaRecoverySuppressed("acc-1") {
		t.Fatal("未失败过的账号不应被抑制")
	}
	manager.markCaptchaRecoveryFailure("acc-1")
	if !manager.captchaRecoverySuppressed("acc-1") {
		t.Fatal("失败后应进入抑制窗口")
	}
	// 其它账号不受影响。
	if manager.captchaRecoverySuppressed("acc-2") {
		t.Fatal("抑制窗口必须按账号隔离")
	}
	manager.clearCaptchaRecoveryFailure("acc-1")
	if manager.captchaRecoverySuppressed("acc-1") {
		t.Fatal("成功后应清除抑制记录")
	}

	// 窗口过期后必须恢复自动尝试。
	manager.markCaptchaRecoveryFailure("acc-1")
	manager.captchaGuardMu.Lock()
	manager.captchaFailedAt["acc-1"] = time.Now().Add(-competitorDetailCaptchaCooldown - time.Minute)
	manager.captchaGuardMu.Unlock()
	if manager.captchaRecoverySuppressed("acc-1") {
		t.Fatal("抑制窗口过期后应恢复尝试")
	}
	// 抑制与验证结果日志都不得带有 query 参数，且日志器缺失时必须安全回退。
	manager.logCompetitorDetailRecoverySuppressed("item-1", "https://h5api.m.goofish.com/h5/x5sec.htm?x5secdata=secret")
	manager.logCompetitorDetailRecoveryFailure("item-1", "https://h5api.m.goofish.com/h5/x5sec.htm?x5secdata=secret", errors.New("滑块验证失败"))
	manager.logCompetitorDetailRecoverySuccess("item-1", "playwright")
}
