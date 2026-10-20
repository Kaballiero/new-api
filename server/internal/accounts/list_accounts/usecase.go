package list_accounts

import "github.com/QuantumNous/new-api/internal/accounts/domain"

type Reader interface {
	List(PageInput, SortInput) ([]*domain.User, int64, error)
}

type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase { return Usecase{reader: reader} }
func (u Usecase) Execute(page PageInput, sort SortInput) ([]*domain.User, int64, error) {
	return u.reader.List(page, sort)
}
