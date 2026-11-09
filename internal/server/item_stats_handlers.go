// Package server: 我的商品统计（浏览量/想要量）刷新端点与 DTO。
package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/logsafe"
)

// itemStatsResponse 是单个商品的统计响应；商品尚未抓取过统计时列表接口返回 null。
type itemStatsResponse struct {
	// CookieID 是统计所属账号。
	CookieID string `json:"cookie_id"`
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// BrowseCnt 是浏览量。
	BrowseCnt int64 `json:"browse_cnt"`
	// WantCnt 是想要人数。
	WantCnt int64 `json:"want_cnt"`
	// CollectCnt 是收藏数。
	CollectCnt int64 `json:"collect_cnt"`
	// SoldCnt 是已售数。
	SoldCnt int64 `json:"sold_cnt"`
	// FetchedAt 是最近一次成功抓取时间（RFC3339）；从未抓取时为空串。
	FetchedAt string `json:"fetched_at"`
	// Fresh 表示数据是否仍在缓存有效期内。
	Fresh bool `json:"fresh"`
}

// itemStatsBatchRequest 是单轮批量刷新请求体。
type itemStatsBatchRequest struct {
	// CookieID 是商品归属账号。
	CookieID string `json:"cookie_id"`
	// ItemIDs 是本轮待刷新的商品集合；服务端会去重并截断到硬上限。
	ItemIDs []string `json:"item_ids"`
	// Limit 是调用方期望的单轮上限；超过硬上限时被服务端截断。
	Limit int `json:"limit"`
}

// itemStatsBatchResponse 是单轮批量刷新响应。
type itemStatsBatchResponse struct {
	// Refreshed 是本次成功刷新的统计，按抓取顺序排列。
	Refreshed []itemStatsResponse `json:"refreshed"`
	// Skipped 是因数据仍新鲜而跳过的数量。
	Skipped int `json:"skipped"`
	// Remaining 是被上限截断、留待下次刷新的数量。
	Remaining int `json:"remaining"`
	// Stopped 表示是否因平台风控提前停止。
	Stopped bool `json:"stopped"`
	// StopReason 是停止原因；未停止时为空。
	StopReason string `json:"stop_reason,omitempty"`
}

// refreshItemStats 强制刷新单个商品的统计；用户点击刷新即明确要求最新值，因此忽略缓存。
func (s *Server) refreshItemStats(w http.ResponseWriter, r *http.Request) {
	// svc 是商品统计用例；未装配返回 503。
	svc := s.itemStatsApplication()
	if svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "商品统计功能未启用")
		return
	}
	// sess 是当前登录会话。
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// cookieID、itemID 是路径参数，任一为空都无法定位统计。
	cookieID, itemID := strings.TrimSpace(chi.URLParam(r, "cookie_id")), strings.TrimSpace(chi.URLParam(r, "item_id"))
	if cookieID == "" || itemID == "" {
		writeErr(w, http.StatusBadRequest, "缺少账号或商品标识")
		return
	}
	// stats、err 是本次刷新结果与错误。
	stats, err := svc.RefreshItemStats(r.Context(), sess.UserID, cookieID, itemID)
	if err != nil {
		s.writeItemStatsError(w, itemID, err)
		return
	}
	writeJSON(w, http.StatusOK, itemStatsDTO(stats))
}

// refreshItemStatsBatch 执行一轮限量批量刷新；单轮上限与风控即停策略由应用层强制。
func (s *Server) refreshItemStatsBatch(w http.ResponseWriter, r *http.Request) {
	// svc 是商品统计用例；未装配返回 503。
	svc := s.itemStatsApplication()
	if svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "商品统计功能未启用")
		return
	}
	// sess 是当前登录会话。
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// req 是解析后的请求体。
	var req itemStatsBatchRequest
	if // decodeErr 是请求体解析失败原因。
	decodeErr := decodeJSON(r, &req); decodeErr != nil {
		writeErr(w, http.StatusBadRequest, decodeErr.Error())
		return
	}
	// cookieID 是去空白后的账号标识；为空时无法定位商品归属。
	cookieID := strings.TrimSpace(req.CookieID)
	if cookieID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 cookie_id 参数")
		return
	}
	// result、err 是批量刷新结果与错误。
	result, err := svc.RefreshStatsBatch(r.Context(), sess.UserID, cookieID, req.ItemIDs, req.Limit)
	if err != nil {
		s.writeItemStatsError(w, "", err)
		return
	}
	// refreshed 是响应中的刷新结果集合。
	refreshed := make([]itemStatsResponse, 0, len(result.Refreshed))
	// stats 是本次成功刷新的统计。
	for _, stats := range result.Refreshed {
		refreshed = append(refreshed, itemStatsDTO(stats))
	}
	writeJSON(w, http.StatusOK, itemStatsBatchResponse{
		Refreshed: refreshed, Skipped: result.Skipped, Remaining: result.Remaining,
		Stopped: result.Stopped, StopReason: result.StopReason,
	})
}

