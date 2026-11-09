// Package items: 我的商品统计服务——按需抓取浏览量/想要量等公开计数，并负责缓存与批量限流。
// 真正的约束是平台请求次数：单轮批量有硬上限、串行执行、命中风控立即停止且保留已抓结果。
package items

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// 商品统计相关常量。
const (
	// ItemStatsCacheTTL 是统计缓存有效期；24 小时内列表与批量刷新都复用已有数据。
	ItemStatsCacheTTL = 24 * time.Hour
	// ItemStatsBatchLimit 是单轮批量刷新的硬上限。
	// 该值由服务端强制：无论调用方传入多大都会被截断，避免一次点击产生过多平台请求。
	ItemStatsBatchLimit = 10
)

// 商品统计服务错误哨兵；抓取类错误由适配层从本机助手场景错误翻译而来。
var (
	// ErrItemStatsNotOwned 表示目标商品不属于指定账号。
	ErrItemStatsNotOwned = errors.New("商品不属于指定账号")
	// ErrItemStatsNoItems 表示批量刷新没有收到任何商品标识。
	ErrItemStatsNoItems = errors.New("批量刷新需要至少一个商品")
	// ErrItemStatsUnavailable 表示本机抓取助手不可用（未配置、未启动或隧道断开）。
	ErrItemStatsUnavailable = errors.New("本机抓取助手不可用")
	// ErrItemStatsRiskControl 表示平台要求人机验证；批量刷新必须立即停止。
	ErrItemStatsRiskControl = errors.New("闲鱼要求人机验证，请在助手浏览器中完成验证")
	// ErrItemStatsNotLoggedIn 表示助手专用浏览器尚未登录闲鱼。
	ErrItemStatsNotLoggedIn = errors.New("抓取助手尚未登录闲鱼")
	// ErrItemStatsUnauthorized 表示助手拒绝了本次调用（共享令牌不匹配）。
	ErrItemStatsUnauthorized = errors.New("抓取助手拒绝了本次调用")
)

// ItemStatsStopRiskControl 是批量刷新因平台风控停止时的停止原因。
const ItemStatsStopRiskControl = "risk_control"

// ItemStatsCounts 是一次抓取得到的四项公开计数；平台未提供某字段时该项为 0。
type ItemStatsCounts struct {
	// BrowseCnt 是浏览量。
	BrowseCnt int64
	// WantCnt 是想要人数。
	WantCnt int64
	// CollectCnt 是收藏数。
	CollectCnt int64
	// SoldCnt 是已售数。
	SoldCnt int64
}

// ItemStats 是应用层商品统计模型（与持久层模型解耦）。
type ItemStats struct {
	// CookieID 是统计所属账号。
	CookieID string
	// ItemID 是平台商品标识。
	ItemID string
	// BrowseCnt 是浏览量。
	BrowseCnt int64
	// WantCnt 是想要人数。
	WantCnt int64
	// CollectCnt 是收藏数。
	CollectCnt int64
	// SoldCnt 是已售数。
	SoldCnt int64
	// FetchedAt 是最近一次成功抓取时间；零值表示尚未抓取。
	FetchedAt time.Time
	// Fresh 表示本次返回的数据是否仍在缓存有效期内，前端据此决定是否提示刷新。
	Fresh bool
}

// StatsBatchResult 是批量刷新结果。
type StatsBatchResult struct {
	// Refreshed 是本次成功刷新的统计，按抓取顺序排列。
	Refreshed []ItemStats
	// Skipped 是本次因数据仍新鲜而跳过的商品数量。
	Skipped int
	// Remaining 是候选集合中被上限截断、留待下次刷新的商品数量。
	Remaining int
	// Stopped 表示是否因平台风控提前停止。
	Stopped bool
	// StopReason 是停止原因；Stopped 为 true 时非空。
	StopReason string
}

// ItemStatsFetcher 是商品统计抓取端口；实现方保证单次调用只抓取一个商品，不做任何扇出。
type ItemStatsFetcher interface {
	// FetchItemStats 抓取单个商品的公开计数；返回应用层自有错误哨兵。
	FetchItemStats(ctx context.Context, itemID string) (ItemStatsCounts, error)
}

