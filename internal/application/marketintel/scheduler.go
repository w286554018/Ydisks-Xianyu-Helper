package marketintel

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// schedulerDailyHour 是每日扫描执行的小时（北京时间），复用 sourcing 每日调度的低峰时段。
const schedulerDailyHour = 8

// schedulerCheckInterval 是调度器的检查间隔；按小时轮询即可。
const schedulerCheckInterval = 10 * time.Minute

// Scheduler 负责每日一次执行市场扫描；幂等由 RunDailyScan 内部保证。
// 生命周期约定：由组合根创建与持有，Start 只启动一个后台协程，Stop 幂等并等待协程退出。
type Scheduler struct {
	// service 执行每日扫描。
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

// NewScheduler 构造市场情报每日调度器。
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
	// doneCh 是本次协程的完成通道；局部变量避免 Stop 置 nil 后后台协程 close 空指针。
	doneCh := make(chan struct{})
	s.done = doneCh
	s.started = true
	s.mu.Unlock()
	// loopBody 每次检查是否到达执行时间。
	go func() {
		defer close(doneCh)
		// ticker 是小时级检查计时器。
		ticker := time.NewTicker(schedulerCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				s.runIfDue(runCtx)
			}
		}
	}()
}

// runIfDue 在到达每日执行小时且当天未执行时触发扫描。
func (s *Scheduler) runIfDue(ctx context.Context) {
	// now 是当前北京时间。
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	if now.Hour() < schedulerDailyHour {
		return
	}
	// date 是当前归属日。
	date := now.Format("2006-01-02")
	s.mu.Lock()
	if s.lastRunDate == date {
		s.mu.Unlock()
		return
	}
	s.lastRunDate = date
	s.mu.Unlock()
	// result、err 是扫描结果与错误；失败只记日志，当天不自动重试。
	result, err := s.service.RunDailyScan(ctx)
	if err != nil {
		s.logger.Warn("市场情报每日扫描失败", "date", date, "err", err)
		return
	}
	s.logger.Info("市场情报每日扫描完成", "date", date, "scanned", result.Scanned, "skipped", result.Skipped, "verdict_failed", result.VerdictFailed)
}

// Stop 幂等停止后台协程并等待退出。
func (s *Scheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.started || s.cancel == nil || s.done == nil {
		s.started = false
		s.mu.Unlock()
		return
	}
	s.started = false
	// cancel 与 done 是本次停止使用的取消函数与完成通道。
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	cancel()
	<-done
}
