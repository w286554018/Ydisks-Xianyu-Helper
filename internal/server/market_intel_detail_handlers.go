// Package server: 竞品详情查看、AI 改写/分析与一键搬运的 HTTP 端点。
package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	marketintel "xianyu-go/internal/application/marketintel"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/logsafe"
)

// marketIntelDetailResponse 是竞品详情响应 DTO。
type marketIntelDetailResponse struct {
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// Price 是价格文本。
	Price string `json:"price"`
	// DescText 是商品描述纯文本。
	DescText string `json:"desc_text"`
	// Images 是商品图片 URL 列表。
	Images []string `json:"images"`
	// SellerNickname 是卖家昵称。
	SellerNickname string `json:"seller_nickname"`
	// SellerID 是卖家 userId。
	SellerID string `json:"seller_id"`
	// Cached 表示是否来自缓存。
	Cached bool `json:"cached"`
}

// marketIntelRewriteResponse 是 AI 改写响应 DTO。
type marketIntelRewriteResponse struct {
	// Title 是改写后的标题。
	Title string `json:"title"`
	// Desc 是改写后的描述。
	Desc string `json:"desc"`
}

// marketIntelAnalyzeResponse 是 AI 卖点分析响应 DTO。
type marketIntelAnalyzeResponse struct {
	// Analysis 是分析文本。
	Analysis string `json:"analysis"`
}

// marketIntelCarryImage 是搬运结果中的单张资源库图片。
type marketIntelCarryImage struct {
	// ID 是资源库图片 ID。
	ID int64 `json:"id"`
	// Filename 是图片文件名。
	Filename string `json:"filename"`
}

// marketIntelCarryResponse 是一键搬运响应：预填数据 + 资源库图片。
type marketIntelCarryResponse struct {
	// Title 是预填标题（AI 改写后）。
	Title string `json:"title"`
	// Desc 是预填描述（AI 改写后）。
	Desc string `json:"desc"`
	// Price 是预填价格。
	Price string `json:"price"`
	// Images 是已下载入库的资源库图片。
	Images []marketIntelCarryImage `json:"images"`
	// SameImagesHint 提示图片与竞品相同。
	SameImagesHint string `json:"same_images_hint"`
}

// marketIntelItemDetail 返回竞品详情（7 天缓存，未命中实时抓取）。
func (s *Server) marketIntelItemDetail(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例端口。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// itemID 是路径参数。
	itemID := chi.URLParam(r, "item_id")
	// detail、err 是详情查询结果与查询错误；实时抓取可能耗时较长。
	detail, err := svc.GetCompetitorDetail(r.Context(), itemID)
	if err != nil {
		s.writeMarketIntelDetailError(w, itemID, err)
		return
	}
	// images 是图片列表；nil 转空数组。
	images := detail.Images
	if images == nil {
		images = []string{}
	}
	writeJSON(w, http.StatusOK, marketIntelDetailResponse{
		ItemID: detail.ItemID, Title: detail.Title, Price: detail.Price,
		DescText: detail.DescText, Images: images,
		SellerNickname: detail.SellerNickname, SellerID: detail.SellerID, Cached: detail.Cached,
	})
}

