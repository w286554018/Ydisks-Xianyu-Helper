// Package server: 市场情报 HTTP 端点——词表 CRUD、扫描触发与概览/趋势/卖家榜查询。
package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	marketintel "xianyu-go/internal/application/marketintel"
)

// marketIntelKeywordResponse 是关注词响应 DTO。
type marketIntelKeywordResponse struct {
	// ID 是关注词主键。
	ID int64 `json:"id"`
	// Keyword 是搜索词文本。
	Keyword string `json:"keyword"`
	// Enabled 表示是否参与每日扫描。
	Enabled bool `json:"enabled"`
	// SortOrder 是展示排序值。
	SortOrder int `json:"sort_order"`
}

// marketIntelKeywordRequest 是关注词创建/更新请求。
type marketIntelKeywordRequest struct {
	// Keyword 是搜索词文本。
	Keyword string `json:"keyword"`
	// Enabled 表示是否参与每日扫描。
	Enabled *bool `json:"enabled"`
}

// marketIntelKeywordListResponse 是关注词列表响应。
type marketIntelKeywordListResponse struct {
	// Items 是关注词集合。
	Items []marketIntelKeywordResponse `json:"items"`
	// MaxEnabled 是启用词上限。
	MaxEnabled int `json:"max_enabled"`
}

// marketIntelDeleteResponse 是关注词删除的统一成功响应。
type marketIntelDeleteResponse struct {
	// Success 表示删除是否完成。
	Success bool `json:"success"`
}

// marketIntelScanResponse 是手动扫描触发响应。
type marketIntelScanResponse struct {
	// Date 是扫描归属日。
	Date string `json:"date"`
	// Scanned 是本次实际扫描的词数。
	Scanned int `json:"scanned"`
	// Skipped 是幂等跳过的词数。
	Skipped int `json:"skipped"`
	// VerdictFailed 是判定失败的词数。
	VerdictFailed int `json:"verdict_failed"`
	// StoppedReason 是中止原因；正常跑完为空。
	StoppedReason string `json:"stopped_reason,omitempty"`
}

// marketIntelAggregateDTO 是聚合行的展示 DTO。
type marketIntelAggregateDTO struct {
	// Keyword 是关注词。
	Keyword string `json:"keyword"`
	// Date 是归属日。
	Date string `json:"date"`
	// TotalCount 是当日商品总数。
	TotalCount int `json:"total_count"`
	// AICount 是 AI 相关条数。
	AICount int `json:"ai_count"`
	// TotalWant 是想要人数总和。
	TotalWant int `json:"total_want"`
	// VerdictStatus 是判定状态。
	VerdictStatus string `json:"verdict_status"`
}

// marketIntelOverviewResponse 是市场情报概览响应：热词涨幅 + 各词最新聚合。
type marketIntelOverviewResponse struct {
	// Momentum 是热词涨幅榜。
	Momentum sourcingHotKeywordMomentumResponse `json:"momentum"`
	// Keywords 是各词最新聚合摘要。
	Keywords []marketIntelAggregateDTO `json:"keywords"`
}

// requireMarketIntel 返回已装配的市场情报服务端口；未装配时写出 503。
func (s *Server) requireMarketIntel(w http.ResponseWriter) (MarketIntelPort, bool) {
	// svc 是已装配的市场情报用例。
	svc := s.marketIntelApplication()
	if svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "市场情报功能未启用")
		return nil, false
	}
	return svc, true
}

// listMarketIntelKeywords 返回全部关注词。
func (s *Server) listMarketIntelKeywords(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// keywords 是词表。
	// keywords 是词表；listErr 是读取错误。
	keywords, listErr := svc.ListKeywords(r.Context())
	if listErr != nil {
		writeErr(w, http.StatusInternalServerError, "读取关注词失败")
		return
	}
	// items 是响应集合。
	items := make([]marketIntelKeywordResponse, 0, len(keywords))
	// keyword 是当前词。
	for _, keyword := range keywords {
		items = append(items, marketIntelKeywordResponse{ID: keyword.ID, Keyword: keyword.Keyword, Enabled: keyword.Enabled, SortOrder: keyword.SortOrder})
	}
	writeJSON(w, http.StatusOK, marketIntelKeywordListResponse{Items: items, MaxEnabled: marketintel.MaxEnabledKeywords})
}

