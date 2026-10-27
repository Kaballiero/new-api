package accountsadapter

import (
	"github.com/QuantumNous/new-api/common"
	core "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	"github.com/QuantumNous/new-api/model"
)

type ClearBindingOperation struct {
	SQL          core.Store
	ClearBinding func(*model.User, string) error

	Role  func() int
	Audit func(userID int, username, bindingType string)
	user  *model.User
}

func (op *ClearBindingOperation) LoadTarget(id int) (int, error) {
	var err error
	op.user, err = op.SQL.GetUserById(id, false)
	if err != nil {
		return 0, err
	}
	return op.user.Role, nil
}
func (op *ClearBindingOperation) ActorRole() int { return op.Role() }
func (*ClearBindingOperation) RootRole() int     { return common.RoleRootUser }
func (op *ClearBindingOperation) Clear(bindingType string) error {
	return op.ClearBinding(op.user, bindingType)
}
func (op *ClearBindingOperation) AuditCurrent(bindingType string) {
	op.Audit(op.user.Id, op.user.Username, bindingType)
}
