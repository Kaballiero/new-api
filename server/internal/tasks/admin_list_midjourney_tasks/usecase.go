package admin_list_midjourney_tasks

import "github.com/QuantumNous/new-api/internal/tasks/domain"

type Reader interface {
	ListAllMidjourney(offset, limit int, query domain.MidjourneyQuery) []*domain.Midjourney
	CountAllMidjourney(query domain.MidjourneyQuery) int64
}
type Input struct {
	UserID int
	Offset int
	Limit  int
	Query  domain.MidjourneyQuery
}
type Result struct {
	Items []*domain.Midjourney
	Total int64
}
type Usecase struct {
	reader         Reader
	forwardEnabled func() bool
	serverAddress  func() string
}

func NewUsecase(reader Reader, forwardEnabled func() bool, serverAddress func() string) Usecase {
	return Usecase{reader: reader, forwardEnabled: forwardEnabled, serverAddress: serverAddress}
}
func (u Usecase) Execute(in Input) (Result, error) {
	items := u.reader.ListAllMidjourney(in.Offset, in.Limit, in.Query)
	total := u.reader.CountAllMidjourney(in.Query)
	if u.forwardEnabled() {
		for i, item := range items {
			item.ImageUrl = u.serverAddress() + "/mj/image/" + item.MjId
			items[i] = item
		}
	}
	return Result{Items: items, Total: total}, nil
}
