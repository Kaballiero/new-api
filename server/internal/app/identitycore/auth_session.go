package identitycore

import (
	"github.com/QuantumNous/new-api/common"
	cacheaccounts "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	sessionadapter "github.com/QuantumNous/new-api/internal/adapter/identity/session"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
)

func CreateLoginSession(userID int, loginMethod, ip, userAgent string) (*identitydto.AuthBundle, error) {
	return createLoginSession(userID, 0, loginMethod, ip, userAgent)
}

func CreateLoginSessionAtAuthVersion(userID int, expectedAuthVersion int64, loginMethod, ip, userAgent string) (*identitydto.AuthBundle, error) {
	if expectedAuthVersion <= 0 {
		return nil, domain.ErrLoginSessionInvalid
	}
	return createLoginSession(userID, expectedAuthVersion, loginMethod, ip, userAgent)
}

func createLoginSession(userID int, expectedAuthVersion int64, loginMethod, ip, userAgent string) (*identitydto.AuthBundle, error) {
	var bundle *identitydto.AuthBundle
	op := &sessionadapter.CreateSessionOperation{Cache: accountstorage.Cache(), SQL: identitystorage.SQL(), UserID: userID,
		NewSession: func(version int64) (*identityrecord.UserSession, string, error) {
			return sessionadapter.NewLoginSession(userID, version, loginMethod, ip, userAgent)
		},
		Issue: func(session *identityrecord.UserSession, token string) error {
			var err error
			bundle, err = sessionadapter.IssueAuthBundle(session, token, true)
			return err
		},
	}
	if err := identityRuntime().CreateSession.Execute(op, expectedAuthVersion); err != nil {
		return nil, err
	}
	return bundle, nil
}

func ValidateLoginSession(identity domain.AuthIdentity) (*identityrecord.UserSession, *cacheaccounts.UserBase, error) {
	op := &sessionadapter.ValidateSessionOperation{Cache: accountstorage.Cache(), SQL: identitystorage.SQL()}
	if err := identityRuntime().ValidateSession.Execute(op, identity.UserID, identity.SessionID, identity.UserAuthVersion, identity.SessionVersion); err != nil {
		return nil, nil, err
	}
	return op.Session, op.User, nil
}

// AdvanceCurrentSessionToUserVersion follows a committed security mutation.
func AdvanceCurrentSessionToUserVersion(identity domain.AuthIdentity, reason string) (*identitydto.AuthBundle, error) {
	op := &sessionadapter.SecurityAdvancement{Cache: accountstorage.Cache(), SQL: identitystorage.SQL(), Identity: identity, Reason: reason, IssueFunc: sessionadapter.IssueAuthBundle}
	if err := identityRuntime().AdvanceSessionSecurity.Execute(op, identity.UserAuthVersion); err != nil {
		return nil, err
	}
	return op.Bundle, nil
}

func RefreshLoginSession(rawRefreshToken, expectedSID, ip, userAgent string) (*identitydto.AuthBundle, *accountrecord.User, error) {
	var bundle *identitydto.AuthBundle
	op := &sessionadapter.RefreshSessionOperation{Accounts: accountstorage.SQL(), Cache: accountstorage.Cache(), SQL: identitystorage.SQL(), Hash: sessionadapter.HashRefreshSecret, Grace: domain.RefreshReplayWindow, Metadata: sessionadapter.TruncateAuthMetadata,
		Issue: func(session *identityrecord.UserSession, token string) error {
			var err error
			bundle, err = sessionadapter.IssueAuthBundle(session, token, true)
			return err
		},
	}
	if err := identityRuntime().RefreshSession.Execute(op, rawRefreshToken, expectedSID, ip, userAgent); err != nil {
		return nil, nil, err
	}
	return bundle, op.CurrentUser, nil
}

func RevokeByRefreshToken(rawRefreshToken, expectedSID, reason string) error {
	return identityRuntime().RevokeRefreshToken.Execute(sessionadapter.RevokeRefreshOperation{SQL: identitystorage.SQL(), Hash: sessionadapter.HashRefreshSecret}, rawRefreshToken, expectedSID, reason)
}

func sessionPolicy() domain.SessionPolicy {
	return domain.SessionPolicy{
		EnabledStatus: common.UserStatusEnabled, ActiveStatus: identityrecord.UserSessionStatusActive,
		ActiveLimit:           func() int64 { return int64(common.UserSessionActiveLimit) },
		IssuanceWindowSeconds: func() int64 { return common.UserSessionIssuanceWindowSeconds },
		IssuanceLimit:         func() int64 { return int64(common.UserSessionIssuanceLimit) },
	}
}
