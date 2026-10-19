package openrouter

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
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
	durable := info.RelayMode == relayconstant.RelayModeRerank || info.OpenRouterCost.BufferedAudio || info.RelayMode == relayconstant.RelayModeAudioSpeech || info.RelayMode == relayconstant.RelayModeAudioTranscription
	if durable {
		info.PriceData.Quota = quota
		if err := service.BeginOpenRouterSyncTask(c, info, quota); err != nil {
			return err
		}
	}
	info.ForcePreConsume = true
	info.TieredBillingSnapshot = nil
	info.PriceData.FreeModel = false
	if info.Billing == nil {
		if apiErr := service.PreConsumeBilling(c, quota, info); apiErr != nil {
			return apiErr
		}
	} else if err := info.Billing.Reserve(quota); err != nil {
		return err
	}
	info.FinalPreConsumedQuota = info.Billing.GetPreConsumedQuota()
	if durable {
		return service.CommitOpenRouterSyncReservation(c, info)
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
	originalBody := body
	if operation == "audio/transcriptions" {
		body, err = transcriptionReservationPayload(c.Request.Header.Get("Content-Type"), body)
		if err != nil {
			return nil, err
		}
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
		tokens, countErr := service.CountRequestToken(c, meta, info)
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
			promptTokens := max(info.GetEstimatePromptTokens(), service.CountTextToken(string(body), models[0]))
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
	if len(models) == 1 {
		reserve := info.ChannelSetting.OpenRouterReserveUSD(models[0])
		if reserve != nil {
			info.OpenRouterCost.ApproximateReserveUSD = reserve
			info.OpenRouterCost.ApproximateReserveModel = models[0]
			if models[0] == "perplexity/sonar-deep-research" {
				info.OpenRouterCost.ApproximateResearchReserveUSD = common.GetPointer(*reserve)
			}
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
	}
	if err := ReserveCost(c, info, estimate); err != nil {
		return nil, err
	}
	if service.HasOpenRouterSyncTask(info) {
		headers := http.Header{}
		if err := adaptor.SetupRequestHeader(c, &headers, info); err != nil {
			return nil, err
		}
		overrides, err := channel.ResolveHeaderOverride(info, c)
		if err != nil {
			return nil, err
		}
		for key, value := range overrides {
			headers.Set(key, value)
		}
		if err := service.ConfigureOpenRouterSyncSubmission(info, strings.TrimRight(info.ChannelBaseUrl, "/")+"/v1/generation", headers); err != nil {
			return nil, err
		}
	}

	return bytes.NewReader(originalBody), nil
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
	if !info.OpenRouterCost.Final {
		return CostError("OpenRouter terminal usage.cost is missing or invalid")
	}
	return nil
}
