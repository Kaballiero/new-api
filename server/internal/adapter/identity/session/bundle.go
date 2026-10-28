package session

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	"github.com/QuantumNous/new-api/internal/adapter/identity/authtoken"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"

	"github.com/google/uuid"
)

func NewLoginSession(userID int, authVersion int64, loginMethod, ip, userAgent string) (*identityrecord.UserSession, string, error) {
	refreshSecret, err := common.GenerateRandomCharsKey(64)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().Unix()
	session := &identityrecord.UserSession{
		SID:             uuid.NewString(),
		UserID:          userID,
		Version:         1,
		UserAuthVersion: authVersion,
		Status:          identityrecord.UserSessionStatusActive,
		RefreshHash:     HashRefreshSecret(refreshSecret),
		LoginMethod:     strings.TrimSpace(loginMethod),
		IP:              TruncateAuthMetadata(ip, 64),
		UserAgent:       TruncateAuthMetadata(userAgent, 512),
		CreatedAt:       now,
		LastActiveAt:    now,
		ExpiresAt:       time.Unix(now, 0).Add(domain.LoginSessionTTL).Unix(),
	}
	if session.LoginMethod == "" {
		session.LoginMethod = "unknown"
	}
	return session, refreshSecret, nil
}

func IssueAuthBundle(session *identityrecord.UserSession, rawRefreshToken string, current bool) (*dto.AuthBundle, error) {
	identity := domain.AuthIdentity{
		UserID:          session.UserID,
		SessionID:       session.SID,
		UserAuthVersion: session.UserAuthVersion,
		SessionVersion:  session.Version,
	}
	accessToken, accessExpiresAt, err := authtoken.IssueAccessToken(identity)
	if err != nil {
		return nil, err
	}
	return &dto.AuthBundle{
		AccessToken:     accessToken,
		TokenType:       "Bearer",
		AccessExpiresAt: accessExpiresAt,
		Session:         SessionView(session, current),
		RefreshToken:    rawRefreshToken,
	}, nil
}

func SessionView(session *identityrecord.UserSession, current bool) dto.LoginSessionView {
	return dto.LoginSessionView{
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

func SplitRefreshToken(raw string) (string, string, bool) {
	sid, secret, ok := strings.Cut(strings.TrimSpace(raw), ".")
	if !ok || sid == "" || secret == "" || strings.Contains(secret, ".") {
		return "", "", false
	}
	if _, err := uuid.Parse(sid); err != nil {
		return "", "", false
	}
	return sid, secret, true
}

func HashRefreshSecret(secret string) string {
	return common.GenerateHMACWithKey(authtoken.SigningKey("refresh"), secret)
}

func DeriveNextRefreshSecret(sid, currentSecret string) string {
	return common.GenerateHMACWithKey(authtoken.SigningKey("refresh-rotate"), sid+"."+currentSecret)
}

func TruncateAuthMetadata(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func ListLoginSessions(sql identitystore.Store, userID int, currentSID string) ([]dto.LoginSessionView, error) {
	sessions, err := sql.ListActiveUserSessions(userID, currentSID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	views := make([]dto.LoginSessionView, 0, len(sessions))
	for i := range sessions {
		views = append(views, SessionView(&sessions[i], sessions[i].SID == currentSID))
	}
	return views, nil
}

func RefreshTokenSID(rawRefreshToken string) (string, bool) {
	sid, _, ok := SplitRefreshToken(rawRefreshToken)
	return sid, ok
}
