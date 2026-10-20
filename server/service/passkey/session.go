package passkey

import (
	"github.com/QuantumNous/new-api/internal/adapter/identity/passkeyflow"
	"github.com/QuantumNous/new-api/internal/identity/consume_passkey_flow"
	"github.com/QuantumNous/new-api/internal/identity/create_passkey_flow"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/model"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"time"
)

const passkeyFlowTTL = 5 * time.Minute

type FlowSecurity = domain.PasskeyFlowSecurity

func CreateSessionDataFlow(purpose string, security FlowSecurity, data *webauthn.SessionData) (string, int64, error) {
	op := &passkeyflow.Creation{Purpose: purpose, Security: security, Data: data}
	expires, err := create_passkey_flow.NewUsecase(op).Execute(purpose, security, data != nil, passkeyFlowTTL)
	if err != nil {
		return "", 0, err
	}
	return op.Token, expires.Unix(), nil
}
func PopSessionDataFlow(token, purpose string, identity model.AuthSessionIdentity) (*webauthn.SessionData, *FlowSecurity, error) {
	op := &passkeyflow.Consumption{Token: token, Purpose: purpose, Identity: identity}
	if err := consume_passkey_flow.NewUsecase(op).Execute(purpose, identity); err != nil {
		return nil, nil, err
	}
	return &op.Payload.SessionData, &op.Payload.Security, nil
}
