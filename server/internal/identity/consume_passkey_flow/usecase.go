package consume_passkey_flow

import (
	"time"

	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type Operation interface {
	Consume(check func(domain.PasskeyFlowSecurity) error) error
	ValidateCurrentSession() error
}

func (usecase Usecase) Execute(operation Operation, purpose string, identity domain.AuthIdentity) error {
	return operation.Consume(func(security domain.PasskeyFlowSecurity) error {
		if purpose == domain.AuthFlowPurposePasskeyLogin {
			return nil
		}
		if purpose == domain.AuthFlowPurposeLoginPasskey {
			if security.AuthSessionIdentity != identity || identity.UserID <= 0 || identity.UserAuthVersion <= 0 || identity.SessionID != "" || identity.SessionVersion != 0 || security.LoginFlowID <= 0 || security.LoginExpiresAt <= time.Now().Unix() {
				return domain.ErrAuthFlowInvalid
			}
			return nil
		}
		if security.AuthSessionIdentity != identity || security.Scope == "" || security.ContextHash == "" {
			return domain.ErrAuthFlowInvalid
		}
		if purpose == domain.AuthFlowPurposePasskeyRegister && (security.Authorization == nil || security.Authorization.ProofID <= 0 || security.Authorization.AuthSessionIdentity != identity) {
			return domain.ErrAuthFlowInvalid
		}
		return operation.ValidateCurrentSession()
	})
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
