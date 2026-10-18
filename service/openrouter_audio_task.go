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
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func HasOpenRouterAudioTask(info *relaycommon.RelayInfo) bool {
	return info != nil && info.OpenRouterCost != nil && info.OpenRouterCost.TaskID != ""
}

// BeginOpenRouterAudioTask persists an intent before any hold changes. An
// interrupted reservation deliberately records unknown applied ledger legs.
func BeginOpenRouterAudioTask(c *gin.Context, info *relaycommon.RelayInfo, quota int) error {
	if !GetTaskArtifactStore().Enabled() {
		return errors.New("durable Task artifact storage is required for OpenRouter audio")
	}
	if quota <= 0 || HasOpenRouterAudioTask(info) {
		return errors.New("OpenRouter audio requires a new positive reservation")
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{Action: "audio"}
	}
	task := InitTaskSubmission(c, constant.TaskPlatformOpenRouterAudio, info)
	task.Quota = 0
	task.PrivateData.BillingSource = ""
	task.PrivateData.SubscriptionId = 0
	task.PrivateData.Reconciliation.ReservedQuota = 0
	now := time.Now()
	task.PrivateData.OpenRouterAudio = &model.OpenRouterAudioTask{
		RequestID: info.RequestId, BillingPreference: common.NormalizeBillingPreference(info.UserSetting.BillingPreference),
		PlannedQuota: quota, LeaseUntil: now.Add(5 * time.Minute).Unix(), Deadline: now.Add(24 * time.Hour).Unix(),
	}
	projectOpenRouterAudioState(task)
	if err := task.Insert(); err != nil {
		return err
	}
	info.OpenRouterCost.TaskID = task.TaskID
	c.Header("X-Task-Id", task.TaskID)
	return nil
}

func openRouterAudioTask(info *relaycommon.RelayInfo) (*model.Task, error) {
	if !HasOpenRouterAudioTask(info) {
		return nil, errors.New("missing durable OpenRouter audio task")
	}
	task, exists, err := model.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
	if err != nil {
		return nil, err
	}
	if !exists || task.PrivateData.OpenRouterAudio == nil || task.Platform != constant.TaskPlatformOpenRouterAudio ||
		task.UserId != info.UserId || task.ChannelId != info.ChannelId {
		return nil, errors.New("OpenRouter audio task identity changed")
	}
	return task, nil
}

func CommitOpenRouterAudioReservation(c *gin.Context, info *relaycommon.RelayInfo) error {
	task, err := openRouterAudioTask(info)
	if err != nil {
		return err
	}
	a, r := task.PrivateData.OpenRouterAudio, task.PrivateData.Reconciliation
	if !r.ReservationPending || time.Now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter audio reservation lease expired")
	}
	task.PrivateData.BillingSource, task.PrivateData.SubscriptionId = info.BillingSource, info.SubscriptionId
	task.Quota = info.Billing.GetPreConsumedQuota()
	r.ReservedQuota = task.Quota
	revision := a.Revision
	projectOpenRouterAudioState(task)
	if err := task.UpdateOpenRouterAudio(revision); err != nil {
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
	projectOpenRouterAudioState(task)
	return task.UpdateOpenRouterAudio(a.Revision)
}

func ConfigureOpenRouterAudioSubmission(info *relaycommon.RelayInfo, metadataURL string, headers http.Header) error {
	task, err := openRouterAudioTask(info)
	if err != nil {
		return err
	}
	a, r := task.PrivateData.OpenRouterAudio, task.PrivateData.Reconciliation
	if r.ReservationPending || !r.SubmissionPending || a.MetadataURL != "" || time.Now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter audio submission intent is not available")
	}
	endpoint, err := url.Parse(metadataURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return errors.New("invalid OpenRouter audio metadata endpoint")
	}
	a.MetadataURL, a.Headers, a.Transport = metadataURL, headers.Clone(), info.ChannelSetting
	a.LeaseUntil = time.Now().Add(5 * time.Minute).Unix()
	return task.UpdateOpenRouterAudio(a.Revision)
}

