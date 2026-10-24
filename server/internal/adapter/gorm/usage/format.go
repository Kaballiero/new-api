package usage

import (
	"encoding/base64"
	"strings"

	"github.com/QuantumNous/new-api/common"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/model"
)

func appendRelayAdmin(other *model.LogOther, relayInfo *domain.RequestFacts, metadata logMetadata) {
	if relayInfo == nil || !relayInfo.ContextPresent || other == nil {
		return
	}
	other.SetAdmin("use_channel", relayInfo.UseChannel)
	if state := relayInfo.OpenRouterCost; state != nil {
		other.SetAdmin("openrouter_cost_basis", storedCostBasis(state.Basis))
		other.SetAdmin("reserved_quota", relayInfo.FinalPreConsumedQuota)
		if len(metadata.CostDetails) > 0 {
			other.SetAdmin("openrouter_cost_details", metadata.CostDetails)
		}
		if state.IsBYOK != nil {
			other.SetAdmin("openrouter_is_byok", *state.IsBYOK)
		}
		if state.Final {
			other.SetAdmin("upstream_cost_usd", state.CostUSD)
		} else if state.Sent {
			other.SetAdmin("openrouter_financial_outcome", "unknown")
			other.SetAdmin("openrouter_unknown_reason", state.UnknownReason)
			if relayInfo.HasSyncTask {
				other.SetAdmin("openrouter_error_policy", "hold_for_reconciliation_without_retry")
			} else {
				other.SetAdmin("openrouter_error_policy", "refund_without_retry")
			}
		}
	}
	if native := relayInfo.NativeImage; native != nil {
		other.SetAdmin("image_estimated_cost_usd", native.EstimatedCostUSD)
		other.SetAdmin("image_provider", native.ProviderTag)
		if native.ActualCostUSD != nil {
			other.SetAdmin("image_actual_cost_usd", *native.ActualCostUSD)
		}
		if native.OutcomeUnknown {
			other.SetAdmin("image_financial_outcome", "unknown")
			if relayInfo.HasSyncTask {
				other.SetAdmin("image_error_policy", "hold_for_reconciliation_without_retry")
			} else {
				other.SetAdmin("image_error_policy", "refund_without_retry")
			}
		}
	}
	if billingModel := relayInfo.BillingModelName; billingModel != "" && billingModel != relayInfo.OriginModelName {
		other.SetAdmin("billing_model", billingModel)
	}
	if diagnostics := relayInfo.Diagnostics; len(diagnostics) > 0 {
		stored := make([]storedDiagnostic, len(diagnostics))
		for i, d := range diagnostics {
			stored[i] = storedDiagnostic(d)
		}
		other.SetAdmin("conversion_diagnostics", stored)
	}
	if relayInfo.DiagnosticsTruncated {
		other.SetAdmin("conversion_diagnostics_truncated", true)
	}
	if relayInfo.MultiKey {
		other.SetAdmin("is_multi_key", true)
		other.SetAdmin("multi_key_index", relayInfo.MultiKeyIndex)
	}
	if relayInfo.LocalCount {
		other.SetAdmin("local_count_tokens", true)
	}

	if metadata.Affinity != nil {
		other.SetAdmin("channel_affinity", metadata.Affinity)
	}
}

