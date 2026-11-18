package reconcile_sync_task

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Store persists one revision claim on the task's existing transaction boundary.
type Store interface {
	UpdateRevision(task *domain.Task, revision int) error
}

// Projection encodes the existing public task-data view.
type Projection interface{ Project(task *domain.Task) }

// Settlement prepares a frozen target and applies only an eligible durable claim.
type Settlement interface {
	Prepare(task *domain.Task)
	SettleDurable(task *domain.Task) error
}

// Metadata reads account-bound provider metadata without submitting paid requests.
type Metadata interface {
	Generation(ctx context.Context, task *domain.Task, endpoint string) (domain.SyncMetadata, int, time.Duration, string)
	Model(ctx context.Context, task *domain.Task, endpoint string) (domain.SyncModelMetadata, int, time.Duration, string)
}
type Usecase struct {
	store      Store
	codec      Projection
	settlement Settlement
	metadata   Metadata
	now        func() time.Time
}

func NewUsecase(store Store, codec Projection, settlement Settlement, metadata Metadata, now func() time.Time) Usecase {
	return Usecase{store: store, codec: codec, settlement: settlement, metadata: metadata, now: now}
}

// pollOpenRouterSync uses the existing Task scheduler, never a paid submit.
// Only the winner of a revision-checked terminal claim may move funds.
func (u Usecase) Execute(ctx context.Context, task *domain.Task) error {
	validIdentity := task.Platform == string(domain.TaskPlatformOpenRouterAudio) && task.Action == domain.TaskActionAudio && task.PrivateData.OpenRouterSync == nil ||
		(task.Platform == string(domain.TaskPlatformOpenRouterRerank) && task.Action == domain.TaskActionRerank ||
			task.Platform == string(domain.TaskPlatformOpenRouterImage) && task.Action == domain.TaskActionImage ||
			task.Platform == string(domain.TaskPlatformOpenRouterChatImage) && domain.IsOpenRouterChatImageAction(task.Action)) && task.PrivateData.OpenRouterAudio == nil
	if !validIdentity {
		domain.MarkSyncManual(task, "operation_identity_changed", u.now().Unix())
		u.codec.Project(task)
		return u.store.UpdateRevision(task, task.PrivateData.OpenRouterSyncState().Revision)
	}
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	if a.IdentityConflict {
		domain.MarkSyncManual(task, "generation_identity_unverified", u.now().Unix())
		u.codec.Project(task)
		return u.store.UpdateRevision(task, a.Revision)
	}
	now := u.now()
	if r.ReservationPending || r.SubmissionPending {
		if now.Unix() < a.LeaseUntil {
			return nil
		}
		if !r.ReservationPending {
			a.RecordResultReason("result_outcome_unknown")
		}
		if r.ReservationPending || task.PrivateData.UpstreamTaskID == "" {
			domain.MarkSyncManual(task, "interrupted_reservation_or_submission", u.now().Unix())
			u.codec.Project(task)
			return u.store.UpdateRevision(task, a.Revision)
		}
		r.SubmissionPending = false
		a.ResultFailed = !a.ResultReady
	}
	if r.SettlementPending || r.Finalized {
		if now.Unix() < a.NextPoll {
			return nil
		}
		return u.finishSettlement(task)
	}
	if now.Unix() >= a.Deadline || a.Attempts >= 16 {
		domain.MarkSyncManual(task, "cost_lookup_deadline_or_attempt_limit", u.now().Unix())
		u.codec.Project(task)
		return u.store.UpdateRevision(task, a.Revision)
	}
	if now.Unix() < a.NextPoll {
		return nil
	}
	if task.Platform == string(domain.TaskPlatformOpenRouterImage) || task.PrivateData.UpstreamTaskID == "" || a.MetadataURL == "" {
		domain.MarkSyncManual(task, "missing_generation_identity", u.now().Unix())
		u.codec.Project(task)
		return u.store.UpdateRevision(task, a.Revision)
	}
	delay := time.Minute * time.Duration(1<<min(a.Attempts, 6))
	delay = min(delay, time.Hour)
	a.Attempts++
	// Persist the attempt budget and next due time before network I/O. A stale
	// poller loses this claim; a crash cannot reset the metadata lookup budget.
	if task.Platform == string(domain.TaskPlatformOpenRouterChatImage) {
		a.NextPoll = now.Add(delay).Unix()
		if err := u.store.UpdateRevision(task, a.Revision); err != nil {
			return err
		}
	}
	cost, status, retryAfter, class := u.lookup(ctx, task)
	a.LastHTTPStatus, a.LastError, a.LastLookupClass = status, class, class
	if class == "" {
		r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD = true, true, cost
		priorStatus, priorProgress, priorFailReason := task.Status, task.Progress, task.FailReason
		domain.FinishSyncTask(task, u.now().Unix(), u.settlement.Prepare)
		if r.SettlementPending {
			// The Task turns terminal only once its settlement applies, so the
			// poller retries a settlement that failed or was interrupted.
			task.Status, task.Progress, task.FinishTime, task.FailReason = priorStatus, priorProgress, 0, priorFailReason
		}
	} else if class == "auth" || class == "account_binding_changed" || class == "identity_or_invalid_cost" || class == "permanent_http_error" {
		domain.MarkSyncManual(task, class, u.now().Unix())
	} else {
		a.NextPoll = now.Add(max(delay, retryAfter)).Unix()
		if a.NextPoll >= a.Deadline || a.Attempts >= 16 {
			domain.MarkSyncManual(task, "cost_lookup_deadline_or_attempt_limit", u.now().Unix())
		}
	}
	u.codec.Project(task)
	if err := u.store.UpdateRevision(task, a.Revision); err != nil {
		return err
	}
	if r.SettlementPending {
		return u.finishSettlement(task)
	}
	return nil
}

