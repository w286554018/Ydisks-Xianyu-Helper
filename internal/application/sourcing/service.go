package sourcing

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// beijingLocation 是快照归属日与每日调度使用的北京时间时区；用固定偏移避免依赖系统 tzdata。
var beijingLocation = time.FixedZone("CST", 8*3600)

// validOrderStatuses 是畅销榜纳入统计的订单状态集合，与订单分析口径保持一致（含历史数字码别名）。
var validOrderStatuses = map[string]struct{}{
	"pending_ship": {},
	"paid":         {},
	"2":            {},
	"shipped":      {},
	"3":            {},
	"completed":    {},
	"4":            {},
	"11":           {},
}

// Service 编排选品参考用例，不感知 HTTP、数据库连接或浏览器实现细节。
type Service struct {
	// repository 提供快照与订单原始数据。
	repository Repository
	// browser 提供浏览器态数据采集能力；为空表示浏览器不可用，相关用例必须明确降级。
	browser BrowserGateway
	// credentials 提供账号 cookie 的解密读取。
	credentials CredentialReader
	// chat 提供 AI 文案补全；为空表示 AI 未配置。
	chat ChatClient
	// now 返回当前时间，测试可注入确定时钟。
	now func() time.Time

	// cooldownMu 保护 cooldownUntil，键为账号与关键词组合。
	cooldownMu sync.Mutex
	// cooldownUntil 记录每个账号关键词组合允许再次查询的时间点。
	cooldownUntil map[string]time.Time
}

// NewService 构造选品参考应用服务；browser、credentials、chat 允许为空，对应能力按降级处理。
func NewService(repository Repository, browser BrowserGateway, credentials CredentialReader, chat ChatClient) *Service {
	return &Service{
		repository:    repository,
		browser:       browser,
		credentials:   credentials,
		chat:          chat,
		now:           time.Now,
		cooldownUntil: make(map[string]time.Time),
	}
}

// WithClock 注入自定义时钟，仅用于测试确定性时间断言。
func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

// TodayDate 返回当前北京时间归属日，格式 YYYY-MM-DD。
func (s *Service) TodayDate() string {
	// moment 是当前时刻，按北京时间归属日格式化。
	moment := s.now().In(beijingLocation)
	return moment.Format("2006-01-02")
}

// cutoffDate 返回保留策略允许的最早快照日期。
func (s *Service) cutoffDate() string {
	// start 是保留窗口起点，含当天共保留 SnapshotRetentionDays 天。
	start := s.now().In(beijingLocation).AddDate(0, 0, -(SnapshotRetentionDays - 1))
	return start.Format("2006-01-02")
}

// HotKeywords 读取当日热词快照；没有快照时返回空词条且状态为空，界面据此提示尚未采集。
func (s *Service) HotKeywords(ctx context.Context) (HotKeywordView, error) {
	// date 是当前快照归属日。
	date := s.TodayDate()
	// snapshot、err 是当日快照读取结果。
	snapshot, err := s.repository.GetSnapshot(ctx, KindHotKeywords, date)
	if err != nil {
		if errors.Is(err, ErrSnapshotNotFound) {
			return HotKeywordView{Date: date, Keywords: []string{}, Status: ""}, nil
		}
		return HotKeywordView{}, err
	}
	// payload 是热词快照内容。
	var payload HotKeywordSnapshotPayload
	if snapshot.PayloadJSON != "" {
		// decodeErr 表示快照内容不是合法 JSON；此时按空词条处理，避免把损坏数据展示给用户。
		if decodeErr := json.Unmarshal([]byte(snapshot.PayloadJSON), &payload); decodeErr != nil {
			payload.Keywords = nil
		}
	}
	if payload.Keywords == nil {
		payload.Keywords = []string{}
	}
	return HotKeywordView{
		Date:         date,
		Keywords:     payload.Keywords,
		Status:       snapshot.Status,
		ErrorMessage: snapshot.ErrorMessage,
	}, nil
}

