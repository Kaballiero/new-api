package complete_oauth

// Outcome is the successful completion of an OAuth callback.
type Outcome uint8

const (
	// OutcomeNone writes no response: the consumed flow had no known intent.
	OutcomeNone Outcome = iota
	OutcomeBound
	OutcomeLogin
	OutcomeVerified
)

type Result struct {
	Outcome                               Outcome
	BindingSucceeded, NotificationWarning bool
}
