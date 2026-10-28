package identitycore

import (
	"time"

	"github.com/QuantumNous/new-api/internal/adapter/identity/passkeyflow"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	webauthn "github.com/go-webauthn/webauthn/webauthn"
)

const passkeyFlowTTL = 5 * time.Minute

func CreateSessionDataFlow(purpose string, security domain.PasskeyFlowSecurity, data *webauthn.SessionData) (string, int64, error) {
	op := &passkeyflow.Creation{SQL: identitystorage.SQL(), Purpose: purpose, Security: security, Data: data}
	expires, err := identityRuntime().CreatePasskeyFlow.Execute(op, purpose, security, data != nil, passkeyFlowTTL)
	if err != nil {
		return "", 0, err
	}
	return op.Token, expires.Unix(), nil
}
func PopSessionDataFlow(token, purpose string, identity domain.AuthIdentity) (*webauthn.SessionData, *domain.PasskeyFlowSecurity, error) {
	op := &passkeyflow.Consumption{SQL: identitystorage.SQL(), Token: token, Purpose: purpose, Identity: identity}
	if err := identityRuntime().ConsumePasskeyFlow.Execute(op, purpose, identity); err != nil {
		return nil, nil, err
	}
	return &op.Payload.SessionData, &op.Payload.Security, nil
}