// createMarketIntelKeyword 新增关注词。
func (s *Server) createMarketIntelKeyword(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// req 是请求体。
	var req marketIntelKeywordRequest
	// decodeErr 是请求体解析错误。
	decodeErr := decodeJSON(r, &req)
	if decodeErr != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// enabled 是启用状态，缺省 true。
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	// created 是创建结果。
	// created 是创建结果；createErr 是创建错误。
	created, createErr := svc.CreateKeyword(r.Context(), req.Keyword, enabled)
	if createErr != nil {
		s.writeMarketIntelKeywordError(w, createErr)
		return
	}
	writeJSON(w, http.StatusCreated, marketIntelKeywordResponse{ID: created.ID, Keyword: created.Keyword, Enabled: created.Enabled, SortOrder: created.SortOrder})
}

// updateMarketIntelKeyword 更新关注词。
func (s *Server) updateMarketIntelKeyword(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// id 是路径参数。
	id, parseErr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, "关注词 ID 格式错误")
		return
	}
	// req 是请求体。
	var req marketIntelKeywordRequest
	// decodeErr 是请求体解析错误。
	decodeErr := decodeJSON(r, &req)
	if decodeErr != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// enabled 是启用状态，缺省 true。
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	// updateErr 是更新错误。
	if updateErr := svc.UpdateKeyword(r.Context(), id, req.Keyword, enabled); updateErr != nil {
		s.writeMarketIntelKeywordError(w, updateErr)
		return
	}
	writeJSON(w, http.StatusOK, marketIntelKeywordResponse{ID: id, Keyword: strings.TrimSpace(req.Keyword), Enabled: enabled})
}

// deleteMarketIntelKeyword 删除关注词。
func (s *Server) deleteMarketIntelKeyword(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// id 是路径参数。
	id, parseErr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, "关注词 ID 格式错误")
		return
	}
	// deleteErr 是删除错误。
	if deleteErr := svc.DeleteKeyword(r.Context(), id); deleteErr != nil {
		s.writeMarketIntelKeywordError(w, deleteErr)
		return
	}
	writeJSON(w, http.StatusOK, marketIntelDeleteResponse{Success: true})
}

// writeMarketIntelKeywordError 把词表错误映射为统一错误信封。
func (s *Server) writeMarketIntelKeywordError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, marketintel.ErrKeywordLimitReached):
		writeErr(w, http.StatusBadRequest, marketintel.ErrKeywordLimitReached.Error())
	case errors.Is(err, marketintel.ErrKeywordEmpty):
		writeErr(w, http.StatusBadRequest, marketintel.ErrKeywordEmpty.Error())
	case errors.Is(err, marketintel.ErrKeywordDuplicate):
		writeErr(w, http.StatusBadRequest, marketintel.ErrKeywordDuplicate.Error())
	case errors.Is(err, marketintel.ErrKeywordNotFound):
		writeErr(w, http.StatusNotFound, marketintel.ErrKeywordNotFound.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "关注词操作失败")
	}
}

// scanMarketIntel 手动触发一次扫描（幂等：当日已扫描的词跳过）。
func (s *Server) scanMarketIntel(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// result 是扫描结果。
	result, err := svc.RunDailyScan(r.Context())
	if err != nil {
		// 风控/会话失效返回 409，其余 500。
		if errors.Is(err, marketintel.ErrRiskControl) || errors.Is(err, marketintel.ErrSessionExpired) {
			writeErr(w, http.StatusConflict, result.StoppedReason)
			return
		}
		writeErr(w, http.StatusInternalServerError, "执行市场扫描失败")
		return
	}
	writeJSON(w, http.StatusOK, marketIntelScanResponse{
		Date: result.Date, Scanned: result.Scanned, Skipped: result.Skipped,
		VerdictFailed: result.VerdictFailed, StoppedReason: result.StoppedReason,
	})
}

