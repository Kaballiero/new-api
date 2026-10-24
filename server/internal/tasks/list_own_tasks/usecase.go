package list_own_tasks

import "github.com/QuantumNous/new-api/internal/tasks/domain"

type Reader interface {
	ListOwn(userID, offset, limit int, query domain.TaskQuery) []*domain.Task
	CountOwn(userID int, query domain.TaskQuery) int64
}
type Input struct {
	UserID     int
	Offset     int
	Limit      int
	Query      domain.TaskQuery
	Visibility domain.Visibility
}
type Result struct {
	Items []*domain.TaskView
	Total int64
}
type Usecase struct {
	reader      Reader
	projectData func([]byte, string) ([]byte, error)
}

func NewUsecase(reader Reader, projectData func([]byte, string) ([]byte, error)) Usecase {
	return Usecase{reader: reader, projectData: projectData}
}
func (u Usecase) Execute(in Input) (Result, error) {
	items := u.reader.ListOwn(in.UserID, in.Offset, in.Limit, in.Query)
	total := u.reader.CountOwn(in.UserID, in.Query)

	views := make([]*domain.TaskView, len(items))
	for i, task := range items {
		views[i] = domain.ProjectTaskView(task, domain.Visibility{}, u.projectData)
	}
	return Result{Items: views, Total: total}, nil
}
