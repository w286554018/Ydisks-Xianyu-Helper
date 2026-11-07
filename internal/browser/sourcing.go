package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// 选品参考浏览器能力的固定参数；这些值同时约束单次调用的请求次数与结果规模。
const (
	// sourcingGuestPoolKey 是游客态热词采集复用的浏览器池键；该键永不注入账号 cookie。
	sourcingGuestPoolKey = "sourcing-guest"
	// sourcingSearchAPIMarker 是搜索接口路径标识，用于筛选需要读取的响应。
	sourcingSearchAPIMarker = "mtop.taobao.idlemtopsearch.pc.search"
	// sourcingActivateAPIMarker 是搜索热词接口路径标识（游客态可用）。
	sourcingActivateAPIMarker = "mtop.taobao.idlemtopsearch.pc.item.search.activate"
	// sourcingShadeAPIMarker 是搜索框遮罩热词接口路径标识（游客态可用）。
	sourcingShadeAPIMarker = "mtop.taobao.idlemtopsearch.pc.search.shade"
	// sourcingSearchResultLimit 是单次查询返回的商品条数上限，禁止翻页扩大。
	sourcingSearchResultLimit = 30
	// sourcingHotKeywordLimit 是单次采集保留的热词条数上限。
	sourcingHotKeywordLimit = 20
	// sourcingPollInterval 是等待页面渲染结果的轮询间隔。
	sourcingPollInterval = 1500 * time.Millisecond
	// sourcingPollAttempts 是轮询次数上限；配合间隔给出约 9 秒的渲染等待预算。
	sourcingPollAttempts = 6
	// sourcingNavigationTimeout 是打开搜索页的导航超时毫秒数。
	sourcingNavigationTimeout = 30000
)

// 选品参考浏览器能力的领域错误，便于应用层区分提示与降级策略。
var (
	// ErrSourcingRiskControl 表示闲鱼返回风控验证要求，必须人工处理且不得重试。
	ErrSourcingRiskControl = errors.New("闲鱼返回风控验证要求，请先在闲鱼网页手动完成验证后再试")
	// ErrSourcingSessionExpired 表示账号登录态失效，需要重新登录。
	ErrSourcingSessionExpired = errors.New("闲鱼登录态已失效，请重新登录该账号")
	// ErrSourcingNoResult 表示本次查询没有取到商品。
	ErrSourcingNoResult = errors.New("本次查询没有取到商品")
	// ErrSourcingHotKeywordUnavailable 表示热词接口未返回可解析的词条。
	ErrSourcingHotKeywordUnavailable = errors.New("本次没有取到平台热词")
)

// SearchItem 是搜索结果中的单条商品摘要；该结构只承载页面公开展示字段，不含任何凭证。
type SearchItem struct {
	// ItemID 是平台商品标识，来自商品链接的 id 参数。
	ItemID string
	// Title 是商品标题文本。
	Title string
	// Price 是页面展示的价格文本，缺失时为空。
	Price string
	// WantCount 是页面展示的"多少人想要"文本，缺失时为空。
	WantCount string
	// Location 是页面展示的卖家所在地区文本，缺失时为空。
	Location string
	// SellerNickname 是卖家昵称，来自搜索接口结构化数据；缺失时为空。
	SellerNickname string
	// PublishTime 是商品发布时间文本（毫秒时间戳转写）；缺失时为空。
	PublishTime string
	// DetailURL 是商品详情页地址。
	DetailURL string
}

// sourcingSearchCard 是页面抓取脚本返回的单张商品卡片原始文本。
type sourcingSearchCard struct {
	// ItemID 是商品标识，来自链接 href。
	ItemID string `json:"itemId"`
	// Text 是卡片可见文本，标题、价格与附加信息按页面顺序拼接。
	Text string `json:"text"`
}

// idRegexp 从商品链接提取 id 参数。
var idRegexp = regexp.MustCompile(`id=([^&]+)`)

// sourcingSearchPayload 是页面抓取脚本的返回结构。
type sourcingSearchPayload struct {
	// Cards 是页面上的商品卡片列表。
	Cards []sourcingSearchCard `json:"cards"`
}

