package get_custom_oauth_provider

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Store interface {
	Get(int) (domain.CustomOAuthProvider, error)
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (u Usecase) Execute(id int) (domain.CustomOAuthProvider, error) {
	provider, err := u.store.Get(id)
	if err != nil {
		return domain.CustomOAuthProvider{}, domain.ErrCustomOAuthProviderMissing
	}
	return provider, nil
}
