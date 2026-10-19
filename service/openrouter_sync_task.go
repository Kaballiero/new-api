package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func HasOpenRouterSyncTask(info *relaycommon.RelayInfo) bool {
	return info != nil && info.OpenRouterCost != nil && info.OpenRouterCost.TaskID != ""
}

func openRouterSyncIdentity(info *relaycommon.RelayInfo) (constant.TaskPlatform, string, error) {
	switch info.RelayMode {
	case relayconstant.RelayModeRerank:
		return constant.TaskPlatformOpenRouterRerank, "rerank", nil
	case relayconstant.RelayModeAudioSpeech, relayconstant.RelayModeAudioTranscription:
		return constant.TaskPlatformOpenRouterAudio, "audio", nil
	case relayconstant.RelayModeChatCompletions:
		if info.OpenRouterCost != nil && info.OpenRouterCost.BufferedAudio {
			return constant.TaskPlatformOpenRouterAudio, "audio", nil
		}
	}
	return "", "", errors.New("unsupported OpenRouter synchronous operation")
}

// BeginOpenRouterSyncTask persists an intent before any hold changes. An
// interrupted reservation deliberately records unknown applied ledger legs.
func BeginOpenRouterSyncTask(c *gin.Context, info *relaycommon.RelayInfo, quota int) error {
	if !GetTaskArtifactStore().Enabled() {
		return errors.New("durable Task artifact storage is required for OpenRouter synchronous")
	}
	if quota <= 0 || HasOpenRouterSyncTask(info) {
		return errors.New("OpenRouter synchronous requires a new positive reservation")
	}
	platform, action, err := openRouterSyncIdentity(info)
	if err != nil {
		return err
	}
	info.TaskRelayInfo = &relaycommon.TaskRelayInfo{Action: action}

	task := InitTaskSubmission(c, platform, info)
	task.Quota = 0
	task.PrivateData.BillingSource = ""
	task.PrivateData.SubscriptionId = 0
	task.PrivateData.Reconciliation.ReservedQuota = 0
	now := time.Now()
	state := &model.OpenRouterSyncTask{
		RequestID: info.RequestId, BillingPreference: common.NormalizeBillingPreference(info.UserSetting.BillingPreference),
		PlannedQuota: quota, LeaseUntil: now.Add(5 * time.Minute).Unix(), Deadline: now.Add(24 * time.Hour).Unix(),
	}
	if platform == constant.TaskPlatformOpenRouterAudio {
		task.PrivateData.OpenRouterAudio = state
	} else {
		task.PrivateData.OpenRouterSync = state
	}
	projectOpenRouterSyncState(task)
	if err := task.Insert(); err != nil {
		return err
	}
	info.OpenRouterCost.TaskID = task.TaskID
	c.Header("X-Task-Id", task.TaskID)
	return nil
}

func openRouterSyncTask(info *relaycommon.RelayInfo) (*model.Task, error) {
	if !HasOpenRouterSyncTask(info) {
		return nil, errors.New("missing durable OpenRouter synchronous task")
	}
	task, exists, err := model.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
	if err != nil {
		return nil, err
	}
	platform, action, err := openRouterSyncIdentity(info)
	if err != nil {
		return nil, err
	}

	if !exists || task.PrivateData.OpenRouterSyncState() == nil || task.Platform != platform || task.Action != action ||
		task.UserId != info.UserId || task.ChannelId != info.ChannelId || task.PrivateData.TokenId != info.TokenId || task.Properties.UpstreamModelName != info.UpstreamModelName || task.PrivateData.OpenRouterSyncState().RequestID != info.RequestId ||
		(platform == constant.TaskPlatformOpenRouterRerank) != (task.PrivateData.OpenRouterSync != nil) ||
		task.PrivateData.OpenRouterSync != nil && task.PrivateData.OpenRouterAudio != nil {
		return nil, errors.New("OpenRouter synchronous task identity changed")
	}
	return task, nil
}

func CommitOpenRouterSyncReservation(c *gin.Context, info *relaycommon.RelayInfo) error {
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	if !r.ReservationPending || time.Now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter synchronous reservation lease expired")
	}
	task.PrivateData.BillingSource, task.PrivateData.SubscriptionId = info.BillingSource, info.SubscriptionId
	task.Quota = info.Billing.GetPreConsumedQuota()
	info.PriceData.Quota = task.Quota
	r.ReservedQuota = task.Quota
	revision := a.Revision
	projectOpenRouterSyncState(task)
	if err := task.UpdateOpenRouterSync(revision); err != nil {
		return err
	}
	// Transfer the existing hold without charging a second time. Controller and
	// consumption helpers thereafter leave settlement entirely to the Task.
	if err := info.Billing.Settle(task.Quota); err != nil {
		return err
	}
	if err := LogTaskConsumption(c, info, task); err != nil {
		return err
	}
	r.ReservationPending, r.SubmissionPending = false, true
	task.Status, task.Progress, task.StartTime = model.TaskStatusInProgress, "10%", time.Now().Unix()
	projectOpenRouterSyncState(task)
	return task.UpdateOpenRouterSync(a.Revision)
}