func baseOther(relayInfo domain.RequestFacts, p domain.Prices, metadata logMetadata) *model.LogOther {
	other := new(model.LogOther)
	appendBillingFX(other, &relayInfo, relayInfo.PriceData, "request")
	other.SetPublic("model_ratio", p.ModelRatio)
	other.SetPublic("group_ratio", p.GroupRatio)
	other.SetPublic("completion_ratio", p.CompletionRatio)
	other.SetPublic("cache_tokens", p.CacheTokens)
	other.SetPublic("cache_ratio", p.CacheRatio)
	other.SetPublic("model_price", p.ModelPrice)
	other.SetPublic("user_group_ratio", p.UserGroupRatio)
	other.SetPublic("frt", relayInfo.FirstResponseMilliseconds)
	if relayInfo.ReasoningEffort != "" {
		other.SetPublic("reasoning_effort", relayInfo.ReasoningEffort)
	}
	if relayInfo.IsModelMapped {
		other.SetPublic("is_model_mapped", true)
		other.SetPublic("upstream_model_name", relayInfo.UpstreamModelName)
	}

	isSystemPromptOverwritten := relayInfo.SystemPromptOverwritten
	if isSystemPromptOverwritten {
		other.SetPublic("is_system_prompt_overwritten", true)
	}

	appendRelayAdmin(other, &relayInfo, metadata)
	if relayInfo.Path != "" {
		other.SetPublic("request_path", relayInfo.Path)
	} else if relayInfo.RequestURLPath != "" {
		path, _, _ := strings.Cut(relayInfo.RequestURLPath, "?")
		other.SetPublic("request_path", path)
	}
	if len(relayInfo.Formats) > 0 {
		other.SetPublic("request_conversion", relayInfo.Formats)
	}
	if relayInfo.FinalClaude {
		other.SetPublic("claude", true)
	}
	appendSubscriptionUsage(&relayInfo, other)
	if len(relayInfo.ParamOverrideAudit) > 0 {
		other.SetPublic("po", relayInfo.ParamOverrideAudit)
	}
	appendStreamStatus(&relayInfo, other)
	return other
}

// appendBillingFX adds the captured basis and its pure/effective group
// projection only to the existing admin scope. Public log fields keep their
// historical pure group meaning.
func appendBillingFX(other *model.LogOther, relayInfo *domain.RequestFacts, priceData domain.PriceFacts, stage string) {
	if other == nil || relayInfo == nil {
		return
	}
	rate, err := billingdomain.PublishedUSD(billingdomain.FXPublication{
		Rates: map[string]float64{"USD": relayInfo.FX.Rate},
	})
	if err != nil {
		return
	}
	factor, err := billingdomain.ApplyBillingFX(1, relayInfo.FX.Factor)
	if err != nil || rate/billingdomain.FXRateDenomination != factor ||
		relayInfo.FX.Source == "" || relayInfo.FX.PublicationVersion < 0 ||
		relayInfo.FX.EffectiveAt <= 0 || relayInfo.FX.FetchedAt <= 0 {
		return
	}
	other.SetAdmin("billing_fx", map[string]any{
		"schema_version":      1,
		"source":              relayInfo.FX.Source,
		"rate":                relayInfo.FX.Rate,
		"publication_version": relayInfo.FX.PublicationVersion,
		"effective_at":        relayInfo.FX.EffectiveAt,
		"fetched_at":          relayInfo.FX.FetchedAt,
	})
	other.SetAdmin("billing_stage", stage)
	group := map[string]any{
		"pure_ratio":      priceData.GroupRatioInfo.GroupRatio,
		"effective_ratio": priceData.EffectiveRatio,
	}
	if priceData.GroupRatioInfo.HasSpecialRatio {
		group["special_ratio"] = priceData.GroupRatioInfo.GroupSpecialRatio
	}
	other.SetAdmin("billing_applied_group", group)
}

func appendSubscriptionUsage(relayInfo *domain.RequestFacts, other *model.LogOther) {
	if relayInfo == nil || other == nil {
		return
	}
	// billing_source: "wallet" or "subscription"
	if relayInfo.BillingSource != "" {
		other.SetPublic("billing_source", relayInfo.BillingSource)
	}
	if relayInfo.BillingPreference != "" {
		other.SetPublic("billing_preference", relayInfo.BillingPreference)
	}
	if relayInfo.BillingSource == "subscription" {
		if relayInfo.SubscriptionId != 0 {
			other.SetPublic("subscription_id", relayInfo.SubscriptionId)
		}
		if relayInfo.SubscriptionPreConsumed > 0 {
			other.SetPublic("subscription_pre_consumed", relayInfo.SubscriptionPreConsumed)
		}
		// post_delta: settlement delta applied after actual usage is known (can be negative for refund)
		if relayInfo.SubscriptionPostDelta != 0 {
			other.SetPublic("subscription_post_delta", relayInfo.SubscriptionPostDelta)
		}
		if relayInfo.SubscriptionPlanId != 0 {
			other.SetPublic("subscription_plan_id", relayInfo.SubscriptionPlanId)
		}
		if relayInfo.SubscriptionPlanTitle != "" {
			other.SetPublic("subscription_plan_title", relayInfo.SubscriptionPlanTitle)
		}
		// Compute "this request" subscription consumed + remaining
		consumed, usedFinal, remain := domain.SubscriptionUsage(*relayInfo)
		if relayInfo.SubscriptionAmountTotal > 0 {
			other.SetPublic("subscription_total", relayInfo.SubscriptionAmountTotal)
			other.SetPublic("subscription_used", usedFinal)
			other.SetPublic("subscription_remain", remain)
		}
		if consumed > 0 {
			other.SetPublic("subscription_consumed", consumed)
		}
		// Wallet quota is not deducted when billed from subscription.
		other.SetPublic("wallet_quota_deducted", 0)
	}
}

