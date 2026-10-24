package admin_list_tasks

import "github.com/QuantumNous/new-api/internal/tasks/domain"

type Reader interface {
	ListAll(offset, limit int, query domain.TaskQuery) []*domain.Task
	CountAll(query domain.TaskQuery) int64
	FillUsernames(tasks []*domain.Task)
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
	items := u.reader.ListAll(in.Offset, in.Limit, in.Query)
	total := u.reader.CountAll(in.Query)
	u.reader.FillUsernames(items)
	views := make([]*domain.TaskView, len(items))
	for i, task := range items {
		views[i] = domain.ProjectTaskView(task, in.Visibility, u.projectData)
	}
	return Result{Items: views, Total: total}, nil
}
