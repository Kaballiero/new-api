package billing

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	routing "github.com/QuantumNous/new-api/gateway/routing"
	tokenusage "github.com/QuantumNous/new-api/gateway/tokenusage"
	"github.com/QuantumNous/new-api/internal/billing/calculate_text_cost"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

func (g *Gateway) TextCost(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) TextCostSummary {
	in := calculate_text_cost.Input{
		Usage:               CostUsage(usage),
		Prices:              costPrices(info.PriceData),
		ModelName:           info.GetBillingModelName(),
		Semantic:            usageSemanticFromUsage(info, usage),
		Estimate:            info.GetEstimatePromptTokens(),
		NativeCost:          info.NativeImage != nil && info.NativeImage.ActualCostUSD != nil,
		LegacyClaudeDerived: isLegacyClaudeDerivedOpenAIUsage(info, usage),
		OpenRouterClaude: info.HasChannelMeta() && info.ChannelType == constant.ChannelTypeOpenRouter &&
			usageSemanticFromUsage(info, usage) == "anthropic",
		Snapshot:   costSnapshot(info.TieredBillingSnapshot),
		Request:    costRequest(info.BillingRequestInput),
		Reserved:   info.FinalPreConsumedQuota,
		PriorClamp: costClamp(info.QuotaClamp),
	}
	if state := info.OpenRouterCost; state != nil {
		in.Provider = domain.ProviderCost{
			Present: true,
			Final:   state.Final,
			Valid:   state.Valid,
			CostUSD: state.CostUSD,
			Basis: domain.ProviderBasis{
				Version:             state.Basis.Version,
				EffectiveGroupRatio: state.Basis.EffectiveGroupRatio,
				QuotaPerUnit:        state.Basis.QuotaPerUnit,
				EstimatedUSD:        state.Basis.EstimatedUSD,
			},
		}
	}
	if info.ResponsesUsageInfo != nil {
		for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
			if tool != nil {
				in.Tools = append(in.Tools, domain.ToolCall{Name: name, Count: tool.CallCount})
			}
		}
	}
	if info.RelayMode != relayconstant.RelayModeResponses && strings.HasSuffix(in.ModelName, "search-preview") {
		in.Tools = append(in.Tools, domain.ToolCall{Name: dto.BuildInToolWebSearchPreview, Count: 1})
	}
	in.Tools = append(in.Tools, domain.ToolCall{Name: dto.BuildInToolWebSearch, Count: c.GetInt("claude_web_search_requests")})
	if c.GetBool("gemini_google_search_call") {
		in.Tools = append(in.Tools, domain.ToolCall{Name: dto.BuildInToolGoogleSearch, Count: 1})
	}
	result := g.costs.Text.Execute(in)
	if result.Clamp != in.PriorClamp {
		info.QuotaClamp = gatewayClamp(result.Clamp)
	}
	out := TextCostSummary{
		PromptTokens:     result.PromptTokens,
		CompletionTokens: result.CompletionTokens,

		CacheTokens:           result.CacheTokens,
		CacheCreationTokens:   result.CacheCreationTokens,
		CacheCreationTokens5m: result.CacheCreationTokens5m,
		CacheCreationTokens1h: result.CacheCreationTokens1h,
		ImageTokens:           result.ImageTokens,
		AudioTokens:           result.AudioTokens,
		ModelName:             result.ModelName,
		CompletionRatio:       result.CompletionRatio,
		CacheRatio:            result.CacheRatio,
		ImageRatio:            result.ImageRatio,
		ModelRatio:            result.ModelRatio,
		GroupRatio:            result.GroupRatio,

		ModelPrice:            result.ModelPrice,
		CacheCreationRatio:    result.CacheCreationRatio,
		CacheCreationRatio5m:  result.CacheCreationRatio5m,
		CacheCreationRatio1h:  result.CacheCreationRatio1h,
		Quota:                 result.Quota,
		IsClaudeUsageSemantic: result.IsClaudeUsageSemantic,

		AudioInputPrice: result.AudioInputPrice,

		TieredResult:      gatewayTiered(result.TieredResult),
		TieredApplied:     result.TieredApplied,
		AudioDisplayQuota: result.AudioDisplayQuota,
		CacheWriteTokens:  result.CacheWriteTokens,
		BillableUsage:     result.HasBillableUsage(),
	}
	out.ToolSurchargeItems = result.ToolSurchargeItems
	return out
}
func isLegacyClaudeDerivedOpenAIUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) bool {
	if relayInfo == nil || usage == nil {
		return false
	}
	if relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return false
	}
	if usage.UsageSource != "" || usage.UsageSemantic != "" {
		return false
	}
	return usage.ClaudeCacheCreation5mTokens > 0 || usage.ClaudeCacheCreation1hTokens > 0
}

func usageSemanticFromUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) string {
	if usage != nil && usage.UsageSemantic != "" {
		return usage.UsageSemantic
	}
	if relayInfo != nil && relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return "anthropic"
	}
	return "openai"
}

func PostTextConsumeQuota(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, extra []string) {
	defaultGateway.PostText(c, info, usage, extra)
}
func (g *Gateway) PostText(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, extra []string) {
	if relaycommon.HasOpenRouterSyncTask(info) {
		return
	}
	effective := tokenusage.EffectiveBillingUsage(usage)
	if usage != nil {
		routing.ObserveChannelAffinityUsageCacheByRelayFormat(c, effective, info.GetFinalRequestRelayFormat())
	}
	elapsed := time.Now().Unix() - info.StartTime.Unix()
	summary := g.TextCost(c, info, effective)
	summary.TokenName = c.GetString("token_name")
	summary.UseTimeSeconds = elapsed
	g.recordTextCost(c, info, usage, effective, summary, extra)
}
