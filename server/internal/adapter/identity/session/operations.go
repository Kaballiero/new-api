package session

import (
	"time"

	cacheaccounts "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

// CreateSessionOperation keeps the canonical records and the original codec callback.
type CreateSessionOperation struct {
	Cache cacheaccounts.Store

	SQL identitystore.Store

	UserID        int
	NewSession    func(int64) (*identityrecord.UserSession, string, error)
	Issue         func(*identityrecord.UserSession, string) error
	Session       *identityrecord.UserSession
	refreshSecret string
}

func (op *CreateSessionOperation) LoadAccount() (domain.AccountState, error) {
	u, e := op.Cache.GetUserCache(op.UserID)
	if e != nil {
		return domain.AccountState{}, e
	}
	return domain.AccountState{Status: u.Status, AuthVersion: u.AuthVersion}, nil
}
func (op *CreateSessionOperation) CountActive(now int64) (int64, error) {
	return op.SQL.CountActiveUserSessions(op.UserID, now)
}
func (op *CreateSessionOperation) CountIssued(since int64) (int64, error) {
	return op.SQL.CountUserSessionsCreatedSince(op.UserID, since)
}
func (op *CreateSessionOperation) Create(authVersion int64) error {
	s, secret, e := op.NewSession(authVersion)
	if e != nil {
		return e
	}
	op.Session, op.refreshSecret = s, secret
	return op.SQL.CreateUserSession(s)
}
func (op *CreateSessionOperation) IssueBundle() error {
	return op.Issue(op.Session, op.Session.SID+"."+op.refreshSecret)
}
func (op *CreateSessionOperation) RevokeCreated(reason string) error {
	_, e := op.SQL.RevokeUserSession(op.UserID, op.Session.SID, reason)
	return e
}

type ValidateSessionOperation struct {
	Cache cacheaccounts.Store

	SQL identitystore.Store

	Session *identityrecord.UserSession
	User    *cacheaccounts.UserBase
}

func (op *ValidateSessionOperation) LoadSession(sid string) (domain.SessionState, error) {
	s, e := op.SQL.GetUserSessionCached(sid)
	if e != nil {
		return domain.SessionState{}, e
	}
	op.Session = s
	return sessionState(s), nil
}
func (op *ValidateSessionOperation) LoadAccount(userID int) (domain.AccountState, error) {
	u, e := op.Cache.GetUserCache(userID)
	if e != nil {
		return domain.AccountState{}, e
	}
	op.User = u
	return domain.AccountState{Status: u.Status, AuthVersion: u.AuthVersion}, nil
}

func sessionState(s *identityrecord.UserSession) domain.SessionState {
	return domain.SessionState{UserID: s.UserID, UserAuthVersion: s.UserAuthVersion, Version: s.Version, Status: s.Status, RevokedAt: s.RevokedAt, ExpiresAt: s.ExpiresAt}
}

type RefreshSessionOperation struct {
	Cache cacheaccounts.Store

	Accounts accountstore.Store

	SQL identitystore.Store

	Hash            func(string) string
	PresentedSecret string
	Grace           time.Duration
	Metadata        func(string, int) string
	Issue           func(*identityrecord.UserSession, string) error
	Session         *identityrecord.UserSession
	CurrentUser     *accountrecord.User
	rotated         *identityrecord.UserSession
	nextSecret      string
}

func (op *RefreshSessionOperation) LoadSession(sid string) (domain.SessionState, error) {
	s, e := op.SQL.GetUserSessionCached(sid)
	if e != nil {
		return domain.SessionState{}, e
	}
	op.Session = s
	return sessionState(s), nil
}
func (op *RefreshSessionOperation) LoadCachedAccount(userID int) (domain.AccountState, error) {
	u, e := op.Cache.GetUserCache(userID)
	if e != nil {
		return domain.AccountState{}, e
	}
	return domain.AccountState{Status: u.Status, AuthVersion: u.AuthVersion}, nil
}
func (op *RefreshSessionOperation) LoadCurrentAccount(userID int) (domain.AccountState, error) {
	u, e := op.Accounts.GetSelfUserById(userID)
	if e != nil {
		return domain.AccountState{}, e
	}
	op.CurrentUser = u
	return domain.AccountState{Status: u.Status, AuthVersion: u.AuthVersion}, nil
}
func (op *RefreshSessionOperation) Revoke(reason string) error {
	_, e := op.SQL.RevokeUserSession(op.Session.UserID, op.Session.SID, reason)
	return e
}
func (op *RefreshSessionOperation) Rotate(nextSecret string) error {
	op.nextSecret = nextSecret
	var e error
	op.rotated, e = op.SQL.RotateUserSessionRefresh(op.Session.UserID, op.Session.SID, op.Hash(op.PresentedSecret), op.Hash(nextSecret), time.Now().Unix(), op.Grace)
	return e
}
func (op *RefreshSessionOperation) RotationMatchesNextSecret() bool {
	return op.rotated != nil && op.Hash(op.nextSecret) == op.rotated.RefreshHash
}
func (op *RefreshSessionOperation) SetResponseMetadata(ip, userAgent string) {
	op.rotated.IP = op.Metadata(ip, 64)
	op.rotated.UserAgent = op.Metadata(userAgent, 512)
}
func (op *RefreshSessionOperation) IssueBundle() error {
	return op.Issue(op.rotated, op.Session.SID+"."+op.nextSecret)
}

type RevokeRefreshOperation struct {
	SQL  identitystore.Store
	Hash func(string) string
}

func (op RevokeRefreshOperation) RevokeByHash(sid, secret, reason string) error {
	_, e := op.SQL.RevokeUserSessionByRefreshHash(sid, op.Hash(secret), reason)
	return e
}

func (op *RefreshSessionOperation) SetPresentedSecret(secret string) { op.PresentedSecret = secret }