// sourcingActivateResponse 是搜索热词接口的最小解析结构。
type sourcingActivateResponse struct {
	// Data 是接口业务数据。
	Data struct {
		// CardList 是热词卡片列表。
		CardList []struct {
			// CardData 保存卡片业务数据。
			CardData struct {
				// Keywords 是推荐搜索词列表。
				Keywords []struct {
					// Text 是词条展示文本。
					Text string `json:"text"`
				} `json:"keywords"`
			} `json:"cardData"`
		} `json:"cardList"`
	} `json:"data"`
}

// sourcingShadeResponse 是搜索框遮罩热词接口的最小解析结构。
type sourcingShadeResponse struct {
	// Data 是接口业务数据。
	Data struct {
		// SingleShadeWords 是单个遮罩词列表。
		SingleShadeWords []struct {
			// ShowText 是展示文本。
			ShowText string `json:"showText"`
			// RealShade 是实际搜索词。
			RealShade string `json:"realShade"`
		} `json:"singleShadeWords"`
	} `json:"data"`
}

// sourcingRetError 把接口 ret 文本转换为领域错误；无匹配时返回 nil。
// 该函数只做错误归类，不触发任何重试。
func sourcingRetError(rets []string) error {
	// ret 是当前遍历的接口返回码文本。
	for _, ret := range rets {
		// upper 统一大写，便于匹配平台返回码的不同写法。
		upper := strings.ToUpper(ret)
		if strings.Contains(upper, "RGV587") || strings.Contains(upper, "FAIL_SYS_USER_VALIDATE") {
			return ErrSourcingRiskControl
		}
		if strings.Contains(upper, "SESSION_EXPIRED") || strings.Contains(upper, "TOKEN") {
			return ErrSourcingSessionExpired
		}
	}
	return nil
}

