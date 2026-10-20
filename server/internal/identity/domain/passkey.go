package domain

// PasskeyFailure identifies existing transport-specific responses after a ceremony stage.
type PasskeyFailure string

const (
	PasskeyIdentityRequired PasskeyFailure = "identity_required"
	PasskeyRecordMissing    PasskeyFailure = "record_missing"
	PasskeyRecordInvalid    PasskeyFailure = "record_invalid"
	PasskeyLoginInvalid     PasskeyFailure = "login_invalid"
	PasskeyAccountDisabled  PasskeyFailure = "account_disabled"
)
