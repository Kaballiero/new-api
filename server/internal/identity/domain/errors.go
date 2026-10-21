package domain

import "errors"

// Business errors of identity scenarios. The texts are the existing messages:
// several of them reach clients unchanged through the transport, and legacy
// names in model and service alias these values.
var (
	// Server-side sessions (model.ErrUserSession*).
	ErrSessionInvalid        = errors.New("user session is invalid")
	ErrSessionInactive       = errors.New("user session is inactive")
	ErrSessionRefreshInvalid = errors.New("user session refresh token is invalid")
	ErrSessionRefreshRace    = errors.New("user session refresh is already in progress")
	ErrSessionRefreshReuse   = errors.New("user session refresh token was reused")
	ErrSessionLimit          = errors.New("active user session limit reached")
	ErrSessionIssuanceLimit  = errors.New("user session issuance limit reached")

	// Login sessions and refresh tokens (service.ErrLoginSession*, ErrRefresh*).
	ErrLoginSessionInvalid  = errors.New("login session is invalid")
	ErrLoginSessionRevoked  = errors.New("login session is revoked")
	ErrLoginSessionMismatch = errors.New("login session does not match the expected session")
	ErrRefreshTokenInvalid  = errors.New("refresh token is invalid")
	ErrRefreshRace          = errors.New("refresh token was already rotated")

	// Access tokens and security proofs (service.ErrAuthToken*, ErrProof*).
	ErrAuthTokenInvalid = errors.New("authentication token is invalid")
	ErrAuthTokenExpired = errors.New("authentication token has expired")
	ErrProofScope       = errors.New("security proof scope mismatch")
	ErrProofMethod      = errors.New("security proof method mismatch")
	ErrProofContext     = errors.New("security proof context mismatch")
	ErrProofConsumed    = errors.New("security proof has already been consumed")

	// One-time authentication flows (model.ErrAuthFlow*).
	ErrAuthFlowInvalid  = errors.New("auth flow is invalid")
	ErrAuthFlowExpired  = errors.New("auth flow has expired")
	ErrAuthFlowConsumed = errors.New("auth flow has already been consumed")

	// Security verification (service.ErrVerification*, ErrOAuthAccountMismatch).
	ErrVerificationFailed         = errors.New("Verification failed. Please try again.")
	ErrVerificationUnavailable    = errors.New("This verification method is currently unavailable.")
	ErrVerificationLocked         = errors.New("Two-factor authentication is temporarily locked.")
	ErrVerificationFlowRequired   = errors.New("This verification method requires its dedicated verification flow.")
	ErrVerificationContextInvalid = errors.New("The action details are invalid.")
	ErrVerificationForbidden      = errors.New("You do not have permission to perform this action.")
	ErrOAuthAccountMismatch       = errors.New("The OAuth account does not match the account linked to your profile.")

	// Factors (model.ErrTwoFA*, ErrPasskeyNotFound).
	ErrTwoFASetupInvalid   = errors.New("The two-factor setup has expired or changed. Start setup again.")
	ErrTwoFACodeInvalid    = errors.New("The authenticator code is incorrect.")
	ErrTwoFANotEnabled     = errors.New("2fa not enabled")
	ErrTwoFAAlreadyEnabled = errors.New("2fa already enabled")
	ErrPasskeyNotFound     = errors.New("passkey credential not found")

	// Email binding (service.ErrAccountEmail*, ErrEmailBindingDelivery,
	// model.ErrEmailBinding*).
	ErrEmailInvalid            = errors.New("Please enter a valid email address")
	ErrEmailRestricted         = errors.New("This email address is not allowed by the administrator's email policy.")
	ErrEmailBindingDelivery    = errors.New("Verification email could not be sent. Start email verification again.")
	ErrEmailBindingCodeInvalid = errors.New("Email verification code is incorrect.")
	ErrEmailBindingLocked      = errors.New("Too many incorrect codes. Start email verification again.")
	ErrEmailBindingResendWait  = errors.New("Please wait before requesting another verification code.")

	// Account security state (model/account_security.go,
	// model.ErrExternalIdentityAlreadyClaimed).
	ErrCurrentPasswordInvalid = errors.New("Current password is incorrect.")
	ErrPasswordSame           = errors.New("New password must be different from current password")
	ErrPasswordState          = errors.New("Password settings have changed. Reload the page and try again.")
	ErrLastLoginMethod        = errors.New("Add another login method before unlinking this account.")
	ErrBindingChanged         = errors.New("Account bindings have changed. Start this operation again.")
	ErrIdentityClaimed        = errors.New("external identity is already claimed")

	// Telegram login (oauth.ErrTelegram*).
	ErrTelegramOAuthNotConfigured = errors.New("Telegram OAuth is not configured or enabled. Please contact your administrator.")
	ErrTelegramOAuthConflict      = errors.New("The telegram OAuth provider name is reserved. Ask your administrator to rename the conflicting custom provider.")
	ErrTelegramOAuthFailed        = errors.New("Telegram authorization failed. Please try again.")
	ErrTelegramAccountNotBound    = errors.New("This Telegram account is not linked. Sign in using another method and link it first.")

	// Passkey ceremony outcomes with their own responses.
	ErrPasskeyIdentityRequired = errors.New("passkey ceremony requires a session identity")
	ErrPasskeyMissing          = errors.New("user has no passkey")
	ErrPasskeyRecordInvalid    = errors.New("passkey credential record is invalid")
	ErrPasskeyLoginInvalid     = errors.New("passkey login account is invalid")
	ErrPasskeyAccountDisabled  = errors.New("passkey login account is disabled")

	// Outcomes of login, registration and account management scenarios.
	ErrInvalidInput          = errors.New("invalid input")
	ErrCredentialsInvalid    = errors.New("invalid credentials")
	ErrStorageUnavailable    = errors.New("storage is unavailable")
	ErrAccountDeleted        = errors.New("user has been deleted")
	ErrAccountBanned         = errors.New("user is banned")
	ErrRegistrationDisabled  = errors.New("registration is disabled")
	ErrEmailTaken            = errors.New("email is already in use")
	ErrRootDeletion          = errors.New("root account cannot be deleted")
	ErrTargetNotFound        = errors.New("target user not found")
	ErrTargetForbidden       = errors.New("target user role cannot be managed")
	ErrTargetTwoFADisabled   = errors.New("target user has no two-factor authentication")
	ErrProviderIDInvalid     = errors.New("invalid provider id")
	ErrProviderUnknown       = errors.New("unknown oauth provider")
	ErrProviderDisabled      = errors.New("oauth provider is disabled")
	ErrOAuthStateInvalid     = errors.New("oauth state is invalid")
	ErrAlreadyBound          = errors.New("provider account is already bound")
	ErrBindingSessionMissing = errors.New("binding requires a login session")
	ErrRecoveryLinkInvalid   = errors.New("password reset link is invalid")
	ErrSessionIDRequired     = errors.New("session id is required")
	ErrSessionNotFound       = errors.New("session not found")
	ErrPasswordKeyMissing    = errors.New("password encryption key is unavailable")
	ErrBackupCodesGenerate   = errors.New("failed to generate backup codes")
	ErrBackupCodesSave       = errors.New("failed to save backup codes")
	ErrWeChatTaken           = errors.New("wechat account is already bound")
	ErrWeChatCodeMissing     = errors.New("wechat code is empty")
	ErrWeChatCodeInvalid     = errors.New("wechat code is invalid or expired")

	// Password registration outcomes.
	ErrEmailVerificationRequired = errors.New("email verification is required")
	ErrVerificationCodeInvalid   = errors.New("email verification code is invalid")
	ErrUserExists                = errors.New("user already exists")
	ErrRegisterFailed            = errors.New("registration failed")
	ErrDefaultTokenFailed        = errors.New("failed to generate the default token")
	ErrDefaultTokenInsertFailed  = errors.New("failed to create the default token")

	// ErrStopped reports that the scenario already wrote its response (for
	// example, a security proof was requested) and nothing else is written.
	ErrStopped = errors.New("response already written")

	// ErrInternal classifies an unexpected failure. Create it with Internal so
	// that the message stays the text of the cause.
	ErrInternal = errors.New("internal error")
)

