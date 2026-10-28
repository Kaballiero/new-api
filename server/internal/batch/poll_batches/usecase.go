package poll_batches

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/QuantumNous/new-api/internal/batch/domain"
)

type Store interface {
	ByID(id int64) (*domain.Job, error)
	Inventory(now int64) ([]domain.Job, error)
	ReloadClaim(id int64, owner string) (domain.Job, error)
	FinishClaim(job *domain.Job, owner string, now int64, message *string) error
	DeleteLocal(job *domain.Job, now int64) error
	RequireReconciliation(job *domain.Job) error
	SaveUnknownDiagnostic(*domain.Job, string, string, bool) (bool, error)
	FlagProviderWindow(*domain.Job) (bool, error)
	MarkTerminalIncomplete(job *domain.Job) error
	MarkCancelSent(job *domain.Job) error
	Items(id int64) ([]domain.Item, error)
	MarkPreparing(job *domain.Job, owner string) error
	SaveInputFile(job *domain.Job, owner, id string) error
	RejectCreate(job *domain.Job) error
	BindSubmission(job *domain.Job, u domain.SubmissionUpdate) error
	SavePoll(job *domain.Job, owner string, u domain.PollUpdate) error
	LocalCustomID(id int64, providerID string) (string, error)
	RejectUnsubmitted(job *domain.Job, reason string) error
	UnsettledIdentities(id int64) ([]domain.Item, error)
	ClearResults(job *domain.Job) error
	NextUnsettled(id, cursor int64) (domain.Item, error)
	MarkItemUnknown(id int64) error
	NextPurchase(id, cursor int64) (domain.Item, error)
	ReconcilePurchase(job *domain.Job, cost, evidence string) error
	ClaimBatchJob(id int64, owner string) (bool, error)
	ConfirmBatchReservation(*domain.Job) error
	MarkBatchSubmissionUnknown(*domain.Job, string) error
	SyncBatchQuotaEvents(int) error
	SyncBatchConsumeLogs(int) error
	ReleaseSettledBatchHolds(int64) error
	SettleBatchItem(int64, string, int, string) error
	StoreBatchResult(int64, string, string, string) error
	SettleBatchJobCost(int64) error
}
type Gateway interface {
	domain.Gateway
	Failure(string, int, string, []byte) string
	AmbiguousFailure(string, int, string, []byte) string
	UsageFacts(*domain.Job, domain.Result) (domain.UsageFacts, error)
}
type Diagnostics interface {
	Error(string)
	Saturation(*domain.Job, string, error)
}
type Usecase struct {
	store       Store
	gateway     Gateway
	codec       domain.Codec
	now         func() time.Time
	kinds       domain.ChannelKinds
	diagnostics Diagnostics
	billing     Billing
	metadata    Metadata
}

func NewUsecase(store Store, gateway Gateway, codec domain.Codec, billing Billing, metadata Metadata,
	diagnostics Diagnostics, now func() time.Time, kinds domain.ChannelKinds) Usecase {
	return Usecase{store: store, gateway: gateway, codec: codec, billing: billing, metadata: metadata, diagnostics: diagnostics, now: now, kinds: kinds}
}
func (u Usecase) Execute(ctx context.Context, runnerID string) error {
	if err := u.store.SyncBatchQuotaEvents(1000); err != nil {
		u.diagnostics.Error("Batch outbox: " + err.Error())
	}
	if err := u.store.SyncBatchConsumeLogs(100); err != nil {
		u.diagnostics.Error("Batch consume logs: " + err.Error())
	}
	jobs, err := u.store.Inventory(u.now().Unix())
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		claimed, err := u.store.ClaimBatchJob(job.ID, runnerID)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		if job, err = u.store.ReloadClaim(job.ID, runnerID); err != nil {
			return err
		}
		err = u.processBatchJob(ctx, &job, runnerID)
		message := ""
		if err != nil {
			message = "batch requires retry or reconciliation"
			u.diagnostics.Error(fmt.Sprintf("batch %s: %v", job.PublicID, err))
		}
		var lastError *string
		if job.SubmitState != "submission_unknown" && job.SubmitState != "rejected" && (job.Status != "failed" || (err != nil && job.LastError == "")) {
			lastError = &message
		}
		if err := u.store.FinishClaim(&job, runnerID, u.now().Unix(), lastError); err != nil {
			return err
		}
	}
	return nil
}

