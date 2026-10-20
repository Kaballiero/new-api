package get_capabilities

import "github.com/QuantumNous/new-api/internal/authorization/domain"

type Usecase struct {
	catalog func() domain.Catalog
	rules   domain.AccessRules
}

func NewUsecase(catalog func() domain.Catalog, rules domain.AccessRules) Usecase {
	return Usecase{catalog: catalog, rules: rules}
}
func (usecase Usecase) Execute(userID int, systemRole int) domain.PermissionsMap {
	result := make(domain.PermissionsMap, len(usecase.catalog().Resources))
	for _, resource := range usecase.catalog().Resources {
		actions := make(map[string]bool, len(resource.Actions))
		for _, action := range resource.Actions {
			actions[action.Action] = usecase.rules.Allowed(userID, systemRole, domain.Permission{
				Resource: resource.Resource,
				Action:   action.Action,
			})
		}
		result[resource.Resource] = actions
	}
	return result
}