// RecordOpenRouterAudioSubmission captures the generation ID before draining
// the body. A transport failure can never authorize a second POST or refund.
func RecordOpenRouterAudioSubmission(info *relaycommon.RelayInfo, resp *http.Response, requestErr error) error {
	task, err := openRouterAudioTask(info)
	if err != nil {
		return err
	}
	a := task.PrivateData.OpenRouterAudio
	if time.Now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter audio submission lease expired")
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
			manualOpenRouterAudio(task, "submission_outcome_unknown")
		}
	}
	projectOpenRouterAudioState(task)
	return task.UpdateOpenRouterAudio(a.Revision)
}

func CompleteOpenRouterAudioTask(c *gin.Context, info *relaycommon.RelayInfo, body []byte, mimeType, resultType string) error {
	task, err := openRouterAudioTask(info)
	if err != nil {
		return err
	}
	a, r := task.PrivateData.OpenRouterAudio, task.PrivateData.Reconciliation
	if time.Now().Unix() >= a.LeaseUntil || !r.SubmissionPending {
		return errors.New("OpenRouter audio result lease expired")
	}
	state := info.OpenRouterCost
	if state.GenerationID != "" && len(state.GenerationID) <= 512 {
		task.PrivateData.UpstreamTaskID = state.GenerationID
	}
	if !state.Final && task.PrivateData.UpstreamTaskID == "" {
		return errors.New("OpenRouter audio cost and generation identity are unavailable")
	}
	ref, err := GetTaskArtifactStore().Persist(context.Background(), task, types.TaskArtifact{Key: "result", Type: resultType, MimeType: mimeType}, bytes.NewReader(body))
	if err != nil {
		return errors.New("OpenRouter audio result persistence failed")
	}
	a.ResultReady, a.ResultMimeType, a.ResultSize, a.ResultSHA256 = true, mimeType, ref.Size, ref.SHA256
	r.SubmissionPending = false
	a.NextPoll = time.Now().Unix()
	if state.Final && state.Valid {
		r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD = true, true, state.CostUSD
		r.UpstreamIsBYOK, r.UpstreamCostDetails = state.IsBYOK, string(state.CostDetails)
		finishOpenRouterAudio(task)
	}
	projectOpenRouterAudioState(task)
	if err := task.UpdateOpenRouterAudio(a.Revision); err != nil {
		return err
	}
	if r.SettlementPending {
		if err := settleOpenRouterCost(task); err != nil {
			// The durable target/main-applied markers own any partial settlement.
			// Result delivery remains independent of cache/log repair.
			logger.LogWarn(c, fmt.Sprintf("OpenRouter audio task %s requires settlement repair", task.TaskID))
		}
	}
	return nil
}

func FailOpenRouterAudioTask(info *relaycommon.RelayInfo, reason string) error {
	task, err := openRouterAudioTask(info)
	if err != nil {
		return err
	}
	a := task.PrivateData.OpenRouterAudio
	if !task.PrivateData.Reconciliation.SubmissionPending || time.Now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter audio result lease expired")
	}
	a.ResultFailed, a.LastError = true, reason
	task.PrivateData.Reconciliation.SubmissionPending = false
	a.NextPoll = time.Now().Unix()
	if task.PrivateData.UpstreamTaskID == "" {
		manualOpenRouterAudio(task, reason)
	}
	projectOpenRouterAudioState(task)
	return task.UpdateOpenRouterAudio(a.Revision)
}