func ConfigureOpenRouterSyncSubmission(info *relaycommon.RelayInfo, metadataURL string, headers http.Header) error {
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	if r.ReservationPending || !r.SubmissionPending || a.MetadataURL != "" || time.Now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter synchronous submission intent is not available")
	}
	endpoint, err := url.Parse(metadataURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return errors.New("invalid OpenRouter synchronous metadata endpoint")
	}
	a.MetadataURL, a.Headers, a.Transport = metadataURL, headers.Clone(), info.ChannelSetting
	a.LeaseUntil = time.Now().Add(5 * time.Minute).Unix()
	return task.UpdateOpenRouterSync(a.Revision)
}

// RecordOpenRouterSyncSubmission captures the generation ID before draining
// the body. A transport failure can never authorize a second POST or refund.
func RecordOpenRouterSyncSubmission(info *relaycommon.RelayInfo, resp *http.Response, requestErr error) error {
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	a := task.PrivateData.OpenRouterSyncState()
	if time.Now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter synchronous submission lease expired")
	}
	if resp != nil {
		id := resp.Header.Get("X-Generation-Id")
		if id != "" && len(id) <= 512 {
			task.PrivateData.UpstreamTaskID = id
			info.OpenRouterCost.GenerationID = id
		}
	}
	if requestErr != nil || resp == nil || resp.StatusCode != http.StatusOK {
		task.PrivateData.Reconciliation.SubmissionPending = false
		a.ResultFailed, a.LastError = true, "submission_outcome_unknown"
		a.NextPoll = time.Now().Unix()
		if task.PrivateData.UpstreamTaskID == "" {
			manualOpenRouterSync(task, "submission_outcome_unknown")
		}
	}
	projectOpenRouterSyncState(task)
	return task.UpdateOpenRouterSync(a.Revision)
}

func CompleteOpenRouterSyncTask(c *gin.Context, info *relaycommon.RelayInfo, body []byte, mimeType, resultType string) error {
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	if time.Now().Unix() >= a.LeaseUntil || !r.SubmissionPending {
		return errors.New("OpenRouter synchronous result lease expired")
	}
	state := info.OpenRouterCost
	if state.GenerationID != "" && len(state.GenerationID) <= 512 {
		task.PrivateData.UpstreamTaskID = state.GenerationID
	}
	if !state.Final && task.PrivateData.UpstreamTaskID == "" {
		return errors.New("OpenRouter synchronous cost and generation identity are unavailable")
	}
	ref, err := GetTaskArtifactStore().Persist(context.Background(), task, types.TaskArtifact{Key: "result", Type: resultType, MimeType: mimeType}, bytes.NewReader(body))
	if err != nil {
		return errors.New("OpenRouter synchronous result persistence failed")
	}
	a.ResultReady, a.ResultMimeType, a.ResultSize, a.ResultSHA256 = true, mimeType, ref.Size, ref.SHA256
	r.SubmissionPending = false
	a.NextPoll = time.Now().Unix()
	if state.Final && state.Valid {
		r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD = true, true, state.CostUSD
		r.UpstreamIsBYOK, r.UpstreamCostDetails = state.IsBYOK, string(state.CostDetails)
		finishOpenRouterSync(task)
	}
	projectOpenRouterSyncState(task)
	if err := task.UpdateOpenRouterSync(a.Revision); err != nil {
		return err
	}
	if r.SettlementPending {
		if err := settleOpenRouterCost(task); err != nil {
			// The durable target/main-applied markers own any partial settlement.
			// Result delivery remains independent of cache/log repair.
			logger.LogWarn(c, fmt.Sprintf("OpenRouter synchronous task %s requires settlement repair", task.TaskID))
		}
	}
	return nil
}

func FailOpenRouterSyncTask(info *relaycommon.RelayInfo, reason string) error {
	task, err := openRouterSyncTask(info)
	if err != nil {
		return err
	}
	a := task.PrivateData.OpenRouterSyncState()
	if id := info.OpenRouterCost.GenerationID; id != "" && len(id) <= 512 {
		task.PrivateData.UpstreamTaskID = id
	}
	if !task.PrivateData.Reconciliation.SubmissionPending || time.Now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter synchronous result lease expired")
	}
	a.ResultFailed, a.LastError = true, reason
	task.PrivateData.Reconciliation.SubmissionPending = false
	a.NextPoll = time.Now().Unix()
	if task.PrivateData.UpstreamTaskID == "" {
		manualOpenRouterSync(task, reason)
	}
	projectOpenRouterSyncState(task)
	return task.UpdateOpenRouterSync(a.Revision)
}

