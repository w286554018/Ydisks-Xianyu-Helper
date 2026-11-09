// Package browser: 竞品商品详情抓取——打开详情页并拦截页面自发的详情接口响应。
// 数据来自页面自发请求（mtop.taobao.idle.pc.detail），不构造任何 mtop 签名。
package browser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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
	// BrowseCount 是商品浏览量；平台未提供该字段时为 0。
	BrowseCount int64
	// WantCount 是商品想要人数；平台未提供该字段时为 0。
	WantCount int64
	// CollectCount 是商品收藏数；平台未提供该字段时为 0。
	CollectCount int64
	// SoldCount 是商品已售数；平台未提供该字段时为 0。
	SoldCount int64
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
			// Price 是价格分段文本。
			Price []struct {
				// Text 是价格分段文本。
				Text flexString `json:"text"`
			} `json:"price"`
			// SoldPrice 是商品价格文本（新版接口在同一层级直接给出）。
			SoldPrice flexString `json:"soldPrice"`
			// ImageInfos 是商品图片信息列表。
			ImageInfos []struct {
				// URL 是图片地址。
				URL string `json:"url"`
			} `json:"imageInfos"`
			// BrowseCnt 是浏览量文本；兼容字符串与数字两种返回形态。
			BrowseCnt flexString `json:"browseCnt"`
			// WantCnt 是想要人数文本；兼容字符串与数字两种返回形态。
			WantCnt flexString `json:"wantCnt"`
			// CollectCnt 是收藏数文本；兼容字符串与数字两种返回形态。
			CollectCnt flexString `json:"collectCnt"`
			// SoldCnt 是已售数文本；兼容字符串与数字两种返回形态。
			SoldCnt flexString `json:"soldCnt"`
		} `json:"itemDO"`
		// SellerDO 是卖家信息。
		SellerDO struct {
			// SellerID 是卖家 userId。
			SellerID flexString `json:"sellerId"`
			// NickName 是卖家昵称（旧版字段）。
			NickName string `json:"nickName"`
			// Nick 是卖家昵称（新版接口字段）。
			Nick string `json:"nick"`
		} `json:"sellerDO"`
	} `json:"data"`
}

// flexString 是兼容「字符串或数字」的文本字段类型。
// 平台会在不同接口变体里把 sellerId、价格文本一类字段返回成数字或字符串，
// 使用该类型可避免个别字段类型差异导致整段响应解析失败、进而把可用详情误判为不可用。
type flexString string

// UnmarshalJSON 兼容解析字符串、数字与 null：字符串按字面量保存，其它标量按其原始文本保存。
func (f *flexString) UnmarshalJSON(data []byte) error {
	// trimmed 是去除空白后的原始 JSON 片段。
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		// text 是字符串字面量；非法字面量直接返回错误，避免静默丢字段。
		var text string
		if // err 是字符串字面量解析失败原因。
		err := json.Unmarshal([]byte(trimmed), &text); err != nil {
			return err
		}
		*f = flexString(text)
		return nil
	}
	*f = flexString(trimmed)
	return nil
}

// parseFlexCount 把接口返回的计数文本解析为非负整数。
// 空值、负数与非法文本一律按 0 处理：对本功能而言「平台未提供」与「确实是 0」的展示结果相同，
// 都不会造成误导，也不会因为个别字段形态差异而让整段详情解析失败。
func parseFlexCount(raw flexString) int64 {
	// trimmed 是去空白后的计数文本。
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return 0
	}
	if // intValue、intErr 是按整数解析的结果与错误；平台绝大多数情况下直接给整数。
	intValue, intErr := strconv.ParseInt(trimmed, 10, 64); intErr == nil {
		if intValue < 0 {
			return 0
		}
		return intValue
	}
	// floatValue、floatErr 是按浮点解析的结果与错误，用于容忍「22.0」一类写法。
	floatValue, floatErr := strconv.ParseFloat(trimmed, 64)
	if floatErr != nil || math.IsNaN(floatValue) || math.IsInf(floatValue, 0) || floatValue < 0 {
		return 0
	}
	return int64(floatValue)
}

