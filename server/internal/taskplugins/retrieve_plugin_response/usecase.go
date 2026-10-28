package retrieve_plugin_response

import (
	"context"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	tasksdomain "github.com/QuantumNous/new-api/internal/tasks/domain"
)

type Availability struct{ Found, Claims, Sync, Background bool }
type Session interface {
	Owned(int, string) (*tasksdomain.Task, bool, error)
	Protocol(string) Availability
}
type Usecase struct{}
type Input struct {
	Session Session
	UserID  int
	TaskID  string
}
type Result struct {
	Task *tasksdomain.Task
	Mode string
}

func (Usecase) Execute(ctx context.Context, input Input) (Result, error) {
	task, exists, err := input.Session.Owned(input.UserID, input.TaskID)
	if err != nil {
		return Result{}, &domain.RetrieveError{Reason: "lookup_error", Cause: err}
	}
	if !exists || task == nil {
		return Result{}, &domain.RetrieveError{Reason: "missing"}
	}
	plugin := input.Session.Protocol(task.Platform)
	if !plugin.Found {
		return Result{}, &domain.RetrieveError{Reason: "no_plugin"}
	}
	if !plugin.Claims {
		return Result{}, &domain.RetrieveError{Reason: "no_claim"}
	}
	mode := "pending"
	if task.Status == tasksdomain.TaskStatusFailure {
		mode = "failure"
	} else if task.Status == tasksdomain.TaskStatusSuccess {
		mode = "events"
		if plugin.Sync || plugin.Background {
			mode = "final"
		}
	}
	return Result{Task: task, Mode: mode}, nil
}
