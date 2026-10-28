package system

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/system"
	usage "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	"github.com/QuantumNous/new-api/internal/system/claim_system_tasks"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/internal/system/enqueue_log_cleanup"
	"github.com/QuantumNous/new-api/internal/system/enqueue_system_task"
	"github.com/QuantumNous/new-api/internal/system/purge_old_logs"
	"github.com/QuantumNous/new-api/internal/system/schedule_system_tasks"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	// systemTaskRunnerIdleInterval is the fallback poll interval used to pick up
	// tasks created on other nodes and mark expired leases failed.
	systemTaskRunnerIdleInterval = 15 * time.Second
	systemTaskLockTTL            = 60 * time.Second

	// systemTaskSchedulerInterval throttles how often the scheduler/stale-lock
	// pass runs, independent of how often the runner wakes to claim tasks.
	systemTaskSchedulerInterval = 15 * time.Second
	systemTaskStaleLockInterval = 30 * time.Second
)

var (
	systemTaskHandlersMu sync.RWMutex
	systemTaskHandlers   = map[string]domain.SystemTaskHandler{}
)

// RegisterSystemTaskHandler registers a handler keyed by its Type(). It must be
// called before StartSystemTaskRunner (or any time, since the runner snapshots
// the registry every pass). Re-registering a type replaces the previous handler.
func RegisterSystemTaskHandler(h domain.SystemTaskHandler) {
	if h == nil {
		return
	}
	systemTaskHandlersMu.Lock()
	defer systemTaskHandlersMu.Unlock()
	systemTaskHandlers[h.Type()] = h
}

func handlers() []domain.SystemTaskHandler {
	systemTaskHandlersMu.RLock()
	defer systemTaskHandlersMu.RUnlock()
	handlers := make([]domain.SystemTaskHandler, 0, len(systemTaskHandlers))
	for _, h := range systemTaskHandlers {
		handlers = append(handlers, h)
	}
	return handlers
}

type Runtime struct {
	Store        store.Store
	Enqueue      enqueue_system_task.Usecase
	StartCleanup enqueue_log_cleanup.Usecase
	Schedule     schedule_system_tasks.Usecase
	Claim        claim_system_tasks.Usecase
	Cleanup      purge_old_logs.Usecase
}

var defaultOnce sync.Once
var defaultRuntime Runtime

func Default() Runtime {
	defaultOnce.Do(func() { defaultRuntime = newRuntime() })
	return defaultRuntime
}

func newRuntime() Runtime {
	s := store.Store{DB: func() *gorm.DB { return model.DB }}
	logs := usage.Cleanup{DB: func() *gorm.DB { return model.LOG_DB }}
	r := Runtime{
		Store:        s,
		Enqueue:      enqueue_system_task.NewUsecase(s, notifySystemTaskRunner),
		StartCleanup: enqueue_log_cleanup.NewUsecase(s, notifySystemTaskRunner),
		Schedule:     schedule_system_tasks.NewUsecase(s, handlers, common.GetTimestamp, logger.LogWarn),
		Claim:        claim_system_tasks.NewUsecase(s, claim_system_tasks.Runtime{Handlers: handlers, LockUntil: systemTaskLockUntil, Warn: logger.LogWarn, Dispatch: dispatch}),
		Cleanup:      purge_old_logs.NewUsecase(s, logs, FailSystemTask, logSystemTaskLockError),
	}
	// Lazy assembly supplies the default without replacing a caller's earlier registration.
	systemTaskHandlersMu.Lock()
	if _, registered := systemTaskHandlers[domain.SystemTaskTypeLogCleanup]; !registered {
		systemTaskHandlers[domain.SystemTaskTypeLogCleanup] = logCleanupHandler{cleanup: r.Cleanup}
	}
	systemTaskHandlersMu.Unlock()
	return r
}

type logCleanupHandler struct{ cleanup purge_old_logs.Usecase }

func (logCleanupHandler) Type() string { return domain.SystemTaskTypeLogCleanup }
func (h logCleanupHandler) Run(ctx context.Context, task *domain.SystemTask, runnerID string) {
	_ = h.cleanup.Execute(ctx, purge_old_logs.Input{Task: task, RunnerID: runnerID})
}
func dispatch(handler domain.SystemTaskHandler, task *domain.SystemTask, runner string) {
	gopool.Go(func() {
		runWithLeaseHeartbeat(task, runner, func(ctx context.Context) { handler.Run(ctx, task, runner) })
	})
}