// CompetitorItemDetail 打开竞品商品详情页并拦截页面自发的详情接口响应。
// 单次调用最多产生 1 次详情页访问；会话失效立即返回错误。
// 平台对详情接口返回处罚地址（要求在会话内完成人机验证）时，使用同一会话调用既有滑块验证并重试一次；
// 第二个返回值是验证刷新后的完整 Cookie，未发生刷新时为空，供调用方落库。
func (m *Manager) CompetitorItemDetail(ctx context.Context, cookieID, cookieStr, itemID string) (CompetitorDetail, string, error) {
	if m == nil {
		return CompetitorDetail{}, "", errors.New("浏览器管理器未初始化")
	}
	// trimmedItemID 是去空白后的商品标识，空值直接拒绝。
	trimmedItemID := strings.TrimSpace(itemID)
	if trimmedItemID == "" {
		return CompetitorDetail{}, "", errors.New("商品 ID 不能为空")
	}
	if strings.TrimSpace(cookieID) == "" {
		return CompetitorDetail{}, "", errors.New("查看详情需要选择账号")
	}
	// detail、punishURL、err 是首次访问结果、平台处罚地址与错误。
	detail, punishURL, err := m.competitorDetailAttempt(ctx, cookieID, cookieStr, trimmedItemID)
	if err == nil || punishURL == "" {
		return detail, "", err
	}
	// 抑制窗口内不再重复发起自动验证：一次失败尝试本身会消耗多次滑块重试，
	// 反复冲击只会加重平台风控，因此保持人工接管提示并等待窗口结束。
	if m.captchaRecoverySuppressed(cookieID, captchaCookieHash(cookieStr)) {
		m.logCompetitorDetailRecoverySuppressed(trimmedItemID, punishURL)
		return CompetitorDetail{}, "", err
	}
	// 平台要求在会话内完成人机验证：验证地址只在内存中传递，不写日志。
	// refreshed、captchaEngine、recoverErr 是验证后的新 Cookie、使用的引擎与失败原因。
	refreshed, captchaEngine, recoverErr := m.TokenCaptchaRecoverWithEngine(ctx, cookieID, cookieStr, punishURL, true, nil)
	if recoverErr != nil || strings.TrimSpace(refreshed) == "" {
		m.markCaptchaRecoveryFailure(cookieID, cookieStr)
		m.logCompetitorDetailRecoveryFailure(trimmedItemID, punishURL, recoverErr)
		return CompetitorDetail{}, "", err
	}
	m.clearCaptchaRecoveryFailure(cookieID)
	m.logCompetitorDetailRecoverySuccess(trimmedItemID, captchaEngine)
	// 用验证后的 Cookie 重试一次；仍失败时把新 Cookie 交给调用方落库。
	// retried、retryErr 是重试结果与错误。
	retried, _, retryErr := m.competitorDetailAttempt(ctx, cookieID, refreshed, trimmedItemID)
	if retryErr != nil {
		return CompetitorDetail{}, refreshed, retryErr
	}
	return retried, refreshed, nil
}