// settlementRetryDelay spaces the retries of a settlement that moved no balances.
const settlementRetryDelay = time.Minute

// finishSettlement applies the frozen settlement, then records the terminal
// result. A settlement that moved no balances keeps the Task pollable; one that
// moved them is never replayed and leaves only its repair to an operator.
func (u Usecase) finishSettlement(task *domain.Task) error {
	a := task.PrivateData.OpenRouterSyncState()
	settleErr := u.settlement.SettleDurable(task)
	if settleErr != nil && !task.PrivateData.Reconciliation.MainApplied {
		a.NextPoll = u.now().Add(settlementRetryDelay).Unix()
		u.codec.Project(task)
		return errors.Join(settleErr, u.store.UpdateRevision(task, a.Revision))
	}
	domain.FinishSyncTask(task, u.now().Unix(), func(*domain.Task) {})
	u.codec.Project(task)
	if err := u.store.UpdateRevision(task, a.Revision); err != nil {
		return err
	}
	return settleErr
}

func (u Usecase) lookup(ctx context.Context, task *domain.Task) (float64, int, time.Duration, string) {
	a := task.PrivateData.OpenRouterSyncState()
	d, status, retryAfter, class := u.metadata.Generation(ctx, task, a.MetadataURL+"?id="+url.QueryEscape(task.PrivateData.UpstreamTaskID))
	if class != "" {
		return 0, status, retryAfter, class
	}
	generation, matched := domain.ExpectedSyncGeneration(task.Action)
	if matched && d.APIType != generation.APIType {
		return 0, status, 0, "identity_or_invalid_cost"
	}
	if d.ID != task.PrivateData.UpstreamTaskID || d.ErrorPresent {
		return 0, status, 0, "identity_or_invalid_cost"
	}
	modelName := strings.TrimSuffix(strings.TrimSuffix(task.Properties.UpstreamModelName, ":nitro"), ":floor")
	if d.Model != modelName {
		base, ok := strings.CutSuffix(a.MetadataURL, "/generation")
		author, slug, validModel := strings.Cut(modelName, "/")
		if !ok || !validModel || author == "" || slug == "" {
			return 0, status, 0, "identity_or_invalid_cost"
		}
		model, modelStatus, modelRetry, modelClass := u.metadata.Model(ctx, task, base+"/model/"+url.PathEscape(author)+"/"+url.PathEscape(slug))
		if modelClass != "" {
			return 0, modelStatus, modelRetry, modelClass
		}
		if model.ID != modelName || model.CanonicalSlug == "" || model.CanonicalSlug != d.Model || model.ErrorPresent {
			return 0, modelStatus, 0, "identity_or_invalid_cost"
		}
	}
	completedSpeech := d.APIType == "tts" && a.ResultReady && !a.ResultFailed && strings.HasPrefix(a.ResultMimeType, "audio/")
	billedCancel := generation.BilledCancel && d.Cancelled != nil && *d.Cancelled && u.now().Unix()-task.StartTime >= 30
	if d.Cancelled == nil || *d.Cancelled && !billedCancel || d.GenerationTime == nil && !completedSpeech {
		return 0, status, retryAfter, "incomplete_metadata"
	}
	if d.GenerationTime != nil && (*d.GenerationTime < 0 || math.IsNaN(*d.GenerationTime) || math.IsInf(*d.GenerationTime, 0)) {
		return 0, status, 0, "identity_or_invalid_cost"
	}
	if !d.CostPresent {
		return 0, status, retryAfter, "incomplete_metadata"
	}
	if !d.CostValid {
		return 0, status, 0, "identity_or_invalid_cost"
	}
	return d.CostUSD, status, 0, ""
}
