// Package usage maps gateway observations to business-owned recording facts.
package usage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	routing "github.com/QuantumNous/new-api/gateway/routing"
	usageadapter "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	app "github.com/QuantumNous/new-api/internal/app/usage"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/record_consumption"
	"github.com/QuantumNous/new-api/internal/usage/record_failure"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/taskview"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// Gateway billing and Task/MJ presenters share one usage recording assembly.
// Construction is lazy and shared by every binary linking these entrypoints.
var recording = sync.OnceValue(app.New)

func Recording() app.Usecases {
	return recording()
}

func Facts(c *gin.Context, info *relaycommon.RelayInfo) domain.RequestFacts {
	out := domain.RequestFacts{ContextPresent: c != nil}
	if c != nil {
		if c.Request != nil && c.Request.URL != nil {
			out.URLPresent = true
			out.Path = c.Request.URL.Path
		}
		out.UseChannel = c.GetStringSlice("use_channel")
		out.MultiKey = common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		out.MultiKeyIndex = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		out.LocalCount = common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens)
		out.SystemPromptOverwritten = common.GetContextKeyBool(c, constant.ContextKeySystemPromptOverride)
	}
	if info == nil {
		return out
	}
	out.OriginModelName, out.BillingModelName = info.OriginModelName, info.GetBillingModelName()
	out.RequestURLPath = info.RequestURLPath
	out.ReasoningEffort = info.ReasoningEffort
	if info.HasChannelMeta() {
		out.IsModelMapped, out.UpstreamModelName = info.IsModelMapped, info.UpstreamModelName
	}
	out.IsStream = info.IsStream
	out.FirstResponseMilliseconds = float64(info.FirstResponseTime.UnixMilli() - info.StartTime.UnixMilli())
	out.FinalClaude = info.GetFinalRequestRelayFormat() == types.RelayFormatClaude
	for _, format := range info.RequestConversionChain {
		label := string(format)
		switch format {
		case types.RelayFormatOpenAI:
			label = "OpenAI Compatible"
		case types.RelayFormatClaude:
			label = "Claude Messages"
		case types.RelayFormatGemini:
			label = "Google Gemini"
		case types.RelayFormatOpenAIResponses:
			label = "OpenAI Responses"
		}
		out.Formats = append(out.Formats, label)
	}
	for _, diagnostic := range info.ConversionDiagnostics() {
		out.Diagnostics = append(out.Diagnostics, domain.Diagnostic{
			Code:     diagnostic.Code,
			Path:     diagnostic.Path,
			Message:  diagnostic.Message,
			Severity: string(diagnostic.Severity),
			From:     string(diagnostic.From),
			To:       string(diagnostic.To),
		})
	}
	out.DiagnosticsTruncated = info.ConversionDiagnosticsTruncated()
	out.ParamOverrideAudit = info.ParamOverrideAudit
	out.Stream = streamFacts(info)
	out.PriceData = priceFacts(info.PriceData)
	out.FX = domain.FX{
		Rate:               info.BillingFXRate,
		Factor:             info.BillingFXFactor,
		Source:             info.BillingFXSource,
		PublicationVersion: info.BillingFXPublicationVersion,
		EffectiveAt:        info.BillingFXEffectiveAt,
		FetchedAt:          info.BillingFXFetchedAt,
	}
	if p := info.OpenRouterCost; p != nil {
		out.OpenRouterCost = &domain.OpenRouterCostState{
			Basis:   billingdomain.ProviderBasis(p.Basis),
			CostUSD: p.CostUSD,
			Final:   p.Final,
			Sent:    p.Sent,

			IsBYOK:                        p.IsBYOK,
			UnknownReason:                 p.UnknownReason,
			CostSource:                    p.CostSource,
			GenerationID:                  p.GenerationID,
			ConfiguredReserveUSD:          p.ConfiguredReserveUSD,
			ConfiguredReserveModel:        p.ConfiguredReserveModel,
			ConfiguredReserveExprHash:     p.ConfiguredReserveExprHash,
			ApproximateReserveUSD:         p.ApproximateReserveUSD,
			ApproximateReserveModel:       p.ApproximateReserveModel,
			AddOnReserveUSD:               p.AddOnReserveUSD,
			AddOnReserveModel:             p.AddOnReserveModel,
			ApproximateResearchReserveUSD: p.ApproximateResearchReserveUSD,
		}
	}
	if n := info.NativeImage; n != nil {
		out.NativeImage = &domain.NativeImage{
			EstimatedCostUSD: n.EstimatedCostUSD,
			ActualCostUSD:    n.ActualCostUSD,
			ProviderTag:      n.ProviderTag,
			OutcomeUnknown:   n.OutcomeUnknown,
		}
	}
	out.HasSyncTask = relaycommon.HasOpenRouterSyncTask(info)
	out.FinalPreConsumedQuota = info.FinalPreConsumedQuota
	out.BillingSource, out.BillingPreference = info.BillingSource, info.UserSetting.BillingPreference
	out.SubscriptionId, out.SubscriptionPlanId = info.SubscriptionId, info.SubscriptionPlanId
	out.SubscriptionPreConsumed, out.SubscriptionPostDelta = info.SubscriptionPreConsumed, info.SubscriptionPostDelta
	out.SubscriptionAmountUsedAfterPreConsume = info.SubscriptionAmountUsedAfterPreConsume
	out.SubscriptionAmountTotal = info.SubscriptionAmountTotal
	out.SubscriptionPlanTitle = info.SubscriptionPlanTitle
	out.Tiered = Tiered(info, nil)
	if clamp := info.QuotaClamp; clamp != nil {
		out.Clamp = clampFacts(clamp)
		out.ClampWarning = quotaSaturationWarning(info)
	}
	return out
}