func (u Usecase) processBatchJob(ctx context.Context, job *domain.Job, runnerID string) error {
	adapter, err := u.gateway.ForChannel(job.ChannelType)
	if err != nil {
		return err
	}
	if job.DeletedAt == 0 && u.now().Unix() >= job.ExpiresAt && domain.State(job.Status).Terminal() &&
		job.BillingStatus == "settled" && job.PurchaseStatus == "settled" {
		if err := u.store.DeleteLocal(job, u.now().Unix()); err != nil {
			return err
		}
		job.DeletedAt = u.now().Unix()
	}
	if job.DeletedAt > 0 {
		return u.cleanupBatch(ctx, job, adapter)
	}
	if job.SubmitState == "queued" || job.SubmitState == "preparing" {
		if err := u.store.ConfirmBatchReservation(job); err != nil {
			if errors.Is(err, domain.ErrBatchInsufficientQuota) || errors.Is(err, domain.ErrBatchDispatchDenied) {
				return u.rejectUnsubmittedBatch(job, "reservation unavailable")
			}
			return err
		}
		if err := u.submitBatch(ctx, job, adapter, runnerID); err != nil {
			return err
		}
	}
	if job.SubmitState == "submission_unknown" {
		if err := u.store.RequireReconciliation(job); err != nil {
			return err
		}
		job.BillingStatus = "reconciliation_required"
		if !domain.SubmissionOverdue(job, u.now()) {
			return nil
		}
		var failure domain.Failure
		if u.codec.UnmarshalJsonStr(job.LastError, &failure) != nil || failure.Version != 1 {
			failure = domain.Failure{Version: 1, Phase: "create", Code: "submission_unknown", Category: "ambiguous", Message: "The provider Batch creation outcome is unknown."}
		}
		if failure.AlertedAt != 0 {
			return nil
		}
		failure.AlertedAt = u.now().Unix()
		encoded, err := u.codec.Marshal(failure)
		if err != nil {
			return err
		}
		changed, err := u.store.SaveUnknownDiagnostic(job, runnerID, string(encoded), false)
		if err != nil {
			return err
		}
		if changed {
			job.LastError = string(encoded)
			u.diagnostics.Error(fmt.Sprintf("Batch %s: creation outcome unknown >48h; hold %d kept; verify provider, then recover or release", job.PublicID, job.ReservedQuota))
		}
		return nil
	}
	if job.SubmitState == "rejected" {
		reason := job.LastError
		if reason == "" {
			reason = "creation rejected"
		}
		return u.rejectUnsubmittedBatch(job, reason)
	}
	if job.UpstreamID == "" {
		return nil
	}
	if job.SubmitState == "cancel_requested" {
		request, err := adapter.Cancel(job.UpstreamID)
		if err != nil {
			return err
		}
		response, err := u.gateway.Request(ctx, job, request, nil, "")
		if err != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("batch cancellation HTTP %d", response.StatusCode)
		}
		if err := u.store.MarkCancelSent(job); err != nil {
			return err
		}
	}
	var pollErr error
	if u.now().Unix() < job.ExpiresAt {
		pollErr = u.pollBatch(ctx, job, adapter, runnerID)
	}
	if err := u.settleResults(job); err != nil {
		return err
	}
	if domain.ProviderWindowExceeded(job, u.now(), u.kinds) {
		changed, err := u.store.FlagProviderWindow(job)
		if err != nil {
			return err
		}
		if changed {
			job.BillingStatus = "reconciliation_required"
			u.diagnostics.Error(fmt.Sprintf("Batch %s: provider window of 24h plus 2h grace exceeded while %s; hold %d kept for reconciliation", job.PublicID, job.Status, job.ReservedQuota))
		}
	}
	if err := u.store.SyncBatchQuotaEvents(1000); err != nil {
		u.diagnostics.Error("Batch outbox: " + err.Error())
	}
	if err := u.store.SyncBatchConsumeLogs(100); err != nil {
		u.diagnostics.Error("Batch consume logs: " + err.Error())
	}
	if err := u.reconcilePurchase(job); err != nil {
		return err
	}
	if err := u.store.ReleaseSettledBatchHolds(job.ID); err != nil {
		return err
	}
	if domain.State(job.Status).Terminal() {
		if err := u.store.MarkTerminalIncomplete(job); err != nil {
			return err
		}
	}
	return pollErr
}

