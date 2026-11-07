// Package browser: 竞品商品详情抓取——打开详情页并拦截页面自发的详情接口响应。
// 数据来自页面自发请求（mtop.taobao.idle.pc.detail），不构造任何 mtop 签名。
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// competitorDetailAPIMarker 是商品详情接口路径标识，用于筛选需要读取的响应。
const competitorDetailAPIMarker = "mtop.taobao.idle.pc.detail"

// competitorDetailPollAttempts 是等待详情接口响应的轮询次数上限。
const competitorDetailPollAttempts = 10

// competitorDetailPollInterval 是等待详情接口响应的轮询间隔。
const competitorDetailPollInterval = 1500 * time.Millisecond

// CompetitorDetail 是竞品商品的结构化详情。
type CompetitorDetail struct {
	// ItemID 是平台商品标识。
	ItemID string
	// Title 是商品标题。
	Title string
	// Price 是价格文本。
	Price string
	// DescText 是商品描述纯文本。
	DescText string
	// Images 是商品图片 URL 列表。
	Images []string
	// SellerNickname 是卖家昵称。
	SellerNickname string
	// SellerID 是卖家 userId（可用于拼接卖家主页地址）。
	SellerID string
	// WantCountText 是想要人数文本。
	WantCountText string
}

// competitorDetailPayload 是详情接口响应的最小解析结构。
type competitorDetailPayload struct {
	// Ret 是接口返回码。
	Ret []string `json:"ret"`
	// Data 是接口业务数据。
	Data struct {
		// ItemDO 是商品主体信息。
		ItemDO struct {
			// Title 是商品标题。
			Title string `json:"title"`
			// Desc 是商品描述（可含 HTML）。
			Desc string `json:"desc"`
			// Price 是价格文本。
			Price []struct {
				// Text 是价格分段文本。
				Text string `json:"text"`
			} `json:"price"`
			// ImageInfos 是商品图片信息列表。
			ImageInfos []struct {
				// URL 是图片地址。
				URL string `json:"url"`
			} `json:"imageInfos"`
		} `json:"itemDO"`
		// SellerDO 是卖家信息。
		SellerDO struct {
			// SellerID 是卖家 userId。
			SellerID string `json:"sellerId"`
			// NickName 是卖家昵称。
			NickName string `json:"nickName"`
		} `json:"sellerDO"`
	} `json:"data"`
}

