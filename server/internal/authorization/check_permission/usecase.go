package check_permission

import "github.com/QuantumNous/new-api/internal/authorization/domain"

type Usecase struct{ rules domain.AccessRules }

func NewUsecase(rules domain.AccessRules) Usecase { return Usecase{rules: rules} }
func (usecase Usecase) Execute(userID, systemRole int, permission domain.Permission) bool {
	return usecase.rules.Allowed(userID, systemRole, permission)
}
