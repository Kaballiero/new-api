package domain

import (
	"errors"
	"strings"
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

func ValidOpenRouterImageCorrelation(id, apiKey string, limit int) bool {
	return id != "" && len(id) <= limit && !strings.Contains(id, "sk-") && (apiKey == "" || !strings.Contains(id, apiKey)) && strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r))
	}) == -1
}

func MarkSyncManual(task *Task, reason string, now int64) {
	task.Status, task.Progress, task.FinishTime = TaskStatusFailure, "100%", now
	task.FailReason = "OpenRouter synchronous manual reconciliation required: " + reason
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

func OpenRouterSyncData(task *Task) map[string]any {
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	status := "awaiting_cost"
	if r.ReservationPending {
		status = "reservation_unknown"
	} else if r.SubmissionPending {
		status = "submission_pending"
	} else if r.SettlementPending {
		status = "settlement_pending"
	} else if r.Finalized {
		status = "settled"
	} else if task.Status == TaskStatusFailure {
		status = "manual_reconciliation"
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