// Internal marks cause as an unexpected failure: errors.Is(err, ErrInternal)
// holds, errors.Unwrap returns cause and the message is the text of cause.
func Internal(cause error) error { return internalError{cause: cause} }

type internalError struct{ cause error }

func (e internalError) Error() string        { return e.cause.Error() }
func (e internalError) Unwrap() error        { return e.cause }
func (e internalError) Is(target error) bool { return target == ErrInternal }

// ProviderError marks a failure reported by an external identity provider
// (OAuth or WeChat code exchange). The message is the text of the cause.
type ProviderError struct{ Cause error }

func (e *ProviderError) Error() string { return e.Cause.Error() }
func (e *ProviderError) Unwrap() error { return e.Cause }

// InputError reports rejected registration input; Detail is the validator text.
type InputError struct{ Detail string }

func (e *InputError) Error() string { return "invalid input: " + e.Detail }

// LoggedError is a business error whose details are logged after the response
// is written; errors.Is matches Err.
type LoggedError struct {
	Err error
	Log string
}

func (e *LoggedError) Error() string { return e.Err.Error() }
func (e *LoggedError) Unwrap() error { return e.Err }

// ProviderMessageError carries the provider error description shown to the
// client when the provider callback reports an error.
type ProviderMessageError struct{ Message string }

func (e *ProviderMessageError) Error() string { return e.Message }
