package composition

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/adapter"
	itemapp "xianyu-go/internal/application/items"
	marketintelapp "xianyu-go/internal/application/marketintel"
)

// TestTransportPortsPreserveItemServices 验证完整装配后的商品端口可查询临时库；t 管理夹具与失败断言，不启动平台任务。
func TestTransportPortsPreserveItemServices(t *testing.T) {
	// built 保存使用临时 SQLite 构造的服务集合。
	built := buildTestServices(t, true)
	// ports 是 HTTP 层使用的只读服务投影，必须保留已经构造的商品服务。
	ports := built.services.TransportPorts()
	if ports.ItemLifecycle == nil || ports.ItemCopyPublish == nil {
		t.Fatal("商品生命周期和复制发布端口必须完整注入")
	}
	// batches、listErr 分别是用户在空库中的生命周期批次及查询错误。
	batches, listErr := ports.ItemLifecycle.ListBatches(context.Background(), 1, 20)
	if listErr != nil || len(batches) != 0 {
		t.Fatalf("空库批次查询失败: batches=%d err=%v", len(batches), listErr)
	}
	if ports.ItemCopyPublish != built.services.itemCopyPublish {
		t.Fatal("复制发布端口必须沿用组合根构造的服务")
	}
	// previewErr 应为本地账号归属拒绝，证明端口实际调用服务且不会触发平台请求。
	if _, previewErr := ports.ItemCopyPublish.PreviewCopy(context.Background(), 1, "missing-account", "missing-item", nil); !errors.Is(previewErr, itemapp.ErrCopyNotOwned) {
		t.Fatalf("复制预览应拒绝不属于用户的源账号: %v", previewErr)
	}
}

// TestNewUsesRuntimeMarketIntelDependency 验证运行时单独注入的市场情报服务可通过 transport 读取持久化词表；t 管理临时库，不执行扫描。
func TestNewUsesRuntimeMarketIntelDependency(t *testing.T) {
	// built 提供完整依赖与临时 SQLite，TransportApplications 中不预设市场情报服务。
	built := buildTestServices(t, true)
	// ctx 只用于本地词表读写，不访问外部平台。
	ctx := context.Background()
	// keywordStore 是测试词表持久化适配器。
	keywordStore := adapter.NewMarketIntelStore(built.store)
	// marketService 与生产运行时一样通过专用依赖字段传入，仅调用本地查询能力。
	marketService := marketintelapp.NewService(keywordStore, adapter.NewMarketIntelScanStore(built.store), nil, nil, built.dependencies.Logger)
	// keyword、createErr 分别是持久化的测试关注词及写入错误。
	keyword, createErr := marketService.CreateKeyword(ctx, "装配回归测试", true)
	if createErr != nil {
		t.Fatal(createErr)
	}
	built.dependencies.MarketIntel = marketService
	// services、buildErr 分别是按生产依赖入口重新构造的服务集合及装配错误。
	services, buildErr := New(built.dependencies)
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	// port 是 HTTP 层实际消费的市场情报应用端口。
	port := services.TransportPorts().MarketIntel
	if port == nil {
		t.Fatal("运行时注入的市场情报服务未传入 HTTP 端口")
	}
	// keywords、listErr 分别是端口查询得到的词表及读取错误。
	keywords, listErr := port.ListKeywords(ctx)
	if listErr != nil || len(keywords) != 1 || keywords[0].ID != keyword.ID {
		t.Fatalf("市场情报端口未读取已保存的关注词: count=%d err=%v", len(keywords), listErr)
	}
	if port != marketService {
		t.Fatal("市场情报端口必须使用运行时提供的服务实例")
	}
}
