package composition

import (
	"testing"

	libraryapp "xianyu-go/internal/application/library"
	marketintelapp "xianyu-go/internal/application/marketintel"
)

// TestBuildMarketIntelCarry 验证竞品搬运用例的装配与构造期快速失败分支。
func TestBuildMarketIntelCarry(t *testing.T) {
	// marketIntel 是仅用于装配校验的市场情报服务（不执行任何用例）。
	marketIntel := marketintelapp.NewService(nil, nil, nil, nil, nil, nil)
	// library 是仅用于装配校验的资源库服务（不执行任何用例）。
	library := libraryapp.NewService(nil, nil, nil)

	// 市场情报未启用时搬运服务必须为 nil，端点按功能未启用降级。
	if service, err := buildMarketIntelCarry(nil, library); err != nil || service != nil {
		t.Fatalf("市场情报缺失应返回 nil 且不报错: service=%v err=%v", service, err)
	}
	// 资源库未启用时搬运服务必须为 nil，端点按功能未启用降级。
	if service, err := buildMarketIntelCarry(marketIntel, nil); err != nil || service != nil {
		t.Fatalf("资源库缺失应返回 nil 且不报错: service=%v err=%v", service, err)
	}
	// 两者齐备时必须装配出可用服务。
	service, err := buildMarketIntelCarry(marketIntel, library)
	if err != nil || service == nil {
		t.Fatalf("装配搬运服务失败: service=%v err=%v", service, err)
	}

	// 下载函数缺失时必须快速失败，避免运行期空指针。
	if _, err := newMarketIntelCarry(marketIntel, library, nil); err == nil {
		t.Fatal("下载函数缺失应构造失败")
	}
	// 资源库缺失时必须快速失败（绕过前置 nil 降级，覆盖适配器构造分支）。
	if _, err := newMarketIntelCarry(marketIntel, nil, downloadImageURL); err == nil {
		t.Fatal("资源库缺失应构造失败")
	}
	// 详情能力缺失时必须快速失败（覆盖搬运用例构造分支）。
	if _, err := newMarketIntelCarry(nil, library, downloadImageURL); err == nil {
		t.Fatal("详情能力缺失应构造失败")
	}
}
