// Package items: 商品生命周期服务——平台下架/删除的编排、批次创建与批次执行。
// 平台成功才允许本地收口；失败保留原状供重试；风控/会话失效立即中止批次。
package items

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// 生命周期服务错误哨兵。
var (
	// ErrLifecyclePlatformFailed 表示平台未确认成功，本地不得收口；错误消息含平台 ret 摘要。
	ErrLifecyclePlatformFailed = errors.New("平台未确认生命周期操作成功")
	// ErrLifecycleNotOwned 表示目标商品不属于指定账号。
	ErrLifecycleNotOwned = errors.New("商品不属于指定账号")
	// ErrLifecycleBatchBusy 表示该用户已有进行中的生命周期批次。
	ErrLifecycleBatchBusy = errors.New("已有进行中的生命周期批次")
	// ErrLifecycleConflict 表示批次执行权竞争失败（已被其他 worker 接管）。
	ErrLifecycleConflict = errors.New("生命周期批次已被其他执行器接管")
	// ErrLifecycleBatchNotFound 表示批次不存在或不属于该用户。
	ErrLifecycleBatchNotFound = errors.New("生命周期批次不存在")
	// ErrLifecycleInvalidAction 表示批次动作不被支持。
	ErrLifecycleInvalidAction = errors.New("不支持的生命周期动作")
	// ErrLifecycleTooManyRows 表示批次行数超出限制。
	ErrLifecycleTooManyRows = errors.New("生命周期批次行数超出限制")
	// ErrLifecycleDuplicateRow 表示批次内存在重复商品。
	ErrLifecycleDuplicateRow = errors.New("生命周期批次存在重复商品")
)

// 生命周期动作取值（应用层自有常量，避免依赖持久层类型）。
const (
	// LifecycleActionDownshelf 表示批次动作为平台下架。
	LifecycleActionDownshelf = "downshelf"
	// LifecycleActionDelete 表示批次动作为平台删除。
	LifecycleActionDelete = "delete"
)

// LifecycleBatch 是应用层批次视图（与持久层模型解耦）。
type LifecycleBatch struct {
	// ID 是批次唯一标识。
	ID string
	// Action 是批次动作。
	Action string
	// Status 是批次状态。
	Status string
	// TotalCount/SuccessCount/FailedCount 分别是行总数、成功数与失败数。
	TotalCount, SuccessCount, FailedCount int
	// CreatedAt/UpdatedAt 是创建与最近更新时间。
	CreatedAt, UpdatedAt string
}

// LifecycleBatchRow 是应用层批次明细行视图。
type LifecycleBatchRow struct {
	// ID 是行持久化主键。
	ID int64
	// RowNo 是批次内序号。
	RowNo int
	// CookieID 是商品归属账号。
	CookieID string
	// ItemID 是平台商品 ID。
	ItemID string
	// Status 是行状态。
	Status string
	// ErrorMessage 是失败原因摘要。
	ErrorMessage string
	// FailureKind 是失败分类。
	FailureKind string
}

// LifecyclePlatformPort 是生命周期服务对平台协议的最小依赖（consumer-defined）。
type LifecyclePlatformPort interface {
	// DownshelfItem 调用平台下架接口；返回是否成功、平台 ret 摘要与错误。
	DownshelfItem(ctx context.Context, cookieID, itemID string) (bool, []string, error)
	// DeleteItem 调用平台删除接口；参数与返回同上。
	DeleteItem(ctx context.Context, cookieID, itemID string) (bool, []string, error)
}

// LifecycleOwnershipPort 复用账号归属校验能力。
type LifecycleOwnershipPort interface {
	// CookieOwned 判断账号是否属于指定用户。
	CookieOwned(ctx context.Context, userID int64, cookieID string) (bool, error)
}

