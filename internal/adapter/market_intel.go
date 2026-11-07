// Package adapter: 市场情报适配器——搜索端口、扫描持久化与错误分类的平台/数据库实现。
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	marketintel "xianyu-go/internal/application/marketintel"
	sourcingapp "xianyu-go/internal/application/sourcing"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
)

// MarketIntelSearchPort 把浏览器选品搜索包装成市场情报的 SearchPort。
// 复用既有 sourcing 通道（单次 1 请求、30 条上限、风控即停），不新增平台请求形态。
type MarketIntelSearchPort struct {
	// gateway 是既有选品浏览器网关。
	gateway *SourcingBrowserGateway
	// credentials 提供账号 cookie 的解密读取。
	credentials *SourcingCredentialReader
	// admin 是管理员账号目录，用于自动选择扫描账号。
	admin *db.AdminQueries
	// userID 是执行扫描的账号归属用户（系统任务使用管理员）。
	userID int64
	// cookieID 是显式指定的扫描账号；为空时自动选择第一个账号。
	cookieID string
}

// NewMarketIntelSearchPort 构造市场情报搜索端口；cookieID 为空时每次搜索自动选第一个账号。
func NewMarketIntelSearchPort(gateway *SourcingBrowserGateway, credentials *SourcingCredentialReader, admin *db.AdminQueries, userID int64, cookieID string) *MarketIntelSearchPort {
	return &MarketIntelSearchPort{gateway: gateway, credentials: credentials, admin: admin, userID: userID, cookieID: cookieID}
}

// resolveCookieID 返回本次搜索使用的账号标识：显式配置优先，否则自动选第一个账号。
func (p *MarketIntelSearchPort) resolveCookieID(ctx context.Context) (string, error) {
	// trimmed 是去空白后的显式账号。
	trimmed := strings.TrimSpace(p.cookieID)
	if trimmed != "" {
		return trimmed, nil
	}
	if p.admin == nil {
		return "", errors.New("市场情报扫描账号未配置且无账号目录")
	}
	// cookies 是全部账号摘要。
	cookies, err := p.admin.ListCookies(ctx)
	if err != nil {
		return "", fmt.Errorf("读取账号目录失败: %w", err)
	}
	if len(cookies) == 0 {
		return "", errors.New("没有可用的闲鱼账号，请先在账号管理添加并登录")
	}
	return cookies[0].ID, nil
}

// SearchKeyword 以扫描账号的登录态执行单词搜索；错误语义与 sourcing 一致。
func (p *MarketIntelSearchPort) SearchKeyword(ctx context.Context, keyword string) ([]marketintel.SearchResultItem, error) {
	// cookieID 是本次搜索使用的账号标识。
	cookieID, err := p.resolveCookieID(ctx)
	if err != nil {
		return nil, err
	}
	// cookieStr 是解密后的登录态；读取失败直接返回错误。
	cookieStr, err := p.credentials.ReadCookie(ctx, p.userID, cookieID)
	if err != nil {
		return nil, fmt.Errorf("读取扫描账号登录态失败: %w", err)
	}
	// items 是浏览器搜索结果。
	items, err := p.gateway.SearchItems(ctx, cookieID, cookieStr, keyword)
	if err != nil {
		return nil, err
	}
	// out 是转换后的市场情报搜索结果。
	out := make([]marketintel.SearchResultItem, 0, len(items))
	// item 是当前遍历的搜索结果。
	for _, item := range items {
		out = append(out, marketintel.SearchResultItem{
			ItemID: item.ItemID, Title: item.Title, Price: item.Price,
			WantCountText: item.WantCount, SellerNickname: item.SellerNickname,
			PublishTime: item.PublishTime, RawJSON: item.DetailURL,
		})
	}
	return out, nil
}