func projectOpenRouterSyncState(task *model.Task) {
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
	} else if task.Status == model.TaskStatusFailure {
		status = "manual_reconciliation"
	}
	task.SetData(map[string]any{"result_ready": a.ResultReady, "result_failed": a.ResultFailed, "billing_status": status, "cost_lookup_attempts": a.Attempts, "cost_lookup_http_status": a.LastHTTPStatus, "next_cost_lookup": a.NextPoll})
}

func manualOpenRouterSync(task *model.Task, reason string) {
	task.Status, task.Progress, task.FinishTime = model.TaskStatusFailure, "100%", time.Now().Unix()
	task.FailReason = "OpenRouter synchronous manual reconciliation required: " + reason
	task.PrivateData.OpenRouterSyncState().LastError = reason
	task.PrivateData.Reconciliation.Required = true
}

func finishOpenRouterSync(task *model.Task) {
	task.Status, task.Progress, task.FinishTime = model.TaskStatusSuccess, "100%", time.Now().Unix()
	if !task.PrivateData.OpenRouterSyncState().ResultReady {
		task.Status = model.TaskStatusFailure
		task.FailReason = "OpenRouter synchronous result unavailable; upstream cost confirmed"
	}
	prepareOpenRouterCostSettlement(task)
}

// pollOpenRouterSync uses the existing Task scheduler, never a paid submit.
// Only the winner of a revision-checked terminal claim may move funds.
func pollOpenRouterSync(ctx context.Context, task *model.Task) error {
	validIdentity := task.Platform == constant.TaskPlatformOpenRouterAudio && task.Action == "audio" && task.PrivateData.OpenRouterSync == nil ||
		task.Platform == constant.TaskPlatformOpenRouterRerank && task.Action == "rerank" && task.PrivateData.OpenRouterAudio == nil
	if !validIdentity {
		manualOpenRouterSync(task, "operation_identity_changed")
		projectOpenRouterSyncState(task)
		return task.UpdateOpenRouterSync(task.PrivateData.OpenRouterSyncState().Revision)
	}
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	now := time.Now()
	if r.ReservationPending || r.SubmissionPending {
		if now.Unix() < a.LeaseUntil {
			return nil
		}
		if r.ReservationPending || task.PrivateData.UpstreamTaskID == "" {
			manualOpenRouterSync(task, "interrupted_reservation_or_submission")
			projectOpenRouterSyncState(task)
			return task.UpdateOpenRouterSync(a.Revision)
		}
		r.SubmissionPending = false
		a.ResultFailed = !a.ResultReady
	}
	if now.Unix() >= a.Deadline || a.Attempts >= 16 {
		manualOpenRouterSync(task, "cost_lookup_deadline_or_attempt_limit")
		projectOpenRouterSyncState(task)
		return task.UpdateOpenRouterSync(a.Revision)
	}
	if now.Unix() < a.NextPoll {
		return nil
	}
	if task.PrivateData.UpstreamTaskID == "" || a.MetadataURL == "" {
		manualOpenRouterSync(task, "missing_generation_identity")
		projectOpenRouterSyncState(task)
		return task.UpdateOpenRouterSync(a.Revision)
	}
	delay := time.Minute * time.Duration(1<<min(a.Attempts, 6))
	delay = min(delay, time.Hour)
	a.Attempts++
	cost, status, retryAfter, class := lookupOpenRouterSyncCost(ctx, task)
	a.LastHTTPStatus, a.LastError = status, class
	if class == "" {
		r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD = true, true, cost
		finishOpenRouterSync(task)
	} else if class == "auth" || class == "identity_or_invalid_cost" || class == "permanent_http_error" {
		manualOpenRouterSync(task, class)
	} else {
		a.NextPoll = now.Add(max(delay, retryAfter)).Unix()
		if a.NextPoll >= a.Deadline || a.Attempts >= 16 {
			manualOpenRouterSync(task, "cost_lookup_deadline_or_attempt_limit")
		}
	}
	projectOpenRouterSyncState(task)
	if err := task.UpdateOpenRouterSync(a.Revision); err != nil {
		return err
	}
	if r.SettlementPending {
		return settleOpenRouterCost(task)
	}
	return nil
}

