package domain

import (
	"errors"
	"slices"
	"strings"
	"time"
)

type SyncIdentity struct {
	TaskID                     string
	Operation                  func() (platform, action string, err error)
	UserID, ChannelID, TokenID int
	UpstreamModel, RequestID   string
	NativeImage                bool
	ProviderTag                string
}

type SyncCost struct {
	GenerationID                   string
	Final, Valid, IdentityConflict bool
	CostUSD                        float64
	IsBYOK                         *bool
	CostDetails                    []byte
	UsageEstimateUSD               float64
}

type SyncNativeImage struct {
	ProviderTag    string
	ReserveUSD     float64
	PricingUnknown bool
}

type SyncSubmissionAccount struct {
	ChannelType     int
	Transport       TransportSettings
	Headers         map[string]any
	APIKey          string
	CredentialIndex int
	RoutingHash     string
}

type SyncMetadata struct {
	ID, Model, APIType     string
	Cancelled              *bool
	GenerationTime         *float64
	ErrorPresent           bool
	CostPresent, CostValid bool
	CostUSD                float64
}

type SyncModelMetadata struct {
	ID, CanonicalSlug string
	ErrorPresent      bool
}

// SyncGeneration is what /v1/generation reports for the generation of a Task action.
type SyncGeneration struct {
	APIType string
	// BilledCancel accepts the cost of a cancelled generation: a text stream
	// the client left after content was delivered.
	BilledCancel bool
}

// syncGenerations covers the actions whose generation is matched by
// api_type; an audio generation is not.
var syncGenerations = map[string]SyncGeneration{
	TaskActionChatImage:   {APIType: "completions"},
	TaskActionChat:        {APIType: "completions", BilledCancel: true},
	TaskActionCompletions: {APIType: "completions", BilledCancel: true},
	TaskActionEmbeddings:  {APIType: "embeddings"},
	TaskActionDecisions:   {APIType: "decisions"},
	TaskActionRerank:      {APIType: "rerank"},
}

// ExpectedSyncGeneration returns the generation a Task action must match.
func ExpectedSyncGeneration(action string) (SyncGeneration, bool) {
	generation, ok := syncGenerations[action]
	return generation, ok
}

var openRouterChatImageActions = []string{TaskActionChatImage, TaskActionChat, TaskActionCompletions, TaskActionEmbeddings, TaskActionDecisions}

// IsOpenRouterChatImageAction reports an action of the openrouter-chat-image platform.
func IsOpenRouterChatImageAction(action string) bool {
	return slices.Contains(openRouterChatImageActions, action)
}

func ValidOpenRouterImageCorrelation(id, apiKey string, limit int) bool {
	return id != "" && len(id) <= limit && !strings.Contains(id, "sk-") && (apiKey == "" || !strings.Contains(id, apiKey)) && strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r))
	}) == -1
}

// Manual hold reasons of the automatic release: it selects holds parked for an
// unknown submission and re-parks those it may not release, leaving them to root.
const (
	SyncReasonSubmissionOutcomeUnknown = "submission_outcome_unknown"
	SyncReasonAutoReleaseUnavailable   = "auto_release_unavailable"
)

// SyncReasonReservationRefused closes an intent whose quota reservation was
// refused before the Task held any quota.
const SyncReasonReservationRefused = "reservation_refused"

// Billing states of an OpenRouter synchronous task the admin task list selects:
// a manual hold awaiting an operator and an attempt still awaiting its cost.
const (
	SyncBillingManualReconciliation = "manual_reconciliation"
	SyncBillingAwaitingCost         = "awaiting_cost"
)

// SyncBillingSettlementPending is the billing state of a confirmed cost whose
// settlement has not applied yet.
const SyncBillingSettlementPending = "settlement_pending"

// SyncSettlementRetryDelay spaces the retries of a settlement that moved no
// balances.
const SyncSettlementRetryDelay = time.Minute

// SyncSettlementStallAge is how long a settlement retried by the poller may
// stay pending before it is reported to root.
const SyncSettlementStallAge = 30 * time.Minute

// SyncManualFailReason is the stored failure text of a manual hold for reason.
func SyncManualFailReason(reason string) string {
	return "OpenRouter synchronous manual reconciliation required: " + reason
}

// IsOpenRouterSyncManualHold reports a task held for manual reconciliation: a
// failed task whose failure text carries a hold reason.
func (t *Task) IsOpenRouterSyncManualHold() bool {
	return t.Status == TaskStatusFailure && strings.HasPrefix(t.FailReason, SyncManualFailReason(""))
}

func MarkSyncManual(task *Task, reason string, now int64) {
	task.Status, task.Progress, task.FinishTime = TaskStatusFailure, "100%", now
	task.FailReason = SyncManualFailReason(reason)
	task.PrivateData.OpenRouterSyncState().LastError = reason
	task.PrivateData.Reconciliation.Required = true
}

func FinishSyncTask(task *Task, now int64, prepare func(*Task)) {
	task.Status, task.Progress, task.FinishTime = TaskStatusSuccess, "100%", now
	if !task.PrivateData.OpenRouterSyncState().ResultReady {
		task.Status = TaskStatusFailure
		task.FailReason = "OpenRouter synchronous result unavailable; upstream cost confirmed"
	}
	prepare(task)
}

// ConfirmSyncCost freezes the settlement of the confirmed cost. A Task in
// progress turns terminal only once its settlement applies, so the poller
// retries a settlement that failed; it reports whether the caller records the
// terminal result after the settlement. A manual hold claimed by a late cost
// turns terminal at once.
func ConfirmSyncCost(task *Task, now time.Time, prepare func(*Task)) (finishAfterSettlement bool) {
	if task.Status == TaskStatusFailure {
		FinishSyncTask(task, now.Unix(), prepare)
		return false
	}
	prepare(task)
	if !task.PrivateData.Reconciliation.SettlementPending {
		FinishSyncTask(task, now.Unix(), func(*Task) {})
		return false
	}
	a := task.PrivateData.OpenRouterSyncState()
	a.NextPoll, a.SettlementPendingSince = now.Add(SyncSettlementRetryDelay).Unix(), now.Unix()
	return true
}

func OpenRouterSyncData(task *Task) map[string]any {
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	status := SyncBillingAwaitingCost
	if r.ReservationPending {
		status = "reservation_unknown"
	} else if r.SubmissionPending {
		status = "submission_pending"
	} else if r.SettlementPending {
		status = SyncBillingSettlementPending
	} else if r.Finalized {
		status = "settled"
	} else if task.Status == TaskStatusFailure {
		status = SyncBillingManualReconciliation
	}
	return map[string]any{"result_ready": a.ResultReady, "result_failed": a.ResultFailed, "billing_status": status, "cost_lookup_attempts": a.Attempts, "cost_lookup_http_status": a.LastHTTPStatus, "next_cost_lookup": a.NextPoll}
}

// ValidateNewSyncReservation preserves the admission rule before intent creation.
func ValidateNewSyncReservation(quota int, allowZero, existing bool) error {
	if quota < 0 || quota == 0 && !allowZero || existing {
		return errors.New("OpenRouter synchronous requires a new positive reservation")
	}
	return nil
}
