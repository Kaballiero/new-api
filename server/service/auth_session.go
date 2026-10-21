package service

import (
	sessionadapter "github.com/QuantumNous/new-api/internal/adapter/identity/session"
	"github.com/QuantumNous/new-api/internal/identity/advance_session_security"
	"github.com/QuantumNous/new-api/internal/identity/create_session"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/refresh_session"
	"github.com/QuantumNous/new-api/internal/identity/revoke_refresh_token"
	"github.com/QuantumNous/new-api/internal/identity/validate_session"
	"github.com/QuantumNous/new-api/internal/identity/validate_session_reference"

	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const RefreshCookieName = "new_api_refresh"

// SessionHintCookieName is the script-readable companion to RefreshCookieName.
// See writeSessionHintCookie for why it exists and what it is not.
const SessionHintCookieName = "new_api_has_session"

// SessionHintCookieValue is the only value the hint ever carries.
const SessionHintCookieValue = "1"

// Session and token errors are owned by the identity domain; the legacy names
// stay as aliases.
var (
	ErrLoginSessionInvalid  = domain.ErrLoginSessionInvalid
	ErrLoginSessionRevoked  = domain.ErrLoginSessionRevoked
	ErrLoginSessionMismatch = domain.ErrLoginSessionMismatch
	ErrRefreshTokenInvalid  = domain.ErrRefreshTokenInvalid
	ErrRefreshRace          = domain.ErrRefreshRace
	ErrAuthTokenInvalid     = domain.ErrAuthTokenInvalid
	ErrAuthTokenExpired     = domain.ErrAuthTokenExpired
	ErrProofScope           = domain.ErrProofScope
	ErrProofMethod          = domain.ErrProofMethod
	ErrProofContext         = domain.ErrProofContext
	ErrProofConsumed        = domain.ErrProofConsumed
)

type LoginSessionView = identitydto.LoginSessionView
type AuthBundle = identitydto.AuthBundle

func CreateLoginSession(userID int, loginMethod, ip, userAgent string) (*AuthBundle, error) {
	return createLoginSession(userID, 0, loginMethod, ip, userAgent)
}

func CreateLoginSessionAtAuthVersion(userID int, expectedAuthVersion int64, loginMethod, ip, userAgent string) (*AuthBundle, error) {
	if expectedAuthVersion <= 0 {
		return nil, ErrLoginSessionInvalid
	}
	return createLoginSession(userID, expectedAuthVersion, loginMethod, ip, userAgent)
}

func createLoginSession(userID int, expectedAuthVersion int64, loginMethod, ip, userAgent string) (*AuthBundle, error) {
	var bundle *AuthBundle
	op := &sessionadapter.CreateSessionOperation{UserID: userID,
		NewSession: func(version int64) (*model.UserSession, string, error) {
			return newLoginSession(userID, version, loginMethod, ip, userAgent)
		},
		Issue: func(session *model.UserSession, token string) error {
			var err error
			bundle, err = issueAuthBundle(session, token, true)
			return err
		},
	}
	if err := create_session.NewUsecase(op, sessionPolicy()).Execute(expectedAuthVersion); err != nil {
		return nil, err
	}
	return bundle, nil
}

func newLoginSession(userID int, authVersion int64, loginMethod, ip, userAgent string) (*model.UserSession, string, error) {
	refreshSecret, err := common.GenerateRandomCharsKey(64)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().Unix()
	session := &model.UserSession{
		SID:             uuid.NewString(),
		UserID:          userID,
		Version:         1,
		UserAuthVersion: authVersion,
		Status:          model.UserSessionStatusActive,
		RefreshHash:     hashRefreshSecret(refreshSecret),
		LoginMethod:     strings.TrimSpace(loginMethod),
		IP:              truncateAuthMetadata(ip, 64),
		UserAgent:       truncateAuthMetadata(userAgent, 512),
		CreatedAt:       now,
		LastActiveAt:    now,
		ExpiresAt:       time.Unix(now, 0).Add(LoginSessionTTL).Unix(),
	}
	if session.LoginMethod == "" {
		session.LoginMethod = "unknown"
	}
	return session, refreshSecret, nil
}

func ValidateLoginSession(identity AuthIdentity) (*model.UserSession, *model.UserBase, error) {
	op := &sessionadapter.ValidateSessionOperation{}
	if err := validate_session.NewUsecase(op, sessionPolicy()).Execute(identity.UserID, identity.SessionID, identity.UserAuthVersion, identity.SessionVersion); err != nil {
		return nil, nil, err
	}
	return op.Session, op.User, nil
}

// ValidateSessionReference validates a server-side flow bound to an existing
// dashboard session without requiring an access token on the callback request.
func ValidateSessionReference(userID int, sid string) (AuthIdentity, error) {
	return validate_session_reference.NewUsecase(sessionadapter.Reference{ValidateFunc: func(identity AuthIdentity) error { _, _, err := ValidateLoginSession(identity); return err }}).Execute(userID, sid)
}

// AdvanceCurrentSessionSecurity increments the user's global auth version,
// preserves only the current browser session at a new session version and
// returns a replacement access token. Call after a successful 2FA/passkey
// security-setting mutation that did not already advance AuthVersion.
func AdvanceCurrentSessionSecurity(identity AuthIdentity, reason string) (*AuthBundle, error) {
	op := newSessionAdvancement(identity, reason)
	if err := advance_session_security.BumpAndAdvance(op); err != nil {
		return nil, err
	}
	return op.Bundle, nil
}

// AdvanceCurrentSessionToUserVersion is used when the security mutation and
// AuthVersion increment were committed in the same transaction (for example,
// a password change).
func AdvanceCurrentSessionToUserVersion(identity AuthIdentity, reason string) (*AuthBundle, error) {
	op := newSessionAdvancement(identity, reason)
	if err := advance_session_security.AdvanceFromCurrentVersion(identity.UserAuthVersion, common.UserStatusEnabled, op); err != nil {
		return nil, err
	}
	return op.Bundle, nil
}

func RefreshLoginSession(rawRefreshToken, expectedSID, ip, userAgent string) (*AuthBundle, *model.User, error) {
	var bundle *AuthBundle
	op := &sessionadapter.RefreshSessionOperation{Hash: hashRefreshSecret, Grace: RefreshReplayWindow, Metadata: truncateAuthMetadata,
		Issue: func(session *model.UserSession, token string) error {
			var err error
			bundle, err = issueAuthBundle(session, token, true)
			return err
		},
	}
	codec := refresh_session.RefreshCodec{Split: func(token string) (string, string, bool) {
		sid, secret, ok := splitRefreshToken(token)
		op.PresentedSecret = secret
		return sid, secret, ok
	}, DeriveNext: deriveNextRefreshSecret}
	if err := refresh_session.NewUsecase(op, sessionPolicy(), codec).Execute(rawRefreshToken, expectedSID, ip, userAgent); err != nil {
		return nil, nil, err
	}
	return bundle, op.CurrentUser, nil
}

func RevokeByRefreshToken(rawRefreshToken, expectedSID, reason string) error {
	return revoke_refresh_token.NewUsecase(sessionadapter.RevokeRefreshOperation{Hash: hashRefreshSecret}, splitRefreshToken).Execute(rawRefreshToken, expectedSID, reason)
}

func RefreshTokenSID(rawRefreshToken string) (string, bool) {
	sid, _, ok := splitRefreshToken(rawRefreshToken)
	return sid, ok
}

func ListLoginSessions(userID int, currentSID string) ([]LoginSessionView, error) {
	sessions, err := model.ListActiveUserSessions(userID, currentSID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	views := make([]LoginSessionView, 0, len(sessions))
	for i := range sessions {
		views = append(views, sessionView(&sessions[i], sessions[i].SID == currentSID))
	}
	return views, nil
}

func WriteRefreshCookie(c *gin.Context, rawToken string) {
	expiresAt := time.Now().Add(LoginSessionTTL)
	if sid, _, ok := splitRefreshToken(rawToken); ok {
		if session, err := model.GetUserSessionCached(sid); err == nil && session.ExpiresAt > time.Now().Unix() {
			expiresAt = time.Unix(session.ExpiresAt, 0)
		}
	}
	maxAge := max(int(time.Until(expiresAt)/time.Second), 1)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    rawToken,
		Path:     "/api/user/auth",
		MaxAge:   maxAge,
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	writeSessionHintCookie(c, maxAge, expiresAt)
}

func ClearRefreshCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     "/api/user/auth",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	clearSessionHintCookie(c)
}

// writeSessionHintCookie mirrors the Refresh Cookie's lifetime with a
// script-readable marker. The Refresh Cookie itself is HttpOnly and scoped to
// /api/user/auth, so a page at / cannot tell whether a login session exists;
// without this hint the frontend has to POST /api/user/auth/refresh on every
// cold boot just to learn that an anonymous visitor is anonymous. That request
// is guaranteed to 401 and still consumes a slot of the IP-keyed
// CriticalRateLimit budget shared by everyone behind the same address.
//
// The value is the constant "1" and carries no credential: it states that a
// Refresh Cookie was issued, never who for. Authorization still derives solely
// from the Refresh Cookie and the Access Token, so forging this hint only costs
// the forger the round trip it was meant to avoid.
//
// It must be written and cleared in lockstep with the Refresh Cookie, which is
// why it lives inside these two helpers rather than at their call sites: both
// cookies then ride the same response with the same expiry, and no login path
// can set one without the other.
func writeSessionHintCookie(c *gin.Context, maxAge int, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     SessionHintCookieName,
		Value:    SessionHintCookieValue,
		Path:     "/",
		MaxAge:   maxAge,
		Expires:  expiresAt,
		HttpOnly: false,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionHintCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     SessionHintCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: false,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func issueAuthBundle(session *model.UserSession, rawRefreshToken string, current bool) (*AuthBundle, error) {
	identity := AuthIdentity{
		UserID:          session.UserID,
		SessionID:       session.SID,
		UserAuthVersion: session.UserAuthVersion,
		SessionVersion:  session.Version,
	}
	accessToken, accessExpiresAt, err := IssueAccessToken(identity)
	if err != nil {
		return nil, err
	}
	return &AuthBundle{
		AccessToken:     accessToken,
		TokenType:       "Bearer",
		AccessExpiresAt: accessExpiresAt,
		Session:         sessionView(session, current),
		RefreshToken:    rawRefreshToken,
	}, nil
}

func sessionView(session *model.UserSession, current bool) LoginSessionView {
	return LoginSessionView{
		SID:          session.SID,
		Current:      current,
		LoginMethod:  session.LoginMethod,
		IP:           session.IP,
		UserAgent:    session.UserAgent,
		CreatedAt:    session.CreatedAt,
		LastActiveAt: session.LastActiveAt,
		ExpiresAt:    session.ExpiresAt,
	}
}

func splitRefreshToken(raw string) (string, string, bool) {
	sid, secret, ok := strings.Cut(strings.TrimSpace(raw), ".")
	if !ok || sid == "" || secret == "" || strings.Contains(secret, ".") {
		return "", "", false
	}
	if _, err := uuid.Parse(sid); err != nil {
		return "", "", false
	}
	return sid, secret, true
}

func hashRefreshSecret(secret string) string {
	return common.GenerateHMACWithKey(authSigningKey("refresh"), secret)
}

func deriveNextRefreshSecret(sid, currentSecret string) string {
	return common.GenerateHMACWithKey(authSigningKey("refresh-rotate"), sid+"."+currentSecret)
}

func truncateAuthMetadata(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func FormatAuthError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("authentication failed: %v", err)
}

func sessionPolicy() domain.SessionPolicy {
	return domain.SessionPolicy{
		EnabledStatus: common.UserStatusEnabled, ActiveStatus: model.UserSessionStatusActive,
		ActiveLimit:           func() int64 { return int64(common.UserSessionActiveLimit) },
		IssuanceWindowSeconds: func() int64 { return common.UserSessionIssuanceWindowSeconds },
		IssuanceLimit:         func() int64 { return int64(common.UserSessionIssuanceLimit) },
	}
}

func newSessionAdvancement(identity AuthIdentity, reason string) *sessionadapter.SecurityAdvancement {
	return &sessionadapter.SecurityAdvancement{Identity: identity, Reason: reason, IssueFunc: issueAuthBundle}
}
