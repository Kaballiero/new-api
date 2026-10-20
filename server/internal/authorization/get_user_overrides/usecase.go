package get_user_overrides

import "github.com/QuantumNous/new-api/internal/authorization/domain"

type Usecase struct {
	catalog func() domain.Catalog
	policy  func() domain.PolicyReader
}

func NewUsecase(catalog func() domain.Catalog, policy func() domain.PolicyReader) Usecase {
	return Usecase{catalog: catalog, policy: policy}
}
func (usecase Usecase) Execute(userID int) domain.PermissionsMap {
	e := usecase.policy()
	if e == nil {
		return domain.PermissionsMap{}
	}

	result := domain.PermissionsMap{}
	for _, resource := range usecase.catalog().Resources {
		policies, err := e.GetFilteredPolicy(0, domain.UserSubject(userID), resource.Resource)
		if err != nil {
			return domain.PermissionsMap{}
		}
		actions := make(map[string]bool, len(policies))
		for _, policy := range policies {
			if len(policy) >= 3 && usecase.catalog().HasPermission(domain.Permission{Resource: policy[1], Action: policy[2]}) {
				effect := domain.PolicyEffect(policy)
				if effect == domain.EffectAllow || effect == domain.EffectDeny {
					actions[policy[2]] = effect == domain.EffectAllow
				}
			}
		}
		if len(actions) > 0 {
			result[resource.Resource] = actions
		}
	}
	return result
}

// userOverridePolicies returns the override entries that differ from the managed
// role baseline; entries matching the baseline are omitted.
