package claim_system_tasks

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/internal/system/domain"
)

type Store interface {
	FindEarliestPendingSystemTasks([]string) (map[string]*domain.SystemTask, error)
	ClaimSystemTask(int64, string, string, int64) (*domain.SystemTask, bool, error)
}
type Runtime struct {
	Handlers  func() []domain.SystemTaskHandler
	LockUntil func() int64
	Warn      func(context.Context, string, ...any)
	Dispatch  func(domain.SystemTaskHandler, *domain.SystemTask, string)
}
type Usecase struct {
	store   Store
	runtime Runtime
}

func NewUsecase(store Store, runtime Runtime) Usecase { return Usecase{store: store, runtime: runtime} }
func (u Usecase) Execute(runnerID string) error {
	handlers := u.runtime.Handlers()
	taskTypes := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		taskTypes = append(taskTypes, handler.Type())
	}
	pendingTasks, err := u.store.FindEarliestPendingSystemTasks(taskTypes)
	if err != nil {
		u.runtime.Warn(context.Background(), fmt.Sprintf("system task runner query failed: %v", err))
		return err
	}
	for _, handler := range handlers {
		task := pendingTasks[handler.Type()]
		if task == nil {
			continue
		}
		claimedTask, claimed, err := u.store.ClaimSystemTask(task.ID, handler.Type(), runnerID, u.runtime.LockUntil())
		if err != nil {
			u.runtime.Warn(context.Background(), fmt.Sprintf("system task claim failed: %v", err))
			continue
		}
		if !claimed {
			continue
		}
		u.runtime.Dispatch(handler, claimedTask, runnerID)
	}

	return nil
}