// parseWantCount 把「N人想要」/「N.N万人想要」文本解析为整数；无法解析返回 0。
func parseWantCount(text string) int {
	// trimmed 是去空白后的原始文本。
	trimmed := strings.TrimSpace(text)
	trimmed = strings.ReplaceAll(trimmed, "人想要", "")
	trimmed = strings.ReplaceAll(trimmed, "想要", "")
	trimmed = strings.ReplaceAll(trimmed, ",", "")
	if trimmed == "" {
		return 0
	}
	if strings.HasSuffix(trimmed, "万") {
		// wanPart 是「万」前的数值文本。
		wanPart := strings.TrimSuffix(trimmed, "万")
		// wanValue 是万为单位的小数值。
		var wanValue float64
		// err 是本次数据库操作错误。
		if _, err := fmt.Sscanf(wanPart, "%f", &wanValue); err != nil {
			return 0
		}
		return int(wanValue * 10000)
	}
	// value 是直读的整数值。
	var value int
	// err 是本次数据库操作错误。
	if _, err := fmt.Sscanf(trimmed, "%d", &value); err != nil {
		return 0
	}
	return value
}

// MarketIntelScanStore 把 db 仓储适配为扫描编排需要的持久化窄接口。
type MarketIntelScanStore struct {
	// store 是数据库仓储聚合入口。
	store *db.Store
}

// NewMarketIntelScanStore 构造扫描持久化适配器。
func NewMarketIntelScanStore(store *db.Store) *MarketIntelScanStore {
	return &MarketIntelScanStore{store: store}
}