func projectOpenRouterAudioState(task *model.Task) {
	a, r := task.PrivateData.OpenRouterAudio, task.PrivateData.Reconciliation
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

func manualOpenRouterAudio(task *model.Task, reason string) {
	task.Status, task.Progress, task.FinishTime = model.TaskStatusFailure, "100%", time.Now().Unix()
	task.FailReason = "OpenRouter audio manual reconciliation required: " + reason
	task.PrivateData.OpenRouterAudio.LastError = reason
	task.PrivateData.Reconciliation.Required = true
}

func finishOpenRouterAudio(task *model.Task) {
	task.Status, task.Progress, task.FinishTime = model.TaskStatusSuccess, "100%", time.Now().Unix()
	if !task.PrivateData.OpenRouterAudio.ResultReady {
		task.Status = model.TaskStatusFailure
		task.FailReason = "OpenRouter audio result unavailable; upstream cost confirmed"
	}
	prepareOpenRouterCostSettlement(task)
}

// pollOpenRouterAudio uses the existing Task scheduler, never a paid submit.
// Only the winner of a revision-checked terminal claim may move funds.
func pollOpenRouterAudio(ctx context.Context, task *model.Task) error {
	a, r := task.PrivateData.OpenRouterAudio, task.PrivateData.Reconciliation
	now := time.Now()
	if r.ReservationPending || r.SubmissionPending {
		if now.Unix() < a.LeaseUntil {
			return nil
		}
		if r.ReservationPending || task.PrivateData.UpstreamTaskID == "" {
			manualOpenRouterAudio(task, "interrupted_reservation_or_submission")
			projectOpenRouterAudioState(task)
			return task.UpdateOpenRouterAudio(a.Revision)
		}
		r.SubmissionPending = false
		a.ResultFailed = !a.ResultReady
	}
	if now.Unix() >= a.Deadline || a.Attempts >= 16 {
		manualOpenRouterAudio(task, "cost_lookup_deadline_or_attempt_limit")
		projectOpenRouterAudioState(task)
		return task.UpdateOpenRouterAudio(a.Revision)
	}
	if now.Unix() < a.NextPoll {
		return nil
	}
	if task.PrivateData.UpstreamTaskID == "" || a.MetadataURL == "" {
		manualOpenRouterAudio(task, "missing_generation_identity")
		projectOpenRouterAudioState(task)
		return task.UpdateOpenRouterAudio(a.Revision)
	}
	delay := time.Minute * time.Duration(1<<min(a.Attempts, 6))
	delay = min(delay, time.Hour)
	a.Attempts++
	cost, status, retryAfter, class := lookupOpenRouterAudioCost(ctx, task)
	a.LastHTTPStatus, a.LastError = status, class
	if class == "" {
		r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD = true, true, cost
		finishOpenRouterAudio(task)
	} else if class == "auth" || class == "identity_or_invalid_cost" || class == "permanent_http_error" {
		manualOpenRouterAudio(task, class)
	} else {
		a.NextPoll = now.Add(max(delay, retryAfter)).Unix()
		if a.NextPoll >= a.Deadline || a.Attempts >= 16 {
			manualOpenRouterAudio(task, "cost_lookup_deadline_or_attempt_limit")
		}
	}
	projectOpenRouterAudioState(task)
	if err := task.UpdateOpenRouterAudio(a.Revision); err != nil {
		return err
	}
	if r.SettlementPending {
		return settleOpenRouterCost(task)
	}
	return nil
}

func lookupOpenRouterAudioCost(ctx context.Context, task *model.Task) (float64, int, time.Duration, string) {
	a := task.PrivateData.OpenRouterAudio
	client, err := GetHttpClientWithProxySettings(a.Transport.Proxy, a.Transport)
	if err != nil {
		return 0, 0, 0, "transport_unavailable"
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, a.MetadataURL+"?id="+url.QueryEscape(task.PrivateData.UpstreamTaskID), nil)
	if err != nil {
		return 0, 0, 0, "permanent_http_error"
	}
	req.Header = http.Header(a.Headers).Clone()
	req.Header.Del("Content-Length")
	req.Header.Del("Content-Type")
	req.Host = req.Header.Get("Host")
	metadataClient := *client
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := metadataClient.Do(req)
	if err != nil {
		return 0, 0, 0, "transport_error"
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
		return 0, status, 0, "auth"
	}
	if status == 404 || status == 429 || status >= 500 {
		return 0, status, retryAfter, "temporary_http_error"
	}
	if status != 200 {
		return 0, status, 0, "permanent_http_error"
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return 0, status, retryAfter, "transport_error"
	}
	if len(body) > 2<<20 {
		return 0, status, 0, "identity_or_invalid_cost"
	}
	var result struct {
		Data struct {
			ID             string          `json:"id"`
			Model          string          `json:"model"`
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
	modelName := strings.TrimSuffix(strings.TrimSuffix(task.Properties.UpstreamModelName, ":nitro"), ":floor")
	if d.ID != task.PrivateData.UpstreamTaskID || d.Model != modelName || (len(result.Error) > 0 && string(result.Error) != "null") {
		return 0, status, 0, "identity_or_invalid_cost"
	}
	if d.Cancelled == nil || *d.Cancelled || d.GenerationTime == nil {
		return 0, status, retryAfter, "incomplete_metadata"
	}
	if *d.GenerationTime < 0 || math.IsNaN(*d.GenerationTime) || math.IsInf(*d.GenerationTime, 0) {
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
