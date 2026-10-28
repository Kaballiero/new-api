package initialize_authorization

import "github.com/QuantumNous/new-api/internal/authorization/domain"

type Store interface {
	UpsertRole(domain.RoleSpec) error
	ResetRolePolicies([]string) error
}
type Policy interface {
	AddPolicy(string, string, string, string) (bool, error)
}
type Engine interface {
	CreateAndPublish() error
	CurrentPolicy() Policy
}
type Usecase struct {
	catalog func() domain.Catalog
	master  func() bool
	store   Store
	engine  Engine
}

func NewUsecase(catalog func() domain.Catalog, master func() bool, store Store, engine Engine) Usecase {
	return Usecase{catalog: catalog, master: master, store: store, engine: engine}
}
func (usecase Usecase) Execute() error {
	if usecase.master() {
		for _, spec := range usecase.catalog().BuiltInRoles {
			if err := usecase.store.UpsertRole(spec); err != nil {
				return domain.Internal(err)
			}
		}
		roles := usecase.catalog().BuiltInRoles
		subjects := make([]string, 0, len(roles))
		for _, spec := range roles {
			subjects = append(subjects, domain.RoleSubject(spec.Key))
		}
		if err := usecase.store.ResetRolePolicies(subjects); err != nil {
			return domain.Internal(err)
		}
	}
	if err := usecase.engine.CreateAndPublish(); err != nil {
		return domain.Internal(err)
	}
	if !usecase.master() {
		return nil
	}
	e := usecase.engine.CurrentPolicy()
	if e == nil {
		return domain.ErrEnforcerNotInitialized
	}
	for _, spec := range usecase.catalog().BuiltInRoles {
		if spec.Superuser {
			continue
		}
		for _, permission := range usecase.catalog().PermissionsForRole(spec.Key) {
			if _, err := e.AddPolicy(domain.RoleSubject(spec.Key), permission.Resource, permission.Action, domain.EffectAllow); err != nil {
				return domain.Internal(err)
			}
		}
	}
	return nil
}
