package search_accounts

import "github.com/QuantumNous/new-api/internal/accounts/domain"

type Reader interface {
	Search(Input) ([]*domain.User, int64, error)
}

type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase                               { return Usecase{reader: reader} }
func (u Usecase) Execute(input Input) ([]*domain.User, int64, error) { return u.reader.Search(input) }