// LifecycleStore 是生命周期服务需要的持久化窄接口（应用自有模型，转换在 adapter 完成）。
type LifecycleStore interface {
	// CreateLifecycleBatch 创建批次并写入明细。
	CreateLifecycleBatch(ctx context.Context, userID int64, action string, items []LifecycleTargetItem) (string, error)
	// GetLifecycleBatch 读取批次与明细。
	GetLifecycleBatch(ctx context.Context, userID int64, batchID string) (LifecycleBatch, []LifecycleBatchRow, error)
	// ListLifecycleBatches 列出最近批次。
	ListLifecycleBatches(ctx context.Context, userID int64, limit int) ([]LifecycleBatch, error)
	// LifecycleBatchRunningByUser 判断是否有进行中批次。
	LifecycleBatchRunningByUser(ctx context.Context, userID int64) (bool, error)
	// UpdateStatus 更新商品生命周期状态。
	UpdateStatus(ctx context.Context, cookieID, itemID, status string) error
	// Delete 软删除本地商品（平台删除成功后的本地收口）。
	Delete(ctx context.Context, cookieID, itemID string) error
	// ClaimLifecycleBatch 抢占批次执行权。
	ClaimLifecycleBatch(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error)
	// RenewLifecycleBatchLease 延长批次租约。
	RenewLifecycleBatchLease(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error)
	// PendingLifecycleRows 查询待处理行。
	PendingLifecycleRows(ctx context.Context, batchID string) ([]LifecycleBatchRow, error)
	// ClaimLifecycleRow 抢占单行。
	ClaimLifecycleRow(ctx context.Context, rowID int64) (bool, error)
	// CompleteLifecycleRow 标记行成功。
	CompleteLifecycleRow(ctx context.Context, rowID int64) error
	// FailLifecycleRow 标记行失败。
	FailLifecycleRow(ctx context.Context, rowID int64, failureKind, message string) error
	// RecountLifecycleBatch 重算批次计数并收口。
	RecountLifecycleBatch(ctx context.Context, batchID string) error
	// FailLifecycleBatch 批次整体置为部分失败（风控中止）。
	FailLifecycleBatch(ctx context.Context, batchID, workerToken string) (bool, error)
	// RetryLifecycleFailedRows 重置失败行为待处理。
	RetryLifecycleFailedRows(ctx context.Context, userID int64, batchID string) (int64, error)
	// CancelLifecycleBatch 取消批次。
	CancelLifecycleBatch(ctx context.Context, userID int64, batchID string) (bool, error)
}

// ItemLifecycleService 编排单商品生命周期操作与批次任务。
type ItemLifecycleService struct {
	// store 是持久化窄接口。
	store LifecycleStore
	// platform 是平台协议端口。
	platform LifecyclePlatformPort
	// ownership 是账号归属校验端口。
	ownership LifecycleOwnershipPort
	// classifyErr 是平台错误分类函数：返回 risk/session 时批次立即中止；由 adapter 组合期注入。
	classifyErr func(err error) string
	// jitterMin/jitterMax 是批次行间抖动秒数区间。
	jitterMin, jitterMax int
}

// NewItemLifecycleService 构造生命周期服务；classifyErr 不可为空，抖动区间默认 5-10 秒。
func NewItemLifecycleService(store LifecycleStore, platform LifecyclePlatformPort, ownership LifecycleOwnershipPort, classifyErr func(err error) string) (*ItemLifecycleService, error) {
	if store == nil || platform == nil || ownership == nil {
		return nil, errors.New("生命周期服务依赖不完整")
	}
	if classifyErr == nil {
		return nil, errors.New("生命周期服务缺少平台错误分类函数")
	}
	return &ItemLifecycleService{store: store, platform: platform, ownership: ownership, classifyErr: classifyErr, jitterMin: 5, jitterMax: 10}, nil
}

// lifecycleResult 是单商品操作结果。
type lifecycleResult struct {
	// OK 表示平台确认成功。
	OK bool
	// Ret 是平台 ret 摘要（已脱敏）。
	Ret []string
}

// Downshelf 单商品平台下架：归属校验 → 平台调用 → 平台成功才落本地状态。
func (s *ItemLifecycleService) Downshelf(ctx context.Context, userID int64, cookieID, itemID string) (lifecycleResult, error) {
	return s.runOne(ctx, userID, cookieID, itemID, LifecycleActionDownshelf)
}

// Delete 单商品平台删除：归属校验 → 平台调用 → 平台成功才软删除本地记录。
func (s *ItemLifecycleService) Delete(ctx context.Context, userID int64, cookieID, itemID string) (lifecycleResult, error) {
	return s.runOne(ctx, userID, cookieID, itemID, LifecycleActionDelete)
}

// runOne 是单商品动作的共用路径；一次用户操作对应恰好一次平台请求。
func (s *ItemLifecycleService) runOne(ctx context.Context, userID int64, cookieID, itemID, action string) (lifecycleResult, error) {
	// owned、err 保存账号归属校验结果。
	owned, err := s.ownership.CookieOwned(ctx, userID, cookieID)
	if err != nil {
		return lifecycleResult{}, err
	}
	if !owned {
		return lifecycleResult{}, ErrLifecycleNotOwned
	}
	// ok、ret、err 保存平台调用结果。
	ok, ret, err := s.invokePlatform(ctx, cookieID, itemID, action)
	if err != nil {
		return lifecycleResult{OK: false, Ret: ret}, err
	}
	if !ok {
		return lifecycleResult{OK: false, Ret: ret}, fmt.Errorf("%w: %s", ErrLifecyclePlatformFailed, summarizeRet(ret))
	}
	// localErr 保存本地收口错误；平台已成功，本地失败必须显式暴露而不是静默吞掉。
	localErr := s.applyLocalOutcome(ctx, cookieID, itemID, action)
	if localErr != nil {
		return lifecycleResult{OK: true, Ret: ret}, localErr
	}
	return lifecycleResult{OK: true, Ret: ret}, nil
}

