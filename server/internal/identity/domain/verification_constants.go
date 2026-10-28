package domain

const (
	VerificationMethodTwoFA              = "2fa"
	VerificationMethodPasskey            = "passkey"
	VerificationMethodPassword           = "password"
	VerificationMethodOAuth              = "oauth"
	VerificationMethodSession            = "session"
	VerificationScopeChannelKeyRead      = "channel.key.read"
	VerificationScopePasskeyRegister     = "passkey.register"
	VerificationScopePasskeyDelete       = "passkey.delete"
	VerificationScopeTwoFASetup          = "2fa.setup"
	VerificationScopeTwoFADisable        = "2fa.disable"
	VerificationScopeTwoFABackupCodes    = "2fa.backup_codes.regenerate"
	VerificationScopeLogin               = "auth.login"
	VerificationScopeAccessTokenGenerate = "access_token.generate"
	VerificationScopeAccessTokenRevoke   = "access_token.revoke"
	VerificationScopeAccountBind         = "account.binding.bind"
	VerificationScopeAccountUnbind       = "account.binding.unbind"
	VerificationScopePasswordSet         = "account.password.set"
	VerificationScopePasswordChange      = "account.password.change"
	VerificationScopeAccountDelete       = "account.delete"
)
