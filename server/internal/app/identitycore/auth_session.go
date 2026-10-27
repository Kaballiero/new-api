package identitycore

import (
	"github.com/QuantumNous/new-api/common"
	sessionadapter "github.com/QuantumNous/new-api/internal/adapter/identity/session"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
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
	op := &sessionadapter.CreateSessionOperation{UserID: userID,
		NewSession: func(version int64) (*model.UserSession, string, error) {
			return sessionadapter.NewLoginSession(userID, version, loginMethod, ip, userAgent)
		},
		Issue: func(session *model.UserSession, token string) error {
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

func ValidateLoginSession(identity domain.AuthIdentity) (*model.UserSession, *model.UserBase, error) {
	op := &sessionadapter.ValidateSessionOperation{}
	if err := identityRuntime().ValidateSession.Execute(op, identity.UserID, identity.SessionID, identity.UserAuthVersion, identity.SessionVersion); err != nil {
		return nil, nil, err
	}
	return op.Session, op.User, nil
}

// AdvanceCurrentSessionToUserVersion follows a committed security mutation.
func AdvanceCurrentSessionToUserVersion(identity domain.AuthIdentity, reason string) (*identitydto.AuthBundle, error) {
	op := &sessionadapter.SecurityAdvancement{Identity: identity, Reason: reason, IssueFunc: sessionadapter.IssueAuthBundle}
	if err := identityRuntime().AdvanceSessionSecurity.Execute(op, identity.UserAuthVersion); err != nil {
		return nil, err
	}
	return op.Bundle, nil
}

func RefreshLoginSession(rawRefreshToken, expectedSID, ip, userAgent string) (*identitydto.AuthBundle, *model.User, error) {
	var bundle *identitydto.AuthBundle
	op := &sessionadapter.RefreshSessionOperation{Hash: sessionadapter.HashRefreshSecret, Grace: domain.RefreshReplayWindow, Metadata: sessionadapter.TruncateAuthMetadata,
		Issue: func(session *model.UserSession, token string) error {
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
	return identityRuntime().RevokeRefreshToken.Execute(sessionadapter.RevokeRefreshOperation{Hash: sessionadapter.HashRefreshSecret}, rawRefreshToken, expectedSID, reason)
}

func sessionPolicy() domain.SessionPolicy {
	return domain.SessionPolicy{
		EnabledStatus: common.UserStatusEnabled, ActiveStatus: model.UserSessionStatusActive,
		ActiveLimit:           func() int64 { return int64(common.UserSessionActiveLimit) },
		IssuanceWindowSeconds: func() int64 { return common.UserSessionIssuanceWindowSeconds },
		IssuanceLimit:         func() int64 { return int64(common.UserSessionIssuanceLimit) },
	}
}
