package runtime

import (
	"fmt"
	"log/slog"

	"xianyu-go/internal/adapter"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/server"
)

// servicePorts 保存由系统级适配器派生的窄端口。
type servicePorts struct {
	// reconciliationRecovery 是订单补偿扫描的应用层生命周期协调器。
	reconciliationRecovery *orderapp.ReconciliationRecoveryCoordinator
	// databaseHealth 是 HTTP 健康检查使用的窄数据库探测 Port。
	databaseHealth server.DatabaseHealthPort
}

// buildServicePorts 从系统级适配器构造订单补偿协调器与数据库健康检查端口。
// 两者都是 HTTP 装配的必需依赖，任一失败都必须阻止继续装配，而不是留下半可用端口。
func buildServicePorts(systemDependencies *adapter.SystemDependencies, logger *slog.Logger) (servicePorts, error) {
	// orderReconciliationRecovery、reconciliationErr 分别是订单补偿协调器及其构造错误。
	orderReconciliationRecovery, reconciliationErr := orderapp.NewReconciliationRecoveryCoordinator(systemDependencies.NewReconciliationService(logger))
	if reconciliationErr != nil {
		return servicePorts{}, fmt.Errorf("构造订单补偿恢复协调器失败: %w", reconciliationErr)
	}
	// databaseHealth 是 HTTP 健康检查使用的窄数据库探测 Port。
	databaseHealth := systemDependencies.NewDatabaseHealth()
	if databaseHealth == nil {
		return servicePorts{}, fmt.Errorf("构造数据库健康检查端口失败")
	}
	return servicePorts{reconciliationRecovery: orderReconciliationRecovery, databaseHealth: databaseHealth}, nil
}