// ItemStatsRepository 是商品统计持久化窄接口（转换在 adapter 完成）。
type ItemStatsRepository interface {
	// ListItemStats 一次查出某账号下一批商品的统计；未抓取的商品不出现在结果中。
	ListItemStats(ctx context.Context, cookieID string, itemIDs []string) (map[string]ItemStats, error)
	// UpsertItemStats 按 (账号, 商品) 写入或覆盖一条统计。
	UpsertItemStats(ctx context.Context, stats ItemStats) error
}

// ItemStatsOwnershipPort 复用账号归属校验能力，避免统计端点为他人账号取数。
type ItemStatsOwnershipPort interface {
	// CookieOwned 判断账号是否属于指定用户。
	CookieOwned(ctx context.Context, userID int64, cookieID string) (bool, error)
}

// ItemStatsService 编排单商品强制刷新与单轮限量批量刷新。
type ItemStatsService struct {
	// store 是统计持久化窄接口。
	store ItemStatsRepository
	// fetcher 是抓取端口，由本机助手适配器实现。
	fetcher ItemStatsFetcher
	// ownership 是账号归属校验端口。
	ownership ItemStatsOwnershipPort
	// now 返回当前时间，测试可替换以覆盖缓存边界。
	now func() time.Time
	// ttl 是缓存有效期。
	ttl time.Duration
}

// NewItemStatsService 构造商品统计服务；依赖缺失时直接失败，避免出现半可用状态。
func NewItemStatsService(store ItemStatsRepository, fetcher ItemStatsFetcher, ownership ItemStatsOwnershipPort) (*ItemStatsService, error) {
	if store == nil || fetcher == nil || ownership == nil {
		return nil, errors.New("商品统计服务依赖不完整")
	}
	return &ItemStatsService{store: store, fetcher: fetcher, ownership: ownership, now: time.Now, ttl: ItemStatsCacheTTL}, nil
}

// RefreshItemStats 强制刷新单个商品的统计：忽略缓存，成功抓取后立即落库。
// 用户点击「刷新数据」就是明确要求最新值，因此这里不做新鲜度判断。
func (s *ItemStatsService) RefreshItemStats(ctx context.Context, userID int64, cookieID, itemID string) (ItemStats, error) {
	if // authErr 是账号归属校验失败原因。
	authErr := s.ensureOwned(ctx, userID, cookieID); authErr != nil {
		return ItemStats{}, authErr
	}
	// trimmedItemID 是去空白后的商品标识；为空时无法定位目标。
	trimmedItemID := strings.TrimSpace(itemID)
	if trimmedItemID == "" {
		return ItemStats{}, ErrItemStatsNoItems
	}
	// counts、fetchErr 是本次抓取结果与错误。
	counts, fetchErr := s.fetcher.FetchItemStats(ctx, trimmedItemID)
	if fetchErr != nil {
		return ItemStats{}, fetchErr
	}
	// stats 是本次抓取到的统计；抓取成功即视为最新，Fresh 恒为 true。
	stats := ItemStats{
		CookieID: strings.TrimSpace(cookieID), ItemID: trimmedItemID,
		BrowseCnt: counts.BrowseCnt, WantCnt: counts.WantCnt,
		CollectCnt: counts.CollectCnt, SoldCnt: counts.SoldCnt,
		FetchedAt: s.now(), Fresh: true,
	}
	if // saveErr 是统计落库失败原因。
	saveErr := s.store.UpsertItemStats(ctx, stats); saveErr != nil {
		return ItemStats{}, saveErr
	}
	return stats, nil
}

