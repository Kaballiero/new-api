package identitycore

import (
	verificationadapter "github.com/QuantumNous/new-api/internal/adapter/identity/verification"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
)

func StartOAuthVerification(identity domain.AuthIdentity, operation domain.VerificationOperation, provider string) (*verificationadapter.OAuthVerificationFlow, error) {
	op := &verificationadapter.StartOAuth{Identity: identity, Operation: operation, Provider: provider, BindFunc: BindVerificationOperation, FindFunc: GetOAuthVerificationBinding}
	if err := identityRuntime().StartOauthVerification.Execute(op); err != nil {
		return nil, err
	}
	return op.Result, nil
}

// FinishOAuthVerification only verifies an existing binding. It deliberately
// never calls OAuth's login or find-or-create-user paths.

func FinishOAuthVerification(identity domain.AuthIdentity, provider, actualUserID string, flow *verificationadapter.OAuthVerificationFlow) (*identitydto.SecurityProof, error) {
	op := &verificationadapter.FinishOAuth{Identity: identity, Provider: provider, ActualUserID: actualUserID, Flow: flow, FindFunc: GetOAuthVerificationBinding, CompleteFunc: CompleteSecurityVerification}
	if err := identityRuntime().FinishOauthVerification.Execute(op); err != nil {
		return nil, err
	}
	return op.Result, nil
}
