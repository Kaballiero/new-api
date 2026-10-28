package get_key_status

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Reader interface {
	Get(int, int) (*domain.Token, error)
}

func (usecase Usecase) Execute(id int, userID int) (*domain.Token, int64, error) {
	t, e := usecase.reader.Get(id, userID)
	if e != nil {
		return nil, 0, e
	}
	expiry := t.ExpiredTime
	if expiry == -1 {
		expiry = 0
	}
	return t, expiry, nil
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}
