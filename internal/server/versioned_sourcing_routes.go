package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedSourcingRoutes 挂载选品参考的 `/api/v1` 入口。
// 全部端点要求登录；查询与文案生成只作用于当前用户自己的账号与数据。
func (s *Server) mountVersionedSourcingRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Get("/api/v1/sourcing/hot-keywords", s.sourcingHotKeywords)
		r.Post("/api/v1/sourcing/hot-keywords/refresh", s.sourcingRefreshHotKeywords)
		r.Post("/api/v1/sourcing/search", s.sourcingSearch)
		r.Get("/api/v1/sourcing/shop-ranking", s.sourcingShopRanking)
		r.Post("/api/v1/sourcing/copywriting", s.sourcingCopywriting)
	})
}