// CompetitorItemDetail 打开竞品商品详情页并拦截页面自发的详情接口响应。
// 单次调用最多产生 1 次详情页访问，禁止重试；风控与会话失效立即返回错误。
func (m *Manager) CompetitorItemDetail(ctx context.Context, cookieID, cookieStr, itemID string) (CompetitorDetail, error) {
	if m == nil {
		return CompetitorDetail{}, errors.New("浏览器管理器未初始化")
	}
	// trimmedItemID 是去空白后的商品标识，空值直接拒绝。
	trimmedItemID := strings.TrimSpace(itemID)
	if trimmedItemID == "" {
		return CompetitorDetail{}, errors.New("商品 ID 不能为空")
	}
	if strings.TrimSpace(cookieID) == "" {
		return CompetitorDetail{}, errors.New("查看详情需要选择账号")
	}
	// page、release 是本次查询独占的浏览器页面与释放函数；release 必须配对调用。
	page, release, err := m.newPage(ctx, cookieID, cookieStr, true)
	if err != nil {
		return CompetitorDetail{}, err
	}
	defer release()

	// retMu 保护 rets 与 detailPayloads，因为响应回调在 Playwright 内部协程执行。
	var retMu sync.Mutex
	// rets 收集详情接口返回的状态文本，只用于风控与会话归类。
	var rets []string
	// detailPayloads 收集解析成功的详情响应。
	var detailPayloads []competitorDetailPayload
	page.OnResponse(func(response playwright.Response) {
		if !strings.Contains(response.URL(), competitorDetailAPIMarker) {
			return
		}
		// body、bodyErr 是接口响应正文；解析失败只说明该响应不可用，不影响后续轮询。
		body, bodyErr := response.Body()
		if bodyErr != nil {
			return
		}
		// payload、usable 是解析后的详情结构与该响应是否可用于组装详情。
		payload, usable := parseCompetitorDetailPayload(body)
		if !usable {
			return
		}
		retMu.Lock()
		rets = append(rets, payload.Ret...)
		if len(payload.Data.ItemDO.ImageInfos) > 0 || payload.Data.ItemDO.Desc != "" {
			detailPayloads = append(detailPayloads, payload)
		}
		retMu.Unlock()
	})

	// detailURL 是商品详情页地址。
	detailURL := "https://www.goofish.com/item?id=" + trimmedItemID
	// gotoErr 是打开详情页的导航错误（超时或网络失败），直接返回且不重试。
	if _, gotoErr := page.Goto(detailURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(sourcingNavigationTimeout),
	}); gotoErr != nil {
		return CompetitorDetail{}, fmt.Errorf("打开商品详情页失败: %w", gotoErr)
	}

	// captured 是轮询期间观察到的详情数据。
	var captured competitorDetailPayload
	// found 表示是否已捕获到有效详情。
	found := false
	// attempt 是当前轮询序号，达到上限即停止等待。
	for attempt := 0; attempt < competitorDetailPollAttempts; attempt++ {
		// ctxErr 表示调用方已取消或超时，必须立即停止等待并释放页面。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return CompetitorDetail{}, ctxErr
		}
		retMu.Lock()
		// capturedRets 与 capturedPayloads 是当前快照。
		capturedRets := append([]string(nil), rets...)
		if len(detailPayloads) > 0 {
			captured = detailPayloads[len(detailPayloads)-1]
			found = true
		}
		retMu.Unlock()
		// retErr 是根据返回码归类出的风控或会话失效错误，一旦出现立即中止查询。
		if retErr := sourcingRetError(capturedRets); retErr != nil {
			return CompetitorDetail{}, retErr
		}
		if found {
			break
		}
		page.WaitForTimeout(float64(competitorDetailPollInterval.Milliseconds()))
	}

	// finalRets 是结束等待时的返回码快照，用于最终归类。
	retMu.Lock()
	// finalRets 是结束等待时的返回码快照。
	finalRets := append([]string(nil), rets...)
	// finalPayload 是结束时的详情数据快照。
	if len(detailPayloads) > 0 {
		captured = detailPayloads[len(detailPayloads)-1]
		found = true
	}
	retMu.Unlock()
	// retErr 是最终返回码归类出的领域错误，优先于"无结果"返回。
	if retErr := sourcingRetError(finalRets); retErr != nil {
		return CompetitorDetail{}, retErr
	}
	if !found {
		return CompetitorDetail{}, ErrSourcingNoResult
	}

	return buildCompetitorDetail(trimmedItemID, captured), nil
}

// parseCompetitorDetailPayload 解析详情接口响应正文；正文不是 JSON 或结构缺失时返回 usable=false。
// 该函数是纯函数，便于用真实响应样本做确定性回归。
func parseCompetitorDetailPayload(body []byte) (competitorDetailPayload, bool) {
	// payload 是解析后的详情结构。
	var payload competitorDetailPayload
	// err 是本次调用的返回错误。
	if err := json.Unmarshal(body, &payload); err != nil {
		return competitorDetailPayload{}, false
	}
	return payload, true
}

// buildCompetitorDetail 把详情接口数据转换为竞品详情视图：价格按分段拼接，图片去空白后按顺序收集。
func buildCompetitorDetail(itemID string, captured competitorDetailPayload) CompetitorDetail {
	// detail 是转换后的竞品详情。
	detail := CompetitorDetail{
		ItemID:         itemID,
		Title:          strings.TrimSpace(captured.Data.ItemDO.Title),
		DescText:       stripHTMLTags(captured.Data.ItemDO.Desc),
		SellerID:       strings.TrimSpace(captured.Data.SellerDO.SellerID),
		SellerNickname: strings.TrimSpace(captured.Data.SellerDO.NickName),
	}
	// pricePart 拼接分段价格文本。
	var pricePart strings.Builder
	// priceItem 是价格分段。
	for _, priceItem := range captured.Data.ItemDO.Price {
		pricePart.WriteString(priceItem.Text)
	}
	detail.Price = strings.TrimSpace(pricePart.String())
	// imageInfo 遍历收集图片 URL。
	for _, imageInfo := range captured.Data.ItemDO.ImageInfos {
		// imgURL 是去空白后的图片地址。
		imgURL := strings.TrimSpace(imageInfo.URL)
		if imgURL != "" {
			detail.Images = append(detail.Images, imgURL)
		}
	}
	return detail
}

// stripHTMLTags 移除描述文本中的 HTML 标签并压缩空白。
func stripHTMLTags(raw string) string {
	// noTag 是移除标签后的文本。
	noTag := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(raw, " ")
	// parts 是按空白拆分的片段。
	parts := strings.Fields(noTag)
	return strings.Join(parts, " ")
}
