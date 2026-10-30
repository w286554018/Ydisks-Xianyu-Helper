package runtime

import (
	"context"
	"fmt"

	"xianyu-go/internal/adapter"
	"xianyu-go/internal/application/lifecycle"
	sourcingapp "xianyu-go/internal/application/sourcing"
	"xianyu-go/internal/browser"
)

// buildSourcing 装配选品参考应用服务，并在浏览器启用时登记每日热词调度组件。
// 调度组件登记在浏览器之后，因此启动晚于浏览器、关闭早于浏览器。
func buildSourcing(infrastructure RuntimeInfrastructure, browserManager *browser.Manager, coordinator *lifecycle.Coordinator) (*sourcingapp.Service, error) {
	// sourcingBrowser 是选品参考的浏览器采集能力；禁用浏览器时保持 nil 接口，使热词与查货明确降级。
	var sourcingBrowser sourcingapp.BrowserGateway
	if browserManager != nil {
		sourcingBrowser = adapter.NewSourcingBrowserGateway(browserManager)
	}
	// service 是选品参考应用服务：热词每日游客态采集、关键词手动单次查货、本店畅销榜实时计算与 AI 文案。
	service := sourcingapp.NewService(
		adapter.NewSourcingRepository(infrastructure.Store), sourcingBrowser,
		adapter.NewSourcingCredentialReader(infrastructure.Store), adapter.NewSourcingChatClient(infrastructure.Store),
	)
	if browserManager == nil {
		return service, nil
	}
	// scheduler 每日北京时间 8 点后采集一次热词；当天已成功采集时跳过，避免重启触发平台请求。
	scheduler := sourcingapp.NewScheduler(service, infrastructure.Logger)
	// addErr 是热词调度组件登记失败原因。
	if addErr := coordinator.Add(lifecycle.NamedComponent{Name: "sourcing-scheduler", Component: lifecycle.FuncComponent{
		StartFunc: func(ctx context.Context) error { scheduler.Start(ctx); return nil },
		CloseFunc: func(context.Context) error { scheduler.Stop(); return nil },
	}}); addErr != nil {
		return nil, fmt.Errorf("登记选品参考调度组件失败: %w", addErr)
	}
	return service, nil
}