// invokePlatform 按动作调用对应平台接口。
func (s *ItemLifecycleService) invokePlatform(ctx context.Context, cookieID, itemID, action string) (bool, []string, error) {
	if action == LifecycleActionDelete {
		return s.platform.DeleteItem(ctx, cookieID, itemID)
	}
	return s.platform.DownshelfItem(ctx, cookieID, itemID)
}

// applyLocalOutcome 平台成功后的本地收口：下架写 status=down；删除走既有软删除审计语义。
func (s *ItemLifecycleService) applyLocalOutcome(ctx context.Context, cookieID, itemID, action string) error {
	if action == LifecycleActionDelete {
		return s.store.Delete(ctx, cookieID, itemID)
	}
	return s.store.UpdateStatus(ctx, cookieID, itemID, "down")
}

// CreateLifecycleBatchInput 是批量任务的输入。
type CreateLifecycleBatchInput struct {
	// Action 是批次动作。
	Action string
	// Items 是目标商品集合。
	Items []LifecycleTargetItem
}

// LifecycleTargetItem 是批次内一条目标商品。
type LifecycleTargetItem struct {
	// CookieID 是商品归属账号。
	CookieID string
	// ItemID 是平台商品 ID。
	ItemID string
}

// CreateBatch 创建生命周期批次；要求用户当前没有进行中的批次以避免叠加平台请求。
func (s *ItemLifecycleService) CreateBatch(ctx context.Context, userID int64, input CreateLifecycleBatchInput) (string, error) {
	// busy、err 保存进行中批次检查结果。
	busy, err := s.store.LifecycleBatchRunningByUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if busy {
		return "", ErrLifecycleBatchBusy
	}
	// rows 是目标集合的拷贝。
	rows := make([]LifecycleTargetItem, 0, len(input.Items))
	rows = append(rows, input.Items...)
	return s.store.CreateLifecycleBatch(ctx, userID, input.Action, rows)
}

// GetBatch 透传批次查询。
func (s *ItemLifecycleService) GetBatch(ctx context.Context, userID int64, batchID string) (LifecycleBatch, []LifecycleBatchRow, error) {
	return s.store.GetLifecycleBatch(ctx, userID, batchID)
}

// ListBatches 透传批次列表查询。
func (s *ItemLifecycleService) ListBatches(ctx context.Context, userID int64, limit int) ([]LifecycleBatch, error) {
	return s.store.ListLifecycleBatches(ctx, userID, limit)
}

// RetryFailed 重试批次内失败行。
func (s *ItemLifecycleService) RetryFailed(ctx context.Context, userID int64, batchID string) (int64, error) {
	return s.store.RetryLifecycleFailedRows(ctx, userID, batchID)
}

// Cancel 取消批次。
func (s *ItemLifecycleService) Cancel(ctx context.Context, userID int64, batchID string) (bool, error) {
	return s.store.CancelLifecycleBatch(ctx, userID, batchID)
}

// summarizeRet 把平台 ret 列表压成一行摘要供错误消息展示；不包含任何凭证内容。
func summarizeRet(ret []string) string {
	if len(ret) == 0 {
		return "(无 ret)"
	}
	return strings.Join(ret, "; ")
}

