package server

import (
	"errors"
	"net/http"

	sourcingapp "xianyu-go/internal/application/sourcing"
	"xianyu-go/internal/auth"
)

// sourcingApplication 返回当前 Server 绑定的选品参考应用服务；未装配时返回 nil。
func (s *Server) sourcingApplication() SourcingPort {
	return s.applicationServiceSet().sourcing
}

// requireSourcing 返回已装配的选品参考服务；未装配时写出 503 并返回 false，避免空指针。
func (s *Server) requireSourcing(w http.ResponseWriter) (SourcingPort, bool) {
	// port 是组合根注入的选品参考用例。
	port := s.sourcingApplication()
	if port == nil {
		writeErr(w, http.StatusServiceUnavailable, "选品参考功能未启用")
		return nil, false
	}
	return port, true
}

// sourcingHotKeywordResponse 是热词查询的响应 DTO。
type sourcingHotKeywordResponse struct {
	// Date 是快照归属日。
	Date string `json:"date"`
	// Keywords 是热词词条。
	Keywords []string `json:"keywords"`
	// Status 是采集状态，空值表示当日尚未采集。
	Status string `json:"status"`
	// ErrorMessage 是采集失败原因摘要。
	ErrorMessage string `json:"error_message"`
}

// sourcingSearchRequest 是关键词查货的请求 DTO。
type sourcingSearchRequest struct {
	// CookieID 是执行查询使用的账号标识。
	CookieID string `json:"cookie_id"`
	// Keyword 是查询关键词。
	Keyword string `json:"keyword"`
}

// sourcingSearchItemResponse 是查货结果中的单条商品。
type sourcingSearchItemResponse struct {
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// Price 是价格文本。
	Price string `json:"price"`
	// WantCount 是"多少人想要"文本。
	WantCount string `json:"want_count"`
	// Location 是卖家地区文本。
	Location string `json:"location"`
	// DetailURL 是商品详情页地址。
	DetailURL string `json:"detail_url"`
}

// sourcingSearchResponse 是关键词查货的响应 DTO。
type sourcingSearchResponse struct {
	// Keyword 是本次查询使用的关键词。
	Keyword string `json:"keyword"`
	// Items 是查货结果。
	Items []sourcingSearchItemResponse `json:"items"`
}

// sourcingRankingEntryResponse 是畅销榜单条商品的响应 DTO。
type sourcingRankingEntryResponse struct {
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是本地商品标题。
	ItemTitle string `json:"item_title"`
	// SoldQuantity 是窗口内售出数量。
	SoldQuantity int `json:"sold_quantity"`
	// Revenue 是窗口内营收合计。
	Revenue float64 `json:"revenue"`
	// OrderCount 是窗口内订单数。
	OrderCount int `json:"order_count"`
}

// sourcingShopRankingResponse 是畅销榜的响应 DTO。
type sourcingShopRankingResponse struct {
	// Date 是快照归属日。
	Date string `json:"date"`
	// Window 是统计窗口标识。
	Window string `json:"window"`
	// Status 是快照状态。
	Status string `json:"status"`
	// Entries 是排行条目。
	Entries []sourcingRankingEntryResponse `json:"entries"`
}

// sourcingCopywritingRequest 是 AI 文案生成的请求 DTO。
type sourcingCopywritingRequest struct {
	// Mode 是文案模式：listing 或 analysis。
	Mode string `json:"mode"`
	// Topic 是品类关键词或商品标题。
	Topic string `json:"topic"`
	// Reference 是可选参考信息。
	Reference string `json:"reference"`
}

// sourcingCopywritingResponse 是 AI 文案生成的响应 DTO。
type sourcingCopywritingResponse struct {
	// Mode 是本次使用的文案模式。
	Mode string `json:"mode"`
	// Content 是生成的正文内容。
	Content string `json:"content"`
}

