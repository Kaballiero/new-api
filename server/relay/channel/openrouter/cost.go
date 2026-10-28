package openrouter

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	tokenusage "github.com/QuantumNous/new-api/gateway/tokenusage"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	billinggateway "github.com/QuantumNous/new-api/relay/billing"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/provider"
	"github.com/QuantumNous/new-api/relay/taskrecord"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// ReserveCost establishes the hold from a freshly prepared monetary estimate.
// It never executes administrator display expressions.
func ReserveCost(c *gin.Context, info *relaycommon.RelayInfo, estimate float64) error {
	basis := relaycommon.NewOpenRouterCostBasis(info, estimate)
	quota, clamp, err := basis.Quota(estimate)
	if err != nil {
		return err
	}
	if clamp != nil {
		info.QuotaClamp = clamp
		return clamp
	}
	if info.OpenRouterCost == nil {
		info.OpenRouterCost = &relaycommon.OpenRouterCostState{}
	}
	info.OpenRouterCost.Basis = basis
	durable := info.NativeImage != nil && info.NativeImage.ReserveUSD > 0 || info.RelayMode == relayconstant.RelayModeRerank || info.OpenRouterCost.ChatImage || info.OpenRouterCost.BufferedAudio || info.RelayMode == relayconstant.RelayModeAudioSpeech || info.RelayMode == relayconstant.RelayModeAudioTranscription
	if durable {
		info.PriceData.Quota = quota
		if err := taskrecord.BeginOpenRouterSyncTask(c, info, quota); err != nil {
			return err
		}
	}
	info.ForcePreConsume = true
	info.TieredBillingSnapshot = nil
	info.PriceData.FreeModel = false
	if info.Billing == nil {
		if apiErr := billinggateway.PreConsumeBilling(c, quota, info); apiErr != nil {
			return apiErr
		}
	} else if err := info.Billing.Reserve(quota); err != nil {
		return err
	}
	info.FinalPreConsumedQuota = info.Billing.GetPreConsumedQuota()
	if durable {
		return taskrecord.CommitOpenRouterSyncReservation(c, info)
	}
	return nil
}

