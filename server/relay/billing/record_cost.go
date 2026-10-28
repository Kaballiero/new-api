package billing

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	perfmetrics "github.com/QuantumNous/new-api/gateway/metrics"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/usage/count_consumption"
	usagedomain "github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/record_consumption"
	"github.com/QuantumNous/new-api/internal/usage/record_failure"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	usagegateway "github.com/QuantumNous/new-api/relay/usage"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

func (g *Gateway) recordTextCost(
	ctx *gin.Context, info *relaycommon.RelayInfo,
	originUsage, billingUsage *dto.Usage, summary TextCostSummary, extra []string,
) {
	recording := usagegateway.Recording()
	reject := common.GetContextKeyString(ctx, constant.ContextKeyAdminRejectReason)
	prepared, err := describeTextConsumption(ctx, originUsage, summary, extra)
	if err != nil {
		logger.LogError(ctx, "error describing consumption: "+err.Error())
		return
	}
	if !summary.BillableUsage {
		logUnbillableUsage(ctx, info, summary.ModelName)
	}
	count := count_consumption.Input{
		Moment: count_consumption.BeforeSettlement,
		UserID: info.UserId, ChannelID: info.ChannelId, QuotedQuota: summary.Quota,
		Billable: summary.BillableUsage, ProviderCost: info.OpenRouterCost != nil,
	}
	recording.Count.Execute(count)
	if err := g.Settle(ctx, info, summary.Quota); err != nil {
		logger.LogError(ctx, "error settling billing: "+err.Error())
	}
	quota, outcome := textSettlementOutcome(ctx, info, summary.Quota)
	summary.Quota = quota
	count.Moment, count.AppliedQuota = count_consumption.AfterSettlement, summary.Quota
	recording.Count.Execute(count)
	prepared = textConsumption(ctx, info, originUsage, billingUsage, summary, prepared, outcome, reject)
	failure := prepared.DeliveryFailure
	recordContext := usagegateway.RecordingContext(ctx, info)
	recorded, err := recording.Record.Execute(recordContext, record_consumption.Input{
		Step: record_consumption.Persist, Consumption: prepared,
	})
	if err != nil {
		logger.LogError(ctx, "error recording consumption: "+err.Error())
	} else if _, err := recording.Failure.Execute(recordContext, record_failure.Input{
		Step:    record_failure.Persist,
		Failure: usagedomain.Failure{Consumption: &recorded, DeliveryReason: failure},
	}); err != nil {
		logger.LogError(ctx, "error recording delivery failure: "+err.Error())
	}
	gopool.Go(func() { perfmetrics.RecordRelaySample(info, failure == "", int64(summary.CompletionTokens)) })
}

func describeTextConsumption(
	ctx *gin.Context, originUsage *dto.Usage, summary TextCostSummary, extra []string,
) (usagedomain.Consumption, error) {
	return usagegateway.Recording().Record.Execute(ctx, record_consumption.Input{
		Step: record_consumption.Describe,
		Consumption: usagedomain.Consumption{
			Kind:   usagedomain.TextConsumption,
			Record: usagedomain.UsageRecord{ModelName: summary.ModelName},
		},
		Extra: extra,
		Text: usagedomain.TextDescription{
			OriginMissing: originUsage == nil, Tools: summary.ToolSurchargeItems,
			AudioPrice: summary.AudioInputPrice, AudioTokens: summary.AudioTokens,
			AudioQuota: summary.AudioDisplayQuota, Billable: summary.BillableUsage,
		},
	})
}

func textSettlementOutcome(
	ctx *gin.Context, info *relaycommon.RelayInfo, quota int,
) (int, *billingdomain.Outcome) {
	var outcome *billingdomain.Outcome
	if info.OpenRouterCost != nil {
		if session, ok := info.Billing.(interface{ OpenRouterSettlementOutcome() billingdomain.Outcome }); ok {
			result := session.OpenRouterSettlementOutcome()
			outcome = &result
			quota = result.FundingAppliedQuota
			if result.SettlementStatus != "settled" {
				logger.LogWarn(ctx, fmt.Sprintf(
					"OpenRouter settlement unresolved: status=%s target=%d funding_applied=%d token_applied=%d",
					result.SettlementStatus, result.TargetQuota, result.FundingAppliedQuota, result.TokenAppliedQuota,
				))
			}
		}
	}
	return quota, outcome
}

