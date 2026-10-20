package delete_channel

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	Delete(int) error
	IsMissing(error) bool
	Proxy(*domain.Channel) string
}
type ProxyCache interface {
	Reset()
	Invalidate(string)
}
type Usecase struct {
	store   Store
	cache   ProxyCache
	refresh func()
	audit   func(int, string)
}

func NewUsecase(store Store, cache ProxyCache, refresh func(), audit func(int, string)) Usecase {
	return Usecase{store: store, cache: cache, refresh: refresh, audit: audit}
}
func (usecase Usecase) Execute(input Input) *domain.Failure {
	name, proxy := "", ""
	lookupFailed := false
	if existing, err := usecase.store.Get(input.ID, false); err == nil && existing != nil {
		name = existing.Name
		proxy = usecase.store.Proxy(existing)
	} else {
		lookupFailed = true
	}
	if err := usecase.store.Delete(input.ID); err != nil {
		if usecase.store.IsMissing(err) {
			return &domain.Failure{Kind: "missing"}
		}
		return &domain.Failure{Kind: "internal", Cause: err}
	}
	usecase.refresh()
	if lookupFailed {
		usecase.cache.Reset()
	} else {
		usecase.cache.Invalidate(proxy)
	}
	usecase.audit(input.ID, name)
	return nil
}
