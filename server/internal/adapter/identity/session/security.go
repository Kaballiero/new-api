package session

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
)

type Reference struct {
	Invalid      func() error
	ValidateFunc func(domain.AuthIdentity) error
}

func (op Reference) InvalidError() error { return op.Invalid() }
func (Reference) LoadSession(sid string) (int64, int64, error) {
	session, err := model.GetUserSessionCached(sid)
	if err != nil {
		return 0, 0, err
	}
	return session.UserAuthVersion, session.Version, nil
}
func (op Reference) Validate(identity domain.AuthIdentity) error { return op.ValidateFunc(identity) }

type SecurityAdvancement struct {
	Identity  domain.AuthIdentity
	Reason    string
	IssueFunc func(*model.UserSession, string, bool) (*dto.AuthBundle, error)
	Revoked   func() error
	Bundle    *dto.AuthBundle
	session   *model.UserSession
}

func (op *SecurityAdvancement) BumpUserVersion() (int64, error) {
	return model.BumpUserAuthVersion(op.Identity.UserID)
}
func (op *SecurityAdvancement) LoadAccount() (domain.AccountState, error) {
	user, err := model.GetUserCache(op.Identity.UserID)
	if err != nil {
		return domain.AccountState{}, err
	}
	return domain.AccountState{Status: user.Status, AuthVersion: user.AuthVersion}, nil
}
func (op *SecurityAdvancement) RevokedError() error { return op.Revoked() }
func (op *SecurityAdvancement) Advance(version int64) error {
	var err error
	op.session, err = model.AdvanceUserSessionAuthVersion(op.Identity.UserID, op.Identity.SessionID, op.Identity.SessionVersion, op.Identity.UserAuthVersion, version)
	return err
}
func (op *SecurityAdvancement) RevokeOthers() error {
	_, err := model.RevokeOtherUserSessions(op.Identity.UserID, op.Identity.SessionID, op.Reason)
	return err
}
func (op *SecurityAdvancement) Issue() error {
	var err error
	op.Bundle, err = op.IssueFunc(op.session, "", true)
	return err
}
