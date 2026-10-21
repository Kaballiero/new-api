package validate_key

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Store interface {
	Lookup(string) (*domain.Token, error)
	UpdateStatus(int) error
	IsMissing(error) bool
}
type Usecase struct {
	store        Store
	now          func() int64
	cacheEnabled func() bool
	log          func(string)
}

func NewUsecase(store Store, now func() int64, cacheEnabled func() bool, log func(string)) Usecase {
	return Usecase{store: store, now: now, cacheEnabled: cacheEnabled, log: log}
}

// Execute checks that the key exists, is enabled, not expired and not exhausted.
// A failed lookup other than a missing key is returned as domain.Internal.
func (usecase Usecase) Execute(key string) error {
	if key == "" {
		return domain.ErrKeyNotProvided
	}
	token, err := usecase.store.Lookup(key)
	if err == nil {
		if token.Status == domain.StatusExhausted ||
			token.Status == domain.StatusExpired ||
			token.Status != domain.StatusEnabled {
			return domain.ErrKeyInvalid
		}
		if token.ExpiredTime != -1 && token.ExpiredTime < usecase.now() {
			if !usecase.cacheEnabled() {
				token.Status = domain.StatusExpired
				err := usecase.store.UpdateStatus(token.Status)
				if err != nil {
					usecase.log("failed to update token status" + err.Error())
				}
			}
			return domain.ErrKeyInvalid
		}
		if !token.UnlimitedQuota && token.RemainQuota <= 0 {
			if !usecase.cacheEnabled() {
				token.Status = domain.StatusExhausted
				err := usecase.store.UpdateStatus(token.Status)
				if err != nil {
					usecase.log("failed to update token status" + err.Error())
				}
			}
			return domain.ErrKeyInvalid
		}
		return nil
	}
	usecase.log("ValidateUserToken: failed to get token: " + err.Error())
	if usecase.store.IsMissing(err) {
		return domain.ErrKeyInvalid
	}
	return domain.Internal(err)
}
