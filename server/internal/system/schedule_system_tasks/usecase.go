package schedule_system_tasks

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/internal/system/domain"
)

type Store interface {
	GetLatestSystemTasks([]string) (map[string]*domain.SystemTask, error)
	CreateSystemTask(string, any, any) (*domain.SystemTask, error)
	GetActiveSystemTask(string) (*domain.SystemTask, error)
}
type Usecase struct {
	store    Store
	handlers func() []domain.SystemTaskHandler
	now      func() int64
	warn     func(context.Context, string, ...any)
}

func NewUsecase(store Store, handlers func() []domain.SystemTaskHandler, now func() int64, warn func(context.Context, string, ...any)) Usecase {
	return Usecase{store: store, handlers: handlers, now: now, warn: warn}
}
func (u Usecase) Execute() error {
	now := u.now()
	handlers := u.handlers()
	scheduledHandlers := make([]domain.ScheduledSystemTaskHandler, 0, len(handlers))
	taskTypes := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		scheduled, ok := handler.(domain.ScheduledSystemTaskHandler)
		if !ok || !scheduled.Enabled() {
			continue
		}
		scheduledHandlers = append(scheduledHandlers, scheduled)
		taskTypes = append(taskTypes, scheduled.Type())
	}
	latestTasks, err := u.store.GetLatestSystemTasks(taskTypes)
	if err != nil {
		u.warn(context.Background(), fmt.Sprintf("system task scheduler query failed: %v", err))
		return err
	}
	for _, scheduled := range scheduledHandlers {
		latest := latestTasks[scheduled.Type()]
		if latest != nil {
			if latest.Status == domain.SystemTaskStatusPending || latest.Status == domain.SystemTaskStatusRunning {
				continue // an active row already exists
			}
			if now-latest.UpdatedAt < int64(scheduled.Interval().Seconds()) {
				continue // not due yet
			}
		}
		if _, err := u.store.CreateSystemTask(scheduled.Type(), scheduled.NewPayload(), nil); err != nil {
			activeTask, activeErr := u.store.GetActiveSystemTask(scheduled.Type())
			if activeErr == nil && activeTask != nil {
				continue
			}
			if activeErr != nil {
				u.warn(context.Background(), fmt.Sprintf("system task scheduler active lookup failed: type=%s err=%v", scheduled.Type(), activeErr))
			}
			u.warn(context.Background(), fmt.Sprintf("system task scheduler create failed: type=%s err=%v", scheduled.Type(), err))
			continue
		}
	}

	return nil
}
