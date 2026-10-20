package service

import (
	verificationadapter "github.com/QuantumNous/new-api/internal/adapter/identity/verification"
	"github.com/QuantumNous/new-api/internal/identity/bind_verification_operation"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/get_verification_requirements"
	"github.com/QuantumNous/new-api/internal/identity/require_verification_method"
	"github.com/QuantumNous/new-api/internal/identity/start_oauth_verification"
	"github.com/QuantumNous/new-api/internal/identity/verification_policy"

	"github.com/QuantumNous/new-api/internal/identity/complete_verification"
	"github.com/QuantumNous/new-api/internal/identity/consume_proof"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/validate_authorization"
	"github.com/QuantumNous/new-api/internal/identity/verify_factor_code"
	"github.com/QuantumNous/new-api/internal/identity/verify_security_input"

	"errors"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
)

const (
	VerificationMethodTwoFA              = domain.VerificationMethodTwoFA
	VerificationMethodPasskey            = domain.VerificationMethodPasskey
	VerificationMethodPassword           = domain.VerificationMethodPassword
	VerificationMethodOAuth              = domain.VerificationMethodOAuth
	VerificationMethodSession            = domain.VerificationMethodSession
	VerificationScopeChannelKeyRead      = domain.VerificationScopeChannelKeyRead
	VerificationScopePasskeyRegister     = domain.VerificationScopePasskeyRegister
	VerificationScopePasskeyDelete       = domain.VerificationScopePasskeyDelete
	VerificationScopeTwoFASetup          = domain.VerificationScopeTwoFASetup
	VerificationScopeTwoFADisable        = domain.VerificationScopeTwoFADisable
	VerificationScopeTwoFABackupCodes    = domain.VerificationScopeTwoFABackupCodes
	VerificationScopeLogin               = domain.VerificationScopeLogin
	VerificationScopeAccessTokenGenerate = domain.VerificationScopeAccessTokenGenerate
	VerificationScopeAccessTokenRevoke   = domain.VerificationScopeAccessTokenRevoke
	VerificationScopeAccountBind         = domain.VerificationScopeAccountBind
	VerificationScopeAccountUnbind       = domain.VerificationScopeAccountUnbind
	VerificationScopePasswordSet         = domain.VerificationScopePasswordSet
	VerificationScopePasswordChange      = domain.VerificationScopePasswordChange
	VerificationScopeAccountDelete       = domain.VerificationScopeAccountDelete
)

var (
	ErrVerificationFailed         = errors.New("Verification failed. Please try again.")
	ErrVerificationUnavailable    = errors.New("This verification method is currently unavailable.")
	ErrVerificationLocked         = errors.New("Two-factor authentication is temporarily locked.")
	ErrVerificationFlowRequired   = errors.New("This verification method requires its dedicated verification flow.")
	ErrVerificationContextInvalid = errors.New("The action details are invalid.")
	ErrVerificationForbidden      = errors.New("You do not have permission to perform this action.")
)

// VerificationOperation describes the action being authorized, independently of
// its authentication method. Each scope owns a strict, normalized context schema.
type VerificationOperation = domain.VerificationOperation

type ChannelKeyReadContext = domain.ChannelKeyReadContext

type AccountBindingContext = domain.AccountBindingContext

type AccountUnbindingContext = domain.AccountUnbindingContext

// VerificationBinding contains no original operation parameters. It can safely
// travel through a signed proof or a server-owned interactive verification flow.
type VerificationBinding = domain.VerificationBinding

func BindVerificationOperation(operation VerificationOperation) (VerificationBinding, error) {
	return bind_verification_operation.NewUsecase(verificationadapter.ContextCodec{SigningKey: authSigningKey, InvalidError: func() error { return ErrVerificationContextInvalid }, ScopeFailure: func() error { return ErrProofScope }}).Execute(operation)
}

type SecurityProof = identitydto.SecurityProof

type VerificationMethodOption = identitydto.VerificationMethodOption

type VerificationOAuthProvider = get_verification_requirements.VerificationOAuthProvider

type VerificationRequirements = get_verification_requirements.VerificationRequirements

