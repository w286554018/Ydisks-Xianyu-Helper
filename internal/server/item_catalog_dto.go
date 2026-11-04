// Package server: 商品目录相关具名响应 DTO——列表、详情、发布与同步接口共用。
package server

// itemListResponse 是本地商品列表接口的具名商品 DTO。
type itemListResponse struct {
	// ID 是本地商品记录主键。
	ID int64 `json:"id"`
	// CookieID 是商品所属账号标识。
	CookieID string `json:"cookie_id"`
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是商品标题。
	ItemTitle string `json:"item_title"`
	// ItemDescription 是商品描述。
	ItemDescription string `json:"item_description"`
	// ItemCategory 是商品分类标识。
	ItemCategory string `json:"item_category"`
	// ItemPrice 是商品价格文本。
	ItemPrice string `json:"item_price"`
	// ItemDetail 是商品详情原始 JSON。
	ItemDetail string `json:"item_detail"`
	// ItemImage 是从详情中提取的商品图片地址。
	ItemImage string `json:"item_image"`
	// IsMultiSpec 表示商品是否有多规格。
	IsMultiSpec bool `json:"is_multi_spec"`
	// MultiQuantityDelivery 表示是否按数量发货。
	MultiQuantityDelivery bool `json:"multi_quantity_delivery"`
	// IsMultiQtyShip 是按数量发货的兼容字段。
	IsMultiQtyShip bool `json:"is_multi_qty_ship"`
	// AIOverride 是商品级 AI 启停覆盖状态。
	AIOverride string `json:"ai_override"`
	// Status 是平台生命周期状态：active 在售 / down 已下架。
	Status string `json:"status"`
}

// itemDetailResponse 是单个本地商品详情接口的具名响应 DTO。
type itemDetailResponse struct {
	// CookieID 是商品所属账号标识。
	CookieID string `json:"cookie_id"`
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是商品标题。
	ItemTitle string `json:"item_title"`
	// ItemDescription 是商品描述。
	ItemDescription string `json:"item_description"`
	// ItemCategory 是商品分类标识。
	ItemCategory string `json:"item_category"`
	// ItemPrice 是商品价格文本。
	ItemPrice string `json:"item_price"`
	// ItemDetail 是商品详情原始 JSON。
	ItemDetail string `json:"item_detail"`
	// IsMultiSpec 表示商品是否有多规格。
	IsMultiSpec bool `json:"is_multi_spec"`
	// MultiQuantityDelivery 表示是否按数量发货。
	MultiQuantityDelivery bool `json:"multi_quantity_delivery"`
}

// itemPublishResponse 是单个商品发布成功接口的具名响应 DTO。
type itemPublishResponse struct {
	// Success 表示商品是否发布成功。
	Success bool `json:"success"`
	// Message 是发布结果说明。
	Message string `json:"message"`
	// ItemID 是新商品的平台标识。
	ItemID string `json:"item_id"`
	// ItemURL 是新商品的平台详情地址。
	ItemURL string `json:"item_url"`
	// ItemImage 是新商品主图地址。
	ItemImage string `json:"item_image"`
	// ItemTitle 是新商品标题。
	ItemTitle string `json:"item_title"`
	// ItemPrice 是新商品价格文本。
	ItemPrice string `json:"item_price"`
	// Quantity 是新商品库存数量。
	Quantity int `json:"quantity"`
	// CategoryID 是新商品分类标识。
	CategoryID string `json:"category_id"`
	// CategoryName 是新商品分类名称。
	CategoryName string `json:"category_name"`
}

// itemSyncResponse 是商品全集同步接口的具名响应 DTO。
type itemSyncResponse struct {
	// Success 表示同步是否完成。
	Success bool `json:"success"`
	// Message 是同步结果说明。
	Message string `json:"message"`
	// TotalCount 是平台返回的商品总数。
	TotalCount int `json:"total_count"`
	// TotalPages 是平台商品总页数。
	TotalPages int `json:"total_pages"`
	// SavedCount 是本地保存的商品数量。
	SavedCount int `json:"saved_count"`
	// DeletedCount 是本地删除标记的商品数量。
	DeletedCount int `json:"deleted_count"`
}

// itemPageSyncResponse 是商品分页同步接口的具名响应 DTO。
type itemPageSyncResponse struct {
	// Success 表示同步是否完成。
	Success bool `json:"success"`
	// Message 是同步结果说明。
	Message string `json:"message"`
	// PageNumber 是当前同步页码。
	PageNumber int `json:"page_number"`
	// PageSize 是当前同步页大小。
	PageSize int `json:"page_size"`
	// CurrentCount 是当前页商品数量。
	CurrentCount int `json:"current_count"`
	// SavedCount 是本地保存的商品数量。
	SavedCount int `json:"saved_count"`
}
