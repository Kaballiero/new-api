package domain

import "errors"

// OpenRouterSyncOperatorMaxCostUSD bounds one operator cost settlement.
const OpenRouterSyncOperatorMaxCostUSD = 50

var (
	// ErrOpenRouterSyncOperatorInvalid marks a malformed or out-of-bounds decision.
	ErrOpenRouterSyncOperatorInvalid = errors.New("invalid OpenRouter operator decision")
	// ErrOpenRouterSyncOperatorConflict marks a Task that cannot take the decision.
	ErrOpenRouterSyncOperatorConflict = errors.New("OpenRouter operator decision conflict")
)

type OpenRouterSyncOperatorResult struct {
	TaskID         string
	Platform       string
	Action         string
	CostUSD        string
	BillingState   string
	ReservedQuota  int
	TargetQuota    int
	AlreadyApplied bool
}
