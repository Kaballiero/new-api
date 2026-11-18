package openrouter

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/shopspring/decimal"
)

// TextCostEstimateUSD prices the tokens of a delivered text at the largest
// prompt and output tariffs of its reserve endpoints, capped by the reserve;
// zero without a tariff. Per-request and web search fees are not estimated.
func TextCostEstimateUSD(info *relaycommon.RelayInfo, usage *dto.Usage) float64 {
	reserve := info.OpenRouterCost.OutputReserve
	if reserve == nil {
		return 0
	}
	prompt := reserve.PromptPriceUSD.Mul(decimal.NewFromInt(int64(max(usage.PromptTokens, 0))))
	completion := reserve.OutputPriceUSD.Mul(decimal.NewFromInt(int64(max(usage.CompletionTokens, 0))))
	estimate := decimal.Min(prompt.Add(completion), decimal.NewFromFloat(info.OpenRouterCost.Basis.EstimatedUSD))
	if !estimate.IsPositive() {
		return 0
	}
	return estimate.InexactFloat64()
}
