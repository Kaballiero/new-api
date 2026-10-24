package list_own_midjourney_tasks

import "github.com/QuantumNous/new-api/internal/tasks/domain"

type Reader interface {
	ListOwnMidjourney(userID, offset, limit int, query domain.MidjourneyQuery) []*domain.Midjourney
	CountOwnMidjourney(userID int, query domain.MidjourneyQuery) int64
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
	items := u.reader.ListOwnMidjourney(in.UserID, in.Offset, in.Limit, in.Query)
	total := u.reader.CountOwnMidjourney(in.UserID, in.Query)
	if u.forwardEnabled() {
		for i, item := range items {
			item.ImageUrl = u.serverAddress() + "/mj/image/" + item.MjId
			items[i] = item
		}
	}
	return Result{Items: items, Total: total}, nil
}