func appendStreamStatus(relayInfo *domain.RequestFacts, other *model.LogOther) {
	if relayInfo == nil || other == nil || !relayInfo.IsStream || relayInfo.Stream == nil {
		return
	}
	ss := relayInfo.Stream
	status := "ok"
	if !ss.Normal || ss.ErrorCount > 0 {
		status = "error"
	}
	streamInfo := map[string]any{
		"status":     status,
		"end_reason": ss.EndReason,
	}
	if ss.EndError != "" {
		streamInfo["end_error"] = ss.EndError
	}
	if ss.ErrorCount > 0 {
		streamInfo["error_count"] = ss.ErrorCount
		// Preserve [] rather than null when an error was counted without retained messages.
		messages := append([]string{}, ss.Errors...)
		streamInfo["errors"] = messages
	}
	other.SetPublic("stream_status", streamInfo)
}

func appendTiered(other *model.LogOther, in *domain.Tiered) {
	if other == nil {
		return
	}
	if in == nil {
		return
	}
	result := in.Actual
	other.SetPublic("billing_mode", "tiered_expr")
	other.SetPublic("expr_b64", base64.StdEncoding.EncodeToString([]byte(in.ExprString)))
	if result != nil {
		other.SetPublic("matched_tier", result.MatchedTier)
		if result.BillingUnit != "" {
			other.SetPublic("billing_unit", result.BillingUnit)
		}
		if result.FixedPrice != nil {
			other.SetPublic("fixed_price", *result.FixedPrice)
		}
		if len(result.RequestRules) > 0 {
			rules := make([]storedRule, len(result.RequestRules))
			for i, rule := range result.RequestRules {
				rules[i] = storedRule(rule)
			}
			other.SetPublic("request_rules", rules)
		}
	} else if in.EstimatedBillingUnit != "" {
		other.SetPublic("matched_tier", in.EstimatedTier)
		other.SetPublic("billing_unit", in.EstimatedBillingUnit)
		if in.EstimatedFixedPrice != nil {
			other.SetPublic("fixed_price", *in.EstimatedFixedPrice)
		}
	}
}

func consumptionOther(in domain.Consumption, metadata logMetadata) *model.LogOther {
	other := baseOther(in.Request, in.Prices, metadata)
	switch in.Kind {
	case domain.TextConsumption:
		appendTextConsumption(other, in.Request, in.Text)
	case domain.AudioConsumption:
		appendAudioConsumption(other, in.Request, in.Audio)
	case domain.ChannelTestConsumption:
		appendTiered(other, in.Request.Tiered)
	}
	if in.DeliveryFailure != "" {
		other.SetPublic("delivery_status", "failed")
		other.SetPublic("delivery_reason", in.DeliveryFailure)
	}
	appendClamp(other, in.Request.Clamp)
	return other
}

