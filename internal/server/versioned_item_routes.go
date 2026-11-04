package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedItems 挂载商品列表、详情、发布、更新和删除的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedItems(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Get("/api/v1/items", s.listItems)
		r.Get("/api/v1/items/cookie/{cookie_id}", s.listItemsByCookie)
		r.Get("/api/v1/items/{cookie_id}/{item_id}", s.getItem)
		r.Get("/api/v1/items/{cookie_id}/{item_id}/ai-settings", s.getItemAISettings)
		r.Post("/api/v1/items/{cookie_id}", s.createItem)
		r.Post("/api/v1/items/publish", s.publishItem)
		r.Post("/api/v1/items/copy-preview", s.previewItemCopy)
		r.Post("/api/v1/items/copy-tasks", s.createItemCopyTask)
		r.Put("/api/v1/items/{cookie_id}/{item_id}/ai-settings", s.updateItemAISettings)
		r.Put("/api/v1/items/{cookie_id}/{item_id}", s.updateItem)
		r.Delete("/api/v1/items/{cookie_id}/{item_id}", s.deleteItem)
		r.Post("/api/v1/items/lifecycle", s.createItemLifecycleBatch)
		r.Get("/api/v1/items/lifecycle-batches", s.listItemLifecycleBatches)
		r.Get("/api/v1/items/lifecycle-batches/{batch_id}", s.getItemLifecycleBatch)
		r.Post("/api/v1/items/lifecycle-batches/{batch_id}/retry-failed", s.retryItemLifecycleBatch)
		r.Post("/api/v1/items/lifecycle-batches/{batch_id}/cancel", s.cancelItemLifecycleBatch)
		r.Put("/api/v1/items/{cookie_id}/{item_id}/multi-spec", s.setItemMultiSpec)
		r.Put("/api/v1/items/{cookie_id}/{item_id}/multi-quantity-delivery", s.setItemMultiQuantity)
	})
}
