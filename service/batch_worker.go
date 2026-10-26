package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/batch"
	"gorm.io/gorm"
	"io"
	"os"
	"regexp"
	"strconv"
	"time"
)

type batchPollHandler struct{}

func (batchPollHandler) Type() string            { return "provider_batch_poll" }
func (batchPollHandler) Enabled() bool           { return constant.UpdateTask }
func (batchPollHandler) Interval() time.Duration { return 15 * time.Second }
func (batchPollHandler) NewPayload() any         { return nil }
func (batchPollHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	if err := RunBatchPollingOnce(ctx, runnerID); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, nil, ""); err != nil {
		common.SysError("failed to finish Batch poll: " + err.Error())
	}
}
func init() { RegisterSystemTaskHandler(batchPollHandler{}) }

func RunBatchPollingOnce(ctx context.Context, runnerID string) error {
	if err := model.SyncBatchQuotaEvents(1000); err != nil {
		common.SysError("Batch outbox: " + err.Error())
	}
	if err := model.SyncBatchConsumeLogs(100); err != nil {
		common.SysError("Batch consume logs: " + err.Error())
	}
	var jobs []model.BatchJob
	if err := model.DB.Where("billing_status <> ? OR purchase_status <> ? OR status NOT IN ? OR (deleted_at > 0 AND cleanup_done = ?) OR (expires_at <= ? AND cleanup_done = ?)", "settled", "settled", []string{"completed", "failed", "expired", "cancelled"}, false, time.Now().Unix(), false).Order("updated_at asc").Limit(20).Find(&jobs).Error; err != nil {
		return err
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		claimed, err := model.ClaimBatchJob(job.ID, runnerID)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		if err := model.DB.Where("id = ? AND lease_owner = ?", job.ID, runnerID).First(&job).Error; err != nil {
			return err
		}
		err = processBatchJob(ctx, &job, runnerID)
		message := ""
		if err != nil {
			message = "batch requires retry or reconciliation"
			common.SysError(fmt.Sprintf("batch %s: %v", job.PublicID, err))
		}
		updates := map[string]any{"lease_until": 0, "lease_owner": "", "updated_at": time.Now().Unix()}
		// An unknown creation keeps its first normalized cause for the operator;
		// a rejected one keeps its reason, including an operator release marker.
		if job.SubmitState != "submission_unknown" && job.SubmitState != "rejected" && (job.Status != "failed" || (err != nil && job.LastError == "")) {
			updates["last_error"] = message
		}
		if err := model.DB.Model(&model.BatchJob{}).Where("id = ? AND lease_owner = ?", job.ID, runnerID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func processBatchJob(ctx context.Context, job *model.BatchJob, runnerID string) error {
	adapter, err := batch.ForChannel(job.ChannelType)
	if err != nil {
		return err
	}
	if job.DeletedAt == 0 && time.Now().Unix() >= job.ExpiresAt && batch.State(job.Status).Terminal() && job.BillingStatus == "settled" && job.PurchaseStatus == "settled" {
		if err := model.DB.Model(job).Update("deleted_at", time.Now().Unix()).Error; err != nil {
			return err
		}
		job.DeletedAt = time.Now().Unix()
	}
	if job.DeletedAt > 0 {
		return cleanupBatch(ctx, job, adapter)
	}
	if job.SubmitState == "queued" || job.SubmitState == "preparing" {
		if err := model.ConfirmBatchReservation(job); err != nil {
			if errors.Is(err, model.ErrBatchInsufficientQuota) || errors.Is(err, model.ErrBatchDispatchDenied) {
				return rejectUnsubmittedBatch(job, "reservation unavailable")
			}
			return err
		}
		if err := submitBatch(ctx, job, adapter, runnerID); err != nil {
			return err
		}
	}
	if job.SubmitState == "submission_unknown" {
		if err := model.DB.Model(job).Update("billing_status", "reconciliation_required").Error; err != nil {
			return err
		}
		job.BillingStatus = "reconciliation_required"
		if !batchSubmissionOverdue(job, time.Now()) {
			return nil
		}
		// Alert once; the hold stays until an operator recovers or releases it.
		var failure model.BatchFailure
		if common.UnmarshalJsonStr(job.LastError, &failure) != nil || failure.Version != 1 {
			failure = model.BatchFailure{Version: 1, Phase: "create", Code: "submission_unknown", Category: "ambiguous", Message: "The provider Batch creation outcome is unknown."}
		}
		if failure.AlertedAt != 0 {
			return nil
		}
		failure.AlertedAt = time.Now().Unix()
		encoded, err := common.Marshal(failure)
		if err != nil {
			return err
		}
		query := model.DB.Model(&model.BatchJob{}).Where("id = ? AND lease_owner = ?", job.ID, runnerID)
		if job.LastError == "" {
			query = query.Where("(last_error = '' OR last_error IS NULL)")
		} else {
			query = query.Where("last_error = ?", job.LastError)
		}
		changed := query.Update("last_error", string(encoded))
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected == 1 {
			job.LastError = string(encoded)
			common.SysError(fmt.Sprintf("Batch %s: creation outcome unknown >48h; hold %d kept; verify provider, then recover or release", job.PublicID, job.ReservedQuota))
		}
		return nil
	}
	if job.SubmitState == "rejected" {
		reason := job.LastError
		if reason == "" {
			reason = "creation rejected"
		}
		return rejectUnsubmittedBatch(job, reason)
	}
	if job.UpstreamID == "" {
		return nil
	}
	if job.SubmitState == "cancel_requested" {
		request, err := adapter.Cancel(job.UpstreamID)
		if err != nil {
			return err
		}
		response, err := doBatchRequest(ctx, job, request, nil, "")
		if err != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("batch cancellation HTTP %d", response.StatusCode)
		}
		if err := model.DB.Model(job).Where("submit_state = ?", "cancel_requested").Updates(map[string]any{"submit_state": "cancel_sent", "status": "cancelling"}).Error; err != nil {
			return err
		}
	}
	var pollErr error
	if time.Now().Unix() < job.ExpiresAt {
		pollErr = pollBatch(ctx, job, adapter, runnerID)
	}
	if err := SettleBatchResults(job); err != nil {
		return err
	}
	// The provider window is not a refund signal: the paid work may still
	// finish, so the hold stays and only the operator is asked to look.
	if batchProviderWindowExceeded(job, time.Now()) {
		changed := model.DB.Model(&model.BatchJob{}).Where("id = ? AND billing_status = ?", job.ID, "reserved").Update("billing_status", "reconciliation_required")
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected == 1 {
			job.BillingStatus = "reconciliation_required"
			common.SysError(fmt.Sprintf("Batch %s: provider window of 24h plus 2h grace exceeded while %s; hold %d kept for reconciliation", job.PublicID, job.Status, job.ReservedQuota))
		}
	}
	if err := model.SyncBatchQuotaEvents(1000); err != nil {
		common.SysError("Batch outbox: " + err.Error())
	}
	if err := model.SyncBatchConsumeLogs(100); err != nil {
		common.SysError("Batch consume logs: " + err.Error())
	}
	if err := ReconcileBatchPurchase(job); err != nil {
		return err
	}
	if err := model.ReleaseSettledBatchHolds(job.ID); err != nil {
		return err
	}
	if batch.State(job.Status).Terminal() {
		if err := model.DB.Model(job).Where("settled_count < request_count").Update("billing_status", "reconciliation_required").Error; err != nil {
			return err
		}
	}
	return pollErr
}

func submitBatch(ctx context.Context, job *model.BatchJob, adapter batch.Adapter, runnerID string) error {
	var contract BatchBillingSnapshot
	if err := common.UnmarshalJsonStr(job.BillingSnapshot, &contract); err != nil {
		return err
	}
	var items []model.BatchItem
	if err := model.DB.Where("batch_id = ?", job.ID).Order("ordinal asc").Find(&items).Error; err != nil {
		return err
	}
	submission := batch.Submission{ID: job.PublicID, Model: job.UpstreamModel, Endpoint: job.Endpoint, Provider: contract.Provider, InputFileID: job.InputFileID, Items: make([]batch.Item, len(items))}
	for i, item := range items {
		submission.Items[i] = batch.Item{CustomID: item.ProviderCustomID, Body: []byte(item.UpstreamBody)}
	}
	if job.SubmitState == "queued" {
		changed := model.DB.Model(job).Where("submit_state = ? AND lease_owner = ?", "queued", runnerID).Update("submit_state", "preparing")
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return model.ErrBatchConflict
		}
		job.SubmitState = "preparing"
	}
	if job.ChannelType == constant.ChannelTypeOpenAI && job.InputFileID == "" {
		id, err := uploadOpenAIBatchInput(ctx, job, submission)
		if err != nil {
			return err
		}
		changed := model.DB.Model(job).Where("lease_owner = ? AND submit_state = ?", runnerID, "preparing").Update("input_file_id", id)
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return model.ErrBatchConflict
		}
		job.InputFileID, submission.InputFileID = id, id
	}
	request, err := adapter.Create(submission)
	if err != nil {
		return rejectUnsubmittedBatch(job, "invalid native request")
	}
	if job.ChannelType == constant.ChannelTypeGemini && len(request.Body) > 20<<20 {
		return rejectUnsubmittedBatch(job, "Google inline request exceeds 20 MiB")
	}
	// Write the ambiguity marker BEFORE the first byte of the paid POST.
	// No timeout, expired lease or process restart authorizes another POST.
	if err := model.MarkBatchSubmissionUnknown(job, runnerID); err != nil {
		if errors.Is(err, model.ErrBatchInsufficientQuota) || errors.Is(err, model.ErrBatchDispatchDenied) {
			return rejectUnsubmittedBatch(job, "dispatch unavailable")
		}
		return err
	}
	job.SubmitState = "submission_unknown"
	response, err := doBatchRequest(ctx, job, request, nil, "")
	if err != nil {
		recordAmbiguousBatchCreation(job, runnerID, "transport_error", 0, "", nil)
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case 400, 401, 403, 404, 413, 422:
			if err := model.DB.Model(job).Where("submit_state = ?", "submission_unknown").Update("submit_state", "rejected").Error; err != nil {
				return err
			}
			job.SubmitState = "rejected"
			body, _ := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
			diagnostic := normalizedBatchFailure("create", response.StatusCode, response.Header.Get("X-Request-Id"), body)
			common.SysError(fmt.Sprintf("Batch %s creation rejected: %s", job.PublicID, diagnostic))
			return rejectUnsubmittedBatch(job, diagnostic)
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
		recordAmbiguousBatchCreation(job, runnerID, "", response.StatusCode, response.Header.Get("X-Request-Id"), body)
		return fmt.Errorf("ambiguous batch creation HTTP %d", response.StatusCode)
	}
	snapshot, decodeErr := adapter.ReadStatus(response.Body, func(result batch.Result) error { return storeBatchResult(job, result) })
	if snapshot.ID == "" {
		recordAmbiguousBatchCreation(job, runnerID, "missing_batch_id", response.StatusCode, response.Header.Get("X-Request-Id"), nil)
	} else {
		if _, err := adapter.Poll(snapshot.ID); err != nil {
			recordAmbiguousBatchCreation(job, runnerID, "invalid_batch_id", response.StatusCode, response.Header.Get("X-Request-Id"), nil)
			return err
		}
		state := snapshot.State
		if state == "" {
			state = batch.Validating
		}
		updates := map[string]any{"upstream_id": snapshot.ID, "submit_state": "submitted", "status": state}
		if state == batch.Failed {
			job.LastError = normalizedBatchFailure("create", response.StatusCode, response.Header.Get("X-Request-Id"), snapshot.Error)
			updates["last_error"] = job.LastError
		}
		changed := model.DB.Model(job).Where("submit_state = ?", "submission_unknown").Updates(updates)
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return model.ErrBatchConflict
		}
		job.UpstreamID, job.SubmitState, job.Status = snapshot.ID, "submitted", string(state)
	}
	return decodeErr
}

