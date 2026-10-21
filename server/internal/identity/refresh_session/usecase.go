package refresh_session

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"strings"
	"time"
)

type RefreshOperation interface {
	LoadSession(sid string) (domain.SessionState, error)
	LoadCachedAccount(userID int) (domain.AccountState, error)
	LoadCurrentAccount(userID int) (domain.AccountState, error)
	Revoke(reason string) error
	Rotate(secret string) error
	RotationMatchesNextSecret() bool
	SetResponseMetadata(ip, userAgent string)
	IssueBundle() error
}
type RefreshCodec struct {
	Split      func(string) (string, string, bool)
	DeriveNext func(string, string) string
}

func (usecase Usecase) Execute(rawToken string, expectedSID string, ip string, userAgent string) error {
	sid, secret, ok := usecase.codec.Split(rawToken)
	if !ok {
		return domain.ErrRefreshTokenInvalid
	}
	if expectedSID = strings.TrimSpace(expectedSID); expectedSID != "" && expectedSID != sid {
		return domain.ErrLoginSessionMismatch
	}
	session, err := usecase.operation.LoadSession(sid)
	if err != nil {
		if errors.Is(err, domain.ErrSessionInactive) {
			return domain.ErrLoginSessionRevoked
		}
		return domain.ErrRefreshTokenInvalid
	}
	if session.Status != usecase.policy.ActiveStatus || session.RevokedAt != 0 || session.ExpiresAt <= time.Now().Unix() {
		return domain.ErrLoginSessionRevoked
	}
	cached, err := usecase.operation.LoadCachedAccount(session.UserID)
	if err != nil {
		return err
	}
	current, err := usecase.operation.LoadCurrentAccount(session.UserID)
	if err != nil {
		return err
	}
	if cached.Status != usecase.policy.EnabledStatus || cached.AuthVersion != session.UserAuthVersion || current.Status != usecase.policy.EnabledStatus || current.AuthVersion != session.UserAuthVersion {
		_ = usecase.operation.Revoke("user_security_changed")
		return domain.ErrLoginSessionRevoked
	}
	nextSecret := usecase.codec.DeriveNext(sid, secret)
	// The operation computes both current callback HMACs before reading the rotation time.
	err = usecase.operation.Rotate(nextSecret)
	if err != nil {
		if errors.Is(err, domain.ErrSessionRefreshRace) && usecase.operation.RotationMatchesNextSecret() {
			return usecase.operation.IssueBundle()
		}
		if errors.Is(err, domain.ErrSessionRefreshReuse) {
			return domain.ErrLoginSessionRevoked
		}
		if errors.Is(err, domain.ErrSessionRefreshInvalid) {
			return domain.ErrRefreshTokenInvalid
		}
		if errors.Is(err, domain.ErrSessionRefreshRace) {
			return domain.ErrRefreshRace
		}
		return err
	}
	usecase.operation.SetResponseMetadata(ip, userAgent)
	return usecase.operation.IssueBundle()
}

type Usecase struct {
	codec     RefreshCodec
	operation RefreshOperation
	policy    domain.SessionPolicy
}

func NewUsecase(operation RefreshOperation, policy domain.SessionPolicy, codec RefreshCodec) Usecase {
	return Usecase{operation: operation, policy: policy, codec: codec}
}