// RefreshHotKeywords 采集一次平台热词并写入当日快照；失败时写入失败快照并返回错误。
// 该路径使用游客身份，不携带任何账号凭证。
func (s *Service) RefreshHotKeywords(ctx context.Context) error {
	// date 是本次采集归属日。
	date := s.TodayDate()
	if s.browser == nil {
		s.saveSnapshot(ctx, KindHotKeywords, date, HotKeywordSnapshotPayload{}, StatusFailed, ErrBrowserUnavailable.Error())
		return ErrBrowserUnavailable
	}
	// keywords、collectErr 是本次采集结果与错误。
	keywords, collectErr := s.browser.FetchHotKeywords(ctx)
	if collectErr != nil {
		s.saveSnapshot(ctx, KindHotKeywords, date, HotKeywordSnapshotPayload{}, StatusFailed, collectErr.Error())
		return mapBrowserError(collectErr)
	}
	if len(keywords) == 0 {
		s.saveSnapshot(ctx, KindHotKeywords, date, HotKeywordSnapshotPayload{}, StatusFailed, ErrHotKeywordUnavailable.Error())
		return ErrHotKeywordUnavailable
	}
	s.saveSnapshot(ctx, KindHotKeywords, date, HotKeywordSnapshotPayload{Keywords: keywords}, StatusOK, "")
	s.pruneSnapshots(ctx)
	return nil
}

// saveSnapshot 序列化并写入快照；写入失败只记录在返回值中，不阻断调用方主流程。
func (s *Service) saveSnapshot(ctx context.Context, kind, date string, payload any, status, errorMessage string) {
	// encoded 是序列化后的快照内容；编码失败时退化为空对象，避免写入非法 JSON。
	encoded, encodeErr := json.Marshal(payload)
	if encodeErr != nil {
		encoded = []byte("{}")
	}
	_ = s.repository.SaveSnapshot(ctx, Snapshot{
		Kind:         kind,
		Date:         date,
		PayloadJSON:  string(encoded),
		Status:       status,
		ErrorMessage: errorMessage,
	})
}

// pruneSnapshots 清理超出保留窗口的快照；清理失败不影响已写入的结果。
func (s *Service) pruneSnapshots(ctx context.Context) {
	_ = s.repository.DeleteSnapshotsBefore(ctx, s.cutoffDate())
}

// ShopRanking 按当前用户实时聚合本地订单得到畅销榜；只读本地数据库，不产生任何平台请求。
// 不使用全局快照，避免多用户之间互相覆盖或读到他人数据。
func (s *Service) ShopRanking(ctx context.Context, userID int64, window string) (ShopRankingView, error) {
	// normalized 是归一化后的窗口标识，无法识别时按 7 天处理。
	normalized := NormalizeRankingWindow(window)
	// entries、err 是当前用户在窗口内的聚合结果。
	entries, err := s.ComputeShopRanking(ctx, userID, normalized)
	if err != nil {
		return ShopRankingView{}, err
	}
	if entries == nil {
		entries = []RankingEntry{}
	}
	return ShopRankingView{Date: s.TodayDate(), Window: normalized, Entries: entries, Status: StatusOK}, nil
}

// ComputeShopRanking 按窗口聚合本店订单为畅销榜条目；聚合在内存完成以保证三种方言口径一致。
func (s *Service) ComputeShopRanking(ctx context.Context, userID int64, window string) ([]RankingEntry, error) {
	// normalized 是归一化窗口标识。
	normalized := NormalizeRankingWindow(window)
	// since 是统计窗口起点，按北京时间解释后换算为时间点。
	since := s.now().In(beijingLocation).AddDate(0, 0, -windowDays(normalized))
	// rows、err 是窗口内的订单原始行。
	rows, err := s.repository.ListOrderRows(ctx, userID, since)
	if err != nil {
		return nil, err
	}
	return aggregateRanking(rows), nil
}

// NormalizeRankingWindow 归一化榜单窗口标识，未知值按 7 天处理。
func NormalizeRankingWindow(window string) string {
	// trimmed 是去空白后的窗口标识。
	trimmed := strings.TrimSpace(window)
	if trimmed == RankingWindow30Days {
		return RankingWindow30Days
	}
	return RankingWindow7Days
}

// windowDays 返回窗口标识对应的天数。
func windowDays(window string) int {
	if window == RankingWindow30Days {
		return 29
	}
	return 6
}

