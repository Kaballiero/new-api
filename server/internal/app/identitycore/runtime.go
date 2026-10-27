package identitycore

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	authtoken "github.com/QuantumNous/new-api/internal/adapter/identity/authtoken"
	emailadapter "github.com/QuantumNous/new-api/internal/adapter/identity/emailbinding"
	loginadapter "github.com/QuantumNous/new-api/internal/adapter/identity/login"
	sessionadapter "github.com/QuantumNous/new-api/internal/adapter/identity/session"
	verificationadapter "github.com/QuantumNous/new-api/internal/adapter/identity/verification"
	"github.com/QuantumNous/new-api/internal/identity/advance_session_security"
	"github.com/QuantumNous/new-api/internal/identity/bind_verification_operation"
	"github.com/QuantumNous/new-api/internal/identity/cleanup_auth_artifacts"
	"github.com/QuantumNous/new-api/internal/identity/complete_login_verification"
	"github.com/QuantumNous/new-api/internal/identity/complete_verification"
	"github.com/QuantumNous/new-api/internal/identity/consume_passkey_flow"
	"github.com/QuantumNous/new-api/internal/identity/consume_proof"
	"github.com/QuantumNous/new-api/internal/identity/create_passkey_flow"
	"github.com/QuantumNous/new-api/internal/identity/create_session"
	"github.com/QuantumNous/new-api/internal/identity/finish_oauth_verification"
	"github.com/QuantumNous/new-api/internal/identity/get_verification_requirements"
	"github.com/QuantumNous/new-api/internal/identity/refresh_session"
	"github.com/QuantumNous/new-api/internal/identity/require_login_verification"
	"github.com/QuantumNous/new-api/internal/identity/require_verification_method"
	"github.com/QuantumNous/new-api/internal/identity/resend_email_binding"
	"github.com/QuantumNous/new-api/internal/identity/revoke_refresh_token"
	"github.com/QuantumNous/new-api/internal/identity/start_email_binding"
	"github.com/QuantumNous/new-api/internal/identity/start_login_verification"
	"github.com/QuantumNous/new-api/internal/identity/start_oauth_verification"
	"github.com/QuantumNous/new-api/internal/identity/start_twofa_setup"
	"github.com/QuantumNous/new-api/internal/identity/validate_authorization"
	"github.com/QuantumNous/new-api/internal/identity/validate_session"
	"github.com/QuantumNous/new-api/internal/identity/verification_policy"
	"github.com/QuantumNous/new-api/internal/identity/verify_factor_code"
	"github.com/QuantumNous/new-api/internal/identity/verify_login_code"
	"github.com/QuantumNous/new-api/internal/identity/verify_security_input"
)

type runtime struct {
	LoginDependencies           loginadapter.Dependencies
	EmailDependencies           emailadapter.Dependencies
	VerificationDependencies    verificationadapter.Dependencies
	Cleanup                     cleanup_auth_artifacts.Usecase
	CreateSession               create_session.Usecase
	ValidateSession             validate_session.Usecase
	RefreshSession              refresh_session.Usecase
	AdvanceSessionSecurity      advance_session_security.Usecase
	RevokeRefreshToken          revoke_refresh_token.Usecase
	VerificationPolicy          verification_policy.Usecase
	GetVerificationRequirements get_verification_requirements.Usecase
	RequireVerificationMethod   require_verification_method.Usecase
	CompleteVerification        complete_verification.Usecase
	ConsumeProof                consume_proof.Usecase
	ValidateAuthorization       validate_authorization.Usecase
	VerifySecurityInput         verify_security_input.Usecase
	VerifyFactorCode            verify_factor_code.Usecase
	StartLoginVerification      start_login_verification.Usecase
	RequireLoginVerification    require_login_verification.Usecase
	VerifyLoginCode             verify_login_code.Usecase
	CompleteLoginVerification   complete_login_verification.Usecase
	StartEmailBinding           start_email_binding.Usecase
	ResendEmailBinding          resend_email_binding.Usecase
	StartTwofaSetup             start_twofa_setup.Usecase
	StartOauthVerification      start_oauth_verification.Usecase
	FinishOauthVerification     finish_oauth_verification.Usecase
	CreatePasskeyFlow           create_passkey_flow.Usecase
	ConsumePasskeyFlow          consume_passkey_flow.Usecase
	Bind                        bind_verification_operation.Usecase
}

var runtimeOnce sync.Once
var runtimeValue runtime

func identityRuntime() *runtime {
	runtimeOnce.Do(func() {
		runtimeValue = runtime{
			LoginDependencies:           loginDependencies(),
			EmailDependencies:           emailBindingDependencies(),
			VerificationDependencies:    verificationDependencies(),
			Cleanup:                     cleanup_auth_artifacts.NewUsecase(sessionadapter.ArtifactCleanup{}, func() int { return common.UserSessionHourlyAlertThreshold }, common.SysError),
			CreateSession:               create_session.NewUsecase(sessionPolicy()),
			ValidateSession:             validate_session.NewUsecase(sessionPolicy()),
			RefreshSession:              refresh_session.NewUsecase(sessionPolicy(), refresh_session.RefreshCodec{Split: sessionadapter.SplitRefreshToken, DeriveNext: sessionadapter.DeriveNextRefreshSecret}),
			AdvanceSessionSecurity:      advance_session_security.NewUsecase(common.UserStatusEnabled),
			RevokeRefreshToken:          revoke_refresh_token.NewUsecase(sessionadapter.SplitRefreshToken),
			VerificationPolicy:          verification_policy.NewUsecase(common.RoleRootUser),
			GetVerificationRequirements: get_verification_requirements.NewUsecase(common.UserStatusEnabled, common.RoleRootUser),
			RequireVerificationMethod:   require_verification_method.NewUsecase(),
			CompleteVerification:        complete_verification.NewUsecase(),
			ConsumeProof:                consume_proof.NewUsecase(),
			ValidateAuthorization:       validate_authorization.NewUsecase(),
			VerifySecurityInput:         verify_security_input.NewUsecase(),
			VerifyFactorCode:            verify_factor_code.NewUsecase(),
			StartLoginVerification:      start_login_verification.NewUsecase(common.UserStatusEnabled),
			RequireLoginVerification:    require_login_verification.NewUsecase(common.UserStatusEnabled),
			VerifyLoginCode:             verify_login_code.NewUsecase(),
			CompleteLoginVerification:   complete_login_verification.NewUsecase(),
			StartEmailBinding:           start_email_binding.NewUsecase(),
			ResendEmailBinding:          resend_email_binding.NewUsecase(),
			StartTwofaSetup:             start_twofa_setup.NewUsecase(),
			StartOauthVerification:      start_oauth_verification.NewUsecase(),
			FinishOauthVerification:     finish_oauth_verification.NewUsecase(),
			CreatePasskeyFlow:           create_passkey_flow.NewUsecase(),
			ConsumePasskeyFlow:          consume_passkey_flow.NewUsecase(),
			Bind:                        bind_verification_operation.NewUsecase(verificationadapter.ContextCodec{SigningKey: authtoken.SigningKey}),
		}
	})
	return &runtimeValue
}
