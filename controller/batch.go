package controller

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shopspring/decimal"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/batch"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const MaxBatchRequestBytes = 16 << 20
const MaxBatchRequests = 1000

var batchCustomID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type BatchCreateRequest struct {
	Model    string       `json:"model"`
	Group    string       `json:"group,omitempty"`
	Endpoint string       `json:"endpoint"`
	Requests []batch.Item `json:"requests"`
}

func batchError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"type": "batch_error", "code": code, "message": message}})
}

// PrepareBatchRequest validates the envelope before channel selection and
// constrains that selection to providers supporting the requested Batch format.
func PrepareBatchRequest(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBatchRequestBytes)
	var request BatchCreateRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		batchError(c, 400, "invalid_request", "invalid or oversized Batch JSON")
		return
	}
	if request.Model == "" || len(request.Model) > 255 || len(request.Requests) == 0 || len(request.Requests) > MaxBatchRequests {
		batchError(c, 400, "invalid_request", "model and 1..1000 requests are required")
		return
	}
	seen := make(map[string]bool, len(request.Requests))
	for _, item := range request.Requests {
		if !batchCustomID.MatchString(item.CustomID) || seen[item.CustomID] {
			batchError(c, 400, "invalid_custom_id", "custom_id must be unique and match [a-zA-Z0-9_-]{1,64}")
			return
		}
		seen[item.CustomID] = true
		if _, err := validateBatchItem(item.Body, request.Model, request.Endpoint); err != nil {
			batchError(c, 400, "invalid_request", fmt.Sprintf("request %s: %v", item.CustomID, err))
			return
		}
	}
	publicID := "batch_" + common.GetRandomString(32)
	idempotencyKey := c.GetHeader("Idempotency-Key")
	if len(idempotencyKey) > 256 {
		batchError(c, 400, "invalid_idempotency_key", "Idempotency-Key must not exceed 256 bytes")
		return
	}
	if idempotencyKey == "" {
		idempotencyKey = publicID
	}
	keyHash := fmt.Sprintf("%x", sha256.Sum256([]byte(idempotencyKey)))
	requestData, err := common.Marshal(request)
	if err != nil {
		batchError(c, 400, "invalid_request", err.Error())
		return
	}
	requestHash := fmt.Sprintf("%x", sha256.Sum256(requestData))
	var existing model.BatchJob
	if err := model.DB.Where("user_id = ? AND idempotency_key = ?", c.GetInt("id"), keyHash).First(&existing).Error; err == nil {
		if existing.RequestHash != requestHash {
			batchError(c, 409, "idempotency_conflict", "Idempotency-Key was used with a different batch")
			return
		}
		c.AbortWithStatusJSON(http.StatusOK, existing)
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		batchError(c, 503, "storage_unavailable", "batch storage is unavailable")
		return
	}
	c.Set("batch_public_id", publicID)
	c.Set("batch_key_hash", keyHash)
	c.Set("batch_request_hash", requestHash)
	c.Set("batch_create_request", request)
	service.GetChannelConstraints(c).AddFilter(hostdto.ChannelFilter{Kind: hostdto.FilterBatchEndpoint, BatchEndpoint: request.Endpoint})
}