// sourcingHotKeywords 返回当日平台热词快照。
func (s *Server) sourcingHotKeywords(w http.ResponseWriter, r *http.Request) {
	// sourcing 是已装配的选品参考用例。
	sourcing, ok := s.requireSourcing(w)
	if !ok {
		return
	}
	// view、err 是热词快照读取结果。
	view, err := sourcing.HotKeywords(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取平台热词失败")
		return
	}
	// items 是响应词条；nil 转为空数组，避免前端拿到 null。
	items := view.Keywords
	if items == nil {
		items = []string{}
	}
	writeJSON(w, http.StatusOK, sourcingHotKeywordResponse{
		Date: view.Date, Keywords: items, Status: view.Status, ErrorMessage: view.ErrorMessage,
	})
}

// sourcingRefreshHotKeywords 手动触发一次热词采集。
func (s *Server) sourcingRefreshHotKeywords(w http.ResponseWriter, r *http.Request) {
	// sourcing 是已装配的选品参考用例。
	sourcing, ok := s.requireSourcing(w)
	if !ok {
		return
	}
	// err 是采集结果；失败时按领域错误映射状态码，不重试。
	if err := sourcing.RefreshHotKeywords(r.Context()); err != nil {
		writeSourcingError(w, err, "采集平台热词失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// sourcingSearch 执行单次关键词查货。
func (s *Server) sourcingSearch(w http.ResponseWriter, r *http.Request) {
	// sourcing 是已装配的选品参考用例。
	sourcing, ok := s.requireSourcing(w)
	if !ok {
		return
	}
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// req 是解码后的查货请求。
	var req sourcingSearchRequest
	// err 表示请求体不是合法的查货 JSON。
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// items、err 是查货结果与应用层错误。
	items, err := sourcing.SearchItems(r.Context(), sess.UserID, req.CookieID, req.Keyword)
	if errors.Is(err, sourcingapp.ErrNoResult) {
		// 没取到商品是正常业务结果，返回空列表而不是错误。
		writeJSON(w, http.StatusOK, sourcingSearchResponse{Keyword: req.Keyword, Items: []sourcingSearchItemResponse{}})
		return
	}
	if err != nil {
		writeSourcingError(w, err, "查询商品失败")
		return
	}
	// out 是响应条目集合。
	out := make([]sourcingSearchItemResponse, 0, len(items))
	// item 是当前遍历到的商品摘要。
	for _, item := range items {
		out = append(out, sourcingSearchItemResponse{
			ItemID: item.ItemID, Title: item.Title, Price: item.Price,
			WantCount: item.WantCount, Location: item.Location, DetailURL: item.DetailURL,
		})
	}
	writeJSON(w, http.StatusOK, sourcingSearchResponse{Keyword: req.Keyword, Items: out})
}

// sourcingShopRanking 按当前用户实时返回本店畅销榜。
func (s *Server) sourcingShopRanking(w http.ResponseWriter, r *http.Request) {
	// sourcing 是已装配的选品参考用例。
	sourcing, ok := s.requireSourcing(w)
	if !ok {
		return
	}
	// sess 是当前请求的认证会话，畅销榜只统计该用户名下账号。
	sess := auth.SessionFromContext(r.Context())
	// view、err 是畅销榜读取结果。
	view, err := sourcing.ShopRanking(r.Context(), sess.UserID, r.URL.Query().Get("window"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取本店畅销榜失败")
		return
	}
	// out 是响应条目集合，nil 转为空数组。
	out := make([]sourcingRankingEntryResponse, 0, len(view.Entries))
	// entry 是当前遍历到的排行条目。
	for _, entry := range view.Entries {
		out = append(out, sourcingRankingEntryResponse{
			ItemID: entry.ItemID, ItemTitle: entry.ItemTitle, SoldQuantity: entry.SoldQuantity,
			Revenue: entry.Revenue, OrderCount: entry.OrderCount,
		})
	}
	writeJSON(w, http.StatusOK, sourcingShopRankingResponse{
		Date: view.Date, Window: view.Window, Status: view.Status, Entries: out,
	})
}

// sourcingCopywriting 生成 AI 文案。
func (s *Server) sourcingCopywriting(w http.ResponseWriter, r *http.Request) {
	// sourcing 是已装配的选品参考用例。
	sourcing, ok := s.requireSourcing(w)
	if !ok {
		return
	}
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// req 是解码后的文案请求。
	var req sourcingCopywritingRequest
	// err 表示请求体不是合法的文案生成 JSON。
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// result、err 是文案生成结果与应用层错误。
	result, err := sourcing.GenerateCopywriting(r.Context(), sess.UserID, sourcingapp.CopywritingRequest{
		Mode: req.Mode, Topic: req.Topic, Reference: req.Reference,
	})
	if err != nil {
		writeSourcingError(w, err, "生成文案失败")
		return
	}
	writeJSON(w, http.StatusOK, sourcingCopywritingResponse{Mode: result.Mode, Content: result.Content})
}

// writeSourcingError 把选品参考领域错误映射为稳定的 HTTP 状态码与提示。
// 风控与会话失败不回退为重试，必须由用户人工处理。
func writeSourcingError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, sourcingapp.ErrSearchCooling):
		writeErr(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, sourcingapp.ErrAccountNotOwned):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, sourcingapp.ErrRiskControl):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, sourcingapp.ErrSessionExpired):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, sourcingapp.ErrAIConfigMissing):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, sourcingapp.ErrBrowserUnavailable):
		writeErr(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, sourcingapp.ErrInvalidCopywritingMode), errors.Is(err, sourcingapp.ErrEmptyKeyword):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, sourcingapp.ErrHotKeywordUnavailable):
		writeErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, fallback)
	}
}

