package enqueue_system_task

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

type Input struct {
	Type    string
	Payload any
}
type Result struct {
	Task    *domain.SystemTask
	Created bool
}

func (u Usecase) Execute(input Input) (Result, error) {
	activeTask, err := u.store.GetActiveSystemTask(input.Type)
	if err != nil {
		return Result{}, err
	}
	if activeTask != nil {
		return Result{Task: activeTask}, nil
	}

	task, err := u.store.CreateSystemTask(input.Type, input.Payload, nil)
	if err != nil {
		activeTask, activeErr := u.store.GetActiveSystemTask(input.Type)
		if activeErr == nil && activeTask != nil {
			return Result{Task: activeTask}, nil
		}
		return Result{}, err
	}
	u.notify()
	return Result{Task: task, Created: true}, nil
}
