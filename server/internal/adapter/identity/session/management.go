package session

import (
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	"github.com/QuantumNous/new-api/internal/identity/dto"
)

type Management struct {
	SQL identitystore.Store

	ListFunc func(int, string) ([]dto.LoginSessionView, error)
}

func (op Management) List(id int, sid string) ([]dto.LoginSessionView, error) {
	return op.ListFunc(id, sid)
}
func (op Management) Revoke(id int, sid, reason string) (bool, error) {
	return op.SQL.RevokeUserSession(id, sid, reason)
}
func (op Management) RevokeOthers(id int, sid, reason string) (int64, error) {
	return op.SQL.RevokeOtherUserSessions(id, sid, reason)
}
