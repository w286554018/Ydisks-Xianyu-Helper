package marketintel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// snapshotRetentionDays 是原始商品快照的保留天数，超出部分在每日扫描末尾清理。
const snapshotRetentionDays = 90

// scanJitterMinMillis 与 scanJitterMaxMillis 是词与词之间的抖动间隔范围（毫秒）。
const (
	scanJitterMinMillis = 5000
	scanJitterMaxMillis = 10000
)

// SearchResultItem 是搜索端口返回的单条商品摘要（页面公开展示字段）。
type SearchResultItem struct {
	// ItemID 是平台商品标识。
	ItemID string
	// Title 是商品标题。
	Title string
	// Price 是价格文本。
	Price string
	// WantCountText 是页面原始想要人数文本。
	WantCountText string
	// SellerNickname 是卖家昵称。
	SellerNickname string
	// PublishTime 是发布时间文本。
	PublishTime string
	// RawJSON 是搜索结果原始 JSON 片段。
	RawJSON string
}

// SearchPort 是平台搜索边界；实现方必须保证单次调用只发出 1 次搜索请求。
type SearchPort interface {
	// SearchKeyword 以账号登录态执行单次关键词搜索，返回最多 30 条结果。
	SearchKeyword(ctx context.Context, keyword string) ([]SearchResultItem, error)
}

// VerdictInput 是 AI 判定的单条输入。
type VerdictInput struct {
	// SnapshotID 是快照主键。
	SnapshotID int64
	// Title 是商品标题。
	Title string
	// Price 是价格文本。
	Price string
	// WantCountText 是想要人数文本。
	WantCountText string
}

// VerdictResult 是 AI 判定的单条结果。
type VerdictResult struct {
	// SnapshotID 是对应快照主键。
	SnapshotID int64
	// IsAIRelated 表示是否 AI 相关产品。
	IsAIRelated bool
	// Category 是细分类目。
	Category string
	// Reason 是判定理由摘要。
	Reason string
}

// VerdictPort 是 AI 批量判定边界；实现方保证一次调用只发 1 次模型请求且失败不重试。
type VerdictPort interface {
	// JudgeBatch 对一批快照做结构化判定，返回与输入等长的结果列表。
	JudgeBatch(ctx context.Context, keyword string, items []VerdictInput) ([]VerdictResult, error)
}

// ScanRecord 是一次扫描的持久化模型。
type ScanRecord struct {
	// Keyword 是本次扫描的关注词。
	Keyword string
	// SnapshotDate 是归属日 YYYY-MM-DD。
	SnapshotDate string
	// Items 是搜索结果。
	Items []SearchResultItem
	// Verdicts 是判定结果；判定失败时为空。
	Verdicts []VerdictResult
	// VerdictFailed 表示本次判定是否失败（快照已落库但无聚合）。
	VerdictFailed bool
}

// scanPersistence 是扫描编排需要的持久化窄接口（由 Store 组合扩展或独立实现）。
type scanPersistence interface {
	// SnapshotExists 判断某词某天是否已有成功快照（幂等检查）。
	SnapshotExists(ctx context.Context, keyword, snapshotDate string) (bool, error)
	// SaveScanRecord 落库一次扫描：快照、判定与聚合。
	SaveScanRecord(ctx context.Context, record ScanRecord) error
	// DeleteSnapshotsBefore 清理早于指定日期的原始快照。
	DeleteSnapshotsBefore(ctx context.Context, beforeDate string) (int64, error)
	// ListPendingVerdicts 返回某词某天尚未判定的快照（供补判定）。
	ListPendingVerdicts(ctx context.Context, keyword, snapshotDate string) ([]VerdictInput, error)
	// ClassifyError 把平台错误分类为 risk/session/other。
	ClassifyError(err error) string
}

// Service 是市场情报用例编排服务。
type Service struct {
	// store 提供词表持久化。
	store Store
	// scan 是扫描持久化边界。
	scan scanPersistence
	// search 是平台搜索端口。
	search SearchPort
	// verdict 是 AI 判定端口；可为空（未配置模型时只落快照不判定）。
	verdict VerdictPort
	// logger 记录扫描执行日志，不记录任何凭证。
	logger *slog.Logger
	// now 返回当前时间，测试注入确定时钟。
	now func() time.Time
	// location 是快照归属日使用的时区（北京时间）。
	location *time.Location
	// rng 是抖动随机源。
	rng *rand.Rand
}