// marketIntelOverview 返回市场情报概览：热词涨幅 + 各词最新聚合。
func (s *Server) marketIntelOverview(w http.ResponseWriter, r *http.Request) {
	// sourcing 是选品参考用例（热词涨幅复用）。
	sourcing, ok := s.requireSourcing(w)
	if !ok {
		return
	}
	// svc 是市场情报用例。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// momentum 是热词涨幅榜。
	momentum, err := sourcing.Momentum(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "计算热词涨幅失败")
		return
	}
	// momentumItems 是涨幅词条。
	momentumItems := make([]sourcingKeywordMomentumDTO, 0, len(momentum.Items))
	// entry 是当前涨幅词条。
	for _, entry := range momentum.Items {
		momentumItems = append(momentumItems, sourcingKeywordMomentumDTO{
			Keyword: entry.Keyword, Status: entry.Status, Position: entry.Position,
			PreviousPosition: entry.PreviousPosition, DaysOnList: entry.DaysOnList,
		})
	}
	// aggregates 是各词最新聚合。
	aggregates, err := svc.LatestAggregates(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取市场聚合失败")
		return
	}
	// keywordItems 是聚合 DTO 集合。
	keywordItems := make([]marketIntelAggregateDTO, 0, len(aggregates))
	// aggregate 是当前聚合行。
	for _, aggregate := range aggregates {
		keywordItems = append(keywordItems, marketIntelAggregateDTO{
			Keyword: aggregate.Keyword, Date: aggregate.Date, TotalCount: aggregate.TotalCount,
			AICount: aggregate.AICount, TotalWant: aggregate.TotalWant, VerdictStatus: aggregate.VerdictStatus,
		})
	}
	writeJSON(w, http.StatusOK, marketIntelOverviewResponse{
		Momentum: sourcingHotKeywordMomentumResponse{WindowDays: momentum.WindowDays, LatestDate: momentum.LatestDate, Items: momentumItems},
		Keywords: keywordItems,
	})
}

// marketIntelKeywordTrend 返回单词趋势曲线。
func (s *Server) marketIntelKeywordTrend(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// id 是路径参数。
	id, parseErr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, "关注词 ID 格式错误")
		return
	}
	// days 是查询窗口天数，默认 30，上限 90。
	days := 30
	// raw 是 days 查询参数原文。
	if raw := r.URL.Query().Get("days"); raw != "" {
		// parsed 是解析后的天数；convErr 是解析错误。
		if parsed, convErr := strconv.Atoi(raw); convErr == nil && parsed > 0 && parsed <= 90 {
			days = parsed
		}
	}
	// trend 是趋势数据。
	trend, err := svc.KeywordTrend(r.Context(), id, days)
	if err != nil {
		if errors.Is(err, marketintel.ErrKeywordNotFound) {
			writeErr(w, http.StatusNotFound, marketintel.ErrKeywordNotFound.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "读取趋势失败")
		return
	}
	writeJSON(w, http.StatusOK, trend)
}

// marketIntelSellers 返回竞品卖家榜。
func (s *Server) marketIntelSellers(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// days 是查询窗口天数，默认 30，上限 90。
	days := 30
	// raw 是 days 查询参数原文。
	if raw := r.URL.Query().Get("days"); raw != "" {
		// parsed 是解析后的天数；convErr 是解析错误。
		if parsed, convErr := strconv.Atoi(raw); convErr == nil && parsed > 0 && parsed <= 90 {
			days = parsed
		}
	}
	// sellers 是卖家榜。
	sellers, err := svc.SellerBoard(r.Context(), days)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取卖家榜失败")
		return
	}
	writeJSON(w, http.StatusOK, sellers)
}
