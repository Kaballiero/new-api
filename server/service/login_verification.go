package service

import (
	loginadapter "github.com/QuantumNous/new-api/internal/adapter/identity/login"
	verificationadapter "github.com/QuantumNous/new-api/internal/adapter/identity/verification"
	"github.com/QuantumNous/new-api/internal/identity/complete_login_verification"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/require_login_verification"
	"github.com/QuantumNous/new-api/internal/identity/require_verification_method"
	"github.com/QuantumNous/new-api/internal/identity/start_login_verification"
	"github.com/QuantumNous/new-api/internal/identity/verify_login_code"

	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const LoginVerificationTTL = 5 * time.Minute

type LoginChallenge = identitydto.LoginChallenge

type LoginVerification = loginadapter.LoginVerification

func StartLoginVerification(user *model.User, loginMethod string) (*LoginChallenge, error) {
	op := loginadapter.NewStart(user, loginMethod, loginDependencies())
	if err := start_login_verification.NewUsecase(op, common.UserStatusEnabled).Execute(); err != nil {
		return nil, err
	}
	return op.Challenge(), nil
}

func RequireLoginVerification(token, method string) (*LoginVerification, error) {
	op := loginadapter.NewRequire(token, loginDependencies())
	if err := require_login_verification.NewUsecase(op, common.UserStatusEnabled).Execute(method); err != nil {
		return nil, err
	}
	return op.Verification(), nil
}

func requireLoginVerificationMethod(state *model.UserVerificationState, method string) error {
	methods, err := securityVerificationPolicy(VerificationScopeLogin, *state)
	if err != nil {
		return err
	}
	return require_verification_method.NewUsecase(verificationadapter.MethodOptions(methods)).Execute(method)
}

func VerifyLoginCode(token, code, ip, userAgent string) (*AuthBundle, error) {
	op := loginadapter.NewCode(token, code, ip, userAgent, loginDependencies())
	if err := verify_login_code.NewUsecase(op).Execute(); err != nil {
		return nil, err
	}
	return op.Bundle(), nil
}

// CompleteLoginVerification must only run after a concrete factor ceremony.
// Recheck the bound version and method while consuming the flow and creating the
// session atomically; a different request cannot reuse this authorization.
func CompleteLoginVerification(token string, verification *LoginVerification, method, ip, userAgent string) (*AuthBundle, error) {
	op := loginadapter.NewComplete(token, verification, ip, userAgent, loginDependencies())
	if err := complete_login_verification.NewUsecase(op).Execute(method); err != nil {
		return nil, err
	}
	return op.Bundle(), nil
}

func loginDependencies() loginadapter.Dependencies {
	return loginadapter.Dependencies{TTL: LoginVerificationTTL, PolicyFunc: securityVerificationPolicy, RequireMethodFunc: requireLoginVerificationMethod, RequireVerificationFunc: RequireLoginVerification, VerifyFactorFunc: VerifyTwoFactorCode, CompleteFunc: CompleteLoginVerification, NewSessionFunc: newLoginSession, IssueFunc: issueAuthBundle}
}
