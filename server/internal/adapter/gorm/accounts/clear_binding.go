package accountsadapter

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type ClearBindingOperation struct {
	Role  func() int
	Audit func(userID int, username, bindingType string)
	user  *model.User
}

func (op *ClearBindingOperation) LoadTarget(id int) (int, error) {
	var err error
	op.user, err = model.GetUserById(id, false)
	if err != nil {
		return 0, err
	}
	return op.user.Role, nil
}
func (op *ClearBindingOperation) ActorRole() int { return op.Role() }
func (*ClearBindingOperation) RootRole() int     { return common.RoleRootUser }
func (op *ClearBindingOperation) Clear(bindingType string) error {
	return op.user.ClearBinding(bindingType)
}
func (op *ClearBindingOperation) AuditCurrent(bindingType string) {
	op.Audit(op.user.Id, op.user.Username, bindingType)
}
