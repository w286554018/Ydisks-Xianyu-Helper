package browser

import (
	"errors"
	"testing"
)

// TestSourcingRetErrorClassifiesRiskControl 验证风控返回码被归类为风控错误且不重试。
func TestSourcingRetErrorClassifiesRiskControl(t *testing.T) {
	// rets 是平台真实出现过的风控返回码文本。
	rets := []string{"RGV587_ERROR::SM::哎哟喂,被挤爆啦,请稍后重试!"}
	// err 是调用 sourcingRetError 的结果，按本用例期望断言成功或特定领域错误。
	if err := sourcingRetError(rets); !errors.Is(err, ErrSourcingRiskControl) {
		t.Fatalf("应归类为风控错误, got %v", err)
	}
}

// TestSourcingRetErrorClassifiesUserValidate 验证人机校验返回码同样按风控处理。
func TestSourcingRetErrorClassifiesUserValidate(t *testing.T) {
	// rets 是需要人工校验的返回码。
	rets := []string{"FAIL_SYS_USER_VALIDATE::哎哟喂,被挤爆啦"}
	// err 是调用 sourcingRetError 的结果，按本用例期望断言成功或特定领域错误。
	if err := sourcingRetError(rets); !errors.Is(err, ErrSourcingRiskControl) {
		t.Fatalf("应归类为风控错误, got %v", err)
	}
}

// TestSourcingRetErrorClassifiesSessionExpired 验证会话过期被单独归类。
func TestSourcingRetErrorClassifiesSessionExpired(t *testing.T) {
	// rets 是会话过期与令牌为空的返回码。
	rets := []string{"FAIL_SYS_SESSION_EXPIRED::Session过期", "FAIL_SYS_TOKEN_EMPTY::令牌为空"}
	// err 是调用 sourcingRetError 的结果，按本用例期望断言成功或特定领域错误。
	if err := sourcingRetError(rets); !errors.Is(err, ErrSourcingSessionExpired) {
		t.Fatalf("应归类为会话过期, got %v", err)
	}
}

// TestSourcingRetErrorIgnoresSuccess 验证成功返回码不会产生错误。
func TestSourcingRetErrorIgnoresSuccess(t *testing.T) {
	// rets 是成功返回码。
	rets := []string{"SUCCESS::调用成功"}
	// err 是调用 sourcingRetError 的结果，按本用例期望断言成功或特定领域错误。
	if err := sourcingRetError(rets); err != nil {
		t.Fatalf("成功返回码不应产生错误, got %v", err)
	}
	// err 是调用 sourcingRetError 的结果，按本用例期望断言成功或特定领域错误。
	if err := sourcingRetError(nil); err != nil {
		t.Fatalf("空返回码不应产生错误, got %v", err)
	}
}

// TestExtractSearchTitle 验证标题只截取价格之前的部分。
func TestExtractSearchTitle(t *testing.T) {
	// cases 使用探针抓到的真实卡片文本作为样本。
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "价格前截断",
			text: "华为HUAWEI FreeBuds SE2蓝牙耳机左耳右耳充 电仓盒配件补配。 原装正品 ¥ 19 485人想要 广东 回复超快",
			want: "华为HUAWEI FreeBuds SE2蓝牙耳机左耳右耳充 电仓盒配件补配。 原装正品",
		},
		{
			name: "缺少价格符号时保留全文",
			text: "纯个人闲置，便宜出，贩子勿扰！",
			want: "纯个人闲置，便宜出，贩子勿扰！",
		},
		{
			name: "价格位于开头时不截断为空白",
			text: "¥9.99 最后几个",
			want: "¥9.99 最后几个",
		},
	}
	// c 是当前用例的输入与期望结果。
	for _, c := range cases {
		// got 是 extractSearchTitle 对当前用例输入的实际结果。
		if got := extractSearchTitle(c.text); got != c.want {
			t.Fatalf("%s: extractSearchTitle = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestExtractSearchPrice 验证价格提取能容忍页面中的空格分隔。
func TestExtractSearchPrice(t *testing.T) {
	// cases 覆盖整数、小数、带空格分隔与缺失价格的情形。
	cases := []struct {
		text string
		want string
	}{
		{"某商品 ¥ 19 485人想要 广东", "19"},
		{"某商品 ¥ 34 .80 累计降", "34.80"},
		{"某商品 ¥ 9 .99 ¥180 广东", "9.99"},
		{"某商品没有标价", ""},
	}
	// c 是当前用例的输入与期望结果。
	for _, c := range cases {
		// got 是 extractSearchPrice 对当前用例输入的实际结果。
		if got := extractSearchPrice(c.text); got != c.want {
			t.Fatalf("extractSearchPrice(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

// TestExtractSearchWantCount 验证"人想要"人数提取。
func TestExtractSearchWantCount(t *testing.T) {
	// cases 覆盖有人数、无人数与人数紧贴其他数字的情形。
	cases := []struct {
		text string
		want string
	}{
		{"华为耳机 ¥ 19 485人想要 广东", "485"},
		{"vivo耳机 ¥ 35 227人想要 北京", "227"},
		{"没有任何人想要信息", ""},
	}
	// c 是当前用例的输入与期望结果。
	for _, c := range cases {
		// got 是 extractSearchWantCount 对当前用例输入的实际结果。
		if got := extractSearchWantCount(c.text); got != c.want {
			t.Fatalf("extractSearchWantCount(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

// TestExtractSearchLocation 验证地区提取。
func TestExtractSearchLocation(t *testing.T) {
	// cases 覆盖已知地区与无法识别的文本。
	cases := []struct {
		text string
		want string
	}{
		{"商品 ¥ 19 485人想要 广东 回复超快", "广东"},
		{"商品 ¥ 100 累计降价85% 北京 回复超快", "北京"},
		{"商品 ¥ 20 没有地区信息", ""},
	}
	// c 是当前用例的输入与期望结果。
	for _, c := range cases {
		// got 是 extractSearchLocation 对当前用例输入的实际结果。
		if got := extractSearchLocation(c.text); got != c.want {
			t.Fatalf("extractSearchLocation(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

// TestSourcingSearchLimitIsBounded 验证单次查询上限是 30，防止后续改动放大请求规模。
func TestSourcingSearchLimitIsBounded(t *testing.T) {
	if sourcingSearchResultLimit != 30 {
		t.Fatalf("单次查询上限必须为 30, got %d", sourcingSearchResultLimit)
	}
	if sourcingHotKeywordLimit <= 0 || sourcingHotKeywordLimit > 30 {
		t.Fatalf("热词上限超出合理范围: %d", sourcingHotKeywordLimit)
	}
}