// marketIntelItemRewrite 用 AI 改写竞品文案。
func (s *Server) marketIntelItemRewrite(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例端口。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// itemID 是路径参数，用于失败诊断。
	itemID := chi.URLParam(r, "item_id")
	// detail、err 是改写依据的详情与查询错误。
	detail, err := svc.GetCompetitorDetail(r.Context(), itemID)
	if err != nil {
		s.writeMarketIntelDetailError(w, itemID, err)
		return
	}
	// rewritten 是 AI 改写结果。
	rewritten, rewriteErr := svc.RewriteCompetitorCopy(r.Context(), detail.Title, detail.DescText)
	if rewriteErr != nil {
		writeErr(w, http.StatusInternalServerError, rewriteErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, marketIntelRewriteResponse{Title: rewritten.Title, Desc: rewritten.Desc})
}

// marketIntelItemAnalyze 用 AI 分析竞品卖点。
func (s *Server) marketIntelItemAnalyze(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例端口。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// itemID 是路径参数，用于失败诊断。
	itemID := chi.URLParam(r, "item_id")
	// detail、err 是分析依据的详情与查询错误。
	detail, err := svc.GetCompetitorDetail(r.Context(), itemID)
	if err != nil {
		s.writeMarketIntelDetailError(w, itemID, err)
		return
	}
	// analysis 是 AI 分析结果。
	analysis, analyzeErr := svc.AnalyzeCompetitorSellingPoints(r.Context(), detail.Title, detail.DescText, detail.Price, detail.WantCountText)
	if analyzeErr != nil {
		writeErr(w, http.StatusInternalServerError, analyzeErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, marketIntelAnalyzeResponse{Analysis: analysis})
}

// marketIntelItemCarry 一键搬运：改写文案 + 图片下载入资源库，返回预填数据。
// 编排由应用层搬运用例完成；本端点只做参数解析、错误映射与 DTO 转换。
func (s *Server) marketIntelItemCarry(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例端口。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// sess 是当前会话，用于图片资源归属。
	sess := auth.SessionFromContext(r.Context())
	// result 是搬运用例结果。
	// itemID 是路径参数，用于失败诊断。
	itemID := chi.URLParam(r, "item_id")
	// result、err 是搬运用例结果与执行错误。
	result, err := svc.CarryCompetitorItem(r.Context(), sess.UserID, itemID)
	if err != nil {
		s.writeMarketIntelDetailError(w, itemID, err)
		return
	}
	// images 是响应图片集合；nil 转空数组。
	images := make([]marketIntelCarryImage, 0, len(result.Images))
	// image 是当前入库图片。
	for _, image := range result.Images {
		images = append(images, marketIntelCarryImage{ID: image.ID, Filename: image.Filename})
	}
	writeJSON(w, http.StatusOK, marketIntelCarryResponse{
		Title: result.Title, Desc: result.Desc, Price: result.Price,
		Images: images, SameImagesHint: result.SameImagesHint,
	})
}

// writeMarketIntelDetailError 把详情错误映射为统一错误信封；未归类错误留下可诊断日志。
func (s *Server) writeMarketIntelDetailError(w http.ResponseWriter, itemID string, err error) {
	switch {
	case errors.Is(err, marketintel.ErrRiskControl):
		writeErr(w, http.StatusConflict, marketintel.ErrRiskControl.Error())
	case errors.Is(err, marketintel.ErrCompetitorDetailRiskControl):
		writeErr(w, http.StatusConflict, marketintel.ErrCompetitorDetailRiskControl.Error())
	case errors.Is(err, marketintel.ErrSessionExpired):
		writeErr(w, http.StatusConflict, marketintel.ErrSessionExpired.Error())
	case errors.Is(err, marketintel.ErrCompetitorDetailSessionExpired):
		writeErr(w, http.StatusConflict, marketintel.ErrCompetitorDetailSessionExpired.Error())
	case errors.Is(err, marketintel.ErrAINotConfigured):
		writeErr(w, http.StatusBadRequest, marketintel.ErrAINotConfigured.Error())
	case errors.Is(err, marketintel.ErrImageLibraryUnavailable):
		writeErr(w, http.StatusServiceUnavailable, marketintel.ErrImageLibraryUnavailable.Error())
	default:
		// 未归类错误（含浏览器抓取失败）必须留下可诊断日志，否则线上故障不可见；
		// logsafe.Error 只保留定位所需的错误文本，不含 Cookie 或令牌明文。
		s.Logger.Warn("竞品详情操作失败", "item_id", itemID, "err", logsafe.Error(err))
		writeErr(w, http.StatusInternalServerError, "获取竞品详情失败")
	}
}

// marketIntelKeywordItemDTO 是快照商品摘要 DTO。
type marketIntelKeywordItemDTO struct {
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// Price 是价格文本。
	Price string `json:"price"`
	// WantCountText 是想要人数文本。
	WantCountText string `json:"want_count_text"`
	// SellerNickname 是卖家昵称。
	SellerNickname string `json:"seller_nickname"`
}

// marketIntelKeywordItemsResponse 是某词快照商品列表响应。
type marketIntelKeywordItemsResponse struct {
	// Keyword 是关注词。
	Keyword string `json:"keyword"`
	// Items 是商品列表。
	Items []marketIntelKeywordItemDTO `json:"items"`
}

// marketIntelKeywordItems 返回某词最新一天的快照商品列表。
func (s *Server) marketIntelKeywordItems(w http.ResponseWriter, r *http.Request) {
	// svc 是市场情报用例端口。
	svc, ok := s.requireMarketIntel(w)
	if !ok {
		return
	}
	// keyword 是路径参数。
	keyword := chi.URLParam(r, "keyword")
	// items 是商品列表。
	items, err := svc.ListKeywordItems(r.Context(), keyword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取商品列表失败")
		return
	}
	// out 是响应集合。
	out := make([]marketIntelKeywordItemDTO, 0, len(items))
	// item 是当前商品。
	for _, item := range items {
		out = append(out, marketIntelKeywordItemDTO{
			ItemID: item.ItemID, Title: item.Title, Price: item.Price,
			WantCountText: item.WantCountText, SellerNickname: item.SellerNickname,
		})
	}
	writeJSON(w, http.StatusOK, marketIntelKeywordItemsResponse{Keyword: keyword, Items: out})
}