func appendTextConsumption(other *model.LogOther, request domain.RequestFacts, text *domain.TextFacts) {
	if text == nil {
		return
	}
	if text.Claude {
		other.SetPublic("claude", true)
		other.SetPublic("cache_creation_tokens", text.CacheCreationTokens)
		other.SetPublic("cache_creation_ratio", text.CacheCreationRatio)
		if text.CacheCreationTokens5m != 0 {
			other.SetPublic("cache_creation_tokens_5m", text.CacheCreationTokens5m)
			other.SetPublic("cache_creation_ratio_5m", text.CacheCreationRatio5m)
		}
		if text.CacheCreationTokens1h != 0 {
			other.SetPublic("cache_creation_tokens_1h", text.CacheCreationTokens1h)
			other.SetPublic("cache_creation_ratio_1h", text.CacheCreationRatio1h)
		}
		other.SetPublic("usage_semantic", "anthropic")
	}
	appendOpenRouterConsumption(other, request, text.Outcome)
	other.SetAdmin("usage_billing_path", text.UsagePath)
	if text.RejectReason != "" {
		other.SetAdmin("reject_reason", text.RejectReason)
	}
	if text.ImageTokens != 0 {
		other.SetPublic("image", true)
		other.SetPublic("image_ratio", text.ImageRatio)
		other.SetPublic("image_output", text.ImageTokens)
	}
	if len(text.Tools) > 0 {
		tools := make([]storedToolCharge, len(text.Tools))
		for i, tool := range text.Tools {
			tools[i] = storedToolCharge{Name: tool.Name, Count: tool.Count, Price: tool.Price}
		}
		other.SetPublic("tool_surcharges", tools)
	}
	if text.AudioInputPrice > 0 && text.AudioTokens > 0 {
		other.SetPublic("audio_input_seperate_price", true)
		other.SetPublic("audio_input_token_count", text.AudioTokens)
		other.SetPublic("audio_input_price", text.AudioInputPrice)
	}
	if text.CacheCreationTokens > 0 {
		other.SetPublic("cache_creation_tokens", text.CacheCreationTokens)
		other.SetPublic("cache_creation_ratio", text.CacheCreationRatio)
	}
	if text.CacheCreationTokens5m > 0 {
		other.SetPublic("cache_creation_tokens_5m", text.CacheCreationTokens5m)
		other.SetPublic("cache_creation_ratio_5m", text.CacheCreationRatio5m)
	}
	if text.CacheCreationTokens1h > 0 {
		other.SetPublic("cache_creation_tokens_1h", text.CacheCreationTokens1h)
		other.SetPublic("cache_creation_ratio_1h", text.CacheCreationRatio1h)
	}
	cacheWriteTokens := text.CacheWriteTokens
	if cacheWriteTokens > 0 {
		// cache_write_tokens: normalized cache creation total for UI display.
		// If split 5m/1h values are present, this is their sum; otherwise it falls back
		// to cache_creation_tokens.
		other.SetPublic("cache_write_tokens", cacheWriteTokens)
	}
	if text.InputTokensTotal > 0 {
		other.SetPublic("input_tokens_total", text.InputTokensTotal)
	}

	if text.TieredApplied {
		appendTiered(other, request.Tiered)
	}
}

func appendOpenRouterConsumption(other *model.LogOther, request domain.RequestFacts, outcome *billingdomain.Outcome) {
	if state := request.OpenRouterCost; state != nil {
		other.SetPublic("settlement_mode", "openrouter_actual_cost")
		other.SetAdmin("openrouter_cost_basis", storedCostBasis(state.Basis))
		other.SetAdmin("upstream_cost_usd", state.CostUSD)
		if state.CostSource != "" {
			other.SetAdmin("openrouter_cost_source", state.CostSource)
			other.SetAdmin("openrouter_generation_id", state.GenerationID)
		}
		other.SetAdmin("reserved_quota", request.FinalPreConsumedQuota)
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
		if outcome != nil {
			other.SetPublic("settlement_status", outcome.SettlementStatus)
			other.SetAdmin("openrouter_settlement", storedSettlement(*outcome))
			if outcome.SettlementStatus == "settled" {
				other.SetAdmin("actual_quota", outcome.TargetQuota)
			} else {
				other.SetAdmin("requested_quota", outcome.TargetQuota)
			}
		}
	}
}

func appendAudioConsumption(other *model.LogOther, request domain.RequestFacts, a *domain.AudioFacts) {
	if a == nil {
		return
	}
	if a.Realtime {
		other.SetPublic("ws", true)
	} else {
		other.SetPublic("audio", true)
	}
	other.SetPublic("audio_input", a.InputAudio)
	other.SetPublic("audio_output", a.OutputAudio)
	other.SetPublic("text_input", a.InputText)
	other.SetPublic("text_output", a.OutputText)
	other.SetPublic("audio_ratio", a.AudioRatio)
	other.SetPublic("audio_completion_ratio", a.AudioCompletionRatio)
	if request.Tiered != nil && request.Tiered.Actual != nil {
		appendTiered(other, request.Tiered)
	}
	if a.CacheDiscountUnavailable {
		other.SetAdmin("realtime_cache_discount", map[string]string{"scope": "session", "reason": a.CacheDiscountReason})
	}
}

