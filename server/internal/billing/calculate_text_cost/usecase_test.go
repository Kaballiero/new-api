package calculate_text_cost

import (
	"math"
	"testing"

	adapter "github.com/QuantumNous/new-api/internal/adapter/billing"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func textUsecase() Usecase {
	return NewUsecase(adapter.CostMath{}, adapter.Expression{}, Prices{QuotaUnit: func() float64 { return 500000 }, Tool: func(name, model string) float64 {
		switch name {
		case "web":
			return 10
		case "file":
			return 2.5
		}
		return 0
	}, InputAudio: func(string) float64 { return 1 }, DefaultRatio: func(string) (float64, bool) { return 1, true }}, func(string) {})
}
func TestTextCostAmounts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input Input
		want  int
	}{
		{"fractional completion", Input{Usage: &domain.Usage{PromptTokens: 100, CompletionTokens: 20}, Prices: domain.Prices{ModelRatio: 1.25, CompletionRatio: 2.5, GroupRatio: 1, BillingGroupRatio: 1}}, 188},
		{"overlapping cache prefixes", Input{Usage: &domain.Usage{PromptTokens: 10, CompletionTokens: 20, PromptTokensDetails: domain.TokenDetails{CachedTokens: 8, CacheCreationTokens: 9}}, Prices: domain.Prices{ModelRatio: 1, CompletionRatio: 1, GroupRatio: 1, BillingGroupRatio: 1, CacheRatio: 0.1, CacheCreationRatio: 1.25}}, 32},
		{"tools after multipliers", Input{Usage: &domain.Usage{}, Tools: []domain.ToolCall{{Name: "web", Count: 1}}, Prices: domain.Prices{ModelRatio: 1, CompletionRatio: 1, GroupRatio: 1, BillingGroupRatio: 1, OtherRatios: map[string]float64{"n": 3}}}, 5000},
		{"explicit provider zero", Input{Usage: &domain.Usage{PromptTokens: 100}, Provider: domain.ProviderCost{Present: true, Final: true, Valid: true, CostUSD: 0, Basis: domain.ProviderBasis{QuotaPerUnit: 500000, EffectiveGroupRatio: 1}}}, 0},
		{"terminal clamp", Input{Usage: &domain.Usage{PromptTokens: math.MaxInt32, CompletionTokens: 20}, Prices: domain.Prices{ModelRatio: 1, CompletionRatio: 1, GroupRatio: 1, BillingGroupRatio: 1}}, math.MaxInt32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := textUsecase().Execute(tc.input)
			assert.Equal(t, tc.want, result.Quota)
			if tc.name == "terminal clamp" {
				require.NotNil(t, result.Clamp)
			}
		})
	}
}
func TestTieredToolComposition(t *testing.T) {
	for _, tc := range []struct {
		name                string
		reserved, estimated int
		result              *domain.TieredResult
		fee                 float64
		want                int
	}{
		{"one final round", 0, 0, &domain.TieredResult{ActualQuotaBeforeGroup: 0.505, ActualQuotaAfterGroup: 1}, 0.5, 1},
		{"tool fees", 0, 0, &domain.TieredResult{ActualQuotaBeforeGroup: 1000, ActualQuotaAfterGroup: 1000}, 13000, 14000},
		{"estimated fallback", 1250, 1250, nil, 12500, 13750},
		{"reservation fallback", 2000, 1250, nil, 12500, 14500},
		{"sum saturation", math.MaxInt32, 0, nil, 1, math.MaxInt32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summary := domain.TextCost{ToolCallSurchargeQuota: decimal.NewFromFloat(tc.fee)}
			n := textUsecase().composeTieredQuota(&domain.TieredSnapshot{GroupRatio: 1}, &summary, tc.reserved, tc.result)
			assert.Equal(t, tc.want, n)
		})
	}
}
func TestTextFixedNilUsageAndActualTokenBranch(t *testing.T) {
	for _, tc := range []struct {
		expr  string
		fixed bool
		want  int
	}{
		{`tier("request",fixed(0))`, true, 0},
		{`len<=10 ? tier("request",fixed(0.00002)) : tier("tokens",p*2)`, false, 23},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			in := Input{Estimate: 23, Snapshot: &domain.TieredSnapshot{BillingMode: "tiered_expr", ExprString: tc.expr, ExprHash: billingexpr.ExprHashString(tc.expr), QuotaPerUnit: 500000, GroupRatio: 1}}
			result := textUsecase().Execute(in)
			assert.Equal(t, tc.want, result.Quota)
			assert.Equal(t, tc.fixed, result.FixedPriceBilling)
			assert.True(t, result.TieredApplied)
		})
	}
}
func TestToolMergeOverflow(t *testing.T) {
	u := textUsecase()
	result := u.mergeToolSurchargeItems([]domain.ToolSurchargeItem{{Name: "web", Count: math.MaxInt, Price: 10}, {Name: "web", Count: 1, Price: 10}})
	require.Len(t, result, 1)
	assert.Equal(t, math.MaxInt, result[0].Count)
}

// Replace the former randomized ratio/expression parity probes with explicit
// cache/image/semantic boundaries and exact published quota-unit expectations.
func TestRatioAndTieredParityAtKnownUsage(t *testing.T) {
	for _, tc := range []struct {
		name, semantic, expr string
		usage                domain.Usage
		want                 int
	}{
		{"OpenAI cache", "openai", `tier("base",p*2.5+c*15+cr*0.25)`, domain.Usage{PromptTokens: 1000, CompletionTokens: 500, PromptTokensDetails: domain.TokenDetails{CachedTokens: 200}}, 4775},
		{"Claude cache", "anthropic", `tier("base",p*2.5+c*15+cr*0.25)`, domain.Usage{PromptTokens: 800, CompletionTokens: 500, PromptTokensDetails: domain.TokenDetails{CachedTokens: 200}}, 4775},
		{"image input", "openai", `tier("base",p*2.5+c*15+img*5)`, domain.Usage{PromptTokens: 1000, CompletionTokens: 500, PromptTokensDetails: domain.TokenDetails{ImageTokens: 200}}, 5250},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{Usage: &tc.usage, Semantic: tc.semantic, Prices: domain.Prices{ModelRatio: 1.25, CompletionRatio: 6, CacheRatio: 0.1, ImageRatio: 2, GroupRatio: 1, BillingGroupRatio: 1}}
			ratio := textUsecase().Execute(in)
			in.Snapshot = &domain.TieredSnapshot{BillingMode: "tiered_expr", ExprString: tc.expr, ExprHash: billingexpr.ExprHashString(tc.expr), QuotaPerUnit: 500000, GroupRatio: 1}
			tiered := textUsecase().Execute(in)
			assert.Equal(t, tc.want, ratio.Quota)
			assert.Equal(t, ratio.Quota, tiered.Quota)
		})
	}
}
