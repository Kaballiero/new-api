package billing

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"

	"github.com/shopspring/decimal"
)

// CostMath delegates policy-bound conversions to their existing single owner.
type CostMath struct{}

func Clamp(value *common.QuotaClamp) *domain.Clamp {
	if value == nil {
		return nil
	}
	return &domain.Clamp{Op: value.Op, Kind: string(value.Kind), Original: value.Original, Clamped: value.Clamped}
}
func (CostMath) Decimal(value decimal.Decimal) (int, *domain.Clamp) {
	n, c := common.QuotaFromDecimalChecked(value)
	return n, Clamp(c)
}
func (CostMath) Round(value float64) (int, *domain.Clamp) {
	n, c := common.QuotaRoundChecked(value)
	return n, Clamp(c)
}
func (CostMath) RoundStrict(value float64) (int, error) { return billingexpr.QuotaRoundStrict(value) }
func (CostMath) Provider(b domain.ProviderBasis, value float64) (int, *domain.Clamp, error) {
	n, c, err := (common.OpenRouterCostBasis{Version: b.Version, EffectiveGroupRatio: b.EffectiveGroupRatio, QuotaPerUnit: b.QuotaPerUnit, EstimatedUSD: b.EstimatedUSD}).Quota(value)
	return n, Clamp(c), err
}

type Expression struct{}

func (Expression) UsedVars(expr string) map[string]bool { return billingexpr.UsedVars(expr) }
func (Expression) UsesFixedPricing(expr string) bool    { return billingexpr.UsesFixedPricing(expr) }
func (Expression) Compute(s *domain.TieredSnapshot, p domain.TokenParams, r domain.RequestInput) (domain.TieredResult, error) {
	snap := billingexpr.BillingSnapshot{BillingMode: s.BillingMode, ModelName: s.ModelName, ExprString: s.ExprString, ExprHash: s.ExprHash, GroupRatio: s.GroupRatio, EstimatedPromptTokens: s.EstimatedPromptTokens, EstimatedCompletionTokens: s.EstimatedCompletionTokens, EstimatedQuotaBeforeGroup: s.EstimatedQuotaBeforeGroup, EstimatedQuotaAfterGroup: s.EstimatedQuotaAfterGroup, EstimatedTier: s.EstimatedTier, EstimatedBillingUnit: billingexpr.BillingUnit(s.EstimatedBillingUnit), EstimatedFixedPrice: s.EstimatedFixedPrice, QuotaPerUnit: s.QuotaPerUnit, ExprVersion: s.ExprVersion, TaskUsageBilling: s.TaskUsageBilling, UsageFacts: s.UsageFacts}
	result, err := billingexpr.ComputeTieredQuotaWithRequest(&snap, billingexpr.TokenParams(p), billingexpr.RequestInput(r))
	out := domain.TieredResult{BillingUnit: string(result.BillingUnit), FixedPrice: result.FixedPrice, ActualQuotaBeforeGroup: result.ActualQuotaBeforeGroup, ActualQuotaAfterGroup: result.ActualQuotaAfterGroup, MatchedTier: result.MatchedTier, CrossedTier: result.CrossedTier, Clamp: Clamp(result.Clamp)}
	if result.RequestRules != nil {
		out.RequestRules = make([]domain.RequestRuleTrace, len(result.RequestRules))
		for i, rule := range result.RequestRules {
			out.RequestRules[i] = domain.RequestRuleTrace{Cond: rule.Cond, Multiplier: rule.Multiplier, Matched: rule.Matched}
		}
	}
	return out, err
}