func (u Usecase) submitBatch(ctx context.Context, job *domain.Job, adapter domain.Provider, runnerID string) error {
	contract, err := u.codec.Snapshot(job.BillingSnapshot)
	if err != nil {
		return err
	}
	items, err := u.store.Items(job.ID)
	if err != nil {
		return err
	}
	submission := domain.Submission{ID: job.PublicID, Model: job.UpstreamModel, Endpoint: job.Endpoint,
		Provider: contract.Provider, InputFileID: job.InputFileID, Items: make([]domain.RequestItem,
			len(items))}
	for i, item := range items {
		submission.Items[i] = domain.RequestItem{CustomID: item.ProviderCustomID, Body: []byte(item.UpstreamBody)}
	}
	if job.SubmitState == "queued" {
		if err := u.store.MarkPreparing(job, runnerID); err != nil {
			return err
		}
		job.SubmitState = "preparing"
	}
	if job.ChannelType == u.kinds.OpenAI && job.InputFileID == "" {
		id, err := u.gateway.Upload(ctx, job, submission)
		if err != nil {
			return err
		}
		if err := u.store.SaveInputFile(job, runnerID, id); err != nil {
			return err
		}
		job.InputFileID, submission.InputFileID = id, id
	}
	request, err := adapter.Create(submission)
	if err != nil {
		return u.rejectUnsubmittedBatch(job, "invalid native request")
	}
	if job.ChannelType == u.kinds.Google && len(request.Body) > 20<<20 {
		return u.rejectUnsubmittedBatch(job, "Google inline request exceeds 20 MiB")
	}
	// Write the ambiguity marker BEFORE the first byte of the paid POST.
	// No timeout, expired lease or process restart authorizes another POST.
	if err := u.store.MarkBatchSubmissionUnknown(job, runnerID); err != nil {
		if errors.Is(err, domain.ErrBatchInsufficientQuota) || errors.Is(err, domain.ErrBatchDispatchDenied) {
			return u.rejectUnsubmittedBatch(job, "dispatch unavailable")
		}
		return err
	}
	job.SubmitState = "submission_unknown"
	response, err := u.gateway.Request(ctx, job, request, nil, "")
	if err != nil {
		u.recordAmbiguousCreation(job, runnerID, "transport_error", 0, "", nil)
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case 400, 401, 403, 404, 413, 422:
			if err := u.store.RejectCreate(job); err != nil {
				return err
			}
			job.SubmitState = "rejected"
			body, _ := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
			diagnostic := u.gateway.Failure("create", response.StatusCode, response.RequestID, body)
			u.diagnostics.Error(fmt.Sprintf("Batch %s creation rejected: %s", job.PublicID, diagnostic))
			return u.rejectUnsubmittedBatch(job, diagnostic)
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
		u.recordAmbiguousCreation(job, runnerID, "", response.StatusCode, response.RequestID, body)
		return fmt.Errorf("ambiguous batch creation HTTP %d", response.StatusCode)
	}
	snapshot, decodeErr := adapter.ReadStatus(response.Body, func(result domain.Result) error { return u.recordResult(job, result) })
	if snapshot.ID == "" {
		u.recordAmbiguousCreation(job, runnerID, "missing_batch_id", response.StatusCode, response.RequestID, nil)
	} else {
		if _, err := adapter.Poll(snapshot.ID); err != nil {
			u.recordAmbiguousCreation(job, runnerID, "invalid_batch_id", response.StatusCode, response.RequestID, nil)
			return err
		}
		state := snapshot.State
		if state == "" {
			state = domain.Validating
		}
		update := domain.SubmissionUpdate{ID: snapshot.ID, State: state}
		if state == domain.Failed {
			job.LastError = u.gateway.Failure("create", response.StatusCode, response.RequestID, snapshot.Error)
			update.LastError = &job.LastError
		}
		if err := u.store.BindSubmission(job, update); err != nil {
			return err
		}
		job.UpstreamID, job.SubmitState, job.Status = snapshot.ID, "submitted", string(state)
	}
	return decodeErr
}

