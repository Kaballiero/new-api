package get_passkey_status

import (
	"errors"
	"time"
)

type Reader interface {
	LastUsed(userID int) (*time.Time, error)
	MissingError() error
}

func (usecase Usecase) Execute(userID int) (Output, error) {
	lastUsed, err := usecase.reader.LastUsed(userID)
	if errors.Is(err, usecase.reader.MissingError()) {
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
