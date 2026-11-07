package runtime

import (
	"xianyu-go/internal/adapter"
	marketintelapp "xianyu-go/internal/application/marketintel"
	"xianyu-go/internal/browser"
)

// buildMarketIntel 装配市场情报应用服务；浏览器禁用时搜索端口为 nil，扫描明确降级。
func buildMarketIntel(infrastructure RuntimeInfrastructure, browserManager *browser.Manager) *marketintelapp.Service {
	// store 是词表持久化适配器。
	store := adapter.NewMarketIntelStore(infrastructure.Store)
	// scanStore 是扫描持久化适配器。
	scanStore := adapter.NewMarketIntelScanStore(infrastructure.Store)
	// 搜索端口依赖浏览器网关与凭证读取；浏览器禁用时保持 nil，由应用层明确降级。
	// 扫描账号自动选择：cookieID 留空时每次搜索取账号目录第一个账号。
	var searchPort marketintelapp.SearchPort
	if browserManager != nil {
		searchPort = adapter.NewMarketIntelSearchPort(
			adapter.NewSourcingBrowserGateway(browserManager),
			adapter.NewSourcingCredentialReader(infrastructure.Store),
			infrastructure.Store.Admin, 1, "")
	}
	// verdictPort 是 AI 批量判定端口；密钥未配置时判定降级为只落快照。
	verdictPort := adapter.NewMarketIntelVerdictPort(infrastructure.Store, 1)
	return marketintelapp.NewService(store, scanStore, searchPort, verdictPort, infrastructure.Logger)
}
