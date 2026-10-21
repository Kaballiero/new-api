package create_passkey_flow

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"time"
)

type Operation interface {
	EncodePayload() error
	Create(expires time.Time) error
}

func (usecase Usecase) Execute(purpose string, security domain.PasskeyFlowSecurity, hasData bool, ttl time.Duration) (time.Time, error) {
	if !hasData {
		return time.Time{}, errors.New("passkey session data is empty")
	}
	if purpose == domain.AuthFlowPurposeLoginPasskey {
		if security.UserID <= 0 || security.UserAuthVersion <= 0 || security.LoginFlowID <= 0 || security.LoginExpiresAt <= time.Now().Unix() || security.SessionID != "" || security.SessionVersion != 0 {
			return time.Time{}, domain.ErrAuthFlowInvalid
		}
	} else if purpose != domain.AuthFlowPurposePasskeyLogin && (security.UserID <= 0 || security.SessionID == "" || security.Scope == "" || security.ContextHash == "" || security.UserAuthVersion <= 0 || security.SessionVersion <= 0) {
		return time.Time{}, domain.ErrAuthFlowInvalid
	}
	if purpose == domain.AuthFlowPurposePasskeyRegister && (security.Authorization == nil || security.Authorization.ProofID <= 0 || security.Authorization.AuthSessionIdentity != security.AuthSessionIdentity) {
		return time.Time{}, domain.ErrAuthFlowInvalid
	}
	if err := usecase.operation.EncodePayload(); err != nil {
		return time.Time{}, err
	}
	expires := time.Now().Add(ttl)
	if purpose == domain.AuthFlowPurposeLoginPasskey && security.LoginExpiresAt < expires.Unix() {
		expires = time.Unix(security.LoginExpiresAt, 0)
	}
	if err := usecase.operation.Create(expires); err != nil {
		return time.Time{}, err
	}
	return expires, nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
