package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// ToolSurchargeItem is one billable tool-call line for consume logs.
type ToolSurchargeItem struct {
	Name         string  `json:"name"`
	Count        int     `json:"count"`
	Price        float64 `json:"price"`
	DisplayQuota int     `json:"-"`
}

func appendToolSurchargeLogInfo(other *model.LogOther, items []ToolSurchargeItem) {
	if len(items) == 0 {
		return
	}
	other.SetPublic("tool_surcharges", items)
}

type TextCostSummary struct {
	PromptTokens                                                         int
	CompletionTokens                                                     int
	TotalTokens                                                          int
	CacheTokens                                                          int
	CacheCreationTokens                                                  int
	CacheCreationTokens5m                                                int
	CacheCreationTokens1h                                                int
	ImageTokens                                                          int
	AudioTokens                                                          int
	ModelName                                                            string
	TokenName                                                            string
	UseTimeSeconds                                                       int64
	CompletionRatio                                                      float64
	CacheRatio                                                           float64
	ImageRatio                                                           float64
	ModelRatio                                                           float64
	GroupRatio                                                           float64
	BillingGroupRatio                                                    float64
	ModelPrice                                                           float64
	CacheCreationRatio                                                   float64
	CacheCreationRatio5m                                                 float64
	CacheCreationRatio1h                                                 float64
	Quota                                                                int
	IsClaudeUsageSemantic                                                bool
	UsageSemantic                                                        string
	AudioInputPrice                                                      float64
	ToolSurchargeItems                                                   []ToolSurchargeItem
	ToolCallSurchargeQuota                                               decimal.Decimal
	TieredResult                                                         *billingexpr.TieredResult
	TieredApplied, FixedPriceBilling, ProviderCostBilling, BillableUsage bool
	AudioDisplayQuota, CacheWriteTokens                                  int
}

func contextTokenName(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString("token_name")
}
func PostTextConsumeQuota(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, extra []string) {
	BillingGateway.PostText(c, info, usage, extra)
}

