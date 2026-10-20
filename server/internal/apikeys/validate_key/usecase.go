package validate_key

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
)

type Store interface {
	Lookup(string) (*domain.Token, error)
	UpdateStatus(int) error
	IsMissing(error) bool
}
type Errors struct{ NotProvided, Invalid, Database func() error }
type Usecase struct {
	store        Store
	errors       Errors
	now          func() int64
	cacheEnabled func() bool
	log          func(string)
}

func NewUsecase(store Store, errors Errors, now func() int64, cacheEnabled func() bool, log func(string)) Usecase {
	return Usecase{store: store, errors: errors, now: now, cacheEnabled: cacheEnabled, log: log}
}
func (usecase Usecase) Execute(key string) error {
	if key == "" {
		return usecase.errors.NotProvided()
	}
	token, err := usecase.store.Lookup(key)
	if err == nil {
		if token.Status == domain.StatusExhausted ||
			token.Status == domain.StatusExpired ||
			token.Status != domain.StatusEnabled {
			return usecase.errors.Invalid()
		}
		if token.ExpiredTime != -1 && token.ExpiredTime < usecase.now() {
			if !usecase.cacheEnabled() {
				token.Status = domain.StatusExpired
				err := usecase.store.UpdateStatus(token.Status)
				if err != nil {
					usecase.log("failed to update token status" + err.Error())
				}
			}
			return usecase.errors.Invalid()
		}
		if !token.UnlimitedQuota && token.RemainQuota <= 0 {
			if !usecase.cacheEnabled() {
				token.Status = domain.StatusExhausted
				err := usecase.store.UpdateStatus(token.Status)
				if err != nil {
					usecase.log("failed to update token status" + err.Error())
				}
			}
			return usecase.errors.Invalid()
		}
		return nil
	}
	usecase.log("ValidateUserToken: failed to get token: " + err.Error())
	if usecase.store.IsMissing(err) {
		return usecase.errors.Invalid()
	}
	return fmt.Errorf("%w: %v", usecase.errors.Database(), err)
}