// RefreshStatsBatch 执行一轮限量批量刷新：
// 一次查出候选 → 跳过仍新鲜的 → 最旧优先 → 取前 limit 个 → 串行抓取 → 命中风控立即停止。
func (s *ItemStatsService) RefreshStatsBatch(ctx context.Context, userID int64, cookieID string, itemIDs []string, limit int) (StatsBatchResult, error) {
	if // authErr 是账号归属校验失败原因。
	authErr := s.ensureOwned(ctx, userID, cookieID); authErr != nil {
		return StatsBatchResult{}, authErr
	}
	// uniqueIDs 是去重后的目标商品集合；去重避免同一商品在一轮内被重复抓取。
	uniqueIDs := normalizeItemIDs(itemIDs)
	if len(uniqueIDs) == 0 {
		return StatsBatchResult{}, ErrItemStatsNoItems
	}
	// effectiveLimit 是本轮实际生效的上限；非法值一律退回硬上限，且不得超过硬上限。
	effectiveLimit := ItemStatsBatchLimit
	if limit > 0 && limit < ItemStatsBatchLimit {
		effectiveLimit = limit
	}
	// existing、listErr 是候选商品的现有统计与查询错误。
	existing, listErr := s.store.ListItemStats(ctx, cookieID, uniqueIDs)
	if listErr != nil {
		return StatsBatchResult{}, listErr
	}
	// pending 是需要刷新的商品（未抓过或已过期），filtered 是因仍新鲜而跳过的数量。
	pending, filtered := s.pendingCandidates(uniqueIDs, existing)
	// result 是本次批量结果；Refreshed 始终保持非 nil，便于前端直接遍历。
	result := StatsBatchResult{Refreshed: make([]ItemStats, 0, effectiveLimit), Skipped: filtered}
	if len(pending) > effectiveLimit {
		result.Remaining = len(pending) - effectiveLimit
		pending = pending[:effectiveLimit]
	}
	// itemID 是当前串行抓取的商品标识；顺序即最旧优先，便于断言与预期。
	for _, itemID := range pending {
		// ctxErr 表示调用方已取消，必须立即停止且不再发起新的助手调用。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		// counts、fetchErr 是本次抓取结果与错误。
		counts, fetchErr := s.fetcher.FetchItemStats(ctx, itemID)
		if fetchErr != nil {
			if errors.Is(fetchErr, ErrItemStatsRiskControl) {
				// 命中风控必须立即终止本批次：继续请求只会加重平台处罚，且已抓结果保留。
				result.Stopped, result.StopReason = true, ItemStatsStopRiskControl
				return result, nil
			}
			// 助手不可用、未登录、令牌错误属可操作故障，直接上抛让前端给出处理指引。
			return result, fetchErr
		}
		// stats 是本次抓取到的统计。
		stats := ItemStats{
			CookieID: strings.TrimSpace(cookieID), ItemID: itemID,
			BrowseCnt: counts.BrowseCnt, WantCnt: counts.WantCnt,
			CollectCnt: counts.CollectCnt, SoldCnt: counts.SoldCnt,
			FetchedAt: s.now(), Fresh: true,
		}
		if // saveErr 是统计落库失败原因。
		saveErr := s.store.UpsertItemStats(ctx, stats); saveErr != nil {
			return result, saveErr
		}
		result.Refreshed = append(result.Refreshed, stats)
	}
	return result, nil
}

// ItemStatsRef 是一条「账号 + 商品」引用，供列表接口一次提交全量待展示统计。
type ItemStatsRef struct {
	// CookieID 是商品归属账号。
	CookieID string
	// ItemID 是平台商品标识。
	ItemID string
}

// ItemStatsKey 构造统计索引键。
// 同一平台商品可能出现在不同账号下，因此键必须同时包含账号，不能只用商品标识。
func ItemStatsKey(cookieID, itemID string) string {
	return cookieID + "\x00" + itemID
}

// ListItemStats 读取一批商品的统计供列表展示，并按当前时间标注是否仍在缓存有效期内。
func (s *ItemStatsService) ListItemStats(ctx context.Context, cookieID string, itemIDs []string) (map[string]ItemStats, error) {
	// rows、listErr 是持久层返回的统计与错误。
	rows, listErr := s.store.ListItemStats(ctx, cookieID, itemIDs)
	if listErr != nil {
		return nil, listErr
	}
	// now 是本轮的新鲜度判定基准；同一批数据必须用同一时刻判定，避免逐条漂移。
	now := s.now()
	// itemID 是当前待标注的商品标识。
	for itemID, stats := range rows {
		stats.Fresh = isFreshAt(stats.FetchedAt, now, s.ttl)
		rows[itemID] = stats
	}
	return rows, nil
}

