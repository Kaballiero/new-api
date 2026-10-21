package session

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/model"
)

type Logout struct {
	RefreshSIDFunc    func(string) (string, bool)
	ParseAccessFunc   func(string) (domain.AuthIdentity, error)
	RevokeRefreshFunc func(string, string, string) error
}

func (op Logout) RefreshSID(raw string) (string, bool)                { return op.RefreshSIDFunc(raw) }
func (op Logout) ParseAccess(raw string) (domain.AuthIdentity, error) { return op.ParseAccessFunc(raw) }
func (op Logout) Revoke(id int, sid, reason string) (bool, error) {
	return model.RevokeUserSession(id, sid, reason)
}
func (op Logout) RevokeRefresh(raw, sid, reason string) error {
	return op.RevokeRefreshFunc(raw, sid, reason)
}