// SearchItems 注入账号登录态后打开搜索页，抓取单次搜索结果。
// 单次调用最多产生 1 次搜索请求，返回上限 30 条，禁止翻页与重试。
func (m *Manager) SearchItems(ctx context.Context, cookieID, cookieStr, keyword string) ([]SearchItem, error) {
	if m == nil {
		return nil, errors.New("浏览器管理器未初始化")
	}
	// trimmedKeyword 是去空白后的查询关键词，空值直接拒绝以免发出无意义请求。
	trimmedKeyword := strings.TrimSpace(keyword)
	if trimmedKeyword == "" {
		return nil, errors.New("查询关键词不能为空")
	}
	if strings.TrimSpace(cookieID) == "" {
		return nil, errors.New("查询需要选择账号")
	}
	// page、release 是本次查询独占的浏览器页面与释放函数；release 必须配对调用。
	page, release, err := m.newPage(ctx, cookieID, cookieStr, true)
	if err != nil {
		return nil, err
	}
	defer release()

	// retMu 保护 rets 与 apiItems，因为响应回调在 Playwright 内部协程执行。
	var retMu sync.Mutex
	// rets 收集搜索接口返回的状态文本，只用于风控与会话归类。
	var rets []string
	// apiItems 收集搜索接口返回的结构化商品（含卖家昵称与发布时间）。
	var apiItems []SearchItem
	page.OnResponse(func(response playwright.Response) {
		if !strings.Contains(response.URL(), sourcingSearchAPIMarker) {
			return
		}
		// body、bodyErr 是接口响应正文；解析失败只说明该响应不可用，不影响后续轮询。
		body, bodyErr := response.Body()
		if bodyErr != nil {
			return
		}
		// envelope 解析 ret 与 resultList；resultList 是页面自发搜索接口的结构化商品列表。
		var envelope struct {
			Ret  []string `json:"ret"`
			Data struct {
				ResultList []struct {
					Data struct {
						Item struct {
							Main struct {
								ExContent struct {
									ItemID string `json:"itemId"`
									Title  string `json:"title"`
									Price  []struct {
										Text string `json:"text"`
									} `json:"price"`
									UserNickName string `json:"userNickName"`
									Area         string `json:"area"`
								} `json:"exContent"`
								ClickParam struct {
									Args struct {
										PublishTime string `json:"publishTime"`
										WantNum     string `json:"wantNum"`
									} `json:"args"`
								} `json:"clickParam"`
								TargetURL string `json:"targetUrl"`
							} `json:"main"`
						} `json:"item"`
					} `json:"data"`
				} `json:"resultList"`
			} `json:"data"`
		}
		// jsonErr 表示搜索接口正文不是 JSON（例如被重定向到登录页），此时忽略该响应。
		if jsonErr := json.Unmarshal(body, &envelope); jsonErr != nil {
			return
		}
		retMu.Lock()
		rets = append(rets, envelope.Ret...)
		// entry 遍历接口返回的商品列表，转换为结构化摘要。
		for _, entry := range envelope.Data.ResultList {
			// main 是商品主信息。
			main := entry.Data.Item.Main
			// priceText 拼接分段价格文本。
			var priceText strings.Builder
			// part 是价格分段。
			for _, part := range main.ExContent.Price {
				priceText.WriteString(part.Text)
			}
			// publishTimeText 是毫秒时间戳转写的发布时间。
			publishTimeText := ""
			// pubTs 是发布时间毫秒值。
			if pubTs, convErr := strconv.ParseInt(main.ClickParam.Args.PublishTime, 10, 64); convErr == nil && pubTs > 0 {
				publishTimeText = time.UnixMilli(pubTs).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")
			}
			// itemID 兜底从链接提取。
			itemID := main.ExContent.ItemID
			// idMatch 从 targetUrl 提取商品 ID。
			if itemID == "" {
				// match 是链接中的 id 参数。
				if match := idRegexp.FindStringSubmatch(main.TargetURL); len(match) > 1 {
					itemID = match[1]
				}
			}
			if itemID == "" {
				continue
			}
			apiItems = append(apiItems, SearchItem{
				ItemID:         itemID,
				Title:          strings.TrimSpace(main.ExContent.Title),
				Price:          strings.TrimSpace(priceText.String()),
				WantCount:      main.ClickParam.Args.WantNum,
				Location:       main.ExContent.Area,
				SellerNickname: main.ExContent.UserNickName,
				PublishTime:    publishTimeText,
				DetailURL:      "https://www.goofish.com/item?id=" + itemID,
			})
		}
		retMu.Unlock()
	})

	// searchURL 是关键词搜索结果页地址。
	searchURL := "https://www.goofish.com/search?q=" + strings.ReplaceAll(strings.ReplaceAll(trimmedKeyword, " ", "%20"), "#", "%23")
	// gotoErr 是打开搜索页的导航错误（超时或网络失败），直接返回且不重试。
	if _, gotoErr := page.Goto(searchURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(sourcingNavigationTimeout),
	}); gotoErr != nil {
		return nil, fmt.Errorf("打开闲鱼搜索页失败: %w", gotoErr)
	}

	// items 保存本次抓取到的商品摘要，轮询期间逐步填充。
	// 优先使用搜索接口的结构化数据（含卖家昵称）；DOM 提取仅作兜底。
	var items []SearchItem
	// attempt 是当前轮询序号，达到上限即停止等待。
	for attempt := 0; attempt < sourcingPollAttempts; attempt++ {
		// ctxErr 表示调用方已取消或超时，必须立即停止等待并释放页面。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// captured 是当前已观察到的接口返回码快照，避免长时间持锁。
		retMu.Lock()
		// captured 是当前已观察到的接口返回码快照，复制后释放锁再判断，避免长时间持锁。
		captured := append([]string(nil), rets...)
		retMu.Unlock()
		// retErr 是根据返回码归类出的风控或会话失效错误，一旦出现立即中止查询。
		if retErr := sourcingRetError(captured); retErr != nil {
			return nil, retErr
		}
		// 优先采用搜索接口的结构化数据（含卖家昵称与发布时间）。
		retMu.Lock()
		// apiSnapshot 是接口结构化数据的快照副本。
		apiSnapshot := append([]SearchItem(nil), apiItems...)
		retMu.Unlock()
		if len(apiSnapshot) > 0 {
			items = apiSnapshot
			break
		}
		// 接口数据未就绪时用 DOM 提取兜底。
		items = extractSearchItems(page)
		if len(items) > 0 {
			break
		}
		page.WaitForTimeout(float64(sourcingPollInterval.Milliseconds()))
	}

	// finalRets 是结束等待时的返回码快照，用于最终归类。
	retMu.Lock()
	// finalRets 是结束等待时的返回码快照，用于最终风控与会话判断。
	finalRets := append([]string(nil), rets...)
	// finalAPIItems 是结束时的接口结构化数据快照。
	finalAPIItems := append([]SearchItem(nil), apiItems...)
	retMu.Unlock()
	// retErr 是最终返回码归类出的领域错误，优先于"无结果"返回。
	if retErr := sourcingRetError(finalRets); retErr != nil {
		return nil, retErr
	}
	// 接口结构化数据优先；DOM 兜底数据补齐接口缺失的卖家信息（按商品 ID 对齐）。
	if len(finalAPIItems) > 0 {
		// domIndex 是 DOM 提取结果的商品 ID 索引。
		domIndex := make(map[string]SearchItem, len(items))
		// domItem 遍历建立索引。
		for _, domItem := range items {
			domIndex[domItem.ItemID] = domItem
		}
		// merged 是合并后的最终集合。
		merged := make([]SearchItem, 0, len(finalAPIItems))
		// apiItem 遍历接口数据，缺省字段用 DOM 值补齐。
		for _, apiItem := range finalAPIItems {
			if apiItem.Title == "" || apiItem.Price == "" || apiItem.WantCount == "" {
				// domMatch 是同商品的 DOM 提取结果。
				if domMatch, exists := domIndex[apiItem.ItemID]; exists {
					if apiItem.Title == "" {
						apiItem.Title = domMatch.Title
					}
					if apiItem.Price == "" {
						apiItem.Price = domMatch.Price
					}
					if apiItem.WantCount == "" {
						apiItem.WantCount = domMatch.WantCount
					}
				}
			}
			merged = append(merged, apiItem)
		}
		items = merged
	}
	if len(items) == 0 {
		return nil, ErrSourcingNoResult
	}
	if len(items) > sourcingSearchResultLimit {
		items = items[:sourcingSearchResultLimit]
	}
	return items, nil
}

