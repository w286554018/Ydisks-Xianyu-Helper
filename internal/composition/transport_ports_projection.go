// Package composition: HTTP transport 只读应用服务投影。
package composition

// TransportPorts 将 services 中已构造的服务完整投影为 HTTP 层只读端口；可并发读取，nil 接收者返回空集合，调用方不得在运行期替换字段。
func (services *Services) TransportPorts() TransportPorts {
	if services == nil {
		return TransportPorts{}
	}
	return TransportPorts{
		Orders: services.orders, OrderRefreshJobs: services.orderRefreshJobs, ItemSinglePublish: services.itemSinglePublish,
		ItemBatchPreview: services.itemBatchPreview, ItemBatchManagement: services.itemBatchManagement,
		ItemCategoryRecommendation: services.itemCategoryRecommendation, ItemBatchPreviewPersistence: services.itemBatchPreviewPersistence,
		ItemBatchLocalPublish: services.itemBatchLocalPublish, ItemSync: services.itemSync, ItemCatalog: services.itemCatalog,
		ItemCatalogMutation: services.itemCatalogMutation, ItemAISettings: services.itemAISettings, AccountLogin: services.accountLogin, QRLogin: services.qrLogin,
		ItemCopyPublish: services.itemCopyPublish, ItemLifecycle: services.itemLifecycle, ItemStats: services.itemStats,
		PlatformCredentials: services.platformCredentials, Authentication: services.authentication, LoginAudit: services.loginAudit,
		PasswordLogin: services.passwordLogin, AccountDelete: services.accountDelete, AccountProfile: services.accountProfile,
		AccountLongLogin: services.accountLongLogin, AccountSettings: services.accountSettings, AccountRuntime: services.accountRuntime,
		AccountSummaries: services.accountSummaries, AccountTasks: services.accountTasks, Chat: services.chat,
		UncertainNotifications: services.uncertainNotifications, NotificationChannels: services.notificationChannels,
		Analytics: services.analytics, Sourcing: services.sourcing, MarketIntel: services.marketIntel, MarketIntelCarry: services.marketIntelCarry, AutomationIssues: services.automationIssues, AutomationAudit: services.automationAudit, AutomationRules: services.automationRules, DeliveryTemplates: services.deliveryTemplates,
		Cards: services.cards, APICardTester: services.apiCardTester, PublishAutomationRules: services.publishAutomationRules, DefaultReplies: services.defaultReplies, ItemGreetings: services.itemGreetings, Library: services.library,
		Keywords: services.keywords, Settings: services.settings, Admin: services.admin, RuntimeLogs: services.runtimeLogs,
	}
}
