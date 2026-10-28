package validate_token

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"
)

type Store interface {
	Lookup(string) (*domain.Caller, error)
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }

// Execute preserves the legacy token normalization and missing-token behavior.
// Revocation is observed through the live lookup, without a credential cache.
func (u Usecase) Execute(token string) (*domain.Caller, error) {
	if token == "" {
		return nil, nil
	}
	token = strings.Replace(token, "Bearer ", "", 1)
	caller, err := u.store.Lookup(token)
	if err != nil {
		return nil, domain.Internal(err)
	}
	return caller, nil
}