// NewService 构造市场情报服务；scan 与 search 必填，verdict 可空。
func NewService(store Store, scan scanPersistence, search SearchPort, verdict VerdictPort, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store: store, scan: scan, search: search, verdict: verdict, logger: logger,
		now:      func() time.Time { return time.Now() },
		location: time.FixedZone("CST", 8*3600),
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// today 返回当前北京时间的归属日字符串。
func (s *Service) today() string {
	return s.now().In(s.location).Format("2006-01-02")
}

// keywordSnapshotDone 判断某词当天是否已有成功快照（幂等）。
func (s *Service) keywordSnapshotDone(ctx context.Context, keyword string) bool {
	// exists 是幂等检查结果。
	exists, err := s.scan.SnapshotExists(ctx, keyword, s.today())
	if err != nil {
		s.logger.Warn("市场情报幂等检查失败，按未扫描处理", "keyword", keyword, "err", err)
		return false
	}
	return exists
}

// ScanResult 是一次批量扫描的执行摘要。
type ScanResult struct {
	// Date 是扫描归属日。
	Date string `json:"date"`
	// Scanned 是本次实际扫描的词数。
	Scanned int `json:"scanned"`
	// Skipped 是因幂等跳过的词数。
	Skipped int `json:"skipped"`
	// FailedWords 是本次扫描失败的词数（含账号/浏览器不可用等错误）。
	FailedWords int `json:"failed_words"`
	// VerdictFailed 是判定失败的词数。
	VerdictFailed int `json:"verdict_failed"`
	// StoppedReason 是中止原因；正常跑完为空。
	StoppedReason string `json:"stopped_reason,omitempty"`
}

// RunDailyScan 执行每日扫描：逐词串行搜索 + 判定 + 聚合；风控/会话失效立即中止。
func (s *Service) RunDailyScan(ctx context.Context) (ScanResult, error) {
	// result 是执行摘要。
	result := ScanResult{Date: s.today()}
	// keywords 是当前启用的关注词。
	keywords, err := s.store.ListEnabledKeywords(ctx)
	if err != nil {
		return result, err
	}
	if s.search == nil && len(keywords) > 0 {
		result.StoppedReason = "市场情报搜索能力未启用（浏览器不可用）"
		return result, errors.New(result.StoppedReason)
	}
	// index 是词序号。
	for index, keyword := range keywords {
		// err 是上下文取消检查。
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if s.keywordSnapshotDone(ctx, keyword.Keyword) {
			result.Skipped++
			continue
		}
		// verdictFailed 表示本词判定是否失败；scanErr 是单词扫描错误，风控/会话失效时中止整批。
		verdictFailed, scanErr := s.scanKeyword(ctx, keyword.Keyword)
		if scanErr != nil {
			// kind 是错误分类。
			kind := s.scan.ClassifyError(scanErr)
			if kind == "risk" {
				result.StoppedReason = ErrRiskControl.Error()
				return result, ErrRiskControl
			}
			if kind == "session" {
				result.StoppedReason = ErrSessionExpired.Error()
				return result, ErrSessionExpired
			}
			s.logger.Warn("市场情报单词扫描失败，继续下一个词", "keyword", keyword.Keyword, "err", scanErr)
			result.FailedWords++
			continue
		}
		result.Scanned++
		if verdictFailed {
			result.VerdictFailed++
		}
		// 词间抖动：最后一个词后不再等待。
		if index < len(keywords)-1 {
			s.sleepJitter(ctx)
		}
	}
	// 清理 90 天前的原始快照。
	if _, cleanupErr := s.scan.DeleteSnapshotsBefore(ctx, s.now().In(s.location).AddDate(0, 0, -snapshotRetentionDays).Format("2006-01-02")); cleanupErr != nil {
		s.logger.Warn("市场情报历史快照清理失败", "err", cleanupErr)
	}
	return result, nil
}

// scanKeyword 执行单词扫描：搜索 → 落快照 → 判定 → 聚合；返回值表示判定是否失败。
func (s *Service) scanKeyword(ctx context.Context, keyword string) (bool, error) {
	if s.search == nil {
		return false, errors.New("市场情报搜索能力未启用（浏览器不可用），今日扫描已中止")
	}
	// items 是搜索结果。
	items, err := s.search.SearchKeyword(ctx, keyword)
	if err != nil {
		return false, err
	}
	// record 是本次扫描记录。
	record := ScanRecord{Keyword: keyword, SnapshotDate: s.today(), Items: items}
	// 判定：verdict 端口可用且搜索有结果时执行；失败降级不重试。
	if s.verdict != nil && len(items) > 0 {
		// inputs 是判定输入列表。
		inputs := make([]VerdictInput, 0, len(items))
		// item 是当前商品。
		for _, item := range items {
			inputs = append(inputs, VerdictInput{Title: item.Title, Price: item.Price, WantCountText: item.WantCountText})
		}
		// verdicts 是判定结果。
		verdicts, verdictErr := s.verdict.JudgeBatch(ctx, keyword, inputs)
		if verdictErr != nil {
			s.logger.Warn("市场情报 AI 判定失败，本次只落快照", "keyword", keyword, "err", verdictErr)
			record.VerdictFailed = true
		} else {
			record.Verdicts = verdicts
		}
	}
	return record.VerdictFailed, s.scan.SaveScanRecord(ctx, record)
}

// BackfillVerdicts 对指定词当天尚未判定的快照补跑判定并更新聚合；幂等：已有判定跳过。
func (s *Service) BackfillVerdicts(ctx context.Context, keyword string) error {
	if s.verdict == nil {
		return errors.New("市场情报模型未配置，无法补判定")
	}
	// pending 是待判定快照。
	pending, err := s.scan.ListPendingVerdicts(ctx, keyword, s.today())
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	// verdicts 是补判定结果。
	verdicts, err := s.verdict.JudgeBatch(ctx, keyword, pending)
	if err != nil {
		return err
	}
	// record 是补判定记录；Items 为空表示复用已有快照只补判定与聚合。
	return s.scan.SaveScanRecord(ctx, ScanRecord{Keyword: keyword, SnapshotDate: s.today(), Verdicts: verdicts})
}

// sleepJitter 在词与词之间等待随机抖动间隔；上下文取消时立即返回。
func (s *Service) sleepJitter(ctx context.Context) {
	// jitterMillis 是本次抖动时长。
	jitterMillis := scanJitterMinMillis + s.rng.Intn(scanJitterMaxMillis-scanJitterMinMillis+1)
	// timer 是抖动计时器。
	timer := time.NewTimer(time.Duration(jitterMillis) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// WithClock 注入确定时钟，供测试使用。
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// CategoryCount 是类目分布的单项统计。
type CategoryCount struct {
	// Name 是类目名。
	Name string `json:"name"`
	// Count 是该类目商品条数。
	Count int `json:"count"`
}

// SellerStat 是卖家聚合的单项统计。
type SellerStat struct {
	// Nickname 是卖家昵称。
	Nickname string `json:"nickname"`
	// Appearances 是在快照中出现的次数。
	Appearances int `json:"appearances"`
	// AvgPrice 是出现商品的平均价格文本。
	AvgPrice string `json:"avg_price"`
	// SampleURL 是该卖家最近一次出现商品的详情页地址；用户可从商品页进入卖家主页。
	SampleURL string `json:"sample_url,omitempty"`
}

// buildCategoryCounts 从判定结果统计类目分布。
func buildCategoryCounts(verdicts []VerdictResult) []CategoryCount {
	// counts 是类目计数。
	counts := make(map[string]int)
	// verdict 是当前判定。
	for _, verdict := range verdicts {
		if verdict.IsAIRelated && strings.TrimSpace(verdict.Category) != "" {
			counts[verdict.Category]++
		}
	}
	// out 是排序后的分布列表。
	out := make([]CategoryCount, 0, len(counts))
	// name、count 遍历计数表。
	for name, count := range counts {
		out = append(out, CategoryCount{Name: name, Count: count})
	}
	sort.Slice(out /* 条数降序。 */, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

// marshalJSONOrNull 序列化为 JSON；空列表返回空串。
func marshalJSONOrNull(value any) string {
	// data 是序列化结果。
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	if string(data) == "null" || string(data) == "[]" {
		return ""
	}
	return string(data)
}

// scanSummary 生成扫描摘要日志文本。
func scanSummary(result ScanResult) string {
	return fmt.Sprintf("date=%s scanned=%d skipped=%d verdict_failed=%d stopped=%s",
		result.Date, result.Scanned, result.Skipped, result.VerdictFailed, result.StoppedReason)
}
