package get_groups

import "github.com/QuantumNous/new-api/internal/accounts/domain"

type AccountGroups interface {
	UserGroup(int, bool) (string, error)
}
type Usecase struct {
	groups domain.Groups
	reader AccountGroups
}

func NewUsecase(groups domain.Groups, reader AccountGroups) Usecase {
	return Usecase{groups: groups, reader: reader}
}
func (u Usecase) Execute() []string { return u.groups.Names() }
func (u Usecase) ForUser(input Input) map[string]map[string]any {
	group, _ := u.reader.UserGroup(input.UserID, false)
	return u.groups.ForUser(group)
}