func appendClamp(other *model.LogOther, clamp *billingdomain.Clamp) {
	if other != nil && clamp != nil {
		checked := common.QuotaClamp{
			Op: clamp.Op, Kind: common.QuotaClampKind(clamp.Kind), Original: clamp.Original, Clamped: clamp.Clamped,
		}
		other.SetAdmin("quota_saturation", checked.AuditMap())
	}
}

func appendPlugin(other *model.LogOther, in *domain.Plugin) {
	if other == nil || in == nil || in.Key == "" {
		return
	}
	p := map[string]any{"key": in.Key, "name": in.Name, "version": in.Version}
	if in.AuthorName != "" {
		author := map[string]any{"name": in.AuthorName}
		if in.AuthorURL != "" {
			author["url"] = in.AuthorURL
		}
		p["author"] = author
	}
	other.SetAdmin("task_plugin", p)
	other.SetRoot("task_plugin", map[string]any{
		"key": in.Key, "version": in.Version, "api_version": in.APIVersion, "generation": in.Generation,
	})
}

// The stored JSON structs belong to this serializer, not the usage domain.
type storedDiagnostic struct {
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type storedRule struct {
	Cond       string  `json:"cond"`
	Multiplier float64 `json:"multiplier"`
	Matched    bool    `json:"matched"`
}

// Metadata serves the remaining Task/Batch/MJ consumers through app composition.
type Metadata struct{}

func (Metadata) BillingFX(other *model.LogOther, facts domain.RequestFacts, prices domain.PriceFacts, stage string) {
	appendBillingFX(other, &facts, prices, stage)
}

func (Metadata) Clamp(other *model.LogOther, clamp *billingdomain.Clamp) {
	appendClamp(other, clamp)
}

func (Metadata) Tiered(other *model.LogOther, tiered *domain.Tiered) {
	appendTiered(other, tiered)
}

func (Metadata) Plugin(other *model.LogOther, plugin *domain.Plugin) {
	appendPlugin(other, plugin)
}

func (Metadata) MidjourneyOther(facts domain.RequestFacts, prices domain.Prices) *model.LogOther {
	other := new(model.LogOther)
	appendBillingFX(other, &facts, facts.PriceData, "submit")
	other.SetPublic("model_price", prices.ModelPrice)
	other.SetPublic("group_ratio", prices.GroupRatio)
	if facts.PriceData.GroupRatioInfo.HasSpecialRatio {
		other.SetPublic("user_group_ratio", prices.UserGroupRatio)
	}
	if facts.RequestURLPath != "" {
		path, _, _ := strings.Cut(facts.RequestURLPath, "?")
		other.SetPublic("request_path", path)
	}
	return other
}

// Billing supplies values; only this adapter owns their persisted JSON shape.
type storedToolCharge struct {
	Name  string  `json:"name"`
	Count int     `json:"count"`
	Price float64 `json:"price"`
}

type storedSettlement struct {
	TargetQuota         int    `json:"target_quota"`
	ReservedQuota       int    `json:"reserved_quota"`
	FundingAppliedQuota int    `json:"funding_applied_quota"`
	TokenAppliedQuota   int    `json:"token_applied_quota"`
	SettlementStatus    string `json:"settlement_status"`
	FailureStage        string `json:"failure_stage,omitempty"`
}

type storedCostBasis struct {
	Version             string  `json:"version"`
	EffectiveGroupRatio float64 `json:"effective_group_ratio"`
	QuotaPerUnit        float64 `json:"quota_per_unit"`
	EstimatedUSD        float64 `json:"estimated_usd"`
}

func (m Metadata) TaskIdentity(other *model.LogOther, identity domain.TaskIdentity) {
	if other == nil {
		return
	}
	if identity.TaskID != "" {
		other.SetPublic("task_id", identity.TaskID)
	}
	m.Plugin(other, identity.Plugin)
	if identity.UpstreamTaskID == "" && identity.NodeName == "" {
		return
	}
	if identity.UpstreamTaskID != "" {
		other.SetRoot("upstream_task_id", identity.UpstreamTaskID)
	}
	if identity.NodeName != "" {
		other.SetRoot("node_name", identity.NodeName)
	}
}
