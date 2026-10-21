package search_accounts

import "github.com/QuantumNous/new-api/internal/accounts/domain"

type Reader interface {
	Search(Input) ([]*domain.User, int64, error)
}

type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase { return Usecase{reader: reader} }

// Execute searches accounts; a storage failure is domain.Internal.
func (u Usecase) Execute(input Input) ([]*domain.User, int64, error) {
	users, total, err := u.reader.Search(input)
	if err != nil {
		return users, total, domain.Internal(err)
	}
	return users, total, nil
}
