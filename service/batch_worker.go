package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/batch"
	"io"
	"os"
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
		if err != nil || job.Status != "failed" {
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
			if errors.Is(err, model.ErrBatchInsufficientQuota) {
				return rejectUnsubmittedBatch(job, "reservation unavailable")
			}
			return err
		}
		if err := submitBatch(ctx, job, adapter, runnerID); err != nil {
			return err
		}
	}
	if job.SubmitState == "submission_unknown" {
		return model.DB.Model(job).Update("billing_status", "reconciliation_required").Error
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
	var items []model.BatchItem
	if err := model.DB.Where("batch_id = ?", job.ID).Order("ordinal asc").Find(&items).Error; err != nil {
		return err
	}
	submission := batch.Submission{ID: job.PublicID, Model: job.UpstreamModel, Endpoint: job.Endpoint, InputFileID: job.InputFileID, Items: make([]batch.Item, len(items))}
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
	changed := model.DB.Model(job).Where("submit_state = ? AND lease_owner = ?", "preparing", runnerID).Update("submit_state", "submission_unknown")
	if changed.Error != nil {
		return changed.Error
	}
	if changed.RowsAffected != 1 {
		return model.ErrBatchConflict
	}
	job.SubmitState = "submission_unknown"
	response, err := doBatchRequest(ctx, job, request, nil, "")
	if err != nil {
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
			return rejectUnsubmittedBatch(job, fmt.Sprintf("creation HTTP %d", response.StatusCode))
		}
		return fmt.Errorf("ambiguous batch creation HTTP %d", response.StatusCode)
	}
	snapshot, decodeErr := adapter.ReadStatus(response.Body, func(result batch.Result) error { return storeBatchResult(job, result) })
	if snapshot.ID != "" {
		if _, err := adapter.Poll(snapshot.ID); err != nil {
			return err
		}
		state := snapshot.State
		if state == "" {
			state = batch.Validating
		}
		changed := model.DB.Model(job).Where("submit_state = ?", "submission_unknown").Updates(map[string]any{"upstream_id": snapshot.ID, "submit_state": "submitted", "status": state})
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
	if err := model.DB.Model(job).Where("lease_owner = ?", runnerID).Updates(map[string]any{"status": snapshot.State, "output_file_id": snapshot.OutputFileID, "error_file_id": snapshot.ErrorFileID, "upstream_usage": string(snapshot.Usage)}).Error; err != nil {
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
		return model.ErrBatchConflict
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
func RecoverBatchIdentity(ctx context.Context, job *model.BatchJob, upstreamID, runnerID string) error {
	if job.SubmitState != "submission_unknown" || job.UpstreamID != "" {
		return model.ErrBatchConflict
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
	verify := func(result batch.Result) error {
		var count int64
		if err := model.DB.Model(&model.BatchItem{}).Where("batch_id = ? AND provider_custom_id = ?", job.ID, result.CustomID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errors.New("recovery result does not belong to this Batch")
		}
		verified++
		return nil
	}
	snapshot, err := adapter.ReadStatus(response.Body, verify)
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
		return changed.Error
	}
	if changed.RowsAffected != 1 {
		return model.ErrBatchConflict
	}
	return nil
}
