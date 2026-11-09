package domain

import (
	"errors"
	"time"
)

const (
	// OpenRouterSyncOperatorMaxCostUSD bounds one operator cost settlement.
	OpenRouterSyncOperatorMaxCostUSD = 50
	// OpenRouterSyncAutoReleaseDelay is how long, counted from submission, an
	// unknown submission without an upstream identity keeps its reserve before
	// the operator release applies automatically.
	OpenRouterSyncAutoReleaseDelay = time.Hour
)

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