// itemStatsDTO 把应用层统计转换为响应 DTO；时间统一用 RFC3339，零值时间序列化为空串。
func itemStatsDTO(stats itemapp.ItemStats) itemStatsResponse {
	// fetchedAt 是 RFC3339 格式的抓取时间文本；零值时间必须为空串而不是 0001-01-01。
	fetchedAt := ""
	if !stats.FetchedAt.IsZero() {
		fetchedAt = stats.FetchedAt.UTC().Format(time.RFC3339)
	}
	return itemStatsResponse{
		CookieID: stats.CookieID, ItemID: stats.ItemID,
		BrowseCnt: stats.BrowseCnt, WantCnt: stats.WantCnt,
		CollectCnt: stats.CollectCnt, SoldCnt: stats.SoldCnt,
		FetchedAt: fetchedAt, Fresh: stats.Fresh,
	}
}

// writeItemStatsError 把商品统计用例错误映射为统一错误信封；未归类错误留下可诊断日志。
func (s *Server) writeItemStatsError(w http.ResponseWriter, itemID string, err error) {
	switch {
	case errors.Is(err, itemapp.ErrItemStatsNotOwned):
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
	case errors.Is(err, itemapp.ErrItemStatsNoItems):
		writeErr(w, http.StatusBadRequest, "请至少选择一个商品")
	case errors.Is(err, itemapp.ErrItemStatsUnavailable):
		// 助手不可用必须给出可操作提示；此处刻意不回退服务器侧浏览器抓取商品详情：
		// 该路径已被平台确定性判罚，回退只会浪费时间并加重风控。
		writeErr(w, http.StatusServiceUnavailable, "本机抓取助手不可用：请确认助手已启动、反向隧道已连接")
	case errors.Is(err, itemapp.ErrItemStatsUnauthorized):
		writeErr(w, http.StatusServiceUnavailable, "抓取助手拒绝了本次调用：请确认共享令牌一致")
	case errors.Is(err, itemapp.ErrItemStatsNotLoggedIn):
		writeErr(w, http.StatusServiceUnavailable, "抓取助手尚未登录闲鱼：请先在助手浏览器中登录")
	case errors.Is(err, itemapp.ErrItemStatsRiskControl):
		writeErr(w, http.StatusConflict, "闲鱼要求人机验证：请在助手浏览器中完成验证后重试")
	default:
		// 未归类错误必须留下可诊断日志，否则线上故障不可见；logsafe.Error 不含任何凭据明文。
		s.Logger.Warn("商品统计刷新失败", "item_id", itemID, "err", logsafe.Error(err))
		writeErr(w, http.StatusInternalServerError, "刷新商品统计失败")
	}
}

// itemStatsForList 读取商品列表附带的统计；统计功能未启用或查询失败时降级为 nil 并留下告警。
// 列表是主流程，统计只是附加展示：这里刻意不因为统计故障让整个商品列表不可用。
func (s *Server) itemStatsForList(r *http.Request, items []itemapp.CatalogItem) map[string]itemapp.ItemStats {
	// svc 是商品统计用例；未装配或列表为空时直接返回 nil。
	svc := s.itemStatsApplication()
	if svc == nil || len(items) == 0 {
		return nil
	}
	// refs 是本次需要读取统计的「账号 + 商品」引用集合。
	refs := make([]itemapp.ItemStatsRef, 0, len(items))
	// it 是当前遍历到的商品。
	for _, it := range items {
		refs = append(refs, itemapp.ItemStatsRef{CookieID: it.CookieID, ItemID: it.ItemID})
	}
	// statsByKey、err 是统计查询结果与错误。
	statsByKey, err := svc.ListItemStatsForItems(r.Context(), refs)
	if err != nil {
		s.Logger.Warn("商品列表附带统计失败，本次降级为不展示统计", "err", logsafe.Error(err))
		return nil
	}
	return statsByKey
}
