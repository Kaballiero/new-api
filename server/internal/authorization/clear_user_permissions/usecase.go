package clear_user_permissions

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/authorization/domain"
)

type Writer interface {
	RemoveFilteredPolicy(int, ...string) (bool, error)
}
type Usecase struct {
	catalog func() domain.Catalog
	policy  func() Writer
}

func NewUsecase(catalog func() domain.Catalog, policy func() Writer) Usecase {
	return Usecase{catalog: catalog, policy: policy}
}
func (usecase Usecase) Execute(userID int) error {
	e := usecase.policy()
	if e == nil {
		return fmt.Errorf("authz enforcer is not initialized")
	}

	for _, resource := range usecase.catalog().Resources {
		if _, err := e.RemoveFilteredPolicy(0, domain.UserSubject(userID), resource.Resource); err != nil {
			return err
		}
	}
	return nil
}