func priceFacts(p hosttypes.PriceData) domain.PriceFacts {
	return domain.PriceFacts{
		GroupRatioInfo: domain.GroupRatios{
			GroupRatio:        p.GroupRatioInfo.GroupRatio,
			GroupSpecialRatio: p.GroupRatioInfo.GroupSpecialRatio,
			HasSpecialRatio:   p.GroupRatioInfo.HasSpecialRatio,
		},
		EffectiveRatio: p.EffectiveGroupRatio(),
	}
}

func Tiered(info *relaycommon.RelayInfo, result *billingexpr.TieredResult) *domain.Tiered {
	if info == nil || info.TieredBillingSnapshot == nil {
		return nil
	}
	s := info.TieredBillingSnapshot
	out := &domain.Tiered{
		ExprString:           s.ExprString,
		EstimatedTier:        s.EstimatedTier,
		EstimatedBillingUnit: string(s.EstimatedBillingUnit),
		EstimatedFixedPrice:  s.EstimatedFixedPrice,
	}
	if result != nil {
		out.Actual = &domain.TieredResult{MatchedTier: result.MatchedTier, BillingUnit: string(result.BillingUnit), FixedPrice: result.FixedPrice}
		for _, rule := range result.RequestRules {
			out.Actual.RequestRules = append(out.Actual.RequestRules, billingdomain.RequestRuleTrace(rule))
		}
	}
	return out
}

func DeliveryFailure(c *gin.Context, info *relaycommon.RelayInfo) string {
	reason := ""
	if c != nil && c.Request != nil {
		switch c.Request.Context().Err() {
		case context.Canceled:
			reason = "context_canceled"
		case context.DeadlineExceeded:
			reason = "context_deadline_exceeded"
		}
	}
	return domain.DeliveryFailure(reason, streamFacts(info))
}

func UsagePath(local bool, usage *dto.Usage) string {
	if usage == nil || usage.BillingUsage == nil {
		return domain.UsagePath(local, false, false, "")
	}
	effective, ok := usage.BillingUsage.CanonicalUsage()
	if !ok {
		return domain.UsagePath(local, false, false, "")
	}
	return domain.UsagePath(local, true, usage.BillingUsage.Estimated, effective.UsageSemantic)
}