func pollBatch(ctx context.Context, job *model.BatchJob, adapter batch.Adapter, runnerID string) error {
	request, err := adapter.Poll(job.UpstreamID)
	if err != nil {
		return err
	}
	response, err := doBatchRequest(ctx, job, request, nil, "")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	// OpenRouter HTTP402 can describe incurred cost with output withheld.
	if response.StatusCode != 200 && !(job.ChannelType == constant.ChannelTypeOpenRouter && response.StatusCode == 402) {
		return fmt.Errorf("batch retrieval HTTP %d", response.StatusCode)
	}
	// Stage inline output on disk until its enclosing provider identity is verified.
	staged, err := os.CreateTemp("", "getapi-batch-results-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	snapshot, err := adapter.ReadStatus(response.Body, func(result batch.Result) error {
		data, err := common.Marshal(result)
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
	if job.ChannelType == constant.ChannelTypeOpenRouter && len(snapshot.RequestCounts) != 0 && string(snapshot.RequestCounts) != "null" {
		var counts map[string]*int
		if err := common.Unmarshal(snapshot.RequestCounts, &counts); err != nil {
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
		if sum > job.RequestCount || (snapshot.State == batch.Completed && finished != job.RequestCount) {
			return errors.New("upstream batch request counts mismatch")
		}
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		return err
	}
	decoder := common.NewJSONStreamDecoder(staged)
	for {
		var result batch.Result
		err := decoder.Decode(&result)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if err := storeBatchResult(job, result); err != nil {
			return err
		}
	}
	updates := map[string]any{"status": snapshot.State, "output_file_id": snapshot.OutputFileID, "error_file_id": snapshot.ErrorFileID, "upstream_usage": string(snapshot.Usage)}
	if snapshot.State == batch.Failed && (job.Status != "failed" || job.LastError == "" || (len(snapshot.Error) > 0 && string(snapshot.Error) != "null")) {
		job.LastError = normalizedBatchFailure("poll", response.StatusCode, response.Header.Get("X-Request-Id"), snapshot.Error)
		updates["last_error"] = job.LastError
	}
	if err := model.DB.Model(job).Where("lease_owner = ?", runnerID).Updates(updates).Error; err != nil {
		return err
	}
	job.Status = string(snapshot.State)
	if !snapshot.State.Terminal() {
		return nil
	}
	var files []string
	switch job.ChannelType {
	case constant.ChannelTypeOpenAI:
		if snapshot.OutputFileID != "" {
			files = append(files, snapshot.OutputFileID)
		}
		if snapshot.ErrorFileID != "" {
			files = append(files, snapshot.ErrorFileID)
		}
	case constant.ChannelTypeAnthropic:
		files = append(files, "")
	case constant.ChannelTypeGemini:
		if snapshot.OutputFileID != "" {
			files = append(files, snapshot.OutputFileID)
		}
	}
	for _, file := range files {
		request, err := batchResultsRequest(job, file)
		if err != nil {
			return err
		}
		response, err := doBatchRequest(ctx, job, request, nil, "")
		if err != nil {
			return err
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			return fmt.Errorf("batch results HTTP %d", response.StatusCode)
		}
		err = adapter.ReadResults(response.Body, func(result batch.Result) error { return storeBatchResult(job, result) })
		response.Body.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func storeBatchResult(job *model.BatchJob, result batch.Result) error {
	var item model.BatchItem
	if err := model.DB.Select("custom_id").Where("batch_id = ? AND provider_custom_id = ?", job.ID, result.CustomID).First(&item).Error; err != nil {
		return errors.New("provider returned an unknown Batch request identity")
	}
	result.CustomID = item.CustomID
	data, err := common.Marshal(result)
	if err != nil {
		return err
	}
	if len(data) > batch.MaxResultBytes {
		return errors.New("normalized batch result exceeds storage limit")
	}
	var canonical any
	decoder := common.NewJSONStreamDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&canonical); err != nil {
		return err
	}
	hashData, err := common.Marshal(canonical)
	if err != nil {
		return err
	}
	return model.StoreBatchResult(job.ID, result.CustomID, string(data), fmt.Sprintf("%x", sha256.Sum256(hashData)))
}

func rejectUnsubmittedBatch(job *model.BatchJob, reason string) error {
	if job.SubmitState != "queued" && job.SubmitState != "preparing" && job.SubmitState != "rejected" {
		return errors.New("cannot refund an ambiguous or submitted batch")
	}
	changed := model.DB.Model(job).Where("submit_state IN ? AND upstream_id = '' AND lease_owner = ?", []string{"queued", "preparing", "rejected"}, job.LeaseOwner).Updates(map[string]any{"submit_state": "rejected", "status": "failed", "last_error": reason})
	if changed.Error != nil {
		return changed.Error
	}
	if changed.RowsAffected != 1 {
		// MySQL without clientFoundRows reports 0 for an unchanged row, so a
		// retry after an interrupted refund must accept its own final state.
		var current model.BatchJob
		if err := model.DB.Select("submit_state", "status", "upstream_id", "last_error", "lease_owner").First(&current, job.ID).Error; err != nil {
			return err
		}
		if current.SubmitState != "rejected" || current.Status != "failed" || current.UpstreamID != "" || current.LastError != reason || current.LeaseOwner != job.LeaseOwner {
			return model.ErrBatchConflict
		}
	}
	job.Status, job.LastError = "failed", reason
	var items []model.BatchItem
	if err := model.DB.Select("custom_id").Where("batch_id = ? AND billing_status <> ?", job.ID, "settled").Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if err := model.SettleBatchItem(job.ID, item.CustomID, 0, `{"purchase_status":"unbilled","purchase_usd":0,"reason":"batch_not_submitted"}`); err != nil {
			return err
		}
	}
	if err := model.SyncBatchQuotaEvents(1000); err != nil {
		common.SysError("Batch outbox: " + err.Error())
	}
	return model.ReleaseSettledBatchHolds(job.ID)
}

func cleanupBatch(ctx context.Context, job *model.BatchJob, adapter batch.Adapter) error {
	if !batch.State(job.Status).Terminal() || job.BillingStatus != "settled" || job.PurchaseStatus != "settled" {
		return model.ErrBatchConflict
	}
	if job.UpstreamID != "" {
		request, err := adapter.Delete(job.UpstreamID)
		if err != nil && !errors.Is(err, batch.ErrUnsupported) {
			return err
		}
		if err == nil {
			response, err := doBatchRequest(ctx, job, request, nil, "")
			if err != nil {
				return err
			}
			response.Body.Close()
			if response.StatusCode != 200 && response.StatusCode != 204 && response.StatusCode != 404 && response.StatusCode != 410 {
				return fmt.Errorf("batch cleanup HTTP %d", response.StatusCode)
			}
		}
	}
	if err := model.DB.Model(&model.BatchItem{}).Where("batch_id = ?", job.ID).Updates(map[string]any{"result": "", "body": "", "upstream_body": ""}).Error; err != nil {
		return err
	}
	return model.DB.Model(job).Update("cleanup_done", true).Error
}

// RecoverBatchIdentity binds an ambiguous submission only after the provider
// returns private request identities generated for this particular local job.
// The administrator supplies an identity to verify, never authority to resend.
// The caller must hold the job lease as runnerID.
func RecoverBatchIdentity(ctx context.Context, job *model.BatchJob, upstreamID, runnerID string) error {
	if !BatchRecoverEligible(job) {
		return ErrBatchRecoverIneligible
	}
	adapter, err := batch.ForChannel(job.ChannelType)
	if err != nil {
		return err
	}
	request, err := adapter.Poll(upstreamID)
	if err != nil {
		return err
	}
	candidate := *job
	candidate.UpstreamID = upstreamID
	response, err := doBatchRequest(ctx, &candidate, request, nil, "")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("recovery retrieval HTTP %d", response.StatusCode)
	}
	verified := 0
	// Adapters may not wrap callback errors, so a storage failure is kept
	// aside to stay distinguishable from an unverified provider identity.
	var storageErr error
	verify := func(result batch.Result) error {
		var count int64
		if err := model.DB.Model(&model.BatchItem{}).Where("batch_id = ? AND provider_custom_id = ?", job.ID, result.CustomID).Count(&count).Error; err != nil {
			storageErr = err
			return err
		}
		if count != 1 {
			return errors.New("recovery result does not belong to this Batch")
		}
		verified++
		return nil
	}
	snapshot, err := adapter.ReadStatus(response.Body, verify)
	if storageErr != nil {
		return fmt.Errorf("%w: %v", ErrBatchRecoveryStorage, storageErr)
	}
	if err != nil {
		return err
	}
	if snapshot.ID != upstreamID {
		return errors.New("recovery batch identity mismatch")
	}
	if verified == 0 && snapshot.State.Terminal() {
		var files []string
		switch job.ChannelType {
		case constant.ChannelTypeAnthropic:
			files = []string{""}
		case constant.ChannelTypeOpenAI:
			if snapshot.OutputFileID != "" {
				files = append(files, snapshot.OutputFileID)
			}
			if snapshot.ErrorFileID != "" {
				files = append(files, snapshot.ErrorFileID)
			}
		case constant.ChannelTypeGemini:
			if snapshot.OutputFileID != "" {
				files = []string{snapshot.OutputFileID}
			}
		}
		for _, file := range files {
			request, err := batchResultsRequest(&candidate, file)
			if err != nil {
				return err
			}
			response, err := doBatchRequest(ctx, &candidate, request, nil, "")
			if err != nil {
				return err
			}
			if response.StatusCode != 200 {
				response.Body.Close()
				return fmt.Errorf("recovery results HTTP %d", response.StatusCode)
			}
			err = adapter.ReadResults(response.Body, verify)
			response.Body.Close()
			if storageErr != nil {
				return fmt.Errorf("%w: %v", ErrBatchRecoveryStorage, storageErr)
			}
			if err != nil {
				return err
			}
		}
	}
	if verified == 0 {
		return errors.New("provider has not returned request identities proving ownership; keep submission ambiguous")
	}
	changed := model.DB.Model(job).Where("submit_state = ? AND upstream_id = '' AND lease_owner = ?", "submission_unknown", runnerID).Updates(map[string]any{"upstream_id": upstreamID, "submit_state": "submitted", "status": string(snapshot.State), "billing_status": "reserved", "last_error": ""})
	if changed.Error != nil {
		return fmt.Errorf("%w: %v", ErrBatchRecoveryStorage, changed.Error)
	}
	if changed.RowsAffected != 1 {
		return model.ErrBatchConflict
	}
	return nil
}

var (
	ErrBatchRecoverIneligible = errors.New("batch is not eligible for identity recovery")
	ErrBatchRecoveryStorage   = errors.New("batch storage unavailable during identity recovery")
)

// BatchRecoverEligible is true when an operator may bind a provider Batch
// identity: the creation outcome is unknown, no identity is bound yet and the
// channel has a Batch adapter to verify ownership with.
func BatchRecoverEligible(job *model.BatchJob) bool {
	if job.SubmitState != "submission_unknown" || job.UpstreamID != "" || job.DeletedAt != 0 {
		return false
	}
	_, err := batch.ForChannel(job.ChannelType)
	return err == nil
}

const (
	batchProviderWindow    = 24 * time.Hour
	batchProviderGrace     = 2 * time.Hour
	batchUnknownAlertAfter = 48 * time.Hour
)

// batchSubmissionOverdue reports an unknown creation old enough that the
// provider would have finished it; only an operator may recover or release it.
func batchSubmissionOverdue(job *model.BatchJob, now time.Time) bool {
	return job.SubmitState == "submission_unknown" && now.Unix()-job.CreatedAt >= int64(batchUnknownAlertAfter/time.Second)
}

// batchProviderWindowExceeded reports a submitted Batch still not terminal
// after the provider completion window plus a grace period. Only adapters that
// request completion_window "24h" have that window. The clock starts at
// CreatedAt: no recovery timestamp is stored, so a Batch recovered late is
// flagged on its first tick after recovery.
func batchProviderWindowExceeded(job *model.BatchJob, now time.Time) bool {
	if job.ChannelType != constant.ChannelTypeOpenAI && job.ChannelType != constant.ChannelTypeOpenRouter {
		return false
	}
	return job.UpstreamID != "" && !batch.State(job.Status).Terminal() && now.Unix() >= job.CreatedAt+int64((batchProviderWindow+batchProviderGrace)/time.Second)
}

var ambiguousBatchCreationMessages = map[string]string{
	"transport_error":  "The Batch creation request ended without a provider response.",
	"missing_batch_id": "The provider accepted Batch creation without a Batch identity.",
	"invalid_batch_id": "The provider returned an invalid Batch identity.",
}

// recordAmbiguousBatchCreation keeps the first normalized cause of an unknown
// creation outcome. An empty code classifies the provider response body.
func recordAmbiguousBatchCreation(job *model.BatchJob, runnerID, code string, status int, requestID string, body []byte) {
	diagnostic := normalizedBatchFailure("create", status, requestID, body)
	if message, ok := ambiguousBatchCreationMessages[code]; ok {
		failure := model.BatchFailure{Version: 1, Phase: "create", Code: code, Category: "ambiguous", Message: message, HTTPStatus: status}
		if batchProviderRequestID.MatchString(requestID) {
			failure.RequestID = requestID
		}
		encoded, err := common.Marshal(failure)
		if err != nil {
			common.SysError(fmt.Sprintf("Batch %s: cannot encode creation diagnostic: %v", job.PublicID, err))
			return
		}
		diagnostic = string(encoded)
	}
	changed := model.DB.Model(&model.BatchJob{}).Where("id = ? AND submit_state = ? AND lease_owner = ? AND (last_error = '' OR last_error IS NULL)", job.ID, "submission_unknown", runnerID).Update("last_error", diagnostic)
	if changed.Error != nil {
		common.SysError(fmt.Sprintf("Batch %s: cannot store creation diagnostic: %v", job.PublicID, changed.Error))
		return
	}
	if changed.RowsAffected == 1 {
		job.LastError = diagnostic
	}
}

// BatchReconciliationReason classifies why a Batch needs an operator. The
// worker alerts on the same predicates.
func BatchReconciliationReason(job *model.BatchJob, now time.Time) string {
	if job.SubmitState == "submission_unknown" {
		if batchSubmissionOverdue(job, now) {
			return "submission_unknown_overdue"
		}
		return "submission_unknown"
	}
	if job.BillingStatus != "settled" {
		if batchProviderWindowExceeded(job, now) {
			return "provider_window_exceeded"
		}
		var snapshot struct {
			Mode string `json:"settlement_mode"`
		}
		if batch.State(job.Status).Terminal() && job.FinancialDetails == "" && common.UnmarshalJsonStr(job.BillingSnapshot, &snapshot) == nil && snapshot.Mode == BatchSettlementOpenRouterJobCostV2 {
			return "terminal_without_cost"
		}
		return "unsettled_items"
	}
	return "purchase_unconfirmed"
}

// BatchReleaseEligible is true only when no provider Batch identity, result or
// charge exists, so returning the hold cannot forgive delivered work.
func BatchReleaseEligible(job *model.BatchJob) (bool, error) {
	if job.SubmitState != "submission_unknown" || job.UpstreamID != "" || job.DeletedAt != 0 || batch.State(job.Status).Terminal() || job.ResultCount != 0 || job.SettledCount != 0 || job.ChargedQuota != 0 || job.FinancialDetails != "" || job.PurchaseStatus == "settled" {
		return false, nil
	}
	var touched int64
	if err := model.DB.Model(&model.BatchItem{}).Where("batch_id = ? AND (result_hash <> '' OR billing_status IN ?)", job.ID, []string{"settled", "settled_at_batch"}).Count(&touched).Error; err != nil {
		return false, err
	}
	return touched == 0, nil
}

var ErrBatchReleaseIneligible = errors.New("batch is not eligible for operator release")

type BatchReleaseResult struct {
	AlreadyReleased   bool
	Pending           bool
	ReleasedQuota     int64
	PreviousLastError string
}

const batchOperatorReleasedCode = "operator_released_unsubmitted"

// ReleaseUnsubmittedBatch returns the hold of an ambiguous creation after an
// operator confirms that the provider has no Batch for it. The caller must
// hold the job lease as runnerID; job is reloaded in place. Money moves only
// through the existing unsubmitted-rejection path.
func ReleaseUnsubmittedBatch(job *model.BatchJob, runnerID string, operatorID int) (result BatchReleaseResult, err error) {
	defer func() {
		outcome := "released"
		switch {
		case err != nil:
			outcome = "rejected: " + err.Error()
		case result.Pending:
			outcome = "release pending cache confirmation"
		case result.AlreadyReleased:
			outcome = "already released"
		}
		common.SysLog(fmt.Sprintf("Batch %s: root user %d unsubmitted hold release %s; released quota %d", job.PublicID, operatorID, outcome, result.ReleasedQuota))
	}()
	if err = model.DB.Where("id = ? AND lease_owner = ?", job.ID, runnerID).First(job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = model.ErrBatchConflict
		}
		return result, err
	}
	result.PreviousLastError = job.LastError
	var previous model.BatchFailure
	if job.SubmitState == "rejected" && job.UpstreamID == "" && common.UnmarshalJsonStr(job.LastError, &previous) == nil && previous.Code == batchOperatorReleasedCode {
		result.AlreadyReleased = true
		if job.BillingStatus != "settled" {
			if err = rejectUnsubmittedBatch(job, job.LastError); err != nil {
				return result, err
			}
			if err = model.DB.First(job, job.ID).Error; err != nil {
				return result, err
			}
			result.Pending = job.BillingStatus != "settled" || job.ReservedQuota != 0
		}
		return result, nil
	}
	eligible, err := BatchReleaseEligible(job)
	if err != nil {
		return result, err
	}
	if !eligible {
		return result, ErrBatchReleaseIneligible
	}
	held := job.ReservedQuota
	encoded, err := common.Marshal(model.BatchFailure{Version: 1, Phase: "reconciliation", Code: batchOperatorReleasedCode, Message: "Operator confirmed no provider Batch exists; hold released."})
	if err != nil {
		return result, err
	}
	changed := model.DB.Model(&model.BatchJob{}).Where("id = ? AND submit_state = ? AND upstream_id = '' AND result_count = 0 AND lease_owner = ?", job.ID, "submission_unknown", runnerID).Updates(map[string]any{"submit_state": "rejected", "last_error": string(encoded)})
	if changed.Error != nil {
		return result, changed.Error
	}
	if changed.RowsAffected != 1 {
		return result, model.ErrBatchConflict
	}
	job.SubmitState, job.LastError = "rejected", string(encoded)
	if err = rejectUnsubmittedBatch(job, job.LastError); err != nil {
		return result, err
	}
	if err = model.DB.First(job, job.ID).Error; err != nil {
		return result, err
	}
	if job.BillingStatus != "settled" || job.ReservedQuota != 0 {
		result.Pending = true
		return result, nil
	}
	result.ReleasedQuota = held
	return result, nil
}

