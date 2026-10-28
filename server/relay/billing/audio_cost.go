package billing

import (
	"github.com/QuantumNous/new-api/internal/billing/calculate_audio_cost"
	"github.com/QuantumNous/new-api/internal/billing/calculate_realtime_cost"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

func (g *Gateway) AudioCost(info *relaycommon.RelayInfo, usage *dto.Usage) AudioCostResult {
	u := CostUsage(usage)
	price := domain.AudioPrice{
		InputDetails:  u.PromptTokensDetails,
		OutputDetails: u.CompletionTokenDetails,
		ModelName:     info.GetBillingModelName(),
		UsePrice:      info.PriceData.UsePrice,
		ModelRatio:    info.PriceData.ModelRatio,
		GroupRatio:    info.PriceData.EffectiveGroupRatio(),
	}
	prior := costClamp(info.QuotaClamp)
	result := g.costs.Audio.Execute(calculate_audio_cost.Input{Usage: *u, Price: price, Snapshot: costSnapshot(info.TieredBillingSnapshot), Request: costRequest(info.BillingRequestInput), Reserved: info.FinalPreConsumedQuota, PriorClamp: prior})
	if result.Clamp != prior {
		info.QuotaClamp = gatewayClamp(result.Clamp)
	}
	return AudioCostResult{Quota: result.Quota, TieredResult: gatewayTiered(result.TieredResult), Billable: result.Billable}
}
func (g *Gateway) RealtimeCost(info *relaycommon.RelayInfo, model string, usage *dto.RealtimeUsage) (AudioCostResult, error) {
	u := realtimeUsage(usage)
	price := domain.AudioPrice{
		ModelName:  model,
		UsePrice:   info.PriceData.UsePrice,
		ModelPrice: info.PriceData.ModelPrice,
		ModelRatio: info.PriceData.ModelRatio,
		GroupRatio: info.PriceData.GroupRatioInfo.GroupRatio,
		FXFactor:   info.BillingFXFactor,
	}
	if u != nil {
		price.InputDetails = u.InputTokenDetails
		price.OutputDetails = u.OutputTokenDetails
	}
	prior := costClamp(info.QuotaClamp)
	result, err := g.costs.Realtime.Execute(calculate_realtime_cost.Input{Usage: u, Price: price, Snapshot: costSnapshot(info.TieredBillingSnapshot), Request: costRequest(info.BillingRequestInput), Reserved: info.FinalPreConsumedQuota, PriorClamp: prior})
	if result.Clamp != prior {
		info.QuotaClamp = gatewayClamp(result.Clamp)
	}
	return AudioCostResult{Quota: result.Quota, TieredResult: gatewayTiered(result.TieredResult), Billable: result.Billable}, costError(err)
}

func PostAudioConsumeQuota(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, extra string) {
	defaultGateway.PostAudio(c, info, usage, extra)
}
func (g *Gateway) PostAudio(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, extra string) {
	if info.OpenRouterCost != nil {
		g.PostText(c, info, usage, []string{extra})
		return
	}
	if usage == nil {
		usage = &dto.Usage{PromptTokens: info.GetEstimatePromptTokens(), TotalTokens: info.GetEstimatePromptTokens()}
	}
	cost := g.AudioCost(info, usage)
	g.recordAudioCost(c, info, usage, cost, extra)
}
func PostWssConsumeQuota(c *gin.Context, info *relaycommon.RelayInfo, model string, usage *dto.RealtimeUsage, extra string) error {
	return defaultGateway.PostWss(c, info, model, usage, extra)
}
func (g *Gateway) PostWss(c *gin.Context, info *relaycommon.RelayInfo, model string, usage *dto.RealtimeUsage, extra string) error {
	cost, err := g.RealtimeCost(info, model, usage)
	if err != nil {
		return err
	}
	return g.recordRealtimeCost(c, info, model, usage, cost, extra)
}