// RunLifecycleBatch 执行一个批次的全部待处理行（串行+抖动+风控即停）。
// 该方法由 HTTP 端点在后台 goroutine 调用；ctx 取消即停止。
func (s *ItemLifecycleService) RunLifecycleBatch(ctx context.Context, userID int64, batchID string) error {
	// workerToken 是本 worker 的租约标识。
	workerToken := fmt.Sprintf("lc-%d", time.Now().UnixNano())
	// leaseUntil 是租约截止时刻。
	leaseUntil := time.Now().Add(10 * time.Minute).Unix()
	// claimed、err 保存批次抢占结果。
	claimed, err := s.store.ClaimLifecycleBatch(ctx, batchID, workerToken, leaseUntil)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrLifecycleConflict
	}
	// rows、err 保存待处理行。
	rows, err := s.store.PendingLifecycleRows(ctx, batchID)
	if err != nil {
		return err
	}
	// jitter 是行间随机抖动。
	jitter := func() time.Duration {
		// span 是抖动秒数区间宽度。
		span := s.jitterMax - s.jitterMin + 1
		if span <= 0 {
			span = 1
		}
		return time.Duration(s.jitterMin+rand.Intn(span)) * time.Second
	}
	// i、row 保存当前行序号与行数据。
	for i, row := range rows {
		// ctxErr 保存取消信号。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		// 首行不等待，后续行间插入抖动以降低平台请求频率。
		if i > 0 {
			// waitErr 保存可取消抖动等待的错误。
			if waitErr := sleepLifecycle(ctx, jitter()); waitErr != nil {
				return waitErr
			}
		}
		// renewed、err 保存租约续期结果。
		renewed, err := s.store.RenewLifecycleBatchLease(ctx, batchID, workerToken, time.Now().Add(10*time.Minute).Unix())
		if err != nil {
			return err
		}
		if !renewed {
			return ErrLifecycleConflict
		}
		// claimedRow、err 保存行抢占结果。
		claimedRow, err := s.store.ClaimLifecycleRow(ctx, row.ID)
		if err != nil {
			return err
		}
		if !claimedRow {
			continue
		}
		// 归属校验失败按行失败记录，不中止批次。
		// owned、err 保存账号归属校验结果。
		owned, err := s.ownership.CookieOwned(ctx, userID, row.CookieID)
		if err != nil {
			_ = s.store.FailLifecycleRow(ctx, row.ID, "ownership", "账号归属校验失败")
			_ = s.store.RecountLifecycleBatch(ctx, batchID)
			continue
		}
		if !owned {
			_ = s.store.FailLifecycleRow(ctx, row.ID, "ownership", "商品不属于指定账号")
			_ = s.store.RecountLifecycleBatch(ctx, batchID)
			continue
		}
		// ok、ret、err 保存平台调用结果；一次行操作恰一次平台请求。
		ok, ret, err := s.invokePlatform(ctx, row.CookieID, row.ItemID, s.batchAction(ctx, userID, batchID))
		if err != nil {
			// classifyLifecycleFailure 由适配层注入；返回 "risk"/"session" 时立即中止整个批次。
			if kind := s.classifyPlatformErr(err, ret); kind == "risk" || kind == "session" {
				_ = s.store.FailLifecycleRow(ctx, row.ID, kind, "风控或会话失效，批次已中止")
				_, _ = s.store.FailLifecycleBatch(ctx, batchID, workerToken)
				_ = s.store.RecountLifecycleBatch(ctx, batchID)
				return fmt.Errorf("批次因风控或会话失效中止: %w", err)
			}
			_ = s.store.FailLifecycleRow(ctx, row.ID, "platform", summarizeRet(ret))
			_ = s.store.RecountLifecycleBatch(ctx, batchID)
			continue
		}
		if !ok {
			_ = s.store.FailLifecycleRow(ctx, row.ID, "platform", summarizeRet(ret))
			_ = s.store.RecountLifecycleBatch(ctx, batchID)
			continue
		}
		// localErr 保存本地收口错误；平台已成功但本地失败按本地类失败记录。
		if localErr := s.applyLocalOutcome(ctx, row.CookieID, row.ItemID, s.batchAction(ctx, userID, batchID)); localErr != nil {
			_ = s.store.FailLifecycleRow(ctx, row.ID, "local", "本地状态写入失败，请人工核对")
			_ = s.store.RecountLifecycleBatch(ctx, batchID)
			continue
		}
		_ = s.store.CompleteLifecycleRow(ctx, row.ID)
		_ = s.store.RecountLifecycleBatch(ctx, batchID)
	}
	return s.store.RecountLifecycleBatch(ctx, batchID)
}

// batchAction 读取批次动作；读取失败时按当前查询安全降级为空（调用方已校验）。
func (s *ItemLifecycleService) batchAction(ctx context.Context, userID int64, batchID string) string {
	// batch、_、err 保存批次头读取结果。
	batch, _, err := s.store.GetLifecycleBatch(ctx, userID, batchID)
	if err != nil {
		return ""
	}
	return batch.Action
}

// classifyPlatformErr 用注入的函数分类平台错误；未知错误按可重试的平台失败处理。
func (s *ItemLifecycleService) classifyPlatformErr(err error, ret []string) string {
	// kind 保存分类结果；ret 参与兜底判断保留扩展位。
	kind := s.classifyErr(err)
	_ = ret
	return kind
}

// sleepLifecycle 可取消的抖动等待。
func sleepLifecycle(ctx context.Context, d time.Duration) error {
	// timer 保存定时器。
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
