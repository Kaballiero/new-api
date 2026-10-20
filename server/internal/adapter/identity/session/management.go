package session

import (
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
)

type Management struct {
	ListFunc func(int, string) ([]dto.LoginSessionView, error)
}

func (op Management) List(id int, sid string) ([]dto.LoginSessionView, error) {
	return op.ListFunc(id, sid)
}
func (Management) Revoke(id int, sid, reason string) (bool, error) {
	return model.RevokeUserSession(id, sid, reason)
}
func (Management) RevokeOthers(id int, sid, reason string) (int64, error) {
	return model.RevokeOtherUserSessions(id, sid, reason)
}