func lookupOpenRouterSyncCost(ctx context.Context, task *model.Task) (float64, int, time.Duration, string) {
	a := task.PrivateData.OpenRouterSyncState()
	body, status, retryAfter, class := fetchOpenRouterSyncMetadata(ctx, task, a.MetadataURL+"?id="+url.QueryEscape(task.PrivateData.UpstreamTaskID))
	if class != "" {
		return 0, status, retryAfter, class
	}
	var result struct {
		Data struct {
			ID             string          `json:"id"`
			Model          string          `json:"model"`
			APIType        string          `json:"api_type"`
			Cancelled      *bool           `json:"cancelled"`
			GenerationTime *float64        `json:"generation_time"`
			TotalCost      json.RawMessage `json:"total_cost"`
		} `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if common.Unmarshal(body, &result) != nil {
		return 0, status, 0, "identity_or_invalid_cost"
	}
	d := result.Data
	if task.Platform == constant.TaskPlatformOpenRouterRerank && d.APIType != "rerank" {
		return 0, status, 0, "identity_or_invalid_cost"
	}
	if d.ID != task.PrivateData.UpstreamTaskID || (len(result.Error) > 0 && string(result.Error) != "null") {
		return 0, status, 0, "identity_or_invalid_cost"
	}
	modelName := strings.TrimSuffix(strings.TrimSuffix(task.Properties.UpstreamModelName, ":nitro"), ":floor")
	if d.Model != modelName {
		// Generation metadata uses the dated canonical slug. Verify the binding
		// through this account's model metadata, never by trimming a date suffix.
		base, ok := strings.CutSuffix(a.MetadataURL, "/generation")
		author, slug, validModel := strings.Cut(modelName, "/")
		if !ok || !validModel || author == "" || slug == "" {
			return 0, status, 0, "identity_or_invalid_cost"
		}
		metadata, modelStatus, modelRetry, modelClass := fetchOpenRouterSyncMetadata(ctx, task, base+"/model/"+url.PathEscape(author)+"/"+url.PathEscape(slug))
		if modelClass != "" {
			return 0, modelStatus, modelRetry, modelClass
		}
		var modelMetadata struct {
			Data struct {
				ID            string `json:"id"`
				CanonicalSlug string `json:"canonical_slug"`
			} `json:"data"`
			Error json.RawMessage `json:"error"`
		}
		if common.Unmarshal(metadata, &modelMetadata) != nil || modelMetadata.Data.ID != modelName || modelMetadata.Data.CanonicalSlug == "" || modelMetadata.Data.CanonicalSlug != d.Model || (len(modelMetadata.Error) > 0 && string(modelMetadata.Error) != "null") {
			return 0, modelStatus, 0, "identity_or_invalid_cost"
		}
	}
	// TTS does not populate generation_time. A fully persisted speech result,
	// an uncancelled generation and a confirmed cost establish completion instead.
	completedSpeech := d.APIType == "tts" && a.ResultReady && !a.ResultFailed && strings.HasPrefix(a.ResultMimeType, "audio/")
	if d.Cancelled == nil || *d.Cancelled || d.GenerationTime == nil && !completedSpeech {
		return 0, status, retryAfter, "incomplete_metadata"
	}
	if d.GenerationTime != nil && (*d.GenerationTime < 0 || math.IsNaN(*d.GenerationTime) || math.IsInf(*d.GenerationTime, 0)) {
		return 0, status, 0, "identity_or_invalid_cost"
	}
	raw := bytes.TrimSpace(d.TotalCost)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, status, retryAfter, "incomplete_metadata"
	}
	cost, err := common.ValidateOpenRouterCost(raw)
	if err != nil {
		return 0, status, 0, "identity_or_invalid_cost"
	}
	return cost, status, 0, ""
}

func fetchOpenRouterSyncMetadata(ctx context.Context, task *model.Task, endpoint string) ([]byte, int, time.Duration, string) {
	a := task.PrivateData.OpenRouterSyncState()
	client, err := GetHttpClientWithProxySettings(a.Transport.Proxy, a.Transport)
	if err != nil {
		return nil, 0, 0, "transport_unavailable"
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, 0, "permanent_http_error"
	}
	req.Header = http.Header(a.Headers).Clone()
	req.Header.Del("Content-Length")
	req.Header.Del("Content-Type")
	req.Host = req.Header.Get("Host")
	metadataClient := *client
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := metadataClient.Do(req)
	if err != nil {
		return nil, 0, 0, "transport_error"
	}
	defer resp.Body.Close()
	status := resp.StatusCode
	var retryAfter time.Duration
	if seconds, err := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 32); err == nil && seconds > 0 {
		retryAfter = time.Duration(seconds) * time.Second
	} else if until, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
		retryAfter = max(time.Until(until), 0)
	}
	if status == 401 || status == 403 {
		return nil, status, 0, "auth"
	}
	if status == 404 || status == 429 || status >= 500 {
		return nil, status, retryAfter, "temporary_http_error"
	}
	if status != 200 {
		return nil, status, 0, "permanent_http_error"
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return nil, status, retryAfter, "transport_error"
	}
	if len(body) > 2<<20 {
		return nil, status, 0, "identity_or_invalid_cost"
	}
	return body, status, retryAfter, ""
}
