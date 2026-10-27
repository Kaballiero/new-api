package delete_custom_oauth_provider

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Store interface {
	Get(int) (domain.CustomOAuthProvider, error)
	BindingCount(int) (int64, error)
	Delete(int) error
}
type Registry interface{ Unregister(string) }
type Usecase struct {
	store    Store
	registry Registry
}

func NewUsecase(store Store, registry Registry) Usecase {
	return Usecase{store: store, registry: registry}
}
func (u Usecase) Execute(id int) error {
	provider, err := u.store.Get(id)
	if err != nil {
		return domain.ErrCustomOAuthProviderMissing
	}
	count, err := u.store.BindingCount(id)
	if err != nil {
		return domain.ErrCustomOAuthBindingCheck
	}
	if count > 0 {
		return domain.ErrCustomOAuthProviderBound
	}
	if err := u.store.Delete(id); err != nil {
		return err
	}
	u.registry.Unregister(provider.Slug)
	return nil
}