// SnapshotExists 判断某词某天是否已有商品快照（幂等检查）。
func (a *MarketIntelScanStore) SnapshotExists(ctx context.Context, keyword, snapshotDate string) (bool, error) {
	// rows 是该词当天的快照行。
	rows, _, err := a.store.MarketIntel.ListSnapshotsWithVerdicts(ctx, keyword, snapshotDate)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// SaveScanRecord 落库一次扫描：快照、判定与聚合；Items 为空时只补判定与聚合。
func (a *MarketIntelScanStore) SaveScanRecord(ctx context.Context, record marketintel.ScanRecord) error {
	// 若本次带商品结果，先写快照。
	if len(record.Items) > 0 {
		// rows 是待写入的快照行。
		rows := make([]db.MarketIntelSnapshotRow, 0, len(record.Items))
		// item 是当前商品。
		for _, item := range record.Items {
			rows = append(rows, db.MarketIntelSnapshotRow{
				Keyword: record.Keyword, SnapshotDate: record.SnapshotDate,
				ItemID: item.ItemID, Title: item.Title, Price: item.Price,
				WantCount:      parseWantCount(item.WantCountText),
				WantCountText:  item.WantCountText,
				SellerNickname: item.SellerNickname, PublishTime: item.PublishTime,
				RawJSON: item.RawJSON,
			})
		}
		// err 是本次数据库操作错误。
		if _, err := a.store.MarketIntel.SaveSnapshots(ctx, rows); err != nil {
			return err
		}
	}
	// 写判定结果（补判定与首次判定共用）。
	if len(record.Verdicts) > 0 {
		// verdictRows 是待写入的判定行。
		verdictRows := make([]db.MarketIntelVerdictRow, 0, len(record.Verdicts))
		// verdict 是当前判定。
		for _, verdict := range record.Verdicts {
			verdictRows = append(verdictRows, db.MarketIntelVerdictRow{
				SnapshotID: verdict.SnapshotID, IsAIRelated: verdict.IsAIRelated,
				Category: verdict.Category, Reason: verdict.Reason,
			})
		}
		// err 是本次数据库操作错误。
		if err := a.store.MarketIntel.SaveVerdicts(ctx, verdictRows); err != nil {
			return err
		}
	}
	return a.refreshAggregate(ctx, record.Keyword, record.SnapshotDate)
}

// refreshAggregate 重算某词某天的聚合行：读快照与判定，统计后 upsert。
func (a *MarketIntelScanStore) refreshAggregate(ctx context.Context, keyword, snapshotDate string) error {
	// snapshots、verdicts 是该词当天的快照与判定。
	snapshots, verdicts, err := a.store.MarketIntel.ListSnapshotsWithVerdicts(ctx, keyword, snapshotDate)
	if err != nil {
		return err
	}
	// verdictBySnapshot 是快照主键到判定的索引。
	verdictBySnapshot := make(map[int64]db.MarketIntelVerdictRow, len(verdicts))
	// verdict 遍历建索引。
	for _, verdict := range verdicts {
		verdictBySnapshot[verdict.SnapshotID] = verdict
	}
	// totalCount、aiCount、totalWant 是聚合统计值。
	totalCount, aiCount, totalWant := len(snapshots), 0, 0
	// categoryCounts 是类目分布计数。
	categoryCounts := make(map[string]int)
	// sellerStats 是卖家出现次数与价格合计。
	sellerCounts := make(map[string]int)
	// sellerPriceSum 是卖家价格合计（解析失败的行不计入）。
	sellerPriceSum := make(map[string]float64)
	// sellerSampleURLs 记录每个卖家最近一次出现的商品详情链接。
	sellerSampleURLs := make(map[string]string)
	// snapshot 是当前快照行。
	for _, snapshot := range snapshots {
		totalWant += snapshot.WantCount
		// verdict 是该快照的判定（可能不存在）。
		verdict, has := verdictBySnapshot[snapshot.ID]
		// has 表示该快照是否已有判定。
		if !has {
			continue
		}
		if verdict.IsAIRelated {
			aiCount++
			if strings.TrimSpace(verdict.Category) != "" {
				categoryCounts[verdict.Category]++
			}
		}
		if strings.TrimSpace(snapshot.SellerNickname) != "" {
			sellerCounts[snapshot.SellerNickname]++
			// price 是解析后的价格数值。
			var price float64
			// err 是本次数据库操作错误。
			if _, err := fmt.Sscanf(strings.ReplaceAll(strings.ReplaceAll(snapshot.Price, "¥", ""), ",", ""), "%f", &price); err == nil {
				sellerPriceSum[snapshot.SellerNickname] += price
			}
			// 商品链接作为卖家示例入口；raw_json 存的是详情页地址。
			if strings.TrimSpace(snapshot.RawJSON) != "" {
				sellerSampleURLs[snapshot.SellerNickname] = snapshot.RawJSON
			}
		}
	}
	// verdictStatus 依据是否有判定结果决定。
	verdictStatus := db.MarketIntelVerdictPending
	if len(verdicts) > 0 {
		verdictStatus = db.MarketIntelVerdictDone
	}
	// categoryJSON 与 topSellersJSON 是聚合的嵌套 JSON。
	categoryJSON := marshalMarketIntelJSON(categoryCounts)
	// topSellers 是按出现次数取前 5 的卖家。
	topSellers := buildTopSellers(sellerCounts, sellerPriceSum, sellerSampleURLs, 5)
	// topSellersJSON 是头部卖家统计的 JSON 文本。
	topSellersJSON := marshalMarketIntelJSON(topSellers)
	return a.store.MarketIntel.SaveAggregate(ctx, db.MarketIntelAggregateRow{
		Keyword: keyword, SnapshotDate: snapshotDate, TotalCount: totalCount,
		AICount: aiCount, TotalWant: totalWant, CategoryJSON: categoryJSON,
		TopSellersJSON: topSellersJSON, VerdictStatus: verdictStatus,
	})
}

// buildTopSellers 按出现次数取前 N 的卖家统计；sampleURLs 提供每个卖家的示例商品链接。
func buildTopSellers(counts map[string]int, priceSum map[string]float64, sampleURLs map[string]string, limit int) []marketintel.SellerStat {
	// out 是卖家统计集合。
	out := make([]marketintel.SellerStat, 0, len(counts))
	// nickname、count 遍历计数表。
	for nickname, count := range counts {
		// avgPrice 是平均价格文本。
		avgPrice := ""
		if priceSum[nickname] > 0 {
			avgPrice = fmt.Sprintf("%.2f", priceSum[nickname]/float64(count))
		}
		out = append(out, marketintel.SellerStat{Nickname: nickname, Appearances: count, AvgPrice: avgPrice, SampleURL: sampleURLs[nickname]})
	}
	// 简单选择排序取前 N；N 很小（5），无需引入排序库。
	for i := 0; i < len(out) && i < limit; i++ {
		// maxIdx 是当前剩余部分的最大值下标。
		maxIdx := i
		// j 是比较遍历下标。
		for j := i + 1; j < len(out); j++ {
			if out[j].Appearances > out[maxIdx].Appearances {
				maxIdx = j
			}
		}
		out[i], out[maxIdx] = out[maxIdx], out[i]
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// marshalMarketIntelJSON 序列化 map 为 JSON；空 map 返回空串。
func marshalMarketIntelJSON(value any) string {
	// data 是序列化结果。
	data, err := json.Marshal(value)
	if err != nil || string(data) == "{}" || string(data) == "null" {
		return ""
	}
	return string(data)
}

// DeleteSnapshotsBefore 清理早于指定日期的原始快照。
func (a *MarketIntelScanStore) DeleteSnapshotsBefore(ctx context.Context, beforeDate string) (int64, error) {
	return a.store.MarketIntel.DeleteSnapshotsBefore(ctx, beforeDate)
}

// ListPendingVerdicts 返回某词某天尚无判定的快照输入。
func (a *MarketIntelScanStore) ListPendingVerdicts(ctx context.Context, keyword, snapshotDate string) ([]marketintel.VerdictInput, error) {
	// snapshots、verdicts 是该词当天的快照与判定。
	snapshots, verdicts, err := a.store.MarketIntel.ListSnapshotsWithVerdicts(ctx, keyword, snapshotDate)
	if err != nil {
		return nil, err
	}
	// judged 是已判定快照主键集合。
	judged := make(map[int64]struct{}, len(verdicts))
	// verdict 遍历建集合。
	for _, verdict := range verdicts {
		judged[verdict.SnapshotID] = struct{}{}
	}
	// out 是待判定输入。
	out := make([]marketintel.VerdictInput, 0)
	// snapshot 是当前快照行。
	for _, snapshot := range snapshots {
		// has 表示该快照是否已有判定。
		if _, has := judged[snapshot.ID]; has {
			continue
		}
		out = append(out, marketintel.VerdictInput{
			SnapshotID: snapshot.ID, Title: snapshot.Title, Price: snapshot.Price, WantCountText: snapshot.WantCountText,
		})
	}
	return out, nil
}

// ClassifyError 把平台错误分类为 risk/session/other；语义与 sourcing 一致。
func (a *MarketIntelScanStore) ClassifyError(err error) string {
	switch {
	case err == nil:
		return "other"
	case errors.Is(err, sourcingapp.ErrRiskControl), errors.Is(err, browser.ErrSourcingRiskControl):
		return "risk"
	case errors.Is(err, sourcingapp.ErrSessionExpired), errors.Is(err, browser.ErrSourcingSessionExpired):
		return "session"
	default:
		return "other"
	}
}

// MarketIntelStore 把 db 仓储适配为词表持久化接口。
type MarketIntelStore struct {
	// store 是数据库仓储聚合入口。
	store *db.Store
}

// NewMarketIntelStore 构造词表持久化适配器。
func NewMarketIntelStore(store *db.Store) *MarketIntelStore {
	return &MarketIntelStore{store: store}
}

// ListKeywords 返回全部关注词并转换为应用视图。
func (a *MarketIntelStore) ListKeywords(ctx context.Context) ([]marketintel.WatchedKeyword, error) {
	// rows 是数据库关注词行。
	rows, err := a.store.MarketIntel.ListKeywords(ctx)
	if err != nil {
		return nil, err
	}
	// out 是应用视图集合。
	out := make([]marketintel.WatchedKeyword, 0, len(rows))
	// row 是当前数据库行。
	for _, row := range rows {
		out = append(out, marketintel.WatchedKeyword{ID: row.ID, Keyword: row.Keyword, Enabled: row.Enabled, SortOrder: row.SortOrder})
	}
	return out, nil
}

// ListEnabledKeywords 返回启用中的关注词并转换为应用视图。
func (a *MarketIntelStore) ListEnabledKeywords(ctx context.Context) ([]marketintel.WatchedKeyword, error) {
	// rows 是数据库关注词行。
	rows, err := a.store.MarketIntel.ListEnabledKeywords(ctx)
	if err != nil {
		return nil, err
	}
	// out 是应用视图集合。
	out := make([]marketintel.WatchedKeyword, 0, len(rows))
	// row 是当前数据库行。
	for _, row := range rows {
		out = append(out, marketintel.WatchedKeyword{ID: row.ID, Keyword: row.Keyword, Enabled: row.Enabled, SortOrder: row.SortOrder})
	}
	return out, nil
}

// CreateKeyword 新增关注词；重复错误转换为应用哨兵。
func (a *MarketIntelStore) CreateKeyword(ctx context.Context, keyword string, enabled bool, sortOrder int) (int64, error) {
	// id、err 是写入结果。
	id, err := a.store.MarketIntel.CreateKeyword(ctx, keyword, enabled, sortOrder)
	if err != nil {
		if errors.Is(err, db.ErrMarketIntelKeywordDuplicate) {
			return 0, marketintel.ErrKeywordDuplicate
		}
		return 0, err
	}
	return id, nil
}

// UpdateKeyword 更新关注词；错误转换为应用哨兵。
func (a *MarketIntelStore) UpdateKeyword(ctx context.Context, id int64, keyword string, enabled bool, sortOrder int) error {
	// err 是更新结果。
	err := a.store.MarketIntel.UpdateKeyword(ctx, id, keyword, enabled, sortOrder)
	if err != nil {
		if errors.Is(err, db.ErrMarketIntelKeywordNotFound) {
			return marketintel.ErrKeywordNotFound
		}
		if errors.Is(err, db.ErrMarketIntelKeywordDuplicate) {
			return marketintel.ErrKeywordDuplicate
		}
		return err
	}
	return nil
}

// DeleteKeyword 删除关注词；不存在错误转换为应用哨兵。
func (a *MarketIntelStore) DeleteKeyword(ctx context.Context, id int64) error {
	// err 是删除结果。
	err := a.store.MarketIntel.DeleteKeyword(ctx, id)
	if err != nil {
		if errors.Is(err, db.ErrMarketIntelKeywordNotFound) {
			return marketintel.ErrKeywordNotFound
		}
		return err
	}
	return nil
}

// CountKeywords 返回关注词总数与启用数。
func (a *MarketIntelStore) CountKeywords(ctx context.Context) (int, int, error) {
	return a.store.MarketIntel.CountKeywords(ctx)
}

// ListLatestAggregates 返回各词最新聚合并转换为应用视图。
func (a *MarketIntelScanStore) ListLatestAggregates(ctx context.Context) ([]marketintel.AggregateView, error) {
	// rows 是数据库聚合行。
	rows, err := a.store.MarketIntel.ListLatestAggregates(ctx)
	if err != nil {
		return nil, err
	}
	// out 是应用视图集合。
	out := make([]marketintel.AggregateView, 0, len(rows))
	// row 是当前数据库行。
	for _, row := range rows {
		out = append(out, marketintel.AggregateView{
			Keyword: row.Keyword, Date: row.SnapshotDate, TotalCount: row.TotalCount,
			AICount: row.AICount, TotalWant: row.TotalWant, VerdictStatus: row.VerdictStatus,
		})
	}
	return out, nil
}

// ListAggregates 返回某词最近 N 天聚合并转换为应用视图。
func (a *MarketIntelScanStore) ListAggregates(ctx context.Context, keyword string, limitDays int) ([]marketintel.AggregateView, error) {
	// rows 是数据库聚合行。
	rows, err := a.store.MarketIntel.ListAggregates(ctx, keyword, limitDays)
	if err != nil {
		return nil, err
	}
	// out 是应用视图集合。
	out := make([]marketintel.AggregateView, 0, len(rows))
	// row 是当前数据库行。
	for _, row := range rows {
		out = append(out, marketintel.AggregateView{
			Keyword: row.Keyword, Date: row.SnapshotDate, TotalCount: row.TotalCount,
			AICount: row.AICount, TotalWant: row.TotalWant, VerdictStatus: row.VerdictStatus,
		})
	}
	return out, nil
}

// ListKeywordItems 返回某词最新一天的快照商品列表。
func (a *MarketIntelScanStore) ListKeywordItems(ctx context.Context, keyword string) ([]marketintel.ItemSummary, error) {
	// latestDate 是该词最新快照日。
	latestDate, dateErr := a.store.MarketIntel.LatestSnapshotDate(ctx, keyword)
	if dateErr != nil {
		return nil, dateErr
	}
	if latestDate == "" {
		return []marketintel.ItemSummary{}, nil
	}
	// snapshots、_ 是该词当天的快照与判定。
	snapshots, _, err := a.store.MarketIntel.ListSnapshotsWithVerdicts(ctx, keyword, latestDate)
	if err != nil {
		return nil, err
	}
	// out 是商品摘要集合。
	out := make([]marketintel.ItemSummary, 0, len(snapshots))
	// snapshot 是当前快照行。
	for _, snapshot := range snapshots {
		out = append(out, marketintel.ItemSummary{
			ItemID: snapshot.ItemID, Title: snapshot.Title, Price: snapshot.Price,
			WantCountText: snapshot.WantCountText, SellerNickname: snapshot.SellerNickname,
		})
	}
	return out, nil
}

// ListSellerStats 返回窗口天数内按卖家聚合的统计：读范围快照，按卖家分组计数与均价，降序取前 20。
func (a *MarketIntelScanStore) ListSellerStats(ctx context.Context, days int) ([]marketintel.SellerStat, error) {
	// endDate 是今天（北京时间）。
	endDate := time.Now().In(beijingLocationFixed()).Format("2006-01-02")
	// startDate 是窗口起始日。
	startDate := time.Now().In(beijingLocationFixed()).AddDate(0, 0, -days).Format("2006-01-02")
	// snapshots、verdicts 是窗口内全部快照与判定。
	snapshots, verdicts, err := a.store.MarketIntel.ListSnapshotsInRangeWithVerdicts(ctx, startDate, endDate)
	if err != nil {
		return nil, err
	}
	// verdictBySnapshot 是快照主键到判定的索引。
	verdictBySnapshot := make(map[int64]db.MarketIntelVerdictRow, len(verdicts))
	// verdict 遍历建索引。
	for _, verdict := range verdicts {
		verdictBySnapshot[verdict.SnapshotID] = verdict
	}
	// counts 是卖家出现次数。
	counts := make(map[string]int)
	// priceSum 是卖家价格合计。
	priceSum := make(map[string]float64)
	// sampleURLs 记录每个卖家的示例商品链接。
	sampleURLs := make(map[string]string)
	// snapshot 是当前快照行。
	for _, snapshot := range snapshots {
		// 只统计已判定为 AI 相关的商品；无判定数据时统计全部（判定未启用阶段）。
		verdict, has := verdictBySnapshot[snapshot.ID]
		if len(verdictBySnapshot) > 0 && (!has || !verdict.IsAIRelated) {
			continue
		}
		// nickname 是去空白后的卖家昵称。
		nickname := strings.TrimSpace(snapshot.SellerNickname)
		if nickname == "" {
			continue
		}
		counts[nickname]++
		// price 是解析后的价格数值。
		var price float64
		// scanErr 是价格解析错误；解析失败的行不计入均价。
		if _, scanErr := fmt.Sscanf(strings.ReplaceAll(strings.ReplaceAll(snapshot.Price, "¥", ""), ",", ""), "%f", &price); scanErr == nil {
			priceSum[nickname] += price
		}
		// 商品链接作为卖家示例入口。
		if strings.TrimSpace(snapshot.RawJSON) != "" {
			sampleURLs[nickname] = snapshot.RawJSON
		}
	}
	// top 是按出现次数取前 20 的卖家统计。
	top := buildTopSellers(counts, priceSum, sampleURLs, 20)
	return top, nil
}

// beijingLocationFixed 返回北京时间时区（与 sourcing 调度一致）。
func beijingLocationFixed() *time.Location {
	return time.FixedZone("CST", 8*3600)
}

// FetchCompetitorDetail 抓取竞品商品详情并转换为应用视图。
func (p *MarketIntelSearchPort) FetchCompetitorDetail(ctx context.Context, itemID string) (marketintel.CompetitorDetail, error) {
	// cookieID 是本次抓取使用的账号标识。
	cookieID, err := p.resolveCookieID(ctx)
	if err != nil {
		return marketintel.CompetitorDetail{}, err
	}
	// cookieStr 是解密后的登录态。
	cookieStr, readErr := p.credentials.ReadCookie(ctx, p.userID, cookieID)
	if readErr != nil {
		return marketintel.CompetitorDetail{}, fmt.Errorf("读取扫描账号登录态失败: %w", readErr)
	}
	// detail、fetchErr 是浏览器抓取结果与抓取错误。
	detail, fetchErr := p.gateway.manager.CompetitorItemDetail(ctx, cookieID, cookieStr, itemID)
	if fetchErr != nil {
		return marketintel.CompetitorDetail{}, classifyDetailPlatformError(fetchErr)
	}
	return marketintel.CompetitorDetail{
		ItemID: detail.ItemID, Title: detail.Title, Price: detail.Price,
		DescText: detail.DescText, Images: detail.Images,
		SellerNickname: detail.SellerNickname, SellerID: detail.SellerID,
	}, nil
}

// classifyDetailPlatformError 把浏览器详情抓取错误归类为应用层稳定错误。
// 风控与会话失效必须让调用方立即中止并给出可操作提示，不能退化为通用 500；
// 归类为详情专属错误后由 transport 映射为 409，符合「遇风控立即停止、不重试」的平台约定，
// 同时避免把「今日扫描已中止」这类扫描语义误用给详情请求。
func classifyDetailPlatformError(err error) error {
	switch {
	case errors.Is(err, browser.ErrSourcingRiskControl):
		return marketintel.ErrCompetitorDetailRiskControl
	case errors.Is(err, browser.ErrSourcingSessionExpired):
		return marketintel.ErrCompetitorDetailSessionExpired
	default:
		return err
	}
}

// LoadDetailRow 读取竞品详情缓存行。
func (a *MarketIntelScanStore) LoadDetailRow(ctx context.Context, itemID string) (marketintel.DetailCacheRowAlias, error) {
	// row 是数据库缓存行。
	row, err := a.store.MarketIntel.GetItemDetail(ctx, itemID)
	if err != nil {
		return marketintel.DetailCacheRowAlias{}, err
	}
	return marketintel.DetailCacheRowAlias{
		ItemID: row.ItemID, Title: row.Title, Price: row.Price,
		DescText: row.DescText, ImagesJSON: row.ImagesJSON,
		SellerNickname: row.SellerNickname, SellerID: row.SellerID,
		WantCountText: row.WantCountText, FetchedAt: row.FetchedAt,
	}, nil
}

// SaveDetailRow 写入竞品详情缓存行。
func (a *MarketIntelScanStore) SaveDetailRow(ctx context.Context, row marketintel.DetailCacheRowAlias) error {
	return a.store.MarketIntel.SaveItemDetail(ctx, db.MarketIntelItemDetailRow{
		ItemID: row.ItemID, Title: row.Title, Price: row.Price,
		DescText: row.DescText, ImagesJSON: row.ImagesJSON,
		SellerNickname: row.SellerNickname, SellerID: row.SellerID,
		WantCountText: row.WantCountText, FetchedAt: row.FetchedAt,
	})
}