// extractSearchItems 从当前页面抓取商品卡片并解析为结构化摘要；解析失败的卡片会被跳过。
func extractSearchItems(page playwright.Page) []SearchItem {
	// script 在页面内按商品链接收集卡片可见文本，不访问任何需要登录的接口。
	script := `() => {
		const cards = [];
		const anchors = Array.from(document.querySelectorAll('a[href*="item?id="]'));
		for (const a of anchors) {
			const text = (a.innerText || '').trim();
			if (!text) continue;
			const href = a.getAttribute('href') || '';
			const idMatch = href.match(/id=([^&]+)/);
			cards.push({ itemId: idMatch ? decodeURIComponent(idMatch[1]) : '', text });
			if (cards.length >= 30) break;
		}
		return { cards };
	}`
	// raw 是页面脚本返回的原始结构；类型断言失败说明页面尚未渲染出商品卡片。
	raw, err := page.Evaluate(script)
	if err != nil {
		return nil
	}
	// encoded 把 raw 重新编码为 JSON，避免依赖 Playwright 返回的具体 Go 类型。
	encoded, encodeErr := json.Marshal(raw)
	if encodeErr != nil {
		return nil
	}
	// payload 是反序列化后的卡片集合。
	var payload sourcingSearchPayload
	// decodeErr 表示页面脚本返回结构异常，此时视为尚未渲染出商品。
	if decodeErr := json.Unmarshal(encoded, &payload); decodeErr != nil {
		return nil
	}
	// out 汇总解析成功的商品摘要。
	out := make([]SearchItem, 0, len(payload.Cards))
	// card 是当前遍历到的卡片。
	for _, card := range payload.Cards {
		// trimmedText 是去多余空白后的卡片文本，用于拆分标题与附加信息。
		trimmedText := strings.Join(strings.Fields(card.Text), " ")
		if trimmedText == "" {
			continue
		}
		// item 是解析出的商品摘要。
		item := SearchItem{
			ItemID:    card.ItemID,
			Title:     extractSearchTitle(trimmedText),
			Price:     extractSearchPrice(trimmedText),
			WantCount: extractSearchWantCount(trimmedText),
			Location:  extractSearchLocation(trimmedText),
		}
		if item.ItemID != "" {
			item.DetailURL = "https://www.goofish.com/item?id=" + item.ItemID
		}
		if item.Title == "" && item.ItemID == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

// extractSearchTitle 从卡片全文里截取价格之前的标题部分，避免把价格与地区拼进标题。
func extractSearchTitle(fullText string) string {
	// priceIndex 是价格符号在全文中的位置；未出现时整段文本视为标题。
	priceIndex := strings.Index(fullText, "¥")
	// title 是候选标题文本。
	title := fullText
	if priceIndex > 0 {
		title = fullText[:priceIndex]
	}
	title = strings.TrimSpace(title)
	if len([]rune(title)) > 120 {
		title = string([]rune(title)[:120])
	}
	return title
}

// extractSearchPrice 从卡片文本提取价格文本，例如 "¥19" 返回 "19"。
func extractSearchPrice(fullText string) string {
	// priceIndex 是价格符号位置，缺失时说明该卡片没有展示价格。
	priceIndex := strings.Index(fullText, "¥")
	if priceIndex < 0 {
		return ""
	}
	// rest 是价格符号之后的文本片段。
	rest := fullText[priceIndex+len("¥"):]
	// runes 是价格符号之后的字符序列，便于在不破坏字节边界的前提下向前窥视。
	runes := []rune(rest)
	// builder 逐个收集数字与小数点，遇到附加信息起始即停止。
	var builder strings.Builder
	// seenDigit 表示已经收集到至少一个数字。
	seenDigit := false
	// index 是当前字符下标。
	for index := 0; index < len(runes); index++ {
		// r 是当前遍历到的字符。
		r := runes[index]
		switch {
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			seenDigit = true
		case r == '.':
			builder.WriteRune(r)
		case r == ' ':
			// 尚未读到数字时的前导空格直接跳过，例如 "¥ 19"。
			if !seenDigit {
				continue
			}
			// 已读到数字时，只有空格后紧跟小数点才算价格内部空格（如 "34 .80"）；
			// 否则空格之后是"多少人想要"等附加信息，必须在此结束价格，避免把人数并入金额。
			if index+1 < len(runes) && runes[index+1] == '.' {
				continue
			}
			return builder.String()
		default:
			return builder.String()
		}
	}
	return builder.String()
}

// extractSearchWantCount 从卡片文本提取"N人想要"数量文本。
func extractSearchWantCount(fullText string) string {
	// wantIndex 是"人想要"关键字位置，缺失时说明该卡片未展示想要人数。
	wantIndex := strings.Index(fullText, "人想要")
	if wantIndex < 0 {
		return ""
	}
	// prefix 是关键字之前用于回溯数字的文本。
	prefix := fullText[:wantIndex]
	// start 是数字片段起点，从关键字位置向前回溯。
	start := len(prefix)
	for start > 0 {
		// ch 是当前回溯位置的字节。
		ch := prefix[start-1]
		if ch >= '0' && ch <= '9' {
			start--
			continue
		}
		break
	}
	if start == len(prefix) {
		return ""
	}
	return prefix[start:wantIndex]
}

// extractSearchLocation 从卡片文本末尾提取地区文本；无法识别时返回空字符串。
func extractSearchLocation(fullText string) string {
	// suffixes 是闲鱼卡片常见的地区后缀，用于从末尾定位地区文本。
	suffixes := []string{"广东", "北京", "上海", "浙江", "江苏", "四川", "山东", "湖北", "福建", "河南", "湖南", "安徽", "陕西", "重庆", "天津", "河北", "辽宁", "江西", "广西", "云南", "山西", "黑龙江", "吉林", "贵州", "内蒙古", "新疆", "甘肃", "海南", "宁夏", "青海", "西藏"}
	// suffix 是当前尝试匹配的地区后缀。
	for _, suffix := range suffixes {
		if strings.Contains(fullText, suffix) {
			return suffix
		}
	}
	return ""
}

// FetchHotKeywords 以游客身份打开闲鱼搜索页，采集平台搜索热词。
// 不注入任何账号 cookie；单次调用最多产生 2 次热词接口请求，不做搜索翻页。
func (m *Manager) FetchHotKeywords(ctx context.Context) ([]string, error) {
	if m == nil {
		return nil, errors.New("浏览器管理器未初始化")
	}
	// page、release 是游客态页面与释放函数；该池键永不携带账号凭证。
	page, release, err := m.newPage(ctx, sourcingGuestPoolKey, "", true)
	if err != nil {
		return nil, err
	}
	defer release()

	// collectMu 保护 collected，因为响应回调在 Playwright 内部协程执行。
	var collectMu sync.Mutex
	// collected 按出现顺序保存热词，保持平台推荐顺序。
	var collected []string
	// appendKeyword 追加一个去空白后的热词，重复与空值会被忽略。
	appendKeyword := func(word string) {
		// trimmed 是去空白后的词条文本。
		trimmed := strings.TrimSpace(word)
		if trimmed == "" {
			return
		}
		collectMu.Lock()
		defer collectMu.Unlock()
		// existing 是已收集词条，用于去重。
		for _, existing := range collected {
			if existing == trimmed {
				return
			}
		}
		if len(collected) >= sourcingHotKeywordLimit {
			return
		}
		collected = append(collected, trimmed)
	}

	page.OnResponse(func(response playwright.Response) {
		// url 是当前响应地址，用于区分热词接口。
		url := response.URL()
		// isActivate 表示该响应来自搜索热词接口。
		isActivate := strings.Contains(url, sourcingActivateAPIMarker)
		// isShade 表示该响应来自搜索框遮罩接口。
		isShade := strings.Contains(url, sourcingShadeAPIMarker)
		if !isActivate && !isShade {
			return
		}
		// body、bodyErr 是接口响应正文；读取失败只跳过该响应。
		body, bodyErr := response.Body()
		if bodyErr != nil {
			return
		}
		if isActivate {
			// activatePayload 是热词接口的解析结果。
			var activatePayload sourcingActivateResponse
			// jsonErr 表示热词接口正文不可解析，忽略该响应等待遮罩词接口。
			if jsonErr := json.Unmarshal(body, &activatePayload); jsonErr != nil {
				return
			}
			// card 是当前遍历到的热词卡片。
			for _, card := range activatePayload.Data.CardList {
				// keyword 是当前遍历到的推荐词。
				for _, keyword := range card.CardData.Keywords {
					appendKeyword(keyword.Text)
				}
			}
			return
		}
		// shadePayload 是遮罩热词接口的解析结果。
		var shadePayload sourcingShadeResponse
		// jsonErr 表示遮罩词接口正文不可解析，忽略该响应。
		if jsonErr := json.Unmarshal(body, &shadePayload); jsonErr != nil {
			return
		}
		// word 是当前遍历到的遮罩词。
		for _, word := range shadePayload.Data.SingleShadeWords {
			if strings.TrimSpace(word.ShowText) != "" {
				appendKeyword(word.ShowText)
				continue
			}
			appendKeyword(word.RealShade)
		}
	})

	// gotoErr 是打开闲鱼首页的导航错误，热词采集直接失败且当天不重试。
	if _, gotoErr := page.Goto("https://www.goofish.com/", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(sourcingNavigationTimeout),
	}); gotoErr != nil {
		return nil, fmt.Errorf("打开闲鱼首页失败: %w", gotoErr)
	}

	// attempt 是当前轮询序号；首页偶尔延迟加载热词，需要短暂等待。
	for attempt := 0; attempt < sourcingPollAttempts; attempt++ {
		// ctxErr 表示调用方已取消或超时，必须立即停止等待。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		collectMu.Lock()
		// size 是当前已收集的词条数量。
		size := len(collected)
		collectMu.Unlock()
		if size > 0 {
			break
		}
		page.WaitForTimeout(float64(sourcingPollInterval.Milliseconds()))
	}

	collectMu.Lock()
	// result 是最终热词副本，避免把可变切片暴露给调用方。
	result := append([]string(nil), collected...)
	collectMu.Unlock()
	if len(result) == 0 {
		return nil, ErrSourcingHotKeywordUnavailable
	}
	return result, nil
}