// competitorDetailAttempt 执行一次详情页访问与轮询，返回详情、平台处罚地址与错误。
// 处罚地址非空表示平台要求人机验证，调用方可用该地址在会话内完成验证后重试。
func (m *Manager) competitorDetailAttempt(ctx context.Context, cookieID, cookieStr, trimmedItemID string) (CompetitorDetail, string, error) {
	// page、release 是本次查询独占的浏览器页面与释放函数；release 必须配对调用。
	page, release, err := m.newPage(ctx, cookieID, cookieStr, true)
	if err != nil {
		return CompetitorDetail{}, "", err
	}
	defer release()

	// retMu 保护 rets 与 detailPayloads，因为响应回调在 Playwright 内部协程执行。
	var retMu sync.Mutex
	// rets 收集详情接口返回的状态文本，只用于风控与会话归类。
	var rets []string
	// detailPayloads 收集解析成功的详情响应。
	var detailPayloads []competitorDetailPayload
	// diagnostics 收集触发风控或会话失效的响应定位信息，供错误路径留下可诊断日志。
	var diagnostics []competitorDetailDiagnostic
	// punishURL 保存平台处罚地址原始值（含 query），只用于会话内验证，不写日志。
	var punishURL string
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
		// retErr 表示该响应被归类为风控或会话失效；此时必须记录平台原始信息以便定位。
		retErr := sourcingRetError(payload.Ret)
		retMu.Lock()
		rets = append(rets, payload.Ret...)
		if retErr != nil {
			diagnostics = append(diagnostics, buildCompetitorDetailDiagnostic(response.Status(), response.URL(), body, payload.Ret))
			if punishURL == "" {
				punishURL = findCompetitorDetailPunishURL(string(body))
			}
		}
		if len(payload.Data.ItemDO.ImageInfos) > 0 || payload.Data.ItemDO.Desc != "" {
			detailPayloads = append(detailPayloads, payload)
		}
		retMu.Unlock()
	})
	// takeDiagnostics 返回当前已收集的诊断快照；返回前必须先结束回调的临界区。
	takeDiagnostics := func() []competitorDetailDiagnostic {
		retMu.Lock()
		defer retMu.Unlock()
		return append([]competitorDetailDiagnostic(nil), diagnostics...)
	}
	// takePunishURL 返回已捕获的平台处罚地址；未捕获时为空串。
	takePunishURL := func() string {
		retMu.Lock()
		defer retMu.Unlock()
		return punishURL
	}

	// detailURL 是商品详情页地址。
	detailURL := "https://www.goofish.com/item?id=" + trimmedItemID
	// gotoErr 是打开详情页的导航错误（超时或网络失败），直接返回且不重试。
	if _, gotoErr := page.Goto(detailURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(sourcingNavigationTimeout),
	}); gotoErr != nil {
		return CompetitorDetail{}, "", fmt.Errorf("打开商品详情页失败: %w", gotoErr)
	}

	// captured 是轮询期间观察到的详情数据。
	var captured competitorDetailPayload
	// found 表示是否已捕获到有效详情。
	found := false
	// attempt 是当前轮询序号，达到上限即停止等待。
	for attempt := 0; attempt < competitorDetailPollAttempts; attempt++ {
		// ctxErr 表示调用方已取消或超时，必须立即停止等待并释放页面。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return CompetitorDetail{}, "", ctxErr
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
			m.logCompetitorDetailDiagnostics(trimmedItemID, takeDiagnostics())
			return CompetitorDetail{}, takePunishURL(), retErr
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
		m.logCompetitorDetailDiagnostics(trimmedItemID, takeDiagnostics())
		return CompetitorDetail{}, takePunishURL(), retErr
	}
	if !found {
		return CompetitorDetail{}, "", ErrSourcingNoResult
	}

	return buildCompetitorDetail(trimmedItemID, captured), "", nil
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
	// sellerNick 是卖家昵称：新版接口用 nick，旧版接口用 nickName，取先出现的非空值。
	sellerNick := strings.TrimSpace(captured.Data.SellerDO.Nick)
	if sellerNick == "" {
		sellerNick = strings.TrimSpace(captured.Data.SellerDO.NickName)
	}
	// detail 是转换后的竞品详情。
	detail := CompetitorDetail{
		ItemID:         itemID,
		Title:          strings.TrimSpace(captured.Data.ItemDO.Title),
		DescText:       stripHTMLTags(captured.Data.ItemDO.Desc),
		SellerID:       strings.TrimSpace(string(captured.Data.SellerDO.SellerID)),
		SellerNickname: sellerNick,
		BrowseCount:    parseFlexCount(captured.Data.ItemDO.BrowseCnt),
		WantCount:      parseFlexCount(captured.Data.ItemDO.WantCnt),
		CollectCount:   parseFlexCount(captured.Data.ItemDO.CollectCnt),
		SoldCount:      parseFlexCount(captured.Data.ItemDO.SoldCnt),
	}
	// pricePart 拼接分段价格文本。
	var pricePart strings.Builder
	// priceItem 是价格分段。
	for _, priceItem := range captured.Data.ItemDO.Price {
		pricePart.WriteString(string(priceItem.Text))
	}
	detail.Price = strings.TrimSpace(pricePart.String())
	// 新版接口不再给出价格分段，改为同一层级直接给出 soldPrice，此时按该字段回退。
	if detail.Price == "" {
		detail.Price = strings.TrimSpace(string(captured.Data.ItemDO.SoldPrice))
	}
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

