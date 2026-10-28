package poll_tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

const (
	pollClassOK           = "ok"
	pollClassOtherClient  = "other_client"
	pollClassNotFound     = "not_found"
	pollClassAuth         = "auth"
	pollClassTransient    = "transient"
	pollClassUnrecognized = "unrecognized"
	pollClassHookError    = "hook_error"
	pollClassTransport    = "transport_error"
)

// sweepTimedOutTasks expires overdue tasks before ordinary polling.
// Each tick processes at most 100 rows; later ticks process the remainder.
// Per-task status claims avoid overwriting progress from another poller.
func (u *Usecase) sweepTimedOutTasks(ctx context.Context) {
	if u.limits.TimeoutMinutes() <= 0 {
		return
	}
	cutoff := u.now().Unix() - int64(u.limits.TimeoutMinutes())*60
	tasks := u.store.TimedOut(cutoff, 100)
	if len(tasks) == 0 {
		return
	}

	now := u.now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
		reason := u.texts.Timeout(task.UserId, u.limits.TimeoutMinutes())
		if task.PrivateData.OpenRouterSyncState() != nil {
			continue
		}
		if reconciliation := task.PrivateData.Reconciliation; reconciliation != nil {
			reconciliation.Required = true
			reconciliation.HasUpstreamCost = false
			reconciliation.UpstreamCostConfirmed = false
		}
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < domain.TaskRefundLegacyCutoff

		oldStatus := task.Status
		task.Status = domain.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = now
		if isLegacy {
			task.FailReason = u.texts.LegacyTimeout(task.UserId)
			// Legacy tasks are not refundable; clear the quota in the terminal claim
			// so their billing marker cannot authorize another refund.
			task.Quota = 0
		} else {
			task.FailReason = reason
		}

		won, err := u.store.Transition(task, oldStatus)
		if err != nil {
			u.diagnostics.Error(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			u.diagnostics.Info(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
		if !isLegacy && task.Quota != 0 {
			u.completion.Refund(ctx, task, reason)
		}
	}

	if timedOutCount > 0 {
		u.diagnostics.Info(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
}

// TaskPollSummary is the result recorded on an async_task_poll system task row,
// summarizing one polling pass.
type TaskPollSummary struct {
	UnfinishedTasks  int `json:"unfinished_tasks"`
	PlatformsScanned int `json:"platforms_scanned"`
	NullTasksFailed  int `json:"null_tasks_failed"`
}

// Execute performs one lease-cancellable async task polling pass.
func (u *Usecase) Execute(ctx context.Context, report func(processed, total int)) (TaskPollSummary, error) {
	summary := TaskPollSummary{}
	if ctx == nil {
		ctx = context.Background()
	}

	u.diagnostics.SysLog("Task progress polling started")
	u.sweepTimedOutTasks(ctx)
	var allTasks []*domain.Task
	var audioTasks []*domain.Task
	audioLimit := 16
	if u.limits.QueryLimit() > 0 {
		audioLimit = min(audioLimit, u.limits.QueryLimit())
	}
	now := u.now().Unix()
	var afterID int64
	for ctx.Err() == nil {
		page := u.store.UnfinishedPage(u.limits.QueryLimit(), afterID)
		if len(page) == 0 {
			break
		}
		for _, task := range page {
			afterID = task.ID
			if audio := task.PrivateData.OpenRouterSyncState(); audio != nil {
				reconciliation := task.PrivateData.Reconciliation
				activeSubmission := reconciliation != nil && (reconciliation.ReservationPending || reconciliation.SubmissionPending)
				due := now >= audio.NextPoll || now >= audio.Deadline || audio.Attempts >= 16
				if activeSubmission {
					due = now >= audio.LeaseUntil
				}
				if due && len(audioTasks) < audioLimit {
					audioTasks = append(audioTasks, task)
				}
				continue
			}
			if u.factory == nil {
				continue
			}
			if reconciliation := task.PrivateData.Reconciliation; reconciliation != nil &&
				(reconciliation.ReservationPending || reconciliation.SubmissionPending) {
				continue
			}
			allTasks = append(allTasks, task)
			if u.limits.QueryLimit() > 0 && len(allTasks) >= u.limits.QueryLimit() {
				break
			}
		}
		if u.limits.QueryLimit() < 0 || (u.limits.QueryLimit() > 0 && (len(allTasks) >= u.limits.QueryLimit() || len(page) < u.limits.QueryLimit())) {
			break
		}
	}
	summary.UnfinishedTasks = len(allTasks) + len(audioTasks)
	platformTask := make(map[string][]*domain.Task)
	for _, t := range allTasks {
		platformTask[t.Platform] = append(platformTask[t.Platform], t)
	}

	totalPlatforms := len(platformTask)
	processedPlatforms := 0
	for platform, tasks := range platformTask {
		if ctx.Err() != nil {
			break
		}
		if report != nil {
			report(processedPlatforms, totalPlatforms)
		}
		processedPlatforms++
		if len(tasks) == 0 {
			continue
		}
		summary.PlatformsScanned++
		taskChannelM := make(map[int][]string)
		taskM := make(map[string]*domain.Task)
		nullTaskIds := make([]int64, 0)
		for _, task := range tasks {
			upstreamID := task.GetUpstreamTaskID()
			if upstreamID == "" {
				// Collect unfinished rows with no upstream identity.
				nullTaskIds = append(nullTaskIds, task.ID)
				continue
			}
			taskM[upstreamID] = task
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], upstreamID)
		}
		if len(nullTaskIds) > 0 {
			summary.NullTasksFailed += len(nullTaskIds)
			err := u.store.BulkUpdate(nullTaskIds, map[string]any{
				"status":   "FAILURE",
				"progress": "100%",
			})
			if err != nil {
				u.diagnostics.Error(ctx, fmt.Sprintf("Fix null task_id task error: %v", err))
			} else {
				u.diagnostics.Info(ctx, fmt.Sprintf("Fix null task_id task success: %v", nullTaskIds))
			}
		}
		if len(taskChannelM) == 0 {
			continue
		}

		u.dispatchPlatformUpdate(ctx, platform, taskChannelM, taskM)
	}
	// Inventory never waits on metadata. Existing platforms finish first;
	// at most sixteen due audio tasks use four bounded GET workers per pass.
	if len(audioTasks) > 0 {
		summary.PlatformsScanned++
		jobs := make(chan *domain.Task, len(audioTasks))
		for _, task := range audioTasks {
			jobs <- task
		}
		close(jobs)
		var workers sync.WaitGroup
		for range min(4, len(audioTasks)) {
			workers.Go(func() {
				for task := range jobs {
					if ctx.Err() != nil {
						return
					}
					if err := u.reconciler.Poll(ctx, task); err != nil {
						u.diagnostics.Warn(ctx, fmt.Sprintf("OpenRouter audio task %s reconciliation deferred", task.TaskID))
					}
				}
			})
		}
		workers.Wait()
	}
	if report != nil && ctx.Err() == nil {
		report(totalPlatforms, totalPlatforms)
	}
	u.diagnostics.SysLog("Task progress polling completed")
	return summary, ctx.Err()
}

// dispatchPlatformUpdate selects the existing grouped or per-task polling mode.
func (u *Usecase) dispatchPlatformUpdate(ctx context.Context, platform string, taskChannelM map[int][]string, taskM map[string]*domain.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	if platform == "mj" {
		// Midjourney has its own polling scenario.
		return
	}
	adaptor := u.factory.Open(platform)
	if batchAdaptor, ok := adaptor.(BatchAdaptor); ok && batchAdaptor.FetchMode() == "batch" {
		if err := u.updateBatchChannels(ctx, batchAdaptor, taskChannelM, taskM); err != nil {
			u.diagnostics.SysLog(fmt.Sprintf("UpdateBatchTasks fail: %s", err))
		}
		return
	}
	if err := u.updateVideoChannels(ctx, platform, taskChannelM, taskM); err != nil {
		u.diagnostics.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
	}
}

func (u *Usecase) updateBatchChannels(ctx context.Context, adaptor BatchAdaptor, taskChannelM map[int][]string, taskM map[string]*domain.Task) error {
	for channelId, taskIds := range taskChannelM {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := u.updateBatchTasks(ctx, adaptor, channelId, taskIds, taskM)
		if err != nil {
			u.diagnostics.Error(ctx, fmt.Sprintf("Channel #%d failed to update async tasks: %s", channelId, err.Error()))
		}
	}
	return nil
}

func (u *Usecase) updateBatchTasks(ctx context.Context, adaptor BatchAdaptor, channelId int, taskIds []string, taskM map[string]*domain.Task) error {
	u.diagnostics.Info(ctx, fmt.Sprintf("Channel #%d unfinished tasks: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	ch, err := u.channels.Get(channelId)
	if err != nil {
		u.diagnostics.SysLog(fmt.Sprintf("CacheGetChannel: %v", err))
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		err = u.store.BulkUpdate(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("Failed to get channel information; contact the administrator, channel ID: %d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if err != nil {
			u.diagnostics.SysLog(fmt.Sprintf("UpdateSunoTask error: %v", err))
		}
		return err
	}
	proxy := ch.Proxy
	baseURL := ch.BaseURL
	if baseURL == "" {
		baseURL = ch.FallbackBaseURL
	}
	tasks := make([]*domain.Task, 0, len(taskIds))
	for _, upstreamID := range taskIds {
		if task := taskM[upstreamID]; task != nil {
			tasks = append(tasks, task)
		}
	}
	adaptor.Init(baseURL, ch.Key)
	resp, err := adaptor.FetchBatchTasks(baseURL, ch.Key, tasks, proxy)
	if err != nil {
		u.diagnostics.SysLog(fmt.Sprintf("Get Task Do req error: %v", err))
		return u.recordPollFailureForTasks(ctx, adaptor, tasks, pollClassTransport, 0, err.Error())
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		u.diagnostics.SysLog(fmt.Sprintf("Get Suno Task parse body error: %v", err))
		return u.recordPollFailureForTasks(ctx, adaptor, tasks, pollClassTransport, resp.StatusCode, err.Error())
	}
	switch classifyPollHTTP(resp.StatusCode) {
	case pollClassNotFound:
		return u.failTasksFromPoll(ctx, adaptor, tasks, fmt.Sprintf("upstream task not found (HTTP %d)", resp.StatusCode))
	case pollClassAuth:
		u.diagnostics.Warn(ctx, fmt.Sprintf("task poll auth failure channel_id=%d http=%d", channelId, resp.StatusCode))
		return u.recordPollFailureForTasks(ctx, adaptor, tasks, pollClassAuth, resp.StatusCode, "")
	case pollClassTransient:
		return u.recordPollFailureForTasks(ctx, adaptor, tasks, pollClassTransient, resp.StatusCode, "")
	}
	responseItems, err := adaptor.ParseBatchResult(tasks, resp, responseBody)
	if err != nil {
		return u.recordPollFailureForTasks(ctx, adaptor, tasks, pollClassHookError, resp.StatusCode, err.Error())
	}
	for upstreamID, responseItem := range responseItems {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		task := taskM[upstreamID]
		if task == nil {
			u.diagnostics.Warn(ctx, fmt.Sprintf("Batch task response ignored: unknown task_id=%s", upstreamID))
			continue
		}
		snap := task.Snapshot()
		httpClass := classifyPollHTTP(resp.StatusCode)
		parsedStatus := domain.TaskStatus(responseItem.TaskInfo.Status)
		if parsedStatus == domain.TaskStatusUnknown || parsedStatus == "" || !knownPollStatus(parsedStatus) {
			if err := u.recordPollFailure(ctx, adaptor, task, snap.Status, pollClassUnrecognized, resp.StatusCode, responseItem.TaskInfo.Reason); err != nil {
				u.diagnostics.SysLog("UpdateSunoTask task error: " + err.Error())
			}
			continue
		}
		if httpClass == pollClassOtherClient && isNonTerminalPollStatus(parsedStatus) {
			if err := u.recordPollFailure(ctx, adaptor, task, snap.Status, pollClassUnrecognized, resp.StatusCode, responseItem.TaskInfo.Reason); err != nil {
				u.diagnostics.SysLog("UpdateSunoTask task error: " + err.Error())
			}
			continue
		}
		if isNonTerminalPollStatus(parsedStatus) {
			task.PrivateData.PollFailures = 0
		}
		if len(responseItem.TaskInfo.PluginState) > 0 {
			task.PrivateData.PluginState = responseItem.TaskInfo.PluginState
		}
		if parsedStatus != "" {
			task.Status = parsedStatus
		}
		if responseItem.TaskInfo.Reason != "" {
			task.FailReason = responseItem.TaskInfo.Reason
		}
		if responseItem.SubmitTime != 0 {
			task.SubmitTime = responseItem.SubmitTime
		}
		if responseItem.StartTime != 0 {
			task.StartTime = responseItem.StartTime
		}
		if responseItem.FinishTime != 0 {
			task.FinishTime = responseItem.FinishTime
		}
		if responseItem.TaskInfo.Progress != "" {
			task.Progress = responseItem.TaskInfo.Progress
		}
		if responseItem.TaskInfo.Reason != "" || task.Status == domain.TaskStatusFailure {
			u.diagnostics.Info(ctx, task.TaskID+" build failed: "+task.FailReason)
			task.Status = domain.TaskStatusFailure
			task.Progress = "100%"
		}
		if responseItem.TaskInfo.Status == domain.TaskStatusSuccess {
			task.Progress = "100%"
		}
		if responseItem.Data != nil {
			task.Data, _ = u.codec.Marshal(responseItem.Data)
		} else if task.Status == domain.TaskStatusSuccess || task.Status == domain.TaskStatusFailure {
			u.diagnostics.Warn(ctx, fmt.Sprintf(
				"Batch task %s reached terminal status without data; preserving existing task data",
				task.TaskID,
			))
		}
		if responseItem.TaskInfo.Url != "" {
			task.PrivateData.ResultURL = responseItem.TaskInfo.Url
		}

		isDone := task.Status == domain.TaskStatusSuccess || task.Status == domain.TaskStatusFailure
		if isDone && task.PrivateData.BillingContext != nil &&
			(task.PrivateData.BillingContext.SettlementMode == domain.TaskSettlementOpenRouterCostV1 ||
				task.PrivateData.BillingContext.SettlementMode == domain.TaskSettlementOpenRouterCostV2) {
			if snap.Status == domain.TaskStatusSuccess || snap.Status == domain.TaskStatusFailure {
				continue
			}
			u.completion.Prepare(task)
		}
		terminalTransition := isDone && snap.Status != task.Status
		won, updateErr := u.store.Transition(task, snap.Status)
		if updateErr != nil {
			u.diagnostics.SysLog("UpdateSunoTask task error: " + updateErr.Error())
			continue
		}
		if !won {
			u.diagnostics.Warn(ctx, fmt.Sprintf("Batch task %s already transitioned by another process, skip billing", task.TaskID))
			continue
		}
		if terminalTransition {
			billingSettled, billingErr := u.completion.Settle(ctx, task, &responseItem.TaskInfo, adaptor.AdjustBillingOnComplete)
			if billingErr != nil {
				u.diagnostics.Warn(ctx, fmt.Sprintf("Batch task %s billing history is invalid: %s", task.TaskID, billingErr.Error()))
				if u.completion.IndependentRefund(task) {
					u.completion.Refund(ctx, task, task.FailReason)
				}
				continue
			}
			if task.Status == domain.TaskStatusFailure && !billingSettled && task.Quota != 0 {
				u.completion.Refund(ctx, task, task.FailReason)
			}
		}
	}
	return nil
}

// updateVideoChannels polls channels concurrently and tasks sequentially.
func (u *Usecase) updateVideoChannels(ctx context.Context, platform string, taskChannelM map[int][]string, taskM map[string]*domain.Task) error {
	channelIDs := make([]int, 0, len(taskChannelM))
	for channelID := range taskChannelM {
		channelIDs = append(channelIDs, channelID)
	}
	slices.Sort(channelIDs)

	var wg sync.WaitGroup
	for _, channelId := range channelIDs {
		taskIds := taskChannelM[channelId]
		if len(taskIds) == 0 {
			continue
		}
		taskIds = append([]string(nil), taskIds...)

		wg.Add(1)
		u.spawn(func() {
			defer wg.Done()
			if err := u.updateVideoTasks(ctx, platform, channelId, taskIds, taskM); err != nil {
				u.diagnostics.Error(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
			}
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func (u *Usecase) updateVideoTasks(ctx context.Context, platform string, channelId int, taskIds []string, taskM map[string]*domain.Task) error {
	u.diagnostics.Info(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := u.channels.Get(channelId)
	if err != nil {
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		errUpdate := u.store.BulkUpdate(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("Failed to get channel info, channel ID: %d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if errUpdate != nil {
			u.diagnostics.SysLog(fmt.Sprintf("UpdateVideoTask error: %v", errUpdate))
		}
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	adaptor := u.factory.Open(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	adaptor.Init(cacheGetChannel.BaseURL, cacheGetChannel.Key)
	disablePollingSleep := cacheGetChannel.DisablePollingSleep
	for i, taskId := range taskIds {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := u.updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			u.diagnostics.Error(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
		if disablePollingSleep || i == len(taskIds)-1 {
			continue
		}

		// sleep 1 second between tasks for this channel only.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return nil
}

func (u *Usecase) updateVideoSingleTask(ctx context.Context, adaptor Adaptor, ch Channel, taskId string, taskM map[string]*domain.Task) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	baseURL := ch.FallbackBaseURL
	if ch.BaseURL != "" {
		baseURL = ch.BaseURL
	}
	proxy := ch.Proxy

	task := taskM[taskId]
	if task == nil {
		u.diagnostics.Error(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	key := ch.Key

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
	}
	snap := task.Snapshot()
	resp, err := adaptor.FetchTask(baseURL, key, task, proxy)
	if err != nil {
		return u.recordPollFailure(ctx, adaptor, task, snap.Status, pollClassTransport, 0, err.Error())
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return u.recordPollFailure(ctx, adaptor, task, snap.Status, pollClassTransport, resp.StatusCode, err.Error())
	}

	if task.PrivateData.Reconciliation == nil {
		u.diagnostics.Debug(ctx, "updateVideoSingleTask response: %s", responseBody)
	}

	switch classifyPollHTTP(resp.StatusCode) {
	case pollClassNotFound:
		return u.failTaskFromPoll(ctx, adaptor, task, snap.Status, fmt.Sprintf("upstream task not found (HTTP %d)", resp.StatusCode))
	case pollClassAuth:
		u.diagnostics.Warn(ctx, fmt.Sprintf("task poll auth failure channel_id=%d task=%s http=%d", ch.Id, task.TaskID, resp.StatusCode))
		return u.recordPollFailure(ctx, adaptor, task, snap.Status, pollClassAuth, resp.StatusCode, "")
	case pollClassTransient:
		return u.recordPollFailure(ctx, adaptor, task, snap.Status, pollClassTransient, resp.StatusCode, "")
	}

	taskResult := &domain.TaskInfo{}
	if fallback, data, observation, ok := u.codec.ParseGatewayResponse(responseBody); ok {
		if task.PrivateData.Reconciliation == nil {
			u.diagnostics.Debug(ctx, "updateVideoSingleTask parsed as new api response format: %+v", observation)
		}
		taskResult = fallback
		task.Data = data
	} else if taskResult, err = adaptor.ParseTaskResult(task, resp, responseBody); err != nil {
		return u.recordPollFailure(ctx, adaptor, task, snap.Status, pollClassHookError, resp.StatusCode, err.Error())
	}

	if task.PrivateData.Reconciliation == nil {
		u.diagnostics.Debug(ctx, "updateVideoSingleTask taskResult: %+v", taskResult)
	}

	parsedStatus := domain.TaskStatus(taskResult.Status)
	if parsedStatus == domain.TaskStatusUnknown || parsedStatus == "" || !knownPollStatus(parsedStatus) {
		return u.recordPollFailure(ctx, adaptor, task, snap.Status, pollClassUnrecognized, resp.StatusCode,
			u.unrecognizedPollDetail(taskResult.Reason, responseBody))
	}
	if classifyPollHTTP(resp.StatusCode) == pollClassOtherClient && isNonTerminalPollStatus(parsedStatus) {
		return u.recordPollFailure(ctx, adaptor, task, snap.Status, pollClassUnrecognized, resp.StatusCode,
			u.unrecognizedPollDetail(taskResult.Reason, responseBody))
	}

	task.Data = u.codec.RedactResponse(responseBody)
	if reconciliation := task.PrivateData.Reconciliation; reconciliation != nil {
		// Private download URLs and provider errors can contain credentials.
		// The plugin retains the required job identity in PluginState; purchase
		// evidence is recorded below in private financial state.
		task.Data, err = u.codec.Marshal(map[string]any{"status": parsedStatus})
		if err != nil {
			return err
		}
		if parsedStatus == domain.TaskStatusSuccess || parsedStatus == domain.TaskStatusFailure {
			// Only evidence from the terminal response can authorize settlement.
			// An earlier provisional zero must never authorize a later refund.
			reconciliation.HasUpstreamCost = false
			reconciliation.UpstreamCostConfirmed = false
			reconciliation.UpstreamCostUSD = 0
		}
		if (parsedStatus == domain.TaskStatusSuccess || parsedStatus == domain.TaskStatusFailure) && taskResult.UpstreamCostUSD != nil {
			reconciliation.HasUpstreamCost = true
			reconciliation.UpstreamCostUSD = *taskResult.UpstreamCostUSD
			reconciliation.UpstreamCostConfirmed = taskResult.UpstreamCostConfirmed
		}
		if (parsedStatus == domain.TaskStatusSuccess || parsedStatus == domain.TaskStatusFailure) &&
			task.PrivateData.BillingContext != nil &&
			task.PrivateData.BillingContext.SettlementMode == domain.TaskSettlementOpenRouterCostV2 {
			var evidence struct {
				Usage struct {
					IsBYOK      *bool           `json:"is_byok"`
					CostDetails json.RawMessage `json:"cost_details"`
				} `json:"usage"`
			}
			if u.codec.Unmarshal(responseBody, &evidence) == nil {
				reconciliation.UpstreamIsBYOK = evidence.Usage.IsBYOK
				reconciliation.UpstreamCostDetails = string(evidence.Usage.CostDetails)
			}
		}
		if parsedStatus == domain.TaskStatusSuccess {
			reconciliation.Finalized = reconciliation.HasUpstreamCost && reconciliation.UpstreamCostConfirmed &&
				(task.PrivateData.BillingContext == nil || task.PrivateData.BillingContext.SettlementMode == "")
			reconciliation.Required = !reconciliation.Finalized
		} else if parsedStatus == domain.TaskStatusFailure {
			// Keep the durable reconciliation marker until the refund completes.
			// A crash after terminal CAS must not hide a still-charged failure.
			reconciliation.Required = true
		}
	}
	if len(taskResult.PluginState) > 0 {
		task.PrivateData.PluginState = taskResult.PluginState
	}
	if isNonTerminalPollStatus(parsedStatus) {
		task.PrivateData.PollFailures = 0
	}

	now := u.now().Unix()
	shouldFinalizeBilling := false

	task.Status = parsedStatus
	switch parsedStatus {
	case domain.TaskStatusSubmitted:
		task.Progress = "10%"
	case domain.TaskStatusQueued:
		task.Progress = "20%"
	case domain.TaskStatusInProgress:
		task.Progress = "30%"
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case domain.TaskStatusSuccess:
		task.Progress = "100%"
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if strings.HasPrefix(taskResult.Url, "data:") {
			// data: URI (e.g. Vertex base64 encoded video) — keep in Data, not in ResultURL
			task.PrivateData.ResultURL = u.proxyURL(task.TaskID)
		} else if taskResult.Url != "" {
			// Direct upstream URL (e.g. Kling, Ali, Doubao, etc.)
			task.PrivateData.ResultURL = taskResult.Url
		} else {
			// No URL from adaptor — construct proxy URL using public task ID
			task.PrivateData.ResultURL = u.proxyURL(task.TaskID)
		}
		shouldFinalizeBilling = true
	case domain.TaskStatusFailure:
		if task.PrivateData.Reconciliation == nil {
			u.diagnostics.Json(ctx, fmt.Sprintf("Task %s failed", taskId), task)
		}
		task.Status = domain.TaskStatusFailure
		task.Progress = "100%"
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = taskResult.Reason
		u.diagnostics.Info(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = "100%"
		shouldFinalizeBilling = true
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}

	isDone := task.Status == domain.TaskStatusSuccess || task.Status == domain.TaskStatusFailure
	if isDone && task.PrivateData.BillingContext != nil &&
		(task.PrivateData.BillingContext.SettlementMode == domain.TaskSettlementOpenRouterCostV1 ||
			task.PrivateData.BillingContext.SettlementMode == domain.TaskSettlementOpenRouterCostV2) {
		if snap.Status == domain.TaskStatusSuccess || snap.Status == domain.TaskStatusFailure {
			return nil
		}
		u.completion.Prepare(task)
	}

	if isDone && snap.Status != task.Status {
		won, err := u.store.Transition(task, snap.Status)
		if err != nil {
			u.diagnostics.Error(ctx, fmt.Sprintf("UpdateWithStatus failed for task %s: %s", task.TaskID, err.Error()))
			shouldFinalizeBilling = false
		} else if !won {
			u.diagnostics.Warn(ctx, fmt.Sprintf("Task %s CAS lost or no-op update, skip billing", task.TaskID))
			shouldFinalizeBilling = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		if _, err := u.store.Transition(task, snap.Status); err != nil {
			u.diagnostics.Error(ctx, fmt.Sprintf("Failed to update task %s: %s", task.TaskID, err.Error()))
		}
	} else {
		// No changes, skip update
		u.diagnostics.Debug(ctx, "No update needed for task %s", task.TaskID)
	}

	if shouldFinalizeBilling {
		billingSettled, billingErr := u.completion.Settle(ctx, task, taskResult, adaptor.AdjustBillingOnComplete)
		if billingErr != nil {
			u.diagnostics.Warn(ctx, fmt.Sprintf("Task %s billing history is invalid: %s", task.TaskID, billingErr.Error()))
			if u.completion.IndependentRefund(task) {
				u.completion.Refund(ctx, task, task.FailReason)
			}
			return nil
		}
		if task.Status == domain.TaskStatusFailure && !billingSettled && task.Quota != 0 {
			u.completion.Refund(ctx, task, task.FailReason)
		}
	}

	return nil
}

func classifyPollHTTP(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return pollClassOK
	case statusCode == http.StatusNotFound || statusCode == http.StatusGone:
		return pollClassNotFound
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return pollClassAuth
	case statusCode == http.StatusTooManyRequests || statusCode >= 500:
		return pollClassTransient
	case statusCode >= 400 && statusCode < 500:
		return pollClassOtherClient
	default:
		return pollClassTransient
	}
}

func knownPollStatus(status domain.TaskStatus) bool {
	switch status {
	case domain.TaskStatusNotStart, domain.TaskStatusSubmitted, domain.TaskStatusQueued,
		domain.TaskStatusInProgress, domain.TaskStatusSuccess, domain.TaskStatusFailure:
		return true
	default:
		return false
	}
}

func isNonTerminalPollStatus(status domain.TaskStatus) bool {
	switch status {
	case domain.TaskStatusNotStart, domain.TaskStatusSubmitted, domain.TaskStatusQueued, domain.TaskStatusInProgress:
		return true
	default:
		return false
	}
}

func pollFailureReason(class string, statusCode int, detail string) string {
	reason := fmt.Sprintf("poll failed: %s", class)
	if statusCode > 0 {
		reason = fmt.Sprintf("poll failed: %s (HTTP %d)", class, statusCode)
	}
	if detail != "" {
		reason = reason + ": " + detail
	}
	return reason
}

// unrecognizedPollDetail pairs the plugin's reason with a bounded copy of the
// upstream body so the WARN line is enough to diagnose a parser gap.
func (u *Usecase) unrecognizedPollDetail(reason string, body []byte) string {
	const maxBodyChars = 512
	redacted := string(u.codec.RedactResponse(body))
	if len(redacted) > maxBodyChars {
		redacted = redacted[:maxBodyChars] + "…"
	}
	if strings.TrimSpace(reason) == "" {
		return "body=" + redacted
	}
	return reason + "; body=" + redacted
}

func (u *Usecase) recordPollFailure(ctx context.Context, adaptor Adaptor, task *domain.Task,
	fromStatus domain.TaskStatus, class string, statusCode int, detail string) error {
	if task.PrivateData.Reconciliation != nil {
		// Upstream diagnostics, including hook/transport failures and unknown
		// states, are untrusted and may contain usable credentials.
		detail = "upstream task requires reconciliation"
	}
	task.PrivateData.PollFailures++
	if class == pollClassUnrecognized || class == pollClassHookError {
		// The redacted body is intentionally not persisted to Task.Data on these
		// paths, so the WARN line is the only operator-visible copy of what the
		// plugin could not interpret.
		u.diagnostics.Warn(ctx, fmt.Sprintf("task %s poll %s (failures=%d, http=%d): %s",
			task.TaskID, class, task.PrivateData.PollFailures, statusCode, detail))
	}
	// TASK_POLL_MAX_FAILURES <= 0 disables the consecutive-failure cutoff, matching
	// TASK_TIMEOUT_MINUTES semantics; the 24h sweep remains the only backstop.
	if u.limits.MaxFailures() > 0 && task.PrivateData.PollFailures >= u.limits.MaxFailures() {
		return u.failTaskFromPoll(ctx, adaptor, task, fromStatus, pollFailureReason(class, statusCode, detail))
	}
	if _, err := u.store.Transition(task, fromStatus); err != nil {
		return err
	}
	return nil
}

func (u *Usecase) recordPollFailureForTasks(ctx context.Context, adaptor Adaptor, tasks []*domain.Task,
	class string, statusCode int, detail string) error {
	var firstErr error
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if err := u.recordPollFailure(ctx, adaptor, task, task.Status, class, statusCode, detail); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (u *Usecase) failTaskFromPoll(ctx context.Context, adaptor Adaptor, task *domain.Task, fromStatus domain.TaskStatus, reason string) error {
	if reconciliation := task.PrivateData.Reconciliation; reconciliation != nil {
		reconciliation.Required = true
		// A local timeout, missing job or parser failure does not establish the
		// final provider charge, even if an earlier response reported zero.
		reconciliation.HasUpstreamCost = false
		reconciliation.UpstreamCostConfirmed = false
	}
	now := u.now().Unix()
	task.Status = domain.TaskStatusFailure
	task.Progress = "100%"
	if task.FinishTime == 0 {
		task.FinishTime = now
	}
	task.FailReason = reason
	won, err := u.store.Transition(task, fromStatus)
	if err != nil {
		return err
	}
	if !won {
		return nil
	}
	taskResult := &domain.TaskInfo{Status: string(domain.TaskStatusFailure), Reason: reason}
	billingSettled, billingErr := u.completion.Settle(ctx, task, taskResult, adaptor.AdjustBillingOnComplete)
	if billingErr != nil {
		if u.completion.IndependentRefund(task) {
			u.completion.Refund(ctx, task, reason)
		}
		return billingErr
	}
	if !billingSettled && task.Quota != 0 {
		u.completion.Refund(ctx, task, reason)
	}
	return nil
}

func (u *Usecase) failTasksFromPoll(ctx context.Context, adaptor Adaptor, tasks []*domain.Task, reason string) error {
	var firstErr error
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if err := u.failTaskFromPoll(ctx, adaptor, task, task.Status, reason); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