func PrepareRequest(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, reader io.Reader, operation string) (io.Reader, error) {
	body, err := io.ReadAll(io.LimitReader(reader, (32<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 32<<20 {
		return nil, errors.New("OpenRouter request exceeds pricing inspection limit")
	}
	if operation != "audio/transcriptions" {
		body, err = provider.Normalize(body)
		if err != nil {
			return nil, err
		}
	}
	originalBody := body
	if operation == "audio/transcriptions" {
		body, err = transcriptionReservationPayload(c.Request.Header.Get("Content-Type"), body)
		if err != nil {
			return nil, err
		}
	}
	routing, err := provider.Extract(body)
	if err != nil {
		return nil, err
	}
	if err = provider.Validate(operation, routing); err != nil {
		return nil, err
	}
	var meta *types.TokenCountMeta
	switch operation {
	case "rerank":
		// Neither search units nor provider billing tokens are measured at admission.
	case "decisions":
		// Native decisions have no measured input/output token quantities at admission.
	case "audio/transcriptions":
		// File bytes are not text tokens or measured seconds.
	case "audio/speech":
		var request dto.AudioRequest
		if common.Unmarshal(body, &request) == nil {
			meta = request.GetTokenCountMeta()
		}
	case "responses":
		var request dto.OpenAIResponsesRequest
		if common.Unmarshal(body, &request) == nil {
			meta = request.GetTokenCountMeta()
		}
	case "embeddings":
		var request dto.EmbeddingRequest
		if common.Unmarshal(body, &request) == nil {
			meta = request.GetTokenCountMeta()
		}
	default:
		var request dto.GeneralOpenAIRequest
		if common.Unmarshal(body, &request) == nil {
			meta = request.GetTokenCountMeta()
		}
	}
	if meta != nil {
		tokens, countErr := tokenusage.CountRequestToken(c, meta, info)
		if countErr != nil {
			return nil, countErr
		}
		info.SetEstimatePromptTokens(tokens)
	}
	estimator := NewRequestEstimator()
	models, err := ModelsFromRequest(body)
	if err != nil {
		return nil, err
	}
	for _, model := range models {
		if info.ChannelMeta == nil {
			continue
		}
		source, configured := info.ChannelSetting.OpenRouterReservationSource[model]
		if configured && source != dto.OpenRouterReservationUpstream && source != dto.OpenRouterReservationConfiguredPrice {
			return nil, errors.New("invalid OpenRouter reservation source")
		}
		if source == dto.OpenRouterReservationConfiguredPrice && len(models) != 1 {
			return nil, errors.New("OpenRouter configured price reserve only supports one nonstream text chat response")
		}
	}
	for _, model := range models {
		if err := estimator.Discover(c, info, adaptor, model); err != nil {
			return nil, err
		}
	}
	var local *helper.ModelReservationEstimate
	if len(models) == 1 && info.ChannelMeta != nil {
		source := info.ChannelSetting.OpenRouterReservationSource[models[0]]
		if source == dto.OpenRouterReservationConfiguredPrice {
			completionTokens := 0
			if meta != nil {
				completionTokens = meta.MaxTokens
			}
			input := billingexpr.RequestInput{Body: body, Headers: info.RequestHeaders, EvaluatedAt: time.Now().UTC()}
			var unavailableVars []string
			promptTokens := max(info.GetEstimatePromptTokens(), tokenusage.CountTextToken(string(body), models[0]))
			if operation == "audio/transcriptions" || operation == "decisions" || operation == "rerank" {
				promptTokens = 0
				unavailableVars = []string{"p", "c", "len"}
			}
			value, err := helper.EstimateConfiguredModelReservation(info.GetBillingModelName(), promptTokens, completionTokens, input, unavailableVars...)
			if err != nil {
				return nil, err
			}
			local = &value
		}
	}
	var estimate float64
	if local != nil {
		estimate, err = estimator.EstimateWithLocalPrice(info, body, operation, local.USD)
	} else {
		estimate, err = estimator.Estimate(info, body, operation)
	}
	if err != nil {
		return nil, err
	}
	info.OpenRouterCost = &relaycommon.OpenRouterCostState{}
	if local != nil {
		info.OpenRouterCost.ConfiguredReserveUSD = common.GetPointer(local.USD)
		info.OpenRouterCost.ConfiguredReserveModel = local.Model
		info.OpenRouterCost.ConfiguredReserveExprHash = local.ExprHash
	}
	if reserve := info.ChannelSetting.OpenRouterReserveUSD(estimator.ReserveModel); reserve != nil {
		switch estimator.ReserveUse {
		case ReserveWholeHold:
			info.OpenRouterCost.ApproximateReserveUSD = reserve
			info.OpenRouterCost.ApproximateReserveModel = estimator.ReserveModel
			if estimator.ReserveModel == "perplexity/sonar-deep-research" {
				info.OpenRouterCost.ApproximateResearchReserveUSD = common.GetPointer(*reserve)
			}
		case ReserveAddOn:
			info.OpenRouterCost.AddOnReserveUSD = reserve
			info.OpenRouterCost.AddOnReserveModel = estimator.ReserveModel
		}
	}
	var output struct {
		Modalities []string `json:"modalities"`
	}
	_ = common.Unmarshal(body, &output)
	for _, modality := range output.Modalities {
		if modality == "audio" {
			info.OpenRouterCost.BufferedAudio = true
		}
		if modality == "image" && operation == "chat/completions" && info.RelayMode == relayconstant.RelayModeChatCompletions {
			info.OpenRouterCost.ChatImage = true
		}
	}
	if info.OpenRouterCost.BufferedAudio {
		info.OpenRouterCost.ChatImage = false
	}
	// Deferral relies on the OpenAI chat stream handler recording real delivery.
	info.OpenRouterCost.ChatText = operation == "chat/completions" && info.RelayMode == relayconstant.RelayModeChatCompletions && info.RelayFormat == types.RelayFormatOpenAI &&
		!info.OpenRouterCost.ChatImage && !info.OpenRouterCost.BufferedAudio
	if info.OpenRouterCost.ChatImage {
		info.OpenRouterCost.RoutingHash = fmt.Sprintf("%x", sha256.Sum256(routing))
	}
	if err := ReserveCost(c, info, estimate); err != nil {
		return nil, err
	}
	if relaycommon.HasOpenRouterSyncTask(info) {
		if err := configureGenerationLookup(c, info, adaptor); err != nil {
			return nil, err
		}
		if info.OpenRouterCost.ChatImage && !info.OpenRouterCost.BufferedAudio {
			info.OpenRouterCost.CaptureGeneration = func(id string) error {
				return taskrecord.RecordOpenRouterSyncGeneration(info, id)
			}
		}
	}

	return bytes.NewReader(originalBody), nil
}

// configureGenerationLookup binds the Task to this account's generation
// metadata endpoint with the same headers the relayed request used.
func configureGenerationLookup(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor) error {
	headers := http.Header{}
	if err := adaptor.SetupRequestHeader(c, &headers, info); err != nil {
		return err
	}
	overrides, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return err
	}
	for key, value := range overrides {
		headers.Set(key, value)
	}
	return taskrecord.ConfigureOpenRouterSyncSubmission(info, strings.TrimRight(info.ChannelBaseUrl, "/")+"/v1/generation", headers)
}

// DeferCostToGeneration hands a chat text hold whose response carries no
// usable usage.cost to a durable Task that settles it from generation
// metadata. Without a Task the caller still owns the hold and refunds it.
// A stream that delivered output keeps the hold even without a usable
// generation identity; the Task then requires manual reconciliation. A stream
// that delivered nothing is never deferred.
func DeferCostToGeneration(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor) error {
	state := info.OpenRouterCost
	if state == nil || !state.ChatText || info.Billing == nil {
		return errors.New("OpenRouter cost deferral requires a chat text reservation")
	}
	switch state.UnknownReason {
	case "missing_cost", "missing_terminal_cost", "invalid_cost":
	case "malformed_usage", "":
		// A stream can end or break without a terminal usage chunk.
		if !info.IsStream {
			return errors.New("OpenRouter cost is not recoverable from generation metadata")
		}
	case "upstream_error":
		// Only output forwarded before the error chunk reached the client.
		if !info.IsStream {
			return errors.New("OpenRouter upstream error delivered no output")
		}
	default:
		return errors.New("OpenRouter cost is not recoverable from generation metadata")
	}
	identified := state.GenerationID != "" && !state.IdentityConflict
	if info.IsStream && !state.ContentDelivered {
		return errors.New("OpenRouter stream delivered no output")
	}
	if !identified && !info.IsStream {
		return errors.New("OpenRouter generation identity is unavailable")
	}
	hold := info.Billing.GetPreConsumedQuota()
	info.PriceData.Quota = hold
	if err := taskrecord.BeginOpenRouterSyncTask(c, info, hold); err != nil {
		return err
	}
	logger.LogWarn(c, "OpenRouter cost deferred to generation lookup: unknown_reason=%s task_id=%s channel=%d reserve=%d generation_id_present=%t",
		state.UnknownReason, state.TaskID, info.ChannelId, hold, identified)
	if identified {
		// Correlation is durable before the hold moves, so an interrupted
		// transfer still leaves the generation for reconciliation.
		if err := taskrecord.RecordOpenRouterSyncGeneration(info, state.GenerationID); err != nil {
			return err
		}
	}
	if err := taskrecord.CommitOpenRouterSyncReservation(c, info); err != nil {
		return err
	}
	return configureGenerationLookup(c, info, adaptor)
}

// upstreamError carries the upstream message/code of a successful-status
// error envelope instead of reporting a missing cost.
func upstreamError(state *relaycommon.OpenRouterCostState) *types.NewAPIError {
	var raw any
	if len(state.UpstreamError) > 0 && common.Unmarshal(state.UpstreamError, &raw) == nil {
		if upstream := dto.GetOpenAIError(raw); upstream != nil && strings.TrimSpace(upstream.Message) != "" {
			return types.WithOpenAIError(*upstream, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
		}
	}
	return CostError("OpenRouter upstream reported an error")
}

// RejectUpstreamError reports a chat text upstream error envelope. Without a
// Task the controller refunds the hold; a Task retains it for delivered output.
// A nil apiErr is rebuilt from the observed envelope.
func RejectUpstreamError(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) *types.NewAPIError {
	if apiErr == nil {
		apiErr = upstreamError(info.OpenRouterCost)
	}
	var metadata struct {
		ProviderName string `json:"provider_name"`
	}
	if len(apiErr.Metadata) > 0 {
		_ = common.Unmarshal(apiErr.Metadata, &metadata)
	}
	logger.LogWarn(c, "OpenRouter upstream error in successful response: channel=%d reserve=%d task_id=%q generation_id=%q provider=%q",
		info.ChannelId, info.FinalPreConsumedQuota, info.OpenRouterCost.TaskID, info.OpenRouterCost.GenerationID, metadata.ProviderName)
	return apiErr
}

func CostError(reason string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(reason), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

// ValidateResponse runs before any nonstream client write. Cost is parsed
// independently from canonical token projection and fallback token estimates.
func ValidateResponse(resp *http.Response, info *relaycommon.RelayInfo) *types.NewAPIError {
	if resp == nil || resp.Body == nil {
		return CostError("OpenRouter response is missing")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<20)+1))
	_ = resp.Body.Close()
	if err != nil || len(body) > 64<<20 {
		return CostError("OpenRouter response cannot be inspected for authoritative cost")
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	info.OpenRouterCost.Observe(body, true)
	if info.OpenRouterCost.ChatText && info.OpenRouterCost.UnknownReason == "upstream_error" {
		return upstreamError(info.OpenRouterCost)
	}
	if info.OpenRouterCost.ChatImage && info.OpenRouterCost.IdentityConflict {
		return CostError("OpenRouter generation identity is unverified")
	}
	if !info.OpenRouterCost.Final && !info.OpenRouterCost.ChatImage {
		return CostError("OpenRouter terminal usage.cost is missing or invalid")
	}
	return nil
}