// securityVerificationPolicy is the only operation-to-method policy. Device
// support and disabled providers never turn an enrolled factor into an absent one.
func securityVerificationPolicy(scope string, state model.UserVerificationState) ([]VerificationMethodOption, error) {
	op := &verificationadapter.PolicyOperation{Methods: make([]VerificationMethodOption, 0)}
	err := verification_policy.NewUsecase(common.RoleRootUser, op, verificationErrors()).Execute(scope, state.Role, verification_policy.FactorPolicyState{HasPassword: state.HasPassword, HasTwoFA: state.HasTwoFA, TwoFALocked: state.TwoFALocked, HasPasskey: state.HasPasskey})
	if err != nil {
		return nil, err
	}
	return op.Methods, nil
}

func GetVerificationRequirements(identity AuthIdentity, scope string) (*VerificationRequirements, error) {
	op := &verificationadapter.RequirementsOperation{PolicyFunc: securityVerificationPolicy, ProvidersFunc: verificationOAuthProviders, Identity: identity, Scope: scope}
	if err := get_verification_requirements.NewUsecase(op, common.UserStatusEnabled, common.RoleRootUser, verificationErrors()).Execute(scope, identity.UserAuthVersion); err != nil {
		return nil, err
	}
	return op.Requirements, nil
}

func verificationOAuthProviders(user *model.User) ([]VerificationOAuthProvider, error) {
	bindings := map[int]string{}
	if len(oauth.GetEnabledCustomProviders()) > 0 {
		stored, err := model.GetUserOAuthBindingsByUserId(user.Id)
		if err != nil {
			return nil, err
		}
		for _, binding := range stored {
			bindings[binding.ProviderId] = binding.ProviderUserId
		}
	}
	providers := []VerificationOAuthProvider{}
	for slug, provider := range oauth.GetAllProviders() {
		if !provider.IsEnabled() {
			continue
		}
		var userID string
		if custom, ok := provider.(*oauth.GenericOAuthProvider); ok {
			userID = bindings[custom.GetProviderId()]
		} else {
			switch provider.ProviderUserIDColumn() {
			case "github_id":
				userID = user.GitHubId
			case "discord_id":
				userID = user.DiscordId
			case "oidc_id":
				userID = user.OidcId
			case "linux_do_id":
				userID = user.LinuxDOId
			case "telegram_id":
				userID = user.TelegramId
			}
		}
		if userID != "" {
			providers = append(providers, VerificationOAuthProvider{Slug: slug, Name: provider.GetName(), UserID: userID})
		}
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Slug < providers[j].Slug })
	return providers, nil
}

func RequireVerificationMethod(identity AuthIdentity, scope, method string) (*VerificationRequirements, error) {
	requirements, err := GetVerificationRequirements(identity, scope)
	if err != nil {
		return nil, err
	}
	if err := require_verification_method.NewUsecase(verificationadapter.MethodOptions(requirements.Methods), verificationErrors()).Execute(method); err != nil {
		return nil, err
	}
	return requirements, nil
}

// CompleteSecurityVerification runs after the concrete authentication ceremony.
// Recheck the session and policy after potentially slow external authentication.
func CompleteSecurityVerification(identity AuthIdentity, binding VerificationBinding, method string) (*SecurityProof, error) {
	op := &verificationadapter.CompleteSecurityOperation{Dependencies: verificationDependencies(), Identity: identity, Binding: binding}
	if err := complete_verification.NewUsecase(op).Execute(method); err != nil {
		return nil, err
	}
	return op.Proof, nil
}

// ConsumeOperationProof accepts a proof at most once. The consumption commits
// before the caller performs its action; an action failure must not restore it.
func ConsumeOperationProof(raw string, identity AuthIdentity, operation VerificationOperation) (*model.AuthFlowAuthorization, error) {
	op := &verificationadapter.ConsumeProofOperation{Dependencies: verificationDependencies(), Raw: raw, Identity: identity, Operation: operation}
	op.SetProofIdentity(identity)
	if err := consume_proof.NewUsecase(op, verificationErrors()).Execute(); err != nil {
		return nil, err
	}
	return op.Authorization, nil
}

