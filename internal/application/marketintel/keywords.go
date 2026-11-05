// Package marketintel 提供市场情报用例：关注词管理、每日扫描编排、AI 判定与趋势聚合。
// 平台请求遵循防风控铁律：每词每天最多 1 次搜索，串行执行，风控即停不重试。
package marketintel

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// MaxEnabledKeywords 是启用中关注词的数量上限，硬约束每日平台请求规模。
const MaxEnabledKeywords = 30

// 市场情报领域错误。
var (
	// ErrKeywordLimitReached 表示启用词数量已达上限。
	ErrKeywordLimitReached = errors.New("启用关注词已达上限（30 个）")
	// ErrKeywordEmpty 表示关注词文本为空。
	ErrKeywordEmpty = errors.New("关注词不能为空")
	// ErrKeywordNotFound 表示关注词不存在。
	ErrKeywordNotFound = errors.New("关注词不存在")
	// ErrKeywordDuplicate 表示关注词已存在。
	ErrKeywordDuplicate = errors.New("关注词已存在")
	// ErrRiskControl 表示平台返回风控验证要求，当天批次必须立即中止。
	ErrRiskControl = errors.New("平台返回风控验证要求，今日扫描已中止，请明天再试")
	// ErrSessionExpired 表示账号会话失效，当天批次必须立即中止。
	ErrSessionExpired = errors.New("账号登录已失效，今日扫描已中止，请先在账号页恢复登录")
)

// DefaultSeedKeywords 是首次使用时内置的 AI 相关种子词，覆盖课程/代做/绘画/工具账号等类目。
var DefaultSeedKeywords = []string{
	"AI课程", "ChatGPT", "AI绘画", "Midjourney", "AI工具",
	"AI代做", "提示词", "AI头像", "数字人", "AI简历",
	"AI写作", "SD绘画", "AI视频", "GPT账号", "AI设计",
}

// WatchedKeyword 是一条关注词。
type WatchedKeyword struct {
	// ID 是关注词主键。
	ID int64
	// Keyword 是搜索词文本。
	Keyword string
	// Enabled 表示是否参与每日扫描。
	Enabled bool
	// SortOrder 是展示排序值。
	SortOrder int
}

// Store 是市场情报持久化边界，由 db 层实现并在组合根注入。
type Store interface {
	// ListKeywords 返回全部关注词，按排序值升序。
	ListKeywords(ctx context.Context) ([]WatchedKeyword, error)
	// ListEnabledKeywords 返回全部启用中的关注词。
	ListEnabledKeywords(ctx context.Context) ([]WatchedKeyword, error)
	// CreateKeyword 新增关注词；重复时返回 ErrKeywordDuplicate。
	CreateKeyword(ctx context.Context, keyword string, enabled bool, sortOrder int) (int64, error)
	// UpdateKeyword 更新关注词；不存在返回 ErrKeywordNotFound，重复返回 ErrKeywordDuplicate。
	UpdateKeyword(ctx context.Context, id int64, keyword string, enabled bool, sortOrder int) error
	// DeleteKeyword 删除关注词；不存在返回 ErrKeywordNotFound。
	DeleteKeyword(ctx context.Context, id int64) error
	// CountKeywords 返回关注词总数与启用数。
	CountKeywords(ctx context.Context) (total int, enabled int, err error)
}

// normalizeKeyword 把关注词文本去首尾空白；空文本视为非法。
func normalizeKeyword(keyword string) (string, error) {
	// trimmed 是规范化后的词文本。
	trimmed := strings.TrimSpace(keyword)
	if trimmed == "" {
		return "", ErrKeywordEmpty
	}
	return trimmed, nil
}

// ListKeywords 返回全部关注词。
func (s *Service) ListKeywords(ctx context.Context) ([]WatchedKeyword, error) {
	return s.store.ListKeywords(ctx)
}

