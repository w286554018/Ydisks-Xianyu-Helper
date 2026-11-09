package runtime

import (
	"fmt"

	"xianyu-go/internal/adapter"
	itemapp "xianyu-go/internal/application/items"
)

// buildItemStats 装配我的商品统计服务。
//
// 抓取端口固定指向本机抓取助手：服务器侧浏览器抓商品详情已被平台确定性判罚，
// 因此刻意不保留运行时回退。助手未配置时客户端为 nil，调用会返回「助手不可用」，
// 由传输层映射为 503 + 可操作提示，而不是悄悄改走其它路径。
func buildItemStats(infrastructure RuntimeInfrastructure, options RuntimeOptions) (*itemapp.ItemStatsService, error) {
	// fetcher 是本机助手抓取端口；nil 客户端的调用受 nil 接收者保护，返回不可用错误。
	var fetcher itemapp.ItemStatsFetcher = adapter.NewLocalFetchAgentClient(
		options.LocalFetchAgentURL, options.LocalFetchAgentToken, infrastructure.Logger)
	// ownership 复用批量预检端口的非敏感账号归属校验能力，避免统计端点为他人账号取数。
	ownership := adapter.NewItemBatchPreviewPort(infrastructure.Store)
	// service、err 是商品统计服务与构造错误。
	service, err := itemapp.NewItemStatsService(adapter.NewItemStatsStore(infrastructure.Store), fetcher, ownership)
	if err != nil {
		return nil, fmt.Errorf("构造商品统计服务失败: %w", err)
	}
	if options.LocalFetchAgentURL == "" || options.LocalFetchAgentToken == "" {
		infrastructure.Logger.Warn("本机抓取助手未配置，商品统计刷新将返回不可用")
	}
	return service, nil
}