type BatchReconciliationEntry struct {
	ID             string              `json:"id"`
	UserID         int                 `json:"user_id"`
	TokenID        int                 `json:"token_id"`
	ChannelID      int                 `json:"channel_id"`
	Model          string              `json:"model"`
	UpstreamModel  string              `json:"upstream_model"`
	Status         string              `json:"status"`
	SubmitState    string              `json:"submit_state"`
	BillingStatus  string              `json:"billing_status"`
	PurchaseStatus string              `json:"purchase_status"`
	UpstreamID     string              `json:"upstream_id"`
	RequestCount   int                 `json:"request_count"`
	ResultCount    int                 `json:"result_count"`
	SettledCount   int                 `json:"settled_count"`
	ReservedQuota  int64               `json:"reserved_quota"`
	ChargedQuota   int64               `json:"charged_quota"`
	CreatedAt      int64               `json:"created_at"`
	UpdatedAt      int64               `json:"updated_at"`
	Diagnostic     *model.BatchFailure `json:"diagnostic,omitempty"`
	Reason         string              `json:"reason"`
	CanRelease     bool                `json:"can_release"`
	CanRecover     bool                `json:"can_recover"`
}

// ListBatchReconciliation returns the operator view after the Batch with
// public id before, newest first.
func ListBatchReconciliation(before string, limit int) ([]BatchReconciliationEntry, bool, error) {
	var cursor int64
	if before != "" {
		var job model.BatchJob
		if err := model.DB.Select("id").Where("public_id = ? AND deleted_at = 0", before).First(&job).Error; err != nil {
			return nil, false, err
		}
		cursor = job.ID
	}
	jobs, err := model.ListBatchReconciliationJobs(cursor, limit+1)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(jobs) > limit
	if hasMore {
		jobs = jobs[:limit]
	}
	now := time.Now()
	entries := make([]BatchReconciliationEntry, len(jobs))
	for i := range jobs {
		job := &jobs[i]
		canRelease, err := BatchReleaseEligible(job)
		if err != nil {
			return nil, false, err
		}
		entries[i] = BatchReconciliationEntry{ID: job.PublicID, UserID: job.UserID, TokenID: job.TokenID, ChannelID: job.ChannelID, Model: job.Model, UpstreamModel: job.UpstreamModel, Status: job.Status, SubmitState: job.SubmitState, BillingStatus: job.BillingStatus, PurchaseStatus: job.PurchaseStatus, UpstreamID: job.UpstreamID, RequestCount: job.RequestCount, ResultCount: job.ResultCount, SettledCount: job.SettledCount, ReservedQuota: job.ReservedQuota, ChargedQuota: job.ChargedQuota, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, Reason: BatchReconciliationReason(job, now), CanRelease: canRelease, CanRecover: BatchRecoverEligible(job)}
		var diagnostic model.BatchFailure
		if len(job.LastError) <= 4096 && common.UnmarshalJsonStr(job.LastError, &diagnostic) == nil && diagnostic.Version == 1 {
			entries[i].Diagnostic = &diagnostic
		}
	}
	return entries, hasMore, nil
}