func validateBatchItem(body []byte, modelName, endpoint string) (int, error) {
	if len(body) == 0 || len(body) > batch.MaxResultBytes || common.GetJsonType(body) != "object" || common.HasDuplicateJSONKeys(body) {
		return 0, errors.New("body must be a bounded JSON object without duplicate keys")
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil {
		return 0, err
	}
	if raw, ok := fields["model"]; ok {
		var supplied string
		if common.Unmarshal(raw, &supplied) != nil || supplied != modelName {
			return 0, errors.New("item model must match the batch model")
		}
	}
	if raw, ok := fields["stream"]; ok {
		var stream bool
		if common.Unmarshal(raw, &stream) != nil || stream {
			return 0, errors.New("streaming is not supported in Batch")
		}
	}
	if raw, ok := fields["n"]; ok {
		var n uint
		if common.Unmarshal(raw, &n) != nil || n != 1 {
			return 0, errors.New("Batch requires n=1")
		}
	}
	maxOutput := 0
	allowedCaps := map[string]bool{}
	switch endpoint {
	case "/v1/chat/completions":
		allowedCaps["max_tokens"] = true
		allowedCaps["max_completion_tokens"] = true
	case "/v1/responses":
		allowedCaps["max_output_tokens"] = true
	case "/v1/messages":
		allowedCaps["max_tokens"] = true
	}
	for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		if raw, ok := fields[key]; ok {
			if !allowedCaps[key] {
				return 0, fmt.Errorf("%s is not a native output limit for %s", key, endpoint)
			}
			var value uint
			if common.Unmarshal(raw, &value) != nil || value == 0 || value > helper.MaxTokensLimit {
				return 0, fmt.Errorf("%s must be in 1..%d", key, helper.MaxTokensLimit)
			}
			maxOutput = max(maxOutput, int(value))
		}
	}
	if endpoint == "/v1beta/generateContent" {
		maxOutput = 0
		if _, exists := fields["generation_config"]; exists {
			return 0, errors.New("use generationConfig for Batch requests")
		}
		var config map[string]json.RawMessage
		if raw, ok := fields["generationConfig"]; ok {
			if common.HasDuplicateJSONKeys(raw) || common.Unmarshal(raw, &config) != nil {
				return 0, errors.New("invalid generationConfig")
			}
		}
		if _, ok := config["max_output_tokens"]; ok {
			return 0, errors.New("use maxOutputTokens for Batch requests")
		}
		if _, ok := config["candidate_count"]; ok {
			return 0, errors.New("use candidateCount for Batch requests")
		}
		if raw, ok := config["maxOutputTokens"]; ok {
			var value uint
			if common.Unmarshal(raw, &value) != nil || value == 0 || value > helper.MaxTokensLimit {
				return 0, errors.New("invalid maxOutputTokens")
			}
			maxOutput = int(value)
		}
		if raw, ok := config["candidateCount"]; ok {
			var n uint
			if common.Unmarshal(raw, &n) != nil || n != 1 {
				return 0, errors.New("Batch requires candidateCount=1")
			}
		}
	}
	if endpoint != "/v1/embeddings" && maxOutput == 0 {
		return 0, errors.New("an explicit output token limit is required for Batch")
	}
	return maxOutput, nil
}

