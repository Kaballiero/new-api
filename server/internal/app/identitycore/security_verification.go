package identitycore

import (
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	authtoken "github.com/QuantumNous/new-api/internal/adapter/identity/authtoken"
	verificationadapter "github.com/QuantumNous/new-api/internal/adapter/identity/verification"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/get_verification_requirements"
	"github.com/QuantumNous/new-api/internal/identity/start_oauth_verification"
	"github.com/QuantumNous/new-api/internal/identity/verification_policy"
	"github.com/QuantumNous/new-api/internal/identity/verify_security_input"
)

func BindVerificationOperation(operation domain.VerificationOperation) (domain.VerificationBinding, error) {
	return identityRuntime().Bind.Execute(operation)
}

func securityVerificationPolicy(scope string, state identitystore.UserVerificationState) ([]identitydto.VerificationMethodOption, error) {
	op := &verificationadapter.PolicyOperation{Methods: make([]identitydto.VerificationMethodOption, 0)}
	err := identityRuntime().VerificationPolicy.Execute(op, scope, state.Role, verification_policy.FactorPolicyState{HasPassword: state.HasPassword, HasTwoFA: state.HasTwoFA, TwoFALocked: state.TwoFALocked, HasPasskey: state.HasPasskey})
	if err != nil {
		return nil, err
	}
	return op.Methods, nil
}

func GetVerificationRequirements(identity domain.AuthIdentity, scope string) (*get_verification_requirements.VerificationRequirements, error) {
	op := &verificationadapter.RequirementsOperation{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL(), PolicyFunc: securityVerificationPolicy, ProvidersFunc: func(user *accountrecord.User) ([]get_verification_requirements.VerificationOAuthProvider, error) {
		return verificationadapter.OAuthProviders(identitystorage.SQL(), user)
	}, Identity: identity, Scope: scope}
	if err := identityRuntime().GetVerificationRequirements.Execute(op, scope, identity.UserAuthVersion); err != nil {
		return nil, err
	}
	return op.Requirements, nil
}

func RequireVerificationMethod(identity domain.AuthIdentity, scope, method string) (*get_verification_requirements.VerificationRequirements, error) {
	requirements, err := GetVerificationRequirements(identity, scope)
	if err != nil {
		return nil, err
	}
	if err := identityRuntime().RequireVerificationMethod.Execute(verificationadapter.MethodOptions(requirements.Methods), method); err != nil {
		return nil, err
	}
	return requirements, nil
}

// CompleteSecurityVerification runs after the concrete authentication ceremony.
// Recheck the session and policy after potentially slow external authentication.

func CompleteSecurityVerification(identity domain.AuthIdentity, binding domain.VerificationBinding, method string) (*identitydto.SecurityProof, error) {
	op := &verificationadapter.CompleteSecurityOperation{Dependencies: identityRuntime().VerificationDependencies, Identity: identity, Binding: binding}
	if err := identityRuntime().CompleteVerification.Execute(op, method); err != nil {
		return nil, err
	}
	return op.Proof, nil
}

// ConsumeOperationProof accepts a proof at most once. The consumption commits
// before the caller performs its action; an action failure must not restore it.

func ConsumeOperationProof(raw string, identity domain.AuthIdentity, operation domain.VerificationOperation) (*domain.FlowAuthorization, error) {
	op := &verificationadapter.ConsumeProofOperation{Dependencies: identityRuntime().VerificationDependencies, Raw: raw, Identity: identity, Operation: operation}
	op.SetProofIdentity(identity)
	if err := identityRuntime().ConsumeProof.Execute(op); err != nil {
		return nil, err
	}
	return op.Authorization, nil
}

// ValidateFlowAuthorization permits a dedicated configuration flow to outlive
// its consumed proof, while retaining its operation, session and method policy.

func ValidateFlowAuthorization(identity domain.AuthIdentity, operation domain.VerificationOperation, authorization *domain.FlowAuthorization) error {
	return identityRuntime().ValidateAuthorization.Execute(&verificationadapter.FlowAuthorizationOperation{Dependencies: identityRuntime().VerificationDependencies, Identity: identity, Operation: operation, Authorization: authorization})
}

func VerifySecurityInput(identity domain.AuthIdentity, input verify_security_input.VerificationInput) (*identitydto.SecurityProof, error) {
	op := &verificationadapter.SecurityInputOperation{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL(), Dependencies: identityRuntime().VerificationDependencies, Identity: identity, Input: input, Password: input.Password}
	if err := identityRuntime().VerifySecurityInput.Execute(op, input.Scope, input.Method); err != nil {
		return nil, err
	}
	return op.Proof, nil
}

// VerifyTwoFactorCode classifies the input before verification so one failed
// submission cannot increment the failure counter for both TOTP and backup codes.

func VerifyTwoFactorCode(twoFA *identityrecord.TwoFA, code string) error {
	return identityRuntime().VerifyFactorCode.Execute(verificationadapter.FactorCodeOperation{FactorRecordOperation: verificationadapter.FactorRecordOperation{SQL: identitystorage.SQL(), Factor: twoFA}}, code)
}

func GetOAuthVerificationBinding(identity domain.AuthIdentity, scope, provider string) (string, error) {
	requirements, err := RequireVerificationMethod(identity, scope, domain.VerificationMethodOAuth)
	if err != nil {
		return "", err
	}
	return start_oauth_verification.LinkedAccount(requirements.OAuthProviders, provider)
}

func verificationDependencies() verificationadapter.Dependencies {
	return verificationadapter.Dependencies{SQL: identitystorage.SQL(),
		ValidateSessionFunc: func(identity domain.AuthIdentity) error { _, _, err := ValidateLoginSession(identity); return err },
		RequireMethodFunc: func(identity domain.AuthIdentity, scope, method string) error {
			_, err := RequireVerificationMethod(identity, scope, method)
			return err
		},
		IssueProofFunc: func(identity domain.AuthIdentity, method string, binding domain.VerificationBinding) (string, int64, error) {
			return authtoken.IssueSecurityProof(identitystorage.SQL(), identity, method, binding)
		}, BindFunc: BindVerificationOperation,
		VerifyClaimsFunc: func(raw string, identity domain.AuthIdentity, binding domain.VerificationBinding) (*verificationadapter.ProofClaims, error) {
			claims, err := authtoken.VerifySecurityProof(raw, identity, binding)
			if err != nil {
				return nil, err
			}
			return &verificationadapter.ProofClaims{ID: claims.ID, Method: claims.Method}, nil
		},
		VerifyFactorFunc: VerifyTwoFactorCode, CompleteFunc: CompleteSecurityVerification,
	}
}
