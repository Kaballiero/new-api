package get_own_task

import "github.com/QuantumNous/new-api/internal/tasks/domain"

type Reader interface {
	GetOwn(userID int, taskID string) (*domain.Task, bool, error)
}
type Input struct {
	UserID int
	TaskID string
}
type Result struct {
	Task           *domain.Task
	CreatedAt      int64
	FailReason     string
	Reconciliation map[string]any
}
type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase { return Usecase{reader: reader} }
func (u Usecase) Execute(in Input) (Result, error) {
	task, exists, err := u.reader.GetOwn(in.UserID, in.TaskID)
	if err != nil {
		return Result{}, domain.ErrTaskQuery
	}
	if !exists {
		return Result{}, domain.ErrTaskNotFound
	}
	createdAt := task.CreatedAt
	if createdAt == 0 {
		createdAt = task.SubmitTime
	}
	reason := task.FailReason
	if task.Status == domain.TaskStatusSuccess && domain.FailReasonIsLegacyResultURL(reason) {
		reason = ""
	}
	return Result{Task: task, CreatedAt: createdAt, FailReason: reason, Reconciliation: domain.OpenRouterSyncReconciliation(task)}, nil
}
