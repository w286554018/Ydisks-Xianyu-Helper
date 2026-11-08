package runtime

import (
	"xianyu-go/internal/adapter"
	marketintelapp "xianyu-go/internal/application/marketintel"
	"xianyu-go/internal/browser"
)

// buildMarketIntel 装配市场情报应用服务。
//
// 搜索默认使用服务器侧浏览器端口；浏览器禁用时两个端口都保持真正的 nil，
// 由应用层明确降级（typed-nil 会绕过应用层的「能力未装配」判断）。
// 配置了本机抓取助手时，**详情**改走助手（用户本机真实浏览器），并且刻意不保留
// 服务器侧浏览器作为运行时回退：该路径已被平台确定性判罚，回退只会浪费时间并加重风控。
func buildMarketIntel(infrastructure RuntimeInfrastructure, browserManager *browser.Manager, options RuntimeOptions) *marketintelapp.Service {
	// store 是词表持久化适配器。
	store := adapter.NewMarketIntelStore(infrastructure.Store)
	// scanStore 是扫描持久化适配器。
	scanStore := adapter.NewMarketIntelScanStore(infrastructure.Store)
	// searchPort 是搜索端口接口值；浏览器禁用时保持真正的 nil。
	var searchPort marketintelapp.SearchPort
	// detailPort 是详情抓取端口接口值；浏览器禁用且未配置助手时保持真正的 nil。
	var detailPort marketintelapp.DetailFetcher
	if browserManager != nil {
		// browserPort 是服务器侧浏览器端口，同时实现搜索端口与详情端口。
		browserPort := adapter.NewMarketIntelSearchPort(
			adapter.NewSourcingBrowserGateway(browserManager),
			adapter.NewSourcingCredentialReader(infrastructure.Store),
			infrastructure.Store, 1, "", infrastructure.Logger)
		searchPort = browserPort
		detailPort = browserPort
	}
	// verdictPort 是 AI 批量判定端口；密钥未配置时判定降级为只落快照。
	verdictPort := adapter.NewMarketIntelVerdictPort(infrastructure.Store, 1)
	// agentClient 是本机抓取助手客户端；地址或令牌未配置时为 nil，表示该通道未启用。
	agentClient := adapter.NewLocalFetchAgentClient(
		options.LocalFetchAgentURL, options.LocalFetchAgentToken, infrastructure.Logger)
	if agentClient != nil {
		detailPort = agentClient
		// 只记录「已启用」，不记录地址与令牌。
		infrastructure.Logger.Info("竞品详情改用本机抓取助手")
	}
	return marketintelapp.NewService(store, scanStore, searchPort, verdictPort, detailPort, infrastructure.Logger)
}