func AppendTaskBillingFX(other *usagerecord.LogOther, info *relaycommon.RelayInfo, p hosttypes.PriceData, stage string) {
	if info == nil {
		return
	}
	f := domain.RequestFacts{
		FX: domain.FX{
			Rate:               info.BillingFXRate,
			Factor:             info.BillingFXFactor,
			Source:             info.BillingFXSource,
			PublicationVersion: info.BillingFXPublicationVersion,
			EffectiveAt:        info.BillingFXEffectiveAt,
			FetchedAt:          info.BillingFXFetchedAt,
		},
	}
	Recording().Metadata.BillingFX(other, f, priceFacts(p), stage)
}

func AppendTaskClamp(other *usagerecord.LogOther, clamp *common.QuotaClamp) {
	if clamp != nil {
		Recording().Metadata.Clamp(other, clampFacts(clamp))
	}
}

func AppendTaskQuotaSaturation(c *gin.Context, info *relaycommon.RelayInfo, other *usagerecord.LogOther) {
	if info == nil || info.QuotaClamp == nil {
		return
	}
	Recording().Metadata.Clamp(other, clampFacts(info.QuotaClamp))
	var ctx context.Context
	if c != nil {
		ctx = c
	}
	logger.LogWarn(ctx, quotaSaturationWarning(info))
}

func AppendTaskTiered(other *usagerecord.LogOther, info *relaycommon.RelayInfo, result *billingexpr.TieredResult) {
	Recording().Metadata.Tiered(other, Tiered(info, result))
}

func MidjourneyOther(info *relaycommon.RelayInfo, p hosttypes.PriceData) *usagerecord.LogOther {
	f := domain.RequestFacts{PriceData: priceFacts(p)}
	if info != nil {
		f.RequestURLPath = info.RequestURLPath
		f.FX = domain.FX{
			Rate: info.BillingFXRate, Factor: info.BillingFXFactor,
			Source: info.BillingFXSource, PublicationVersion: info.BillingFXPublicationVersion,
			EffectiveAt: info.BillingFXEffectiveAt, FetchedAt: info.BillingFXFetchedAt,
		}
	}
	return Recording().Metadata.MidjourneyOther(f, domain.Prices{
		ModelPrice: p.ModelPrice, GroupRatio: p.GroupRatioInfo.GroupRatio,
		UserGroupRatio: p.GroupRatioInfo.GroupSpecialRatio,
	})
}

func RecordUpstreamFailure(c *gin.Context, channelID int, err *types.NewAPIError, info *relaycommon.RelayInfo) {
	admission, gateErr := Recording().Failure.Execute(c, record_failure.Input{Step: record_failure.AdmitUpstream})
	if gateErr != nil {
		logger.LogError(c, "error admitting upstream failure: "+gateErr.Error())
		return
	}
	if !admission.Allowed() || !types.IsRecordErrorLog(err) {
		return
	}
	// The caller gate is deliberately before metadata capture and elapsed time.
	f := Facts(c, info)
	execution := taskview.TaskExecutionSnapshotFromContext(c)
	var plugin *domain.Plugin
	if execution != nil {
		plugin = usageadapter.Plugin(execution.TaskPlugin)
	}
	start := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if start.IsZero() {
		start = time.Now()
	}
	id := domain.UsageRecord{
		UserID:         c.GetInt("id"),
		ChannelID:      channelID,
		ModelName:      c.GetString("original_model"),
		TokenName:      c.GetString("token_name"),
		TokenID:        c.GetInt("token_id"),
		Group:          c.GetString("group"),
		UseTimeSeconds: int(time.Since(start).Seconds()),
		IsStream:       common.GetContextKeyBool(c, constant.ContextKeyIsStream),
	}
	// The early upstream gate has already been captured above.
	if _, recordErr := Recording().Failure.Execute(RecordingContext(c, info), record_failure.Input{
		Step: record_failure.Persist, Admission: admission, Failure: domain.Failure{
			Record:     id,
			Content:    err.MaskSensitiveErrorWithStatusCode(),
			ErrorType:  string(err.GetErrorType()),
			ErrorCode:  string(err.GetErrorCode()),
			StatusCode: err.StatusCode,
			Request:    f,
			Plugin:     plugin,
		}}); recordErr != nil {
		logger.LogError(c, "error recording upstream failure: "+recordErr.Error())
	}
}