// ListItemStatsForItems 按账号分组读取统计，返回以 ItemStatsKey 为键的映射，供列表接口一次取回展示数据。
// 分组只产生本机数据库查询，不产生任何平台请求；同一账号只查一次。
func (s *ItemStatsService) ListItemStatsForItems(ctx context.Context, refs []ItemStatsRef) (map[string]ItemStats, error) {
	// grouped 是按账号分组的商品标识集合。
	grouped := make(map[string][]string)
	// ref 是本次待分组的「账号 + 商品」引用。
	for _, ref := range refs {
		// cookieID、itemID 是去空白后的引用键组成部分；任一为空都无法定位统计。
		cookieID, itemID := strings.TrimSpace(ref.CookieID), strings.TrimSpace(ref.ItemID)
		if cookieID == "" || itemID == "" {
			continue
		}
		grouped[cookieID] = append(grouped[cookieID], itemID)
	}
	// out 是合并后的结果映射。
	out := make(map[string]ItemStats)
	// cookieID 是当前分组的账号标识；itemIDs 是该账号下的商品标识集合。
	for cookieID, itemIDs := range grouped {
		// rows、err 是该账号的统计查询结果与错误。
		rows, err := s.ListItemStats(ctx, cookieID, itemIDs)
		if err != nil {
			return nil, err
		}
		// itemID 是当前分组的商品标识；stats 是它的统计。
		for itemID, stats := range rows {
			out[ItemStatsKey(cookieID, itemID)] = stats
		}
	}
	return out, nil
}

// ensureOwned 校验账号归属；不归属时返回场景错误，避免为他人账号取数。
func (s *ItemStatsService) ensureOwned(ctx context.Context, userID int64, cookieID string) error {
	// owned、err 是账号归属校验结果与错误。
	owned, err := s.ownership.CookieOwned(ctx, userID, cookieID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrItemStatsNotOwned
	}
	return nil
}

// pendingCandidates 从目标集合中筛出需要刷新的商品并按抓取时间升序排列（未抓过的排最前）。
// 返回值分别是候选集合与因数据仍新鲜而跳过的数量。
func (s *ItemStatsService) pendingCandidates(itemIDs []string, existing map[string]ItemStats) ([]string, int) {
	// now 是本轮的新鲜度判定基准。
	now := s.now()
	// pending 是待刷新（未抓取或已过期）的商品集合。
	pending := make([]string, 0, len(itemIDs))
	// skipped 是数据仍新鲜、无需刷新的商品数量。
	skipped := 0
	// itemID 是当前判定的商品标识。
	for _, itemID := range itemIDs {
		// stats、ok 是该商品的现有统计与是否存在。
		stats, ok := existing[itemID]
		if ok && isFreshAt(stats.FetchedAt, now, s.ttl) {
			skipped++
			continue
		}
		pending = append(pending, itemID)
	}
	// 最旧优先：零值时间（从未抓取）天然排在已抓取数据之前。
	sort.SliceStable(pending, func(i, j int) bool {
		return existing[pending[i]].FetchedAt.Before(existing[pending[j]].FetchedAt)
	})
	return pending, skipped
}

// isFreshAt 判断抓取时间是否仍在 ttl 有效期内；零值时间一律视为不新鲜。
func isFreshAt(fetchedAt, now time.Time, ttl time.Duration) bool {
	if fetchedAt.IsZero() {
		return false
	}
	return now.Sub(fetchedAt) < ttl
}

// normalizeItemIDs 去空白、去重并丢弃空值，保证批量抓取不会重复消耗平台请求。
func normalizeItemIDs(itemIDs []string) []string {
	// out 是归一化后的商品标识集合。
	out := make([]string, 0, len(itemIDs))
	// seen 记录已收集的商品标识，用于 O(1) 去重。
	seen := make(map[string]struct{}, len(itemIDs))
	// itemID 是本次待归一的商品标识，可能重复或为空串。
	for _, itemID := range itemIDs {
		// trimmed 是去空白后的商品标识。
		trimmed := strings.TrimSpace(itemID)
		if trimmed == "" {
			continue
		}
		// ok 表示该商品标识此前是否已收集；重复时必须跳过，避免同轮重复抓取。
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