func (u Usecase) pollBatch(ctx context.Context, job *domain.Job, adapter domain.Provider, runnerID string) error {
	request, err := adapter.Poll(job.UpstreamID)
	if err != nil {
		return err
	}
	response, err := u.gateway.Request(ctx, job, request, nil, "")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	// OpenRouter HTTP402 can describe incurred cost with output withheld.
	if response.StatusCode != 200 && !(job.ChannelType == u.kinds.OpenRouter && response.StatusCode == 402) {
		return fmt.Errorf("batch retrieval HTTP %d", response.StatusCode)
	}
	// Stage inline output on disk until its enclosing provider identity is verified.
	staged, err := os.CreateTemp("", "getapi-batch-results-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	snapshot, err := adapter.ReadStatus(response.Body, func(result domain.Result) error {
		data, err := u.codec.Marshal(result)
		if err != nil {
			return err
		}
		_, err = staged.Write(append(data, '\n'))
		return err
	})
	if err != nil {
		return err
	}
	if snapshot.ID != job.UpstreamID {
		return errors.New("upstream batch identity mismatch")
	}
	if job.ChannelType == u.kinds.OpenRouter && len(snapshot.RequestCounts) != 0 && string(snapshot.RequestCounts) != "null" {
		var counts map[string]*int
		if err := u.codec.Unmarshal(snapshot.RequestCounts, &counts); err != nil {
			return errors.New("invalid upstream batch request counts")
		}
		var sum, finished int
		for key, value := range counts {
			if value == nil || *value < 0 || *value > job.RequestCount {
				return errors.New("upstream batch request counts mismatch")
			}
			if key == "completed" || key == "failed" || key == "pending" {
				sum += *value
			}
			if key == "completed" || key == "failed" {
				finished += *value
			}
		}
		if total, ok := counts["total"]; ok && *total != job.RequestCount {
			return errors.New("upstream batch request counts mismatch")
		}
		if sum > job.RequestCount || (snapshot.State == domain.Completed && finished != job.RequestCount) {
			return errors.New("upstream batch request counts mismatch")
		}
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		return err
	}
	decoder := u.codec.NewDecoder(staged)
	for {
		var result domain.Result
		err := decoder.Decode(&result)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if err := u.recordResult(job, result); err != nil {
			return err
		}
	}
	update := domain.PollUpdate{State: snapshot.State, OutputFileID: snapshot.OutputFileID,
		ErrorFileID: snapshot.ErrorFileID, Usage: string(snapshot.Usage)}
	if snapshot.State == domain.Failed && (job.Status != "failed" || job.LastError == "" ||
		(len(snapshot.Error) > 0 && string(snapshot.Error) != "null")) {
		job.LastError = u.gateway.Failure("poll", response.StatusCode, response.RequestID, snapshot.Error)
		update.LastError = &job.LastError
	}
	if err := u.store.SavePoll(job, runnerID, update); err != nil {
		return err
	}
	job.Status = string(snapshot.State)
	if !snapshot.State.Terminal() {
		return nil
	}
	var files []string
	switch job.ChannelType {
	case u.kinds.OpenAI:
		if snapshot.OutputFileID != "" {
			files = append(files, snapshot.OutputFileID)
		}
		if snapshot.ErrorFileID != "" {
			files = append(files, snapshot.ErrorFileID)
		}
	case u.kinds.Anthropic:
		files = append(files, "")
	case u.kinds.Google:
		if snapshot.OutputFileID != "" {
			files = append(files, snapshot.OutputFileID)
		}
	}
	for _, file := range files {
		request, err := u.gateway.ResultsRequest(job, file)
		if err != nil {
			return err
		}
		response, err := u.gateway.Request(ctx, job, request, nil, "")
		if err != nil {
			return err
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			return fmt.Errorf("batch results HTTP %d", response.StatusCode)
		}
		err = adapter.ReadResults(response.Body, func(result domain.Result) error { return u.recordResult(job, result) })
		response.Body.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (u Usecase) recordResult(job *domain.Job, result domain.Result) error {
	customID, err := u.store.LocalCustomID(job.ID, result.CustomID)
	if err != nil {
		return errors.New("provider returned an unknown Batch request identity")
	}
	result.CustomID = customID
	data, err := u.codec.Marshal(result)
	if err != nil {
		return err
	}
	if len(data) > domain.MaxResultBytes {
		return errors.New("normalized batch result exceeds storage limit")
	}
	var canonical any
	decoder := u.codec.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&canonical); err != nil {
		return err
	}
	hashData, err := u.codec.Marshal(canonical)
	if err != nil {
		return err
	}
	return u.store.StoreBatchResult(job.ID, result.CustomID, string(data), fmt.Sprintf("%x", sha256.Sum256(hashData)))
}

func (u Usecase) rejectUnsubmittedBatch(job *domain.Job, reason string) error {
	return domain.RejectUnsubmitted(u.store, u.diagnostics.Error, job, reason)
}

func (u Usecase) cleanupBatch(ctx context.Context, job *domain.Job, adapter domain.Provider) error {
	if !domain.State(job.Status).Terminal() || job.BillingStatus != "settled" || job.PurchaseStatus != "settled" {
		return domain.ErrBatchConflict
	}
	if job.UpstreamID != "" {
		request, err := adapter.Delete(job.UpstreamID)
		if err != nil && !errors.Is(err, domain.ErrUnsupported) {
			return err
		}
		if err == nil {
			response, err := u.gateway.Request(ctx, job, request, nil, "")
			if err != nil {
				return err
			}
			response.Body.Close()
			if response.StatusCode != 200 && response.StatusCode != 204 && response.StatusCode != 404 && response.StatusCode != 410 {
				return fmt.Errorf("batch cleanup HTTP %d", response.StatusCode)
			}
		}
	}
	return u.store.ClearResults(job)
}

func (u Usecase) recordAmbiguousCreation(job *domain.Job, runnerID, code string, status int, requestID string, body []byte) {
	diagnostic := u.gateway.AmbiguousFailure(code, status, requestID, body)
	changed, err := u.store.SaveUnknownDiagnostic(job, runnerID, diagnostic, true)
	if err != nil {
		u.diagnostics.Error(fmt.Sprintf("Batch %s: cannot store creation diagnostic: %v", job.PublicID, err))
		return
	}
	if changed {
		job.LastError = diagnostic
	}
}