func textConsumption(
	ctx *gin.Context, info *relaycommon.RelayInfo, originUsage, billingUsage *dto.Usage,
	summary TextCostSummary, prepared usagedomain.Consumption, outcome *billingdomain.Outcome, reject string,
) usagedomain.Consumption {
	facts := usagegateway.Facts(ctx, info)
	facts.Tiered = usagegateway.Tiered(info, summary.TieredResult)
	text := &usagedomain.TextFacts{
		Claude:       summary.IsClaudeUsageSemantic,
		UsagePath:    usagegateway.UsagePath(common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens), originUsage),
		RejectReason: reject, ImageTokens: summary.ImageTokens, ImageRatio: summary.ImageRatio,
		Tools: summary.ToolSurchargeItems, AudioInputPrice: summary.AudioInputPrice, AudioTokens: summary.AudioTokens,
		CacheCreationTokens:   summary.CacheCreationTokens,
		CacheCreationTokens5m: summary.CacheCreationTokens5m,
		CacheCreationTokens1h: summary.CacheCreationTokens1h,
		CacheCreationRatio:    summary.CacheCreationRatio,
		CacheCreationRatio5m:  summary.CacheCreationRatio5m,
		CacheCreationRatio1h:  summary.CacheCreationRatio1h,
		CacheWriteTokens:      summary.CacheWriteTokens, Outcome: outcome, TieredApplied: summary.TieredApplied,
	}
	if !facts.FinalClaude && billingUsage != nil && billingUsage.UsageSource != "" && billingUsage.InputTokens > 0 {
		text.InputTokensTotal = billingUsage.InputTokens
	}
	failure := usagegateway.DeliveryFailure(ctx, info)
	prepared.Record = usagedomain.UsageRecord{
		UserID: info.UserId, ChannelID: info.ChannelId, TokenID: info.TokenId,
		ModelName: summary.ModelName, TokenName: summary.TokenName,
		Quota: summary.Quota, PromptTokens: summary.PromptTokens, CompletionTokens: summary.CompletionTokens,
		UseTimeSeconds: int(summary.UseTimeSeconds), IsStream: info.IsStream, Group: info.UsingGroup,
	}
	prepared.Prices = usagedomain.Prices{
		ModelRatio: summary.ModelRatio, GroupRatio: summary.GroupRatio, CompletionRatio: summary.CompletionRatio,
		CacheTokens: summary.CacheTokens, CacheRatio: summary.CacheRatio, ModelPrice: summary.ModelPrice,
		UserGroupRatio: info.PriceData.GroupRatioInfo.GroupSpecialRatio,
	}
	prepared.Request, prepared.Text, prepared.DeliveryFailure = facts, text, failure
	return prepared
}

func (g *Gateway) recordAudioCost(
	ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, cost AudioCostResult, extra string,
) {
	elapsed := time.Now().Unix() - info.StartTime.Unix()
	tokenName := contextTokenName(ctx)
	model := info.GetBillingModelName()
	recording := usagegateway.Recording()
	prepared, err := recording.Record.Execute(ctx, record_consumption.Input{
		Step: record_consumption.Describe,
		Consumption: usagedomain.Consumption{
			Kind: usagedomain.AudioConsumption,
			Record: usagedomain.UsageRecord{
				UserID: info.UserId, ChannelID: info.ChannelId, TokenID: info.TokenId,
				ModelName: model, TokenName: tokenName, Quota: cost.Quota,
				PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
				UseTimeSeconds: int(elapsed), IsStream: info.IsStream, Group: info.UsingGroup,
			},
			Prices: usagedomain.Prices{
				ModelRatio: info.PriceData.ModelRatio, GroupRatio: info.PriceData.GroupRatioInfo.GroupRatio,
				ModelPrice: info.PriceData.ModelPrice, UserGroupRatio: info.PriceData.GroupRatioInfo.GroupSpecialRatio,
			},
			Audio: &usagedomain.AudioFacts{
				InputAudio: usage.PromptTokensDetails.AudioTokens, OutputAudio: usage.CompletionTokenDetails.AudioTokens,
				InputText: usage.PromptTokensDetails.TextTokens, OutputText: usage.CompletionTokenDetails.TextTokens,
			},
		},
		Extra: []string{extra},
		Audio: record_consumption.AudioDescription{
			InputModel: model, UsePrice: info.PriceData.UsePrice, Billable: cost.Billable,
		},
	})
	if err != nil {
		logger.LogError(ctx, "error describing audio consumption: "+err.Error())
		return
	}
	if !cost.Billable {
		logUnbillableUsage(ctx, info, model)
	}
	count := count_consumption.Input{
		Moment: count_consumption.BeforeSettlement, UserID: info.UserId, ChannelID: info.ChannelId,
		Billable: cost.Billable, QuotedQuota: cost.Quota,
	}
	recording.Count.Execute(count)
	if err := g.Settle(ctx, info, cost.Quota); err != nil {
		logger.LogError(ctx, "error settling billing: "+err.Error())
	}
	prepared.Request = usagegateway.Facts(ctx, info)
	prepared.Request.Tiered = usagegateway.Tiered(info, cost.TieredResult)
	if _, err := recording.Record.Execute(usagegateway.RecordingContext(ctx, info), record_consumption.Input{
		Step: record_consumption.Persist, Consumption: prepared,
	}); err != nil {
		logger.LogError(ctx, "error recording audio consumption: "+err.Error())
	}
	gopool.Go(func() { perfmetrics.RecordRelaySample(info, true, int64(usage.CompletionTokens)) })
}