var batchProviderRequestID = regexp.MustCompile(`^(req[-_][0-9a-fA-F-]{1,120}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

// Match the entire known rejection, never arbitrary substrings of provider text.
// The model identifier is used only to recognize the template; it is not stored.
var batchMissingEndpoint = regexp.MustCompile(`^Model '[a-zA-Z0-9_.:/-]{1,191}' does not have a :batch endpoint\.$`)

// normalizedBatchFailure keeps an allowlisted classification rather than trying
// to redact arbitrary provider text, which can contain credentials or prompts.
func normalizedBatchFailure(phase string, status int, requestID string, body []byte) string {
	diagnostic := model.BatchFailure{Version: 1, Phase: phase, Code: "provider_error", Category: "unknown", Message: "The provider could not process the Batch operation.", HTTPStatus: status}
	if batchProviderRequestID.MatchString(requestID) {
		diagnostic.RequestID = requestID
	}
	if len(body) <= 16<<10 {
		type providerFailure struct {
			Code     json.RawMessage `json:"code"`
			Type     string          `json:"type"`
			Status   string          `json:"status"`
			Message  string          `json:"message"`
			Metadata struct {
				ErrorType string `json:"error_type"`
			} `json:"metadata"`
			Error json.RawMessage `json:"error"`
		}
		var native providerFailure
		err := common.Unmarshal(body, &native)
		if err == nil && len(native.Error) > 0 && string(native.Error) != "null" {
			nested := native.Error
			native = providerFailure{}
			err = common.Unmarshal(nested, &native)
		}
		if err == nil {
			var code string
			if common.Unmarshal(native.Code, &code) != nil {
				var numericCode int
				if common.Unmarshal(native.Code, &numericCode) == nil && numericCode >= 100 && numericCode <= 599 {
					diagnostic.Code = strconv.Itoa(numericCode)
				}
			}
			// Prefer the typed OpenRouter reason over a generic protocol code.
			// Anthropic uses type; Google uses status. No raw metadata is retained.
			for _, candidate := range []string{native.Metadata.ErrorType, code, native.Type, native.Status} {
				switch candidate {
				case "invalid_request", "invalid_request_error", "invalid_argument", "INVALID_ARGUMENT", "context_length_exceeded":
					diagnostic.Category = "invalid_request"
					diagnostic.Message = "The provider rejected the Batch request parameters."
				case "model_not_found", "no_endpoints_found":
					diagnostic.Category = "endpoint_unavailable"
					diagnostic.Message = "The provider has no eligible endpoint for this Batch request."
				case "rate_limit_exceeded", "rate_limit_error", "RESOURCE_EXHAUSTED":
					diagnostic.Category = "rate_limit"
					diagnostic.Message = "The provider rejected the Batch request because of a resource or rate limit."
				case "provider_overloaded", "overloaded_error":
					diagnostic.Category = "capacity"
					diagnostic.Message = "The provider reported insufficient capacity for the Batch request."
				case "authentication", "permission_denied", "authentication_error", "permission_error", "PERMISSION_DENIED", "UNAUTHENTICATED":
					diagnostic.Category = "access"
					diagnostic.Message = "The provider rejected access to the Batch operation."
				case "batch_expired":
					diagnostic.Category = "expired"
					diagnostic.Message = "The provider Batch expired."
				default:
					continue
				}
				diagnostic.Code = candidate
				break
			}
			if diagnostic.Category == "unknown" && batchMissingEndpoint.MatchString(native.Message) {
				diagnostic.Code = "batch_endpoint_unavailable"
				diagnostic.Category = "endpoint_unavailable"
				diagnostic.Message = "The provider reported that the requested model has no Batch endpoint."
			}
		}
	}
	encoded, _ := common.Marshal(diagnostic)
	return string(encoded)
}