func RecordChannelTest(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	userID int,
	label string,
	quota int,
	u *dto.Usage,
	p hosttypes.PriceData,
	tiered *billingexpr.TieredResult,
	elapsed int,
) {
	f := Facts(c, info)
	f.Clamp, f.ClampWarning = nil, ""
	f.Tiered = nil
	if tiered != nil {
		f.Tiered = Tiered(info, tiered)
	}
	in := domain.Consumption{
		Kind: domain.ChannelTestConsumption,
		Record: domain.UsageRecord{
			UserID:           userID,
			ChannelID:        info.ChannelId,
			ModelName:        info.OriginModelName,
			TokenName:        label,
			Quota:            quota,
			PromptTokens:     u.PromptTokens,
			CompletionTokens: u.CompletionTokens,
			UseTimeSeconds:   elapsed,
			IsStream:         info.IsStream,
			Group:            info.UsingGroup,
		},
		ProbeLabel: label,
		Request:    f,
		Prices: domain.Prices{
			ModelRatio:      p.ModelRatio,
			GroupRatio:      p.GroupRatioInfo.GroupRatio,
			CompletionRatio: p.CompletionRatio,
			CacheTokens:     u.PromptTokensDetails.CachedTokens,
			CacheRatio:      p.CacheRatio,
			ModelPrice:      p.ModelPrice,
			UserGroupRatio:  p.GroupRatioInfo.GroupSpecialRatio,
		},
	}
	_, err := Recording().Record.Execute(RecordingContext(c, info), record_consumption.Input{
		Step: record_consumption.Persist, Consumption: in,
	})
	if err != nil {
		logger.LogError(c, "error recording channel test: "+err.Error())
	}
}

func streamFacts(info *relaycommon.RelayInfo) *domain.Stream {
	if info == nil {
		return nil
	}
	var stream *domain.Stream
	if ss := info.StreamStatus; ss != nil {
		stream = &domain.Stream{Normal: ss.IsNormalEnd(), EndReason: string(ss.EndReason), ErrorCount: ss.ErrorCount}
		if ss.EndError != nil {
			stream.EndError = ss.EndError.Error()
		}
		for _, e := range ss.Errors {
			stream.Errors = append(stream.Errors, e.Message)
		}
	}
	return stream
}

func quotaSaturationWarning(info *relaycommon.RelayInfo) string {
	clamp := info.QuotaClamp
	return fmt.Sprintf(
		"quota saturation on consume log: op=%s kind=%s original=%g clamped=%d user=%d model=%s",
		clamp.Op, clamp.Kind, clamp.Original, clamp.Clamped, info.UserId, info.GetBillingModelName(),
	)
}

// RecordingContext binds storage-only observations at the gateway boundary.
func RecordingContext(c *gin.Context, info *relaycommon.RelayInfo) context.Context {
	var details []byte
	if info != nil && info.OpenRouterCost != nil {
		details = info.OpenRouterCost.CostDetails
	}
	var ctx context.Context
	if c != nil {
		ctx = app.WithRequestObservation(c, usageadapter.ObserveLogRequest(c))
	}
	return app.WithLogMetadata(ctx, details, routing.ChannelAffinityLogInfo(c))
}

func clampFacts(clamp *common.QuotaClamp) *billingdomain.Clamp {
	if clamp == nil {
		return nil
	}
	return &billingdomain.Clamp{
		Op: clamp.Op, Kind: string(clamp.Kind), Original: clamp.Original, Clamped: clamp.Clamped,
	}
}