// ParseCompetitorDetailResponse 供本机抓取助手复用详情接口响应的解析逻辑：
// 返回解析出的详情、接口返回码与是否可用；rets 始终返回，便于调用方判定风控与成功。
func ParseCompetitorDetailResponse(itemID string, body []byte) (CompetitorDetail, []string, bool) {
	// payload、parsed 是解析后的详情结构与是否解析成功。
	payload, parsed := parseCompetitorDetailPayload(body)
	if !parsed {
		return CompetitorDetail{}, nil, false
	}
	// rets 是接口返回码副本，风控判定依赖它。
	rets := append([]string(nil), payload.Ret...)
	if strings.TrimSpace(payload.Data.ItemDO.Title) == "" {
		return CompetitorDetail{}, rets, false
	}
	return buildCompetitorDetail(itemID, payload), rets, true
}

// stripHTMLTags 移除描述文本中的 HTML 标签并压缩空白。
func stripHTMLTags(raw string) string {
	// noTag 是移除标签后的文本。
	noTag := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(raw, " ")
	// parts 是按空白拆分的片段。
	parts := strings.Fields(noTag)
	return strings.Join(parts, " ")
}

// competitorDetailPunishMarker 是平台处罚地址标识；命中该标识说明需要在会话内完成人机验证。
const competitorDetailPunishMarker = "_____tmd_____"

// competitorDetailCaptchaCooldown 是自动验证失败后的抑制窗口。
// 窗口内不再对同一账号发起自动验证，避免一次失败尝试（含多次滑块重试）反复冲击平台风控。
const competitorDetailCaptchaCooldown = 30 * time.Minute

// captchaRecoverySuppressed 判断账号是否处于自动验证失败抑制窗口内。
// cookieHash 是当前登录态摘要：登录态被外部更新（例如人工粘贴验证后的 Cookie）时不抑制，允许重新尝试。
func (m *Manager) captchaRecoverySuppressed(cookieID, cookieHash string) bool {
	m.captchaGuardMu.Lock()
	defer m.captchaGuardMu.Unlock()
	// failedAt、ok 是该账号最近一次自动验证失败时间与是否存在记录。
	failedAt, ok := m.captchaFailedAt[cookieID]
	if !ok {
		return false
	}
	if m.captchaFailedCookieHash[cookieID] != cookieHash {
		return false
	}
	return time.Since(failedAt) < competitorDetailCaptchaCooldown
}

// captchaIdentityCookieKeys 是判定「登录态是否被外部更新」的稳定 Cookie 键。
// 只保留账号身份与验证通过标记：平台每次请求都会轮换 _m_h5_tk、t 一类的短期令牌，
// 若把整串 Cookie 计入摘要，抑制窗口会被令牌轮换不断打断，从而反复冲击平台风控。
var captchaIdentityCookieKeys = []string{"unb", "cookie2", "cookie17", "lgc", "sgcookie", "x5sec"}

// captchaCookieHash 计算登录态身份摘要，用于判断抑制窗口是否仍对应同一份凭据。
// 只对稳定身份键与验证通过标记做摘要（SHA-256 前 16 位），既不保存 Cookie 明文，
// 也能在人工粘贴验证后的 Cookie（出现或更新 x5sec）时立即重新允许自动尝试。
func captchaCookieHash(cookieStr string) string {
	// pairs 是按稳定身份键收集的 k=v 片段；键顺序固定，保证同一份凭据得到同一摘要。
	pairs := make([]string, 0, len(captchaIdentityCookieKeys))
	// key 是当前参与摘要的稳定身份键。
	for _, key := range captchaIdentityCookieKeys {
		// value、ok 是该键在登录态中的取值与是否存在；缺失说明这份凭据没有该标记。
		value, ok := readCookieValue(cookieStr, key)
		if !ok {
			continue
		}
		pairs = append(pairs, key+"="+value)
	}
	// sum 是稳定身份片段的 SHA-256 摘要。
	sum := sha256.Sum256([]byte(strings.Join(pairs, ";")))
	return hex.EncodeToString(sum[:])[:16]
}

