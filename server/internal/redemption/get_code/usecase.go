package get_code

import "github.com/QuantumNous/new-api/internal/redemption/domain"

// Reader loads a code without opening a scenario transaction.
type Reader interface {
	Get(id int) (domain.Code, error)
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

func (u Usecase) Execute(id int) (domain.Code, error) {
	if id == 0 {
		return domain.Code{}, domain.ErrIDEmpty
	}
	return u.reader.Get(id)
}
