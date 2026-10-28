package domain

const (
	AuthFlowPurposeOAuth             = "oauth"
	AuthFlowPurposeTwoFALogin        = "2fa_login"
	AuthFlowPurposeLoginVerification = "login_verification"
	AuthFlowPurposeLoginPasskey      = "login_passkey"
	AuthFlowPurposePasskeyLogin      = "passkey_login"
	AuthFlowPurposePasskeyRegister   = "passkey_register"
	AuthFlowPurposePasskeyStepUp     = "passkey_step_up"
	AuthFlowPurposeTelegramBind      = "telegram_bind"
	AuthFlowPurposeTelegramAssertion = "telegram_assertion"
	AuthFlowIntentLogin              = "login"
	AuthFlowIntentBind               = "bind"
	AuthFlowIntentVerify             = "verify"
	AuthFlowPurposeTwoFASetup        = "2fa_setup"
	AuthFlowPurposeSecurityProof     = "security_proof"
	AuthFlowPurposeEmailBinding      = "email_binding"
)