// readCookieValue 从 Cookie 串中读取指定键的值；键不存在或片段缺少分隔符时返回 false。
func readCookieValue(cookieStr, key string) (string, bool) {
	// field 是当前遍历到的 Cookie 片段。
	for _, field := range strings.Split(cookieStr, ";") {
		// name、value、found 是该片段的键、值与是否存在分隔符。
		name, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		if strings.TrimSpace(name) == key {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

// markCaptchaRecoveryFailure 记录一次自动验证失败并开启抑制窗口；cookieStr 是本次尝试使用的登录态。
func (m *Manager) markCaptchaRecoveryFailure(cookieID, cookieStr string) {
	m.captchaGuardMu.Lock()
	defer m.captchaGuardMu.Unlock()
	if m.captchaFailedAt == nil {
		m.captchaFailedAt = make(map[string]time.Time)
	}
	m.captchaFailedAt[cookieID] = time.Now()
	if m.captchaFailedCookieHash == nil {
		m.captchaFailedCookieHash = make(map[string]string)
	}
	m.captchaFailedCookieHash[cookieID] = captchaCookieHash(cookieStr)
}

// clearCaptchaRecoveryFailure 清除抑制记录（自动验证成功时调用）。
func (m *Manager) clearCaptchaRecoveryFailure(cookieID string) {
	m.captchaGuardMu.Lock()
	defer m.captchaGuardMu.Unlock()
	delete(m.captchaFailedAt, cookieID)
	// 同时清除摘要记录，避免成功后再被同一账号的陈旧摘要影响后续判断。
	delete(m.captchaFailedCookieHash, cookieID)
}

// logCompetitorDetailRecoverySuppressed 记录自动验证被抑制窗口跳过的信息。
func (m *Manager) logCompetitorDetailRecoverySuppressed(itemID, punishURL string) {
	// logger 是浏览器管理器日志器；缺失时退回默认日志器。
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("竞品详情风控自动验证处于抑制窗口，本次不再尝试", "item_id", itemID,
		"punish_url", sanitizeDiagnosticURL(punishURL), "cooldown_minutes", int(competitorDetailCaptchaCooldown.Minutes()))
}

// findCompetitorDetailPunishURL 从详情响应正文中提取平台处罚地址原始值（保留 query）。
// 该地址只交给会话内验证流程使用，调用方必须避免把它写入日志或错误文本。
func findCompetitorDetailPunishURL(body string) string {
	// normalized 是去掉 JSON 转义斜杠后的正文，保证转义写法同样可提取。
	normalized := strings.ReplaceAll(body, "\\/", "/")
	// start 是下一次搜索的起点。
	start := 0
	for {
		// index 是本次检索到的 http 起始位置。
		index := strings.Index(normalized[start:], "http")
		if index < 0 {
			return ""
		}
		// absolute 是候选地址在原文中的绝对位置。
		absolute := start + index
		// end 是候选地址结束位置（遇到 JSON 分隔符或空白即结束）。
		end := absolute
		for end < len(normalized) && !isCompetitorDetailURLTerminator(rune(normalized[end])) {
			end++
		}
		// candidate 是候选地址原文（含 query）。
		candidate := normalized[absolute:end]
		if strings.Contains(candidate, competitorDetailPunishMarker) || strings.Contains(strings.ToLower(candidate), "punish") {
			return candidate
		}
		start = end
	}
}

// logCompetitorDetailRecoveryFailure 记录详情风控自动验证失败。
// 除去除 query 的处罚地址外，按冻结验证码规范在最终失败时给出完整的人工验证地址
// （verification_url），供人工在本机浏览器完成验证后接管；该字段只在自动验证最终失败时出现。
func (m *Manager) logCompetitorDetailRecoveryFailure(itemID, punishURL string, err error) {
	// logger 是浏览器管理器日志器；缺失时退回默认日志器，保证失败可见。
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("竞品详情风控自动验证失败，保持人工接管", "item_id", itemID,
		"punish_url", sanitizeDiagnosticURL(punishURL), "verification_url", punishURL, "err", err)
}

// logCompetitorDetailRecoverySuccess 记录详情风控自动验证成功与使用的引擎。
func (m *Manager) logCompetitorDetailRecoverySuccess(itemID, captchaEngine string) {
	// logger 是浏览器管理器日志器；缺失时退回默认日志器。
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("竞品详情风控自动验证成功，已用新 Cookie 重试", "item_id", itemID, "captcha_engine", captchaEngine)
}

// competitorDetailDiagnostic 是一次触发风控或会话失效的响应定位信息。
// 该结构只保留地址（已去除 query 与 fragment）、状态与平台返回码，不承载任何凭证。
type competitorDetailDiagnostic struct {
	// Status 是响应的 HTTP 状态码。
	Status int
	// URL 是响应地址，已去除 query 与 fragment。
	URL string
	// Rets 是平台返回码文本。
	Rets []string
	// CandidateURLs 是响应正文中疑似验证或处罚地址，最多保留 3 条且已去除 query。
	CandidateURLs []string
}

// competitorDetailURLCandidateLimit 是单条响应保留的疑似验证地址数量上限。
const competitorDetailURLCandidateLimit = 3

// competitorDetailURLCandidateKeywords 是正文中判定为疑似验证或处罚地址的关键字。
var competitorDetailURLCandidateKeywords = []string{"punish", "captcha", "x5sec", "verify", "verification", "validate"}

// sanitizeDiagnosticURL 只保留地址的 scheme、host 与 path，去掉 query 与 fragment。
// 该函数保证诊断日志不会携带可能含令牌的查询参数。
func sanitizeDiagnosticURL(raw string) string {
	// trimmed 是去空白后的地址。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// withoutFragment 是去掉 fragment 后的地址。
	withoutFragment, fragmentIndex := trimmed, strings.IndexByte(trimmed, '#')
	if fragmentIndex >= 0 {
		withoutFragment = trimmed[:fragmentIndex]
	}
	// withoutQuery 是去掉 query 后的地址。
	if queryIndex := strings.IndexByte(withoutFragment, '?'); queryIndex >= 0 {
		return withoutFragment[:queryIndex]
	}
	return withoutFragment
}

// findVerificationURLs 从响应正文中提取疑似验证或处罚地址，最多返回 competitorDetailURLCandidateLimit 条。
func findVerificationURLs(body []byte) []string {
	// text 是响应正文文本；详情接口响应为 JSON，直接按子串扫描即可。
	text := string(body)
	// out 是按出现顺序收集的候选地址。
	out := make([]string, 0, competitorDetailURLCandidateLimit)
	// start 是下一次搜索的起点。
	start := 0
	for len(out) < competitorDetailURLCandidateLimit {
		// index 是本次检索到的 http 起始位置。
		index := strings.Index(text[start:], "http")
		if index < 0 {
			break
		}
		// absolute 是候选地址在原文中的绝对位置。
		absolute := start + index
		// end 是候选地址结束位置（遇到 JSON 分隔符或空白即结束）。
		end := absolute
		for end < len(text) && !isCompetitorDetailURLTerminator(rune(text[end])) {
			end++
		}
		// candidate 是去 query 后的候选地址。
		candidate := sanitizeDiagnosticURL(text[absolute:end])
		// lower 用于关键字匹配，避免大小写差异漏判。
		lower := strings.ToLower(candidate)
		// keyword 是当前待匹配的关键字。
		for _, keyword := range competitorDetailURLCandidateKeywords {
			if strings.Contains(lower, keyword) {
				out = append(out, candidate)
				break
			}
		}
		start = end
	}
	return out
}

// isCompetitorDetailURLTerminator 判断字符是否结束一个候选地址（JSON 分隔符或空白）。
func isCompetitorDetailURLTerminator(r rune) bool {
	switch r {
	case '"', '\'', '\\', ',', '}', ']':
		return true
	default:
		return unicode.IsSpace(r)
	}
}

// buildCompetitorDetailDiagnostic 组装一次风控响应的定位信息。
func buildCompetitorDetailDiagnostic(status int, responseURL string, body []byte, rets []string) competitorDetailDiagnostic {
	return competitorDetailDiagnostic{
		Status:        status,
		URL:           sanitizeDiagnosticURL(responseURL),
		Rets:          append([]string(nil), rets...),
		CandidateURLs: findVerificationURLs(body),
	}
}

// logCompetitorDetailDiagnostics 记录触发风控或会话失效时的响应定位信息。
// 只记录地址、状态与返回码；地址已去除 query，不含任何凭证或令牌参数。
func (m *Manager) logCompetitorDetailDiagnostics(itemID string, diagnostics []competitorDetailDiagnostic) {
	if len(diagnostics) == 0 {
		return
	}
	// first 是本次调用中第一条触发风控的响应诊断。
	first := diagnostics[0]
	// logger 是浏览器管理器日志器；缺失时退回默认日志器，保证失败始终可见。
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("竞品详情返回风控或会话失效", "item_id", itemID, "responses", len(diagnostics),
		"status", first.Status, "response_url", first.URL,
		"rets", strings.Join(first.Rets, " | "),
		"candidate_urls", strings.Join(first.CandidateURLs, " | "))
}