func (g *Gateway) recordRealtimeCost(
	ctx *gin.Context, info *relaycommon.RelayInfo, model string,
	usage *dto.RealtimeUsage, cost AudioCostResult, extra string,
) error {
	elapsed := time.Now().Unix() - info.StartTime.Unix()
	tokenName := contextTokenName(ctx)
	recording := usagegateway.Recording()
	prepared, err := recording.Record.Execute(ctx, record_consumption.Input{
		Step: record_consumption.Describe,
		Consumption: usagedomain.Consumption{
			Kind: usagedomain.AudioConsumption,
			Record: usagedomain.UsageRecord{
				UserID: info.UserId, ChannelID: info.ChannelId, TokenID: info.TokenId,
				ModelName: model, TokenName: tokenName, Quota: cost.Quota,
				PromptTokens: usage.InputTokens, CompletionTokens: usage.OutputTokens,
				UseTimeSeconds: int(elapsed), IsStream: info.IsStream, Group: info.UsingGroup,
			},
			Prices: usagedomain.Prices{
				ModelRatio: info.PriceData.ModelRatio, GroupRatio: info.PriceData.GroupRatioInfo.GroupRatio,
				ModelPrice: info.PriceData.ModelPrice, UserGroupRatio: info.PriceData.GroupRatioInfo.GroupSpecialRatio,
			},
			Audio: &usagedomain.AudioFacts{
				Realtime:   true,
				InputAudio: usage.InputTokenDetails.AudioTokens, OutputAudio: usage.OutputTokenDetails.AudioTokens,
				InputText: usage.InputTokenDetails.TextTokens, OutputText: usage.OutputTokenDetails.TextTokens,
				CacheDiscountUnavailable: usage.CacheDiscountUnavailable,
				CacheDiscountReason:      usage.CacheDiscountUnavailableReason,
			},
		},
		Extra: []string{extra},
		Audio: record_consumption.AudioDescription{
			InputModel: info.OriginModelName, UsePrice: info.PriceData.UsePrice, Billable: cost.Billable,
		},
	})
	if err != nil {
		return err
	}
	if !cost.Billable {
		logUnbillableUsage(ctx, info, model)
	}
	count := count_consumption.Input{
		Moment: count_consumption.BeforeSettlement, UserID: info.UserId, ChannelID: info.ChannelId,
		Billable: cost.Billable, QuotedQuota: cost.Quota,
	}
	recording.Count.Execute(count)
	if err := g.SettleWss(ctx, info, cost.Quota); err != nil {
		return err
	}
	prepared.Request = usagegateway.Facts(ctx, info)
	prepared.Request.Tiered = usagegateway.Tiered(info, cost.TieredResult)
	_, err = recording.Record.Execute(usagegateway.RecordingContext(ctx, info), record_consumption.Input{
		Step: record_consumption.Persist, Consumption: prepared,
	})
	return err
}
func contextTokenName(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString("token_name")
}
func logUnbillableUsage(ctx *gin.Context, info *relaycommon.RelayInfo, model string) {
	logger.LogError(ctx, fmt.Sprintf(
		"total tokens is 0, cannot consume quota, userId %d, channelId %d, tokenId %d, model %s， pre-consumed quota %d",
		info.UserId, info.ChannelId, info.TokenId, model, info.FinalPreConsumedQuota,
	))
}
