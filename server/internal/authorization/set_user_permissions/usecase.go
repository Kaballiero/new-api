package set_user_permissions

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/authorization/domain"
)

type Policy interface {
	domain.PolicyReader
	AddPolicy(string, string, string, string) (bool, error)
	RemoveFilteredPolicy(int, ...string) (bool, error)
}
type Usecase struct {
	catalog func() domain.Catalog
	policy  func() Policy
}

func NewUsecase(catalog func() domain.Catalog, policy func() Policy) Usecase {
	return Usecase{catalog: catalog, policy: policy}
}
func (usecase Usecase) Execute(userID int, permissions domain.PermissionsMap) error {
	e := usecase.policy()
	if e == nil {
		return fmt.Errorf("authz enforcer is not initialized")
	}

	for resource, actions := range permissions {
		if !usecase.catalog().HasResource(resource) {
			continue
		}
		if _, err := e.RemoveFilteredPolicy(0, domain.UserSubject(userID), resource); err != nil {
			return err
		}
		for _, policy := range domain.UserOverridePolicies(e, usecase.catalog(), resource, actions) {
			if _, err := e.AddPolicy(domain.UserSubject(userID), policy.Resource, policy.Action, policy.Effect); err != nil {
				return err
			}
		}
	}
	return nil
}