// aggregateRanking 把订单原始行聚合为按销量与营收排序的排行榜条目。
func aggregateRanking(rows []OrderRow) []RankingEntry {
	// byItem 汇总每个商品的累计指标。
	byItem := make(map[string]*RankingEntry, 16)
	// titles 保存商品标题，避免每次遍历都覆盖已有标题。
	titles := make(map[string]string, 16)
	// row 是当前遍历到的订单行。
	for _, row := range rows {
		// status 是归一化后的订单状态文本。
		status := strings.ToLower(strings.TrimSpace(row.OrderStatus))
		// ok 表示订单状态属于已付款之后的有效状态，未付款或已取消订单不计入排行。
		if _, ok := validOrderStatuses[status]; !ok {
			continue
		}
		// itemID 是订单所属商品标识，空值无法形成排行条目。
		itemID := strings.TrimSpace(row.ItemID)
		if itemID == "" {
			continue
		}
		// entry 是当前商品的累计条目。
		entry, ok := byItem[itemID]
		if !ok {
			entry = &RankingEntry{ItemID: itemID}
			byItem[itemID] = entry
		}
		// title 是订单行携带的商品标题，仅在首次非空时记录。
		title := strings.TrimSpace(row.ItemTitle)
		if title != "" {
			titles[itemID] = title
		}
		// quantity 是安全解析后的数量；amount 是安全解析后的金额。
		quantity := parseDecimal(row.Quantity)
		// amount 是安全解析后的订单金额（元），非数字按 0 计入营收。
		amount := parseDecimal(row.Amount)
		entry.SoldQuantity += int(quantity)
		entry.Revenue += amount
		entry.OrderCount++
	}
	// out 汇总排序后的排行条目。
	out := make([]RankingEntry, 0, len(byItem))
	// id、entry 是当前遍历到的商品与条目。
	for id, entry := range byItem {
		entry.ItemTitle = titles[id]
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SoldQuantity != out[j].SoldQuantity {
			return out[i].SoldQuantity > out[j].SoldQuantity
		}
		return out[i].Revenue > out[j].Revenue
	})
	if len(out) > rankingLimit {
		out = out[:rankingLimit]
	}
	return out
}

// parseDecimal 把订单中的文本数量或金额转换为非负浮点数，非数字内容按 0 处理。
func parseDecimal(raw string) float64 {
	// trimmed 是去空白后的原始文本。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	// value 是解析结果；err 表示文本不是合法数字。
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// SearchItems 校验冷却后以账号登录态执行单次关键词查询。
// 每次调用最多产生 1 次搜索请求，冷却期内直接拒绝而不发出请求。
func (s *Service) SearchItems(ctx context.Context, userID int64, cookieID, keyword string) ([]SearchItem, error) {
	// trimmedKeyword 是去空白后的关键词。
	trimmedKeyword := strings.TrimSpace(keyword)
	if trimmedKeyword == "" {
		return nil, ErrEmptyKeyword
	}
	if s.browser == nil || s.credentials == nil {
		return nil, ErrBrowserUnavailable
	}
	// cooldownKey 是账号与关键词的组合键，用于独立冷却。
	cooldownKey := strings.TrimSpace(cookieID) + "\x00" + trimmedKeyword
	if !s.beginSearch(cooldownKey) {
		return nil, ErrSearchCooling
	}
	// cookieStr、credErr 是账号解密后的 cookie 文本，只在本次调用内存中使用。
	cookieStr, credErr := s.credentials.ReadCookie(ctx, userID, cookieID)
	if credErr != nil {
		return nil, credErr
	}
	// items、searchErr 是本次查询结果与错误。
	items, searchErr := s.browser.SearchItems(ctx, cookieID, cookieStr, trimmedKeyword)
	if searchErr != nil {
		return nil, mapBrowserError(searchErr)
	}
	if len(items) == 0 {
		return nil, ErrNoResult
	}
	return items, nil
}

// beginSearch 尝试占用冷却窗口；冷却未结束时返回 false 且不修改状态。
func (s *Service) beginSearch(key string) bool {
	// now 是当前时间点。
	now := s.now()
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	// until 是该账号关键词组合允许再次查询的时间点；ok 表示此前查询过、存在冷却记录。
	if until, ok := s.cooldownUntil[key]; ok && now.Before(until) {
		return false
	}
	s.cooldownUntil[key] = now.Add(SearchCooldown)
	return true
}

// mapBrowserError 把浏览器层错误映射为应用层领域错误，保持对外提示与状态码稳定。
func mapBrowserError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrRiskControl):
		return ErrRiskControl
	case errors.Is(err, ErrSessionExpired):
		return ErrSessionExpired
	case errors.Is(err, ErrNoResult):
		return ErrNoResult
	case errors.Is(err, ErrHotKeywordUnavailable):
		return ErrHotKeywordUnavailable
	default:
		return err
	}
}
