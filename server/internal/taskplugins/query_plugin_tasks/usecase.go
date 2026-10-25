package query_plugin_tasks

import (
	"context"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	tasksdomain "github.com/QuantumNous/new-api/internal/tasks/domain"
)

type Reader interface {
	ByPlatforms(int, []string, []string) ([]*tasksdomain.Task, error)
}
type Projector interface {
	View(*tasksdomain.Task) (map[string]any, error)
}
type Usecase struct {
	reader     Reader
	projector  Projector
	pluginType int
	suno       string
}

func NewUsecase(r Reader, p Projector, pluginType int, suno string) Usecase {
	return Usecase{reader: r, projector: p, pluginType: pluginType, suno: suno}
}

type Input struct {
	UserID   int
	Identity domain.Identity
	TaskIDs  []string
}

type Result struct {
	Views []map[string]any
	Found int
}

func (u Usecase) Execute(ctx context.Context, input Input) (Result, error) {
	tasks, err := u.reader.ByPlatforms(input.UserID, input.Identity.Platforms(u.pluginType, u.suno), input.TaskIDs)
	if err != nil {
		return Result{}, &domain.QueryError{Stage: "database_error", Cause: err}
	}
	byID := make(map[string]*tasksdomain.Task, len(tasks))
	for _, task := range tasks {
		byID[task.TaskID] = task
	}
	views := make([]map[string]any, 0, len(input.TaskIDs))
	for _, id := range input.TaskIDs {
		task := byID[id]
		if task == nil {
			return Result{}, &domain.QueryError{Stage: "task_not_found", Found: len(tasks)}
		}
		view, err := u.projector.View(task)
		if err != nil {
			return Result{}, &domain.QueryError{Stage: "view", Found: len(tasks), Cause: err}
		}
		views = append(views, view)
	}
	return Result{Views: views, Found: len(tasks)}, nil
}
