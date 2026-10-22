package billing

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
)

func costClamp(c *common.QuotaClamp) *domain.Clamp {
	if c == nil {
		return nil
	}
	return &domain.Clamp{Op: c.Op, Kind: string(c.Kind), Original: c.Original, Clamped: c.Clamped}
}
func gatewayClamp(c *domain.Clamp) *common.QuotaClamp {
	if c == nil {
		return nil
	}
	return &common.QuotaClamp{Op: c.Op, Kind: common.QuotaClampKind(c.Kind), Original: c.Original, Clamped: c.Clamped}
}

type costFailure struct {
	message string
	cause   error
}

func (e *costFailure) Error() string { return e.message }
func (e *costFailure) Unwrap() error { return e.cause }
func costError(err error) error {
	var clamp *domain.Clamp
	if !errors.As(err, &clamp) {
		return err
	}
	cause := gatewayClamp(clamp)
	if err == clamp {
		return cause
	}
	return &costFailure{message: err.Error(), cause: cause}
}

func costDetails(d dto.InputTokenDetails) domain.TokenDetails {
	var cached *domain.CachedDetails
	if d.CachedTokensDetails != nil {
		cached = &domain.CachedDetails{TextTokens: d.CachedTokensDetails.TextTokens, AudioTokens: d.CachedTokensDetails.AudioTokens, ImageTokens: d.CachedTokensDetails.ImageTokens}
	}
	return domain.TokenDetails{TextTokens: d.TextTokens, AudioTokens: d.AudioTokens, ImageTokens: d.ImageTokens, CachedTokens: d.CachedTokens, CacheCreationTokens: d.CacheCreationTokensTotal(), CachedTokensDetails: cached}
}
func costUsage(u *dto.Usage) *domain.Usage {
	if u == nil {
		return nil
	}
	return &domain.Usage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: u.TotalTokens, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, PromptTokensDetails: costDetails(u.PromptTokensDetails), CompletionTokenDetails: domain.TokenDetails{TextTokens: u.CompletionTokenDetails.TextTokens, AudioTokens: u.CompletionTokenDetails.AudioTokens, ImageTokens: u.CompletionTokenDetails.ImageTokens}, ClaudeCacheCreation5mTokens: u.ClaudeCacheCreation5mTokens, ClaudeCacheCreation1hTokens: u.ClaudeCacheCreation1hTokens, UsageSemantic: u.UsageSemantic, UsageSource: u.UsageSource, Cost: u.Cost}
}
func realtimeUsage(u *dto.RealtimeUsage) *domain.Usage {
	if u == nil {
		return nil
	}
	return &domain.Usage{TotalTokens: u.TotalTokens, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, InputTokenDetails: costDetails(u.InputTokenDetails), OutputTokenDetails: domain.TokenDetails{TextTokens: u.OutputTokenDetails.TextTokens, AudioTokens: u.OutputTokenDetails.AudioTokens, ImageTokens: u.OutputTokenDetails.ImageTokens}, CacheDiscountUnavailable: u.CacheDiscountUnavailable}
}
func costPrices(p hosttypes.PriceData) domain.Prices {
	return domain.Prices{UsePrice: p.UsePrice, ModelPrice: p.ModelPrice, ModelRatio: p.ModelRatio, CompletionRatio: p.CompletionRatio, CacheRatio: p.CacheRatio, ImageRatio: p.ImageRatio, CacheCreationRatio: p.CacheCreationRatio, CacheCreation5mRatio: p.CacheCreation5mRatio, CacheCreation1hRatio: p.CacheCreation1hRatio, GroupRatio: p.GroupRatioInfo.GroupRatio, BillingGroupRatio: p.EffectiveGroupRatio(), OtherRatios: p.OtherRatios()}
}
func costSnapshot(s *billingexpr.BillingSnapshot) *domain.TieredSnapshot {
	if s == nil {
		return nil
	}
	return &domain.TieredSnapshot{BillingMode: s.BillingMode, ModelName: s.ModelName, ExprString: s.ExprString, ExprHash: s.ExprHash, GroupRatio: s.GroupRatio, EstimatedPromptTokens: s.EstimatedPromptTokens, EstimatedCompletionTokens: s.EstimatedCompletionTokens, EstimatedQuotaBeforeGroup: s.EstimatedQuotaBeforeGroup, EstimatedQuotaAfterGroup: s.EstimatedQuotaAfterGroup, EstimatedTier: s.EstimatedTier, EstimatedBillingUnit: string(s.EstimatedBillingUnit), EstimatedFixedPrice: s.EstimatedFixedPrice, QuotaPerUnit: s.QuotaPerUnit, ExprVersion: s.ExprVersion, TaskUsageBilling: s.TaskUsageBilling, UsageFacts: s.UsageFacts}
}
func costRequest(r *billingexpr.RequestInput) domain.RequestInput {
	if r == nil {
		return domain.RequestInput{}
	}
	return domain.RequestInput(*r)
}
func gatewayTiered(r *domain.TieredResult) *billingexpr.TieredResult {
	if r == nil {
		return nil
	}
	out := &billingexpr.TieredResult{BillingUnit: billingexpr.BillingUnit(r.BillingUnit), FixedPrice: r.FixedPrice, ActualQuotaBeforeGroup: r.ActualQuotaBeforeGroup, ActualQuotaAfterGroup: r.ActualQuotaAfterGroup, MatchedTier: r.MatchedTier, CrossedTier: r.CrossedTier, Clamp: gatewayClamp(r.Clamp)}
	if r.RequestRules != nil {
		out.RequestRules = make([]billingexpr.RequestRuleTrace, len(r.RequestRules))
		for i, rule := range r.RequestRules {
			out.RequestRules[i] = billingexpr.RequestRuleTrace{Cond: rule.Cond, Multiplier: rule.Multiplier, Matched: rule.Matched}
		}
	}
	return out
}
