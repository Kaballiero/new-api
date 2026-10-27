package identitycore

import (
	loginadapter "github.com/QuantumNous/new-api/internal/adapter/identity/login"
	sessionadapter "github.com/QuantumNous/new-api/internal/adapter/identity/session"
	verificationadapter "github.com/QuantumNous/new-api/internal/adapter/identity/verification"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
)

func StartLoginVerification(user *model.User, loginMethod string) (*identitydto.LoginChallenge, error) {
	op := loginadapter.NewStart(user, loginMethod, identityRuntime().LoginDependencies)
	if err := identityRuntime().StartLoginVerification.Execute(op); err != nil {
		return nil, err
	}
	return op.Challenge(), nil
}

func RequireLoginVerification(token, method string) (*loginadapter.LoginVerification, error) {
	op := loginadapter.NewRequire(token, identityRuntime().LoginDependencies)
	if err := identityRuntime().RequireLoginVerification.Execute(op, method); err != nil {
		return nil, err
	}
	return op.Verification(), nil
}

func requireLoginVerificationMethod(state *model.UserVerificationState, method string) error {
	methods, err := securityVerificationPolicy(domain.VerificationScopeLogin, *state)
	if err != nil {
		return err
	}
	return identityRuntime().RequireVerificationMethod.Execute(verificationadapter.MethodOptions(methods), method)
}

func VerifyLoginCode(token, code, ip, userAgent string) (*identitydto.AuthBundle, error) {
	op := loginadapter.NewCode(token, code, ip, userAgent, identityRuntime().LoginDependencies)
	if err := identityRuntime().VerifyLoginCode.Execute(op); err != nil {
		return nil, err
	}
	return op.Bundle(), nil
}

// CompleteLoginVerification must only run after a concrete factor ceremony.
// Recheck the bound version and method while consuming the flow and creating the
// session atomically; a different request cannot reuse this authorization.

func CompleteLoginVerification(token string, verification *loginadapter.LoginVerification, method, ip, userAgent string) (*identitydto.AuthBundle, error) {
	op := loginadapter.NewComplete(token, verification, ip, userAgent, identityRuntime().LoginDependencies)
	if err := identityRuntime().CompleteLoginVerification.Execute(op, method); err != nil {
		return nil, err
	}
	return op.Bundle(), nil
}

func loginDependencies() loginadapter.Dependencies {
	return loginadapter.Dependencies{TTL: domain.LoginVerificationTTL, PolicyFunc: securityVerificationPolicy, RequireMethodFunc: requireLoginVerificationMethod, RequireVerificationFunc: RequireLoginVerification, VerifyFactorFunc: VerifyTwoFactorCode, CompleteFunc: CompleteLoginVerification, NewSessionFunc: sessionadapter.NewLoginSession, IssueFunc: sessionadapter.IssueAuthBundle}
}
