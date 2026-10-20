package domain

type VerificationErrors struct {
	FlowInvalid, Inactive, Unavailable, Locked, Forbidden, Scope, Method func() error
	Failed, FlowRequired, TwoFANotEnabled, TwoFAAlreadyEnabled           func() error
	FlowConsumed, FlowExpired, ProofConsumed, TokenExpired, TokenInvalid func() error
}
