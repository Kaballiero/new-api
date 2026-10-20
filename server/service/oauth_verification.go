package service

import (
	"errors"
	verificationadapter "github.com/QuantumNous/new-api/internal/adapter/identity/verification"
	"github.com/QuantumNous/new-api/internal/identity/finish_oauth_verification"
	"github.com/QuantumNous/new-api/internal/identity/start_oauth_verification"
)

var ErrOAuthAccountMismatch = errors.New("The OAuth account does not match the account linked to your profile.")

type OAuthVerificationFlow = verificationadapter.OAuthVerificationFlow

func StartOAuthVerification(identity AuthIdentity, operation VerificationOperation, provider string) (*OAuthVerificationFlow, error) {
	op := &verificationadapter.StartOAuth{Identity: identity, Operation: operation, Provider: provider, BindFunc: BindVerificationOperation, FindFunc: GetOAuthVerificationBinding}
	if err := start_oauth_verification.NewUsecase(op).Execute(); err != nil {
		return nil, err
	}
	return op.Result, nil
}

// FinishOAuthVerification only verifies an existing binding. It deliberately
// never calls OAuth's login or find-or-create-user paths.
func FinishOAuthVerification(identity AuthIdentity, provider, actualUserID string, flow *OAuthVerificationFlow) (*SecurityProof, error) {
	op := &verificationadapter.FinishOAuth{Identity: identity, Provider: provider, ActualUserID: actualUserID, Flow: flow, FindFunc: GetOAuthVerificationBinding, CompleteFunc: CompleteSecurityVerification, Invalid: func() error { return ErrAuthTokenInvalid }, Mismatch: func() error { return ErrOAuthAccountMismatch }}
	if err := finish_oauth_verification.NewUsecase(op).Execute(); err != nil {
		return nil, err
	}
	return op.Result, nil
}