// ValidateFlowAuthorization permits a dedicated configuration flow to outlive
// its consumed proof, while retaining its operation, session and method policy.
func ValidateFlowAuthorization(identity AuthIdentity, operation VerificationOperation, authorization *model.AuthFlowAuthorization) error {
	return validate_authorization.NewUsecase(&verificationadapter.FlowAuthorizationOperation{Dependencies: verificationDependencies(), Identity: identity, Operation: operation, Authorization: authorization}, verificationErrors()).Execute()
}

type VerificationInput = verify_security_input.VerificationInput

func VerifySecurityInput(identity AuthIdentity, input VerificationInput) (*SecurityProof, error) {
	op := &verificationadapter.SecurityInputOperation{Dependencies: verificationDependencies(), Identity: identity, Input: input, Password: input.Password}
	if err := verify_security_input.NewUsecase(op, verificationErrors()).Execute(input.Scope, input.Method); err != nil {
		return nil, err
	}
	return op.Proof, nil
}

// VerifyTwoFactorCode classifies the input before verification so one failed
// submission cannot increment the failure counter for both TOTP and backup codes.
func VerifyTwoFactorCode(twoFA *model.TwoFA, code string) error {
	return verify_factor_code.NewUsecase(verificationadapter.FactorCodeOperation{FactorRecordOperation: verificationadapter.FactorRecordOperation{Factor: twoFA}}, verificationErrors()).Execute(code)
}

func GetOAuthVerificationBinding(identity AuthIdentity, scope, provider string) (string, error) {
	requirements, err := RequireVerificationMethod(identity, scope, VerificationMethodOAuth)
	if err != nil {
		return "", err
	}
	return start_oauth_verification.LinkedAccount(requirements.OAuthProviders, provider, func() error { return ErrVerificationUnavailable })
}

func verificationErrors() domain.VerificationErrors {
	return domain.VerificationErrors{
		FlowInvalid:         func() error { return model.ErrAuthFlowInvalid },
		Inactive:            func() error { return model.ErrUserSessionInactive },
		Unavailable:         func() error { return ErrVerificationUnavailable },
		Locked:              func() error { return ErrVerificationLocked },
		Forbidden:           func() error { return ErrVerificationForbidden },
		Scope:               func() error { return ErrProofScope },
		Method:              func() error { return ErrProofMethod },
		Failed:              func() error { return ErrVerificationFailed },
		FlowRequired:        func() error { return ErrVerificationFlowRequired },
		TwoFANotEnabled:     func() error { return model.ErrTwoFANotEnabled },
		TwoFAAlreadyEnabled: func() error { return model.ErrTwoFAAlreadyEnabled },
		FlowConsumed:        func() error { return model.ErrAuthFlowConsumed },
		FlowExpired:         func() error { return model.ErrAuthFlowExpired },
		ProofConsumed:       func() error { return ErrProofConsumed },
		TokenExpired:        func() error { return ErrAuthTokenExpired },
		TokenInvalid:        func() error { return ErrAuthTokenInvalid },
	}
}

func verificationDependencies() verificationadapter.Dependencies {
	return verificationadapter.Dependencies{
		ValidateSessionFunc: func(identity AuthIdentity) error { _, _, err := ValidateLoginSession(identity); return err },
		RequireMethodFunc: func(identity AuthIdentity, scope, method string) error {
			_, err := RequireVerificationMethod(identity, scope, method)
			return err
		},
		IssueProofFunc: IssueSecurityProof, BindFunc: BindVerificationOperation,
		VerifyClaimsFunc: func(raw string, identity AuthIdentity, binding VerificationBinding) (*verificationadapter.ProofClaims, error) {
			claims, err := verifySecurityProof(raw, identity, binding)
			if err != nil {
				return nil, err
			}
			return &verificationadapter.ProofClaims{ID: claims.ID, Method: claims.Method}, nil
		},
		VerifyFactorFunc: VerifyTwoFactorCode, CompleteFunc: CompleteSecurityVerification,
	}
}
