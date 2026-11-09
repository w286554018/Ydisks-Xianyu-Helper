// Package server: 应用端口输入快照——组合根向 HTTP transport 交付的完整 Port 集合。
package server

// ApplicationPortsInput 是组合根向 HTTP transport 交付的完整应用 Port 快照。
// 所有字段必须在进程启动期构造完成，Server 不会在请求期回填或替换它们。
type ApplicationPortsInput struct {
	Orders                      OrdersPort
	OrderRefreshJobs            OrderRefreshJobsPort
	ItemSinglePublish           ItemSinglePublishPort
	ItemBatchPreview            ItemBatchPreviewPort
	ItemBatchManagement         ItemBatchManagementPort
	ItemCategoryRecommendation  ItemCategoryRecommendationPort
	ItemBatchPreviewPersistence ItemBatchPreviewPersistencePort
	ItemBatchLocalPublish       ItemBatchLocalPublishPort
	ItemCopyPublish             ItemCopyPublishPort
	// ItemLifecycle 是商品平台下架/删除用例。
	ItemLifecycle ItemLifecyclePort
	// ItemStats 是我的商品统计刷新与读取用例。
	ItemStats              ItemStatsPort
	ItemSync               ItemSyncPort
	ItemCatalog            ItemCatalogPort
	ItemCatalogMutation    ItemCatalogMutationPort
	ItemAISettings         ItemAISettingsPort
	AccountLogin           AccountLoginPort
	QRLogin                QRLoginPort
	SessionRecovery        SessionRecoveryPort
	PlatformCredentials    PlatformCredentialPort
	Authentication         AuthenticationPort
	LoginAudit             LoginAuditPort
	PasswordLogin          PasswordLoginPort
	AccountDelete          AccountDeletePort
	AccountProfile         AccountProfilePort
	AccountLongLogin       AccountLongLoginPort
	AccountSettings        AccountSettingsPort
	AccountRuntime         AccountRuntimePort
	AccountSummaries       AccountSummaryPort
	AccountTasks           AccountTasksPort
	Chat                   ChatPort
	UncertainNotifications UncertainNotificationsPort
	NotificationChannels   NotificationChannelsPort
	Analytics              AnalyticsPort
	Sourcing               SourcingPort
	// MarketIntel 是市场情报用例端口；nil 表示功能未启用。
	MarketIntel       MarketIntelPort
	AutomationIssues  AutomationIssuesPort
	AutomationAudit   AutomationAuditPort
	AutomationRules   AutomationRulesPort
	DeliveryTemplates DeliveryTemplatesPort
	Cards             CardsPort
	// RuntimeLogs 是管理员运行日志查询用例。
	RuntimeLogs            RuntimeLogsPort
	APIRequestTester       APIRequestTesterPort
	PublishAutomationRules PublishAutomationRulesPort
	DefaultReplies         DefaultRepliesPort
	ItemGreetings          ItemGreetingsPort
	Library                LibraryPort
	Keywords               KeywordsPort
	Settings               SettingsPort
	Admin                  AdminPort
}

// NewApplicationPorts 将组合根已经验证的用例依赖冻结为 Server 私有快照。
func NewApplicationPorts(input ApplicationPortsInput) *ApplicationPorts {
	return &ApplicationPorts{
		orders: input.Orders, orderRefreshJobs: input.OrderRefreshJobs,
		itemSinglePublish: input.ItemSinglePublish, itemBatchPreview: input.ItemBatchPreview,
		itemBatchManagement: input.ItemBatchManagement, itemCategoryRecommendation: input.ItemCategoryRecommendation,
		itemBatchPreviewPersistence: input.ItemBatchPreviewPersistence, itemBatchLocalPublish: input.ItemBatchLocalPublish,
		itemCopyPublish: input.ItemCopyPublish,
		itemLifecycle:   input.ItemLifecycle,
		itemStats:       input.ItemStats,
		itemSync:        input.ItemSync, itemCatalog: input.ItemCatalog, itemCatalogMutation: input.ItemCatalogMutation, itemAISettings: input.ItemAISettings,
		accountLogin: input.AccountLogin, qrLogin: input.QRLogin, sessionRecovery: input.SessionRecovery,
		platformCredentials: input.PlatformCredentials, authentication: input.Authentication, loginAudit: input.LoginAudit,
		passwordLogin: input.PasswordLogin, accountDelete: input.AccountDelete, accountProfile: input.AccountProfile,
		accountLongLogin: input.AccountLongLogin, accountSettings: input.AccountSettings, accountRuntime: input.AccountRuntime,
		accountSummaries: input.AccountSummaries, accountTasks: input.AccountTasks, chat: input.Chat,
		uncertainNotifications: input.UncertainNotifications, notificationChannels: input.NotificationChannels,
		marketIntel: input.MarketIntel,
		analytics:   input.Analytics, automationIssues: input.AutomationIssues, automationAudit: input.AutomationAudit, automationRules: input.AutomationRules,
		sourcing: input.Sourcing,
		cards:    input.Cards, deliveryTemplates: input.DeliveryTemplates, apiRequestTester: input.APIRequestTester, publishAutomationRules: input.PublishAutomationRules, defaultReplies: input.DefaultReplies, itemGreetings: input.ItemGreetings, library: input.Library,
		keywords: input.Keywords, settings: input.Settings, admin: input.Admin, runtimeLogs: input.RuntimeLogs,
	}
}