func CreateBatch(c *gin.Context) {
	request := c.MustGet("batch_create_request").(BatchCreateRequest)
	userID := c.GetInt("id")
	tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	channel, err := model.CacheGetChannel(channelID)
	if err != nil {
		batchError(c, 503, "channel_unavailable", "selected channel is unavailable")
		return
	}
	if err := service.ValidateBatchChannelHeaders(channel); err != nil {
		batchError(c, 400, "invalid_channel_headers", err.Error())
		return
	}
	if !constant.UpdateTask {
		batchError(c, 503, "batch_worker_unavailable", "Batch polling must be enabled")
		return
	}
	info := &relaycommon.RelayInfo{OriginModelName: request.Model}
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: request.Model}
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		batchError(c, 400, "invalid_model_mapping", err.Error())
		return
	}
	upstreamModel := info.UpstreamModelName
	headers := make(map[string]string)
	for _, name := range []string{"anthropic-beta", "anthropic-version"} {
		if value := c.GetHeader(name); value != "" {
			headers[name] = value
		}
	}
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group := c.GetString("auto_group"); group != "" {
		usingGroup = group
	}
	info.UserId = userID
	info.UserGroup = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	info.TokenGroup = common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	info.UsingGroup = usingGroup
	info.RequestURLPath = request.Endpoint
	snapshot, err := service.CaptureBatchBilling(request.Model, common.GetContextKeyString(c, constant.ContextKeyUserGroup), usingGroup, headers)
	if err != nil {
		batchError(c, 503, "batch_pricing_unavailable", err.Error())
		return
	}
	items := make([]model.BatchItem, len(request.Requests))
	var total int64
	var inputBytes int
	for i, item := range request.Requests {
		var fields map[string]json.RawMessage
		if err := common.Unmarshal(item.Body, &fields); err != nil {
			batchError(c, 400, "invalid_request", err.Error())
			return
		}
		if _, exists := fields["model"]; exists {
			fields["model"], _ = common.Marshal(upstreamModel)
		}
		body, err := common.Marshal(fields)
		if err != nil {
			batchError(c, 400, "invalid_request", err.Error())
			return
		}
		body, err = relaycommon.ApplyParamOverride(body, common.GetContextKeyStringMap(c, constant.ContextKeyChannelParamOverride), relaycommon.BuildParamOverrideContext(info))
		if err != nil {
			batchError(c, 400, "invalid_channel_parameters", err.Error())
			return
		}
		inputBytes += len(body)
		if inputBytes > MaxBatchRequestBytes {
			batchError(c, 400, "batch_too_large", "prepared batch exceeds 16 MiB")
			return
		}
		maxOutput, err := validateBatchItem(body, upstreamModel, request.Endpoint)
		if err != nil {
			batchError(c, 400, "invalid_channel_parameters", err.Error())
			return
		}
		quota, err := service.EstimateBatchItem(c, snapshot, request.Endpoint, body, maxOutput)
		if err != nil {
			batchError(c, 400, "invalid_batch_price", err.Error())
			return
		}
		if total > common.MaxWalletQuota-int64(quota) {
			batchError(c, 400, "batch_quota_overflow", "batch reservation exceeds the wallet quota limit")
			return
		}
		total += int64(quota)
		items[i] = model.BatchItem{CustomID: item.CustomID, Ordinal: i, Body: string(item.Body), UpstreamBody: string(body), BillingStatus: "reserved", ReservedQuota: int64(quota)}
	}
	publicID := c.GetString("batch_public_id")
	keyHash := c.GetString("batch_key_hash")
	requestHash := c.GetString("batch_request_hash")
	encoded, err := common.Marshal(snapshot)
	if err != nil {
		batchError(c, 503, "pricing_unavailable", "cannot capture Batch pricing")
		return
	}
	configHash, err := service.BatchChannelConfigHash(channel)
	if err != nil {
		batchError(c, 503, "channel_unavailable", "cannot capture channel configuration")
		return
	}
	key := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	now := time.Now().Unix()
	job := model.BatchJob{PublicID: publicID, UserID: userID, TokenID: tokenID, IdempotencyKey: keyHash, RequestHash: requestHash, ChannelID: channelID, ChannelType: channel.Type, CredentialIndex: common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex), CredentialHash: fmt.Sprintf("%x", sha256.Sum256([]byte(key))), ChannelConfigHash: configHash, Model: request.Model, UsingGroup: usingGroup, UpstreamModel: upstreamModel, Endpoint: request.Endpoint, Status: "validating", BillingStatus: "reserved", SubmitState: "queued", BillingSnapshot: string(encoded), RequestCount: len(items), ReservedQuota: total, CreatedAt: now, UpdatedAt: now, ExpiresAt: now + 30*24*60*60}
	if err := model.CreateBatchReservation(&job, items); err != nil {
		// The unique owner/key constraint handles concurrent duplicate creates.
		var existing model.BatchJob
		if readErr := model.DB.Where("user_id = ? AND idempotency_key = ?", userID, keyHash).First(&existing).Error; readErr == nil && existing.RequestHash == requestHash {
			c.JSON(200, existing)
			return
		}
		if errors.Is(err, model.ErrBatchInsufficientQuota) {
			batchError(c, 403, "insufficient_quota", err.Error())
			return
		}
		batchError(c, 503, "storage_unavailable", "could not reserve Batch quota")
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func GetBatch(c *gin.Context) {
	job, err := model.GetBatchJob(c.GetInt("id"), c.Param("id"))
	if err != nil {
		batchError(c, 404, "not_found", "batch not found")
		return
	}
	c.JSON(200, job)
}

func ListBatches(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit < 1 || limit > 100 {
		batchError(c, 400, "invalid_limit", "limit must be in 1..100")
		return
	}
	var before int64
	if after := c.Query("after"); after != "" {
		job, err := model.GetBatchJob(c.GetInt("id"), after)
		if err != nil {
			batchError(c, 400, "invalid_cursor", "unknown batch cursor")
			return
		}
		before = job.ID
	}
	jobs, err := model.ListBatchJobs(c.GetInt("id"), before, limit+1)
	if err != nil {
		batchError(c, 503, "storage_unavailable", "batch storage unavailable")
		return
	}
	hasMore := len(jobs) > limit
	if hasMore {
		jobs = jobs[:limit]
	}
	c.JSON(200, gin.H{"object": "list", "data": jobs, "has_more": hasMore})
}

func GetBatchResults(c *gin.Context) {
	job, err := model.GetBatchJob(c.GetInt("id"), c.Param("id"))
	if err != nil {
		batchError(c, 404, "not_found", "batch not found")
		return
	}
	if time.Now().Unix() >= job.ExpiresAt {
		batchError(c, 410, "results_expired", "batch results have expired")
		return
	}
	if job.ResultCount == 0 && !batch.State(job.Status).Terminal() {
		batchError(c, 409, "results_pending", "batch results are not available yet")
		return
	}
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Cache-Control", "no-store")
	var cursor int64
	for {
		var item model.BatchItem
		err := model.DB.Where("batch_id = ? AND id > ? AND result_hash <> ''", job.ID, cursor).Order("id asc").First(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return
		}
		if err != nil {
			c.Abort()
			return
		}
		cursor = item.ID
		if _, err := io.WriteString(c.Writer, item.Result+"\n"); err != nil {
			return
		}
		c.Writer.Flush()
		if c.Request.Context().Err() != nil {
			return
		}
	}
}

func CancelBatch(c *gin.Context) {
	job, err := model.GetBatchJob(c.GetInt("id"), c.Param("id"))
	if err != nil {
		batchError(c, 404, "not_found", "batch not found")
		return
	}
	adapter, err := batch.ForChannel(job.ChannelType)
	if err != nil {
		batchError(c, 400, "unsupported_operation", err.Error())
		return
	}
	if _, err := adapter.Cancel(job.UpstreamID); errors.Is(err, batch.ErrUnsupported) {
		batchError(c, 400, "unsupported_operation", err.Error())
		return
	}
	if batch.State(job.Status).Terminal() {
		c.JSON(200, job)
		return
	}
	if job.SubmitState != "submitted" {
		batchError(c, 409, "submission_pending", "cannot cancel before upstream identity is confirmed")
		return
	}
	if err := model.DB.Model(job).Where("submit_state = ?", "submitted").Update("submit_state", "cancel_requested").Error; err != nil {
		batchError(c, 503, "storage_unavailable", "cannot request cancellation")
		return
	}
	c.JSON(202, gin.H{"id": job.PublicID, "status": "cancelling"})
}

func DeleteBatch(c *gin.Context) {
	job, err := model.GetBatchJob(c.GetInt("id"), c.Param("id"))
	if err != nil {
		batchError(c, 404, "not_found", "batch not found")
		return
	}
	if !batch.State(job.Status).Terminal() || job.BillingStatus != "settled" || job.PurchaseStatus != "settled" {
		batchError(c, 409, "batch_not_reconciled", "only terminal, financially settled batches can be deleted")
		return
	}
	if err := model.DB.Model(job).Update("deleted_at", time.Now().Unix()).Error; err != nil {
		batchError(c, 503, "storage_unavailable", "cannot delete batch")
		return
	}
	// Local deletion hides results immediately; immutable financial receipts
	// remain. Upstream cleanup is performed by the owning worker when supported.
	c.JSON(200, gin.H{"id": job.PublicID, "deleted": true})
}

// RecoverBatch verifies an operator-supplied upstream identity. RootAuth and
// its normal management audit apply at the route; request identities stay private.
func RecoverBatch(c *gin.Context) {
	var request struct {
		UpstreamID string `json:"upstream_id"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.UpstreamID == "" || len(request.UpstreamID) > 191 {
		batchError(c, 400, "invalid_request", "a bounded upstream_id is required")
		return
	}
	var job model.BatchJob
	if err := model.DB.Where("public_id = ? AND deleted_at = 0", c.Param("id")).First(&job).Error; err != nil {
		batchError(c, 404, "not_found", "batch not found")
		return
	}
	runnerID := "batch-recovery-" + common.GetRandomString(20)
	claimed, err := model.ClaimBatchJob(job.ID, runnerID)
	if err != nil || !claimed {
		batchError(c, 409, "batch_busy", "batch reconciliation is already in progress")
		return
	}
	defer model.DB.Model(&model.BatchJob{}).Where("id = ? AND lease_owner = ?", job.ID, runnerID).Updates(map[string]any{"lease_until": 0, "lease_owner": ""})
	if err := service.RecoverBatchIdentity(c.Request.Context(), &job, request.UpstreamID, runnerID); err != nil {
		batchError(c, 409, "recovery_unverified", "provider ownership could not be verified; Batch remains unchanged")
		return
	}
	c.JSON(200, gin.H{"success": true, "id": job.PublicID})
}

func ConfirmBatchPurchase(c *gin.Context) {
	var request struct {
		CostUSD  string `json:"cost_usd"`
		Evidence string `json:"evidence"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.CostUSD) > 128 || strings.TrimSpace(request.Evidence) == "" || len(request.Evidence) > 4096 {
		batchError(c, 400, "invalid_request", "bounded cost_usd and procurement evidence are required")
		return
	}
	cost, err := decimal.NewFromString(request.CostUSD)
	if err != nil || cost.IsNegative() || cost.Exponent() < -12 || cost.Exponent() > 12 || cost.Coefficient().BitLen() > 128 {
		batchError(c, 400, "invalid_purchase_cost", "invalid procurement USD amount")
		return
	}
	if err := model.ConfirmBatchPurchaseCost(c.Param("id"), cost.String(), request.Evidence); err != nil {
		batchError(c, 409, "purchase_reconciliation_conflict", "Batch is unavailable, not terminal, or already reconciled")
		return
	}
	c.JSON(200, gin.H{"success": true, "id": c.Param("id")})
}
