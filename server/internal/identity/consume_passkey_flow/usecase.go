package consume_passkey_flow

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"time"
)

type Operation interface {
	Consume(check func(domain.PasskeyFlowSecurity) error) error
	InvalidError() error
	ValidateCurrentSession() error
}

func (usecase Usecase) Execute(purpose string, identity domain.AuthIdentity) error {
	return usecase.operation.Consume(func(security domain.PasskeyFlowSecurity) error {
		if purpose == domain.AuthFlowPurposePasskeyLogin {
			return nil
		}
		if purpose == domain.AuthFlowPurposeLoginPasskey {
			if security.AuthSessionIdentity != identity || identity.UserID <= 0 || identity.UserAuthVersion <= 0 || identity.SessionID != "" || identity.SessionVersion != 0 || security.LoginFlowID <= 0 || security.LoginExpiresAt <= time.Now().Unix() {
				return usecase.operation.InvalidError()
			}
			return nil
		}
		if security.AuthSessionIdentity != identity || security.Scope == "" || security.ContextHash == "" {
			return usecase.operation.InvalidError()
		}
		if purpose == domain.AuthFlowPurposePasskeyRegister && (security.Authorization == nil || security.Authorization.ProofID <= 0 || security.Authorization.AuthSessionIdentity != identity) {
			return usecase.operation.InvalidError()
		}
		return usecase.operation.ValidateCurrentSession()
	})
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
