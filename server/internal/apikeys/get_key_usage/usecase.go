package get_key_usage

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Reader interface {
	ByKey(string) (*domain.Token, error)
}

func (usecase Usecase) Execute(key string) (*domain.Token, int64, *domain.Failure) {
	t, e := usecase.reader.ByKey(key)
	if e != nil {
		usecase.log("failed to get token by key: " + e.Error())
		return nil, 0, &domain.Failure{Kind: "usage"}
	}
	expiry := t.ExpiredTime
	if expiry == -1 {
		expiry = 0
	}
	return t, expiry, nil
}

type Usecase struct {
	reader Reader
	log    func(string)
}

func NewUsecase(reader Reader, log func(string)) Usecase {
	return Usecase{reader: reader, log: log}
}
