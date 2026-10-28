package set_user_permissions_in_tx

import "github.com/QuantumNous/new-api/internal/authorization/domain"

type Writer interface {
	RemoveResource(int, string) error
	InsertOverrides(int, []domain.OverridePolicy) error
}
type Usecase struct {
	catalog func() domain.Catalog
	policy  func() domain.PolicyReader
	writer  Writer
}

func NewUsecase(catalog func() domain.Catalog, policy func() domain.PolicyReader, writer Writer) Usecase {
	return Usecase{catalog: catalog, policy: policy, writer: writer}
}
func (usecase Usecase) Execute(userID int, permissions domain.PermissionsMap) error {
	e := usecase.policy()
	if e == nil {
		return domain.ErrEnforcerNotInitialized
	}
	for resource, actions := range permissions {
		if !usecase.catalog().HasResource(resource) {
			continue
		}
		if err := usecase.writer.RemoveResource(userID, resource); err != nil {
			return domain.Internal(err)
		}
		policies := domain.UserOverridePolicies(e, usecase.catalog(), resource, actions)
		if len(policies) == 0 {
			continue
		}
		if err := usecase.writer.InsertOverrides(userID, policies); err != nil {
			return domain.Internal(err)
		}
	}
	return nil
}