var (
	systemTaskRunnerOnce sync.Once
	// systemTaskWakeup signals the runner to check for runnable tasks
	// immediately instead of waiting for the idle poll. Buffered so a signal
	// raised while the runner is busy is not lost and is handled on the next loop.
	systemTaskWakeup = make(chan struct{}, 1)
)

// notifySystemTaskRunner wakes the runner without blocking. If a wakeup is
// already pending it is a no-op, which is fine since one pass drains all work.
func notifySystemTaskRunner() {
	select {
	case systemTaskWakeup <- struct{}{}:
	default:
	}
}

func StartSystemTaskRunner() {
	systemTaskRunnerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}

		runnerID := fmt.Sprintf("%s-%s", common.NodeName, common.GetRandomString(8))
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("system task runner started: runner=%s idle_interval=%s", runnerID, systemTaskRunnerIdleInterval))

			ticker := time.NewTicker(systemTaskRunnerIdleInterval)
			defer ticker.Stop()

			var lastScheduler time.Time
			var lastStaleLockCleanup time.Time
			runPass := func() {
				// The scheduler/stale-lock pass is throttled independently of the
				// claim pass: wakeups (e.g. a manual log cleanup) should claim
				// immediately without re-running the scheduler every time.
				now := time.Now()
				if now.Sub(lastStaleLockCleanup) >= systemTaskStaleLockInterval {
					lastStaleLockCleanup = now
					if err := Default().Store.ExpireStaleSystemTaskLocks(common.GetTimestamp()); err != nil {
						logger.LogWarn(context.Background(), fmt.Sprintf("system task stale lock cleanup failed: %v", err))
					}
				}
				if now.Sub(lastScheduler) >= systemTaskSchedulerInterval {
					lastScheduler = now
					_ = Default().Schedule.Execute()
				}
				_ = Default().Claim.Execute(runnerID)
			}

			runPass()
			for {
				select {
				case <-ticker.C:
				case <-systemTaskWakeup:
				}
				runPass()
			}
		})
	})
}

func runWithLeaseHeartbeat(task *domain.SystemTask, runnerID string, fn func(ctx context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interval := systemTaskLockTTL / 3
	if interval <= 0 {
		interval = systemTaskLockTTL
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := Default().Store.RenewSystemTaskLock(task.TaskID, runnerID, systemTaskLockUntil()); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	fn(ctx)
	close(done)
}

func systemTaskLockUntil() int64 {
	return common.GetTimestamp() + int64(systemTaskLockTTL.Seconds())
}

// NewSystemTaskProgressReporter returns a throttled progress callback bound to a
// running task. Handlers call it with (processed, total) as they iterate work;
// it persists a {processed,total,progress} state at most once every ~2s, always
// emitting the first update and the final 100%.
// Lock-loss errors are ignored: the lease heartbeat cancels the handler ctx on
// loss, so progress writes are best-effort and never abort the run themselves.
// The returned func is single-goroutine only (call it from the handler loop).
func NewSystemTaskProgressReporter(task *domain.SystemTask, runnerID string) func(processed, total int) {
	const minWriteInterval = 2 * time.Second
	var (
		lastWriteAt  time.Time
		lastProgress = -1
	)
	return func(processed, total int) {
		progress := 100
		if total > 0 {
			progress = processed * 100 / total
		}
		if progress < 0 {
			progress = 0
		} else if progress > 100 {
			progress = 100
		}

		if progress < 100 {
			if progress == lastProgress {
				return
			}
			if !lastWriteAt.IsZero() && time.Since(lastWriteAt) < minWriteInterval {
				return
			}
		}
		lastProgress = progress
		lastWriteAt = time.Now()

		state := domain.SystemTaskProgress{Total: total, Processed: processed, Progress: progress}
		_ = Default().Store.UpdateSystemTaskState(task.TaskID, runnerID, state)
	}
}

// FailSystemTask records the failure of a running system task and logs persistence errors.
func FailSystemTask(task *domain.SystemTask, runnerID string, err error) {
	logger.LogWarn(context.Background(), fmt.Sprintf("system task %s failed: %v", task.TaskID, err))
	if finishErr := Default().Store.FinishSystemTask(task.TaskID, runnerID, domain.SystemTaskStatusFailed, nil, err.Error()); finishErr != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system task %s failed to save failure state: %v", task.TaskID, finishErr))
	}
}

func logSystemTaskLockError(ctx context.Context, task *domain.SystemTask, err error) {
	if errors.Is(err, domain.ErrSystemTaskLockLost) {
		logger.LogWarn(ctx, fmt.Sprintf("system task %s lock lost", task.TaskID))
		return
	}
	logger.LogWarn(ctx, fmt.Sprintf("system task %s update failed: %v", task.TaskID, err))
}
