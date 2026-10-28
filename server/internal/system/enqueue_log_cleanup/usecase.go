package enqueue_log_cleanup

import "github.com/QuantumNous/new-api/internal/system/domain"

type Store interface {
	GetActiveSystemTask(string) (*domain.SystemTask, error)
	CreateSystemTask(string, any, any) (*domain.SystemTask, error)
}
type Usecase struct {
	store  Store
	notify func()
}

func NewUsecase(store Store, notify func()) Usecase { return Usecase{store: store, notify: notify} }
func (u Usecase) Execute(targetTimestamp int64) (*domain.SystemTask, error) {
	if targetTimestamp <= 0 {
		return nil, domain.ErrTargetTimestamp
	}

	activeTask, err := u.store.GetActiveSystemTask(domain.SystemTaskTypeLogCleanup)
	if err != nil {
		return nil, err
	}
	if activeTask != nil {
		return activeTask, nil
	}

	payload := domain.LogCleanupPayload{
		TargetTimestamp: targetTimestamp,
		BatchSize:       100,
	}
	state := domain.LogCleanupState{}
	task, err := u.store.CreateSystemTask(domain.SystemTaskTypeLogCleanup, payload, state)
	if err != nil {
		activeTask, activeErr := u.store.GetActiveSystemTask(domain.SystemTaskTypeLogCleanup)
		if activeErr == nil && activeTask != nil {
			return activeTask, nil
		}
		return nil, err
	}
	u.notify()
	return task, nil
}
