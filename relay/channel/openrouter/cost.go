package openrouter

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
	info.OpenRouterCost = &relaycommon.OpenRouterCostState{Basis: basis}
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
	var meta *types.TokenCountMeta
	switch operation {
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
		if err := estimator.Discover(c, info, adaptor, model); err != nil {
			return nil, err
		}
	}
	estimate, err := estimator.Estimate(info, body, operation)
	if err != nil {
		return nil, err
	}
	if err := ReserveCost(c, info, estimate); err != nil {
		return nil, err
	}
	return bytes.NewReader(body), nil
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
