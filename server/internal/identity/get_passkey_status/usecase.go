package get_passkey_status

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type Reader interface {
	LastUsed(userID int) (*time.Time, error)
}

func (usecase Usecase) Execute(userID int) (Output, error) {
	lastUsed, err := usecase.reader.LastUsed(userID)
	if errors.Is(err, domain.ErrPasskeyNotFound) {
		return Output{}, nil
	}
	if err != nil {
		return Output{}, err
	}
	return Output{Enabled: true, LastUsedAt: lastUsed}, nil
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}