// CreateKeyword 新增关注词；启用词达到上限时拒绝。
func (s *Service) CreateKeyword(ctx context.Context, keyword string, enabled bool) (WatchedKeyword, error) {
	// trimmed 是规范化后的词文本。
	trimmed, err := normalizeKeyword(keyword)
	if err != nil {
		return WatchedKeyword{}, err
	}
	if enabled {
		// total、enabledCount 是当前词表计数。
		_, enabledCount, countErr := s.store.CountKeywords(ctx)
		if countErr != nil {
			return WatchedKeyword{}, countErr
		}
		if enabledCount >= MaxEnabledKeywords {
			return WatchedKeyword{}, ErrKeywordLimitReached
		}
	}
	// sortOrder 是新词的排序值，追加到末尾。
	sortOrder := 0
	// existing 是当前全部词。
	existing, listErr := s.store.ListKeywords(ctx)
	if listErr != nil {
		return WatchedKeyword{}, listErr
	}
	// item 遍历既有词，取最大排序值。
	for _, item := range existing {
		if item.SortOrder >= sortOrder {
			sortOrder = item.SortOrder + 1
		}
	}
	// id 是新词主键。
	id, err := s.store.CreateKeyword(ctx, trimmed, enabled, sortOrder)
	if err != nil {
		if errors.Is(err, ErrKeywordDuplicate) {
			return WatchedKeyword{}, ErrKeywordDuplicate
		}
		return WatchedKeyword{}, err
	}
	return WatchedKeyword{ID: id, Keyword: trimmed, Enabled: enabled, SortOrder: sortOrder}, nil
}

// UpdateKeyword 更新关注词文本与启用状态；启用超过上限时拒绝。
func (s *Service) UpdateKeyword(ctx context.Context, id int64, keyword string, enabled bool) error {
	// trimmed 是规范化后的词文本。
	trimmed, err := normalizeKeyword(keyword)
	if err != nil {
		return err
	}
	if enabled {
		// enabledCount 是当前启用词数。
		_, enabledCount, countErr := s.store.CountKeywords(ctx)
		if countErr != nil {
			return countErr
		}
		// existing 是当前全部词，用于判断目标词当前是否已启用。
		existing, listErr := s.store.ListKeywords(ctx)
		if listErr != nil {
			return listErr
		}
		// alreadyEnabled 表示目标词当前是否已启用；已启用则更新不增加启用数。
		alreadyEnabled := false
		// item 遍历查找目标词。
		for _, item := range existing {
			if item.ID == id {
				alreadyEnabled = item.Enabled
				break
			}
		}
		if !alreadyEnabled && enabledCount >= MaxEnabledKeywords {
			return ErrKeywordLimitReached
		}
	}
	// sortOrder 保持原值：先读后写。
	// existing 是当前全部词。
	existing, listErr := s.store.ListKeywords(ctx)
	if listErr != nil {
		return listErr
	}
	// currentSort 是目标词当前排序值。
	currentSort := 0
	// found 表示目标词是否存在。
	found := false
	// item 遍历查找目标词的排序值。
	for _, item := range existing {
		if item.ID == id {
			currentSort = item.SortOrder
			found = true
			break
		}
	}
	if !found {
		return ErrKeywordNotFound
	}
	return s.store.UpdateKeyword(ctx, id, trimmed, enabled, currentSort)
}

// DeleteKeyword 删除关注词。
func (s *Service) DeleteKeyword(ctx context.Context, id int64) error {
	return s.store.DeleteKeyword(ctx, id)
}

// EnsureSeedKeywords 在词表为空时写入内置种子词；幂等：已有任何词则跳过。
func (s *Service) EnsureSeedKeywords(ctx context.Context) error {
	// total 是当前词表总数。
	total, _, err := s.store.CountKeywords(ctx)
	if err != nil {
		return err
	}
	if total > 0 {
		return nil
	}
	// sortOrder 是种子词的写入顺序。
	sortOrder := 0
	// seed 是当前遍历的种子词。
	for _, seed := range DefaultSeedKeywords {
		// trimmed 是规范化后的种子词。
		trimmed := strings.TrimSpace(seed)
		if trimmed == "" {
			continue
		}
		// createErr 是种子词写入错误；重复词跳过继续。
		if _, createErr := s.store.CreateKeyword(ctx, trimmed, true, sortOrder); createErr != nil {
			if errors.Is(createErr, ErrKeywordDuplicate) {
				sortOrder++
				continue
			}
			return createErr
		}
		sortOrder++
	}
	return nil
}

// sortKeywordsByID 按主键升序排列关注词，供确定性测试断言。
func sortKeywordsByID(items []WatchedKeyword) {
	sort.Slice(items /* 主键升序。 */, func(i, j int) bool { return items[i].ID < items[j].ID })
}
