package accountsadapter

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	core "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	"github.com/QuantumNous/new-api/model"
)

// AdminReadOperation сохраняет исходный указатель для HTTP-ответа и исходные данные аудита.
type AdminReadOperation struct {
	SQL core.Store

	user   *model.User
	Delete func(int) error
	Audit  func(domain.AdminTarget)
}

func (op *AdminReadOperation) LoadOrigin(id int) (domain.AdminTarget, error) {
	user, err := op.SQL.GetUserById(id, false)
	if err != nil {
		return domain.AdminTarget{}, adminLoadError(err)
	}
	op.user = user
	return adminTarget(user), nil
}
func (op *AdminReadOperation) ResponseAccount() *domain.User          { return adminQueryUser(op.user) }
func (op *AdminReadOperation) HardDelete(id int) error                { return op.Delete(id) }
func (op *AdminReadOperation) AuditDeleted(target domain.AdminTarget) { op.Audit(target) }