// RecordTextCost owns the existing usage counters, settlement and log writes.
func RecordTextCost(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, originUsage, billingUsage *dto.Usage, summary TextCostSummary, extraContent []string) {
	if originUsage == nil {
		extraContent = append(extraContent, "上游无计费信息")
	}
	adminRejectReason := common.GetContextKeyString(ctx, constant.ContextKeyAdminRejectReason)
	tieredResult, tieredBillingApplied := summary.TieredResult, summary.TieredApplied

	for _, item := range summary.ToolSurchargeItems {
		extraContent = append(extraContent, fmt.Sprintf(
			"%s 调用 %d 次，调用花费 %s",
			item.Name,
			item.Count,
			logger.LogQuota(item.DisplayQuota),
		))
	}
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Audio Input 花费 %s", logger.LogQuota(summary.AudioDisplayQuota)))
	}

	if !summary.BillableUsage {
		extraContent = append(extraContent, "上游没有返回计费信息，无法扣费（可能是上游超时）")
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, summary.ModelName, relayInfo.FinalPreConsumedQuota))
	} else if relayInfo.OpenRouterCost == nil {
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, summary.Quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, summary.Quota)
	}

	if err := SettleBilling(ctx, relayInfo, summary.Quota); err != nil {
		logger.LogError(ctx, "error settling billing: "+err.Error())
	}

	var costOutcome *OpenRouterSettlementOutcome
	if relayInfo.OpenRouterCost != nil {
		if session, ok := relayInfo.Billing.(interface {
			OpenRouterSettlementOutcome() OpenRouterSettlementOutcome
		}); ok {
			result := session.OpenRouterSettlementOutcome()
			costOutcome = &result
			summary.Quota = result.FundingAppliedQuota
			if result.SettlementStatus != "settled" {
				extraContent = append(extraContent, "OpenRouter settlement requires manual repair")
				logger.LogWarn(ctx, fmt.Sprintf("OpenRouter settlement unresolved: status=%s target=%d funding_applied=%d token_applied=%d", result.SettlementStatus, result.TargetQuota, result.FundingAppliedQuota, result.TokenAppliedQuota))
			}
		}
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, summary.Quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, summary.Quota)
	}
	logModel := summary.ModelName
	if strings.HasPrefix(logModel, "gpt-4-gizmo") {
		logModel = "gpt-4-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}
	if strings.HasPrefix(logModel, "gpt-4o-gizmo") {
		logModel = "gpt-4o-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}

	logContent := strings.Join(extraContent, ", ")
	var other *model.LogOther
	if summary.IsClaudeUsageSemantic {
		other = GenerateClaudeOtherInfo(ctx, relayInfo,
			summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio,
			summary.CacheTokens, summary.CacheRatio,
			summary.CacheCreationTokens, summary.CacheCreationRatio,
			summary.CacheCreationTokens5m, summary.CacheCreationRatio5m,
			summary.CacheCreationTokens1h, summary.CacheCreationRatio1h,
			summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
		other.SetPublic("usage_semantic", "anthropic")
	} else {
		other = GenerateTextOtherInfo(ctx, relayInfo, summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio, summary.CacheTokens, summary.CacheRatio, summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	}
	if state := relayInfo.OpenRouterCost; state != nil {
		other.SetPublic("settlement_mode", "openrouter_actual_cost")
		other.SetAdmin("openrouter_cost_basis", state.Basis)
		other.SetAdmin("upstream_cost_usd", state.CostUSD)
		if state.CostSource != "" {
			other.SetAdmin("openrouter_cost_source", state.CostSource)
			other.SetAdmin("openrouter_generation_id", state.GenerationID)
		}
		other.SetAdmin("reserved_quota", relayInfo.FinalPreConsumedQuota)
		if state.ConfiguredReserveUSD != nil {
			other.SetAdmin("openrouter_reservation_source", "configured_price")
			other.SetAdmin("openrouter_configured_reserve_usd", *state.ConfiguredReserveUSD)
			other.SetAdmin("openrouter_configured_reserve_model", state.ConfiguredReserveModel)
			if state.ConfiguredReserveExprHash != "" {
				other.SetAdmin("openrouter_configured_reserve_expr_hash", state.ConfiguredReserveExprHash)
			}
		}
		if state.ApproximateReserveUSD != nil {
			other.SetAdmin("openrouter_admission_policy", "model_approximate_reserve")
			other.SetAdmin("openrouter_approximate_reserve_usd", *state.ApproximateReserveUSD)
			other.SetAdmin("openrouter_approximate_reserve_model", state.ApproximateReserveModel)
		}
		if state.AddOnReserveUSD != nil {
			other.SetAdmin("openrouter_admission_policy", "model_reserve_add_on")
			other.SetAdmin("openrouter_add_on_reserve_usd", *state.AddOnReserveUSD)
			other.SetAdmin("openrouter_add_on_reserve_model", state.AddOnReserveModel)
		}
		if state.ApproximateResearchReserveUSD != nil {
			other.SetAdmin("openrouter_admission_policy", "sonar_research_approximate_reserve")
			other.SetAdmin("openrouter_approximate_research_reserve_usd", *state.ApproximateResearchReserveUSD)
		}
		if costOutcome != nil {
			other.SetPublic("settlement_status", costOutcome.SettlementStatus)
			other.SetAdmin("openrouter_settlement", *costOutcome)
			if costOutcome.SettlementStatus == "settled" {
				other.SetAdmin("actual_quota", costOutcome.TargetQuota)
			} else {
				other.SetAdmin("requested_quota", costOutcome.TargetQuota)
			}
		}
	}
	appendUsageBillingPathForLog(other, common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens), originUsage)
	if adminRejectReason != "" {
		other.SetAdmin("reject_reason", adminRejectReason)
	}
	if summary.ImageTokens != 0 {
		other.SetPublic("image", true)
		other.SetPublic("image_ratio", summary.ImageRatio)
		other.SetPublic("image_output", summary.ImageTokens)
	}
	appendToolSurchargeLogInfo(other, summary.ToolSurchargeItems)
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		other.SetPublic("audio_input_seperate_price", true)
		other.SetPublic("audio_input_token_count", summary.AudioTokens)
		other.SetPublic("audio_input_price", summary.AudioInputPrice)
	}
	if summary.CacheCreationTokens > 0 {
		other.SetPublic("cache_creation_tokens", summary.CacheCreationTokens)
		other.SetPublic("cache_creation_ratio", summary.CacheCreationRatio)
	}
	if summary.CacheCreationTokens5m > 0 {
		other.SetPublic("cache_creation_tokens_5m", summary.CacheCreationTokens5m)
		other.SetPublic("cache_creation_ratio_5m", summary.CacheCreationRatio5m)
	}
	if summary.CacheCreationTokens1h > 0 {
		other.SetPublic("cache_creation_tokens_1h", summary.CacheCreationTokens1h)
		other.SetPublic("cache_creation_ratio_1h", summary.CacheCreationRatio1h)
	}
	cacheWriteTokens := summary.CacheWriteTokens
	if cacheWriteTokens > 0 {
		// cache_write_tokens: normalized cache creation total for UI display.
		// If split 5m/1h values are present, this is their sum; otherwise it falls back
		// to cache_creation_tokens.
		other.SetPublic("cache_write_tokens", cacheWriteTokens)
	}
	if relayInfo.GetFinalRequestRelayFormat() != types.RelayFormatClaude && billingUsage != nil && billingUsage.UsageSource != "" && billingUsage.InputTokens > 0 {
		// input_tokens_total: explicit normalized total input used by the usage log UI.
		// Only write this field when upstream/current conversion has already provided a
		// reliable total input value and tagged the usage source. Do not infer it from
		// prompt/cache fields here, otherwise old upstream payloads may be double-counted.
		other.SetPublic("input_tokens_total", billingUsage.InputTokens)
	}
	if tieredBillingApplied {
		InjectTieredBillingInfo(other, relayInfo, tieredResult)
	}
	deliveryFailure := deliveryFailureReason(ctx, relayInfo)
	if deliveryFailure != "" {
		other.SetPublic("delivery_status", "failed")
		other.SetPublic("delivery_reason", deliveryFailure)
	}

	attachQuotaSaturation(ctx, relayInfo, other)

	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     summary.PromptTokens,
		CompletionTokens: summary.CompletionTokens,
		ModelName:        logModel,
		TokenName:        summary.TokenName,
		Quota:            summary.Quota,
		Content:          logContent,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(summary.UseTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other,
	})
	if deliveryFailure != "" && constant.ErrorLogEnabled {
		model.RecordErrorLog(ctx, relayInfo.UserId, relayInfo.ChannelId, logModel, summary.TokenName,
			"response delivery failed: "+deliveryFailure, relayInfo.TokenId, int(summary.UseTimeSeconds),
			relayInfo.IsStream, relayInfo.UsingGroup, other)
	}
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, deliveryFailure == "", int64(summary.CompletionTokens))
	})
}