// sourcingHotKeywordMomentumResponse 是热词涨幅榜的响应 DTO。
type sourcingHotKeywordMomentumResponse struct {
	// WindowDays 是对比窗口天数。
	WindowDays int `json:"window_days"`
	// LatestDate 是最新快照归属日；无快照时为空。
	LatestDate string `json:"latest_date"`
	// Items 是按「新冒头优先、再按名次」排序的涨幅词条。
	Items []sourcingKeywordMomentumDTO `json:"items"`
}

// sourcingKeywordMomentumDTO 是单个热词的涨幅词条。
type sourcingKeywordMomentumDTO struct {
	// Keyword 是热词文本。
	Keyword string `json:"keyword"`
	// Status 是涨幅状态：new/rising/steady/falling。
	Status string `json:"status"`
	// Position 是该词在最新快照中的名次；已跌出时为 0。
	Position int `json:"position"`
	// PreviousPosition 是该词在前一日快照中的名次；此前不在榜时为 0。
	PreviousPosition int `json:"previous_position"`
	// DaysOnList 是该词在窗口内出现的总天数。
	DaysOnList int `json:"days_on_list"`
}

// sourcingHotKeywordMomentum 返回热词涨幅榜。
func (s *Server) sourcingHotKeywordMomentum(w http.ResponseWriter, r *http.Request) {
	// sourcing 是已装配的选品参考用例。
	sourcing, ok := s.requireSourcing(w)
	if !ok {
		return
	}
	// summary 是涨幅榜计算结果。
	summary, err := sourcing.Momentum(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "计算热词涨幅失败")
		return
	}
	// items 是响应词条；nil 转为空数组，避免前端拿到 null。
	items := make([]sourcingKeywordMomentumDTO, 0, len(summary.Items))
	// entry 是当前遍历的涨幅词条。
	for _, entry := range summary.Items {
		items = append(items, sourcingKeywordMomentumDTO{
			Keyword: entry.Keyword, Status: entry.Status, Position: entry.Position,
			PreviousPosition: entry.PreviousPosition, DaysOnList: entry.DaysOnList,
		})
	}
	writeJSON(w, http.StatusOK, sourcingHotKeywordMomentumResponse{
		WindowDays: summary.WindowDays, LatestDate: summary.LatestDate, Items: items,
	})
}
