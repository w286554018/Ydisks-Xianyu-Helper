package sourcing

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// schedulerDailyHour 是每日快照执行的小时（北京时间）。
const schedulerDailyHour = 8

// schedulerCheckInterval 是调度器的检查间隔；不需要精确到分钟，按小时轮询即可。
const schedulerCheckInterval = 10 * time.Minute

// Scheduler 负责每日一次采集平台热词快照（游客态，单次 ≤2 个请求）。
// 生命周期约定：由组合根创建与持有，Start 只启动一个后台协程，Stop 幂等并等待协程退出。
type Scheduler struct {
	// service 执行快照采集与聚合。
	service *Service
	// logger 记录每日执行结果，不记录任何凭证。
	logger *slog.Logger

	// mu 保护 started、cancel 与 done，避免重复启动和重复关闭。
	mu sync.Mutex
	// started 表示后台协程已启动。
	started bool
	// cancel 关闭后台协程的上下文。
	cancel context.CancelFunc
	// done 在后台协程退出时关闭。
	done chan struct{}
	// lastRunDate 记录最近一次执行的归属日，避免同一天重复执行。
	lastRunDate string
}

// NewScheduler 构造每日热词采集调度器；畅销榜按需实时计算，不需要调度。
func NewScheduler(service *Service, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{service: service, logger: logger}
}

// Start 启动后台调度协程；重复调用不会启动第二个协程。
func (s *Scheduler) Start(ctx context.Context) {
	if s == nil || s.service == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	// runCtx、cancel 是后台协程的上下文与取消函数。
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.started = true
	s.mu.Unlock()

	go s.loop(runCtx)
}

// Stop 取消后台协程并等待其退出；重复调用安全。
func (s *Scheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	// cancel 是启动时创建的取消函数；done 是等待协程退出的信号通道。
	cancel := s.cancel
	// done 是启动时创建的退出信号通道，Stop 在锁外等待它关闭以确认协程已退出。
	done := s.done
	s.started = false
	s.cancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// loop 按固定间隔检查是否到达当日执行时刻。
func (s *Scheduler) loop(ctx context.Context) {
	// done 是启动时创建的退出信号通道，必须在本协程退出时关闭。
	s.mu.Lock()
	// done 是本协程负责关闭的退出信号通道，退出时关闭以唤醒 Stop。
	done := s.done
	s.mu.Unlock()
	defer func() {
		if done != nil {
			close(done)
		}
	}()

	// ticker 是检查用的定时器，退出时必须停止。
	ticker := time.NewTicker(schedulerCheckInterval)
	defer ticker.Stop()

	// 启动后先尝试一次，便于服务恰好在执行时刻之后重启时补齐当天快照。
	s.runIfDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runIfDue(ctx)
		}
	}
}

// runIfDue 在到达当日执行时刻且当天尚未执行时生成快照。
func (s *Scheduler) runIfDue(ctx context.Context) {
	// moment 是当前北京时间时刻。
	moment := s.service.now().In(beijingLocation)
	if moment.Hour() < schedulerDailyHour {
		return
	}
	// date 是当日归属日。
	date := s.service.TodayDate()
	s.mu.Lock()
	if s.lastRunDate == date {
		s.mu.Unlock()
		return
	}
	s.lastRunDate = date
	s.mu.Unlock()

	// existing、existingErr 是当日热词快照；服务重启时若已成功采集过则跳过，避免每次重启都访问平台。
	existing, existingErr := s.service.HotKeywords(ctx)
	if existingErr == nil && existing.Status == StatusOK {
		s.logger.Info("选品参考当日热词已采集，跳过", "date", date)
		return
	}
	// err 是热词采集错误；失败已写入失败快照，此处只记录日志，当天不自动重试。
	if err := s.service.RefreshHotKeywords(ctx); err != nil {
		s.logger.Warn("选品参考热词采集失败", "date", date, "err", err)
	}
}
