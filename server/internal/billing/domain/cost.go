package domain

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Cost facts are independent from provider DTOs, request contexts and storage.
type TokenDetails struct {
	TextTokens, AudioTokens, ImageTokens, CachedTokens int
	CacheCreationTokens                                int
	CachedTokensDetails                                *CachedDetails
}
type CachedDetails struct{ TextTokens, AudioTokens, ImageTokens *int }

func (d TokenDetails) CacheCreationTokensTotal() int { return d.CacheCreationTokens }

type Usage struct {
	PromptTokens, CompletionTokens, TotalTokens, InputTokens, OutputTokens int
	PromptTokensDetails, CompletionTokenDetails                            TokenDetails
	InputTokenDetails, OutputTokenDetails                                  TokenDetails
	ClaudeCacheCreation5mTokens, ClaudeCacheCreation1hTokens               int
	UsageSemantic, UsageSource                                             string
	Cost                                                                   any
	CacheDiscountUnavailable                                               bool
}
type Prices struct {
	UsePrice                                                        bool
	ModelPrice, ModelRatio, CompletionRatio, CacheRatio, ImageRatio float64
	CacheCreationRatio, CacheCreation5mRatio, CacheCreation1hRatio  float64
	GroupRatio, BillingGroupRatio                                   float64
	OtherRatios                                                     map[string]float64
}

func (p Prices) ApplyOtherRatiosToDecimal(value decimal.Decimal) decimal.Decimal {
	for _, ratio := range p.OtherRatios {
		if ratio != 1 {
			value = value.Mul(decimal.NewFromFloat(ratio))
		}
	}
	return value
}

type ToolCall struct {
	Name  string
	Count int
}
type ToolSurchargeItem struct {
	Name         string  `json:"name"`
	Count        int     `json:"count"`
	Price        float64 `json:"price"`
	DisplayQuota int     `json:"-"`
}
type Clamp struct {
	Op, Kind string
	Original float64
	Clamped  int
}

func (c *Clamp) Error() string {
	return fmt.Sprintf("quota conversion (%s) %s: original=%g, clamped=%d", c.Op, c.Kind, c.Original, c.Clamped)
}

type ProviderBasis struct {
	Version                                         string
	EffectiveGroupRatio, QuotaPerUnit, EstimatedUSD float64
}
type ProviderCost struct {
	Present, Final, Valid bool
	CostUSD               float64
	Basis                 ProviderBasis
}
type QuotaMath interface {
	Decimal(decimal.Decimal) (int, *Clamp)
	Round(float64) (int, *Clamp)
	RoundStrict(float64) (int, error)
	Provider(ProviderBasis, float64) (int, *Clamp, error)
}
type TokenParams struct{ P, C, Len, CR, CC, CC1h, Img, ImgO, AI, AO, ProviderCost float64 }
type RequestInput struct {
	EvaluatedAt time.Time
	Headers     map[string]string
	Body        []byte
	Usage       map[string]any
}
type TieredSnapshot struct {
	BillingMode, ModelName, ExprString, ExprHash     string
	GroupRatio                                       float64
	EstimatedPromptTokens, EstimatedCompletionTokens int
	EstimatedQuotaBeforeGroup                        float64
	EstimatedQuotaAfterGroup                         int
	EstimatedTier, EstimatedBillingUnit              string
	EstimatedFixedPrice                              *float64
	QuotaPerUnit                                     float64
	ExprVersion                                      int
	TaskUsageBilling                                 bool
	UsageFacts                                       map[string]any
}
type RequestRuleTrace struct {
	Cond       string
	Multiplier float64
	Matched    bool
}
type TieredResult struct {
	BillingUnit            string
	FixedPrice             *float64
	ActualQuotaBeforeGroup float64
	ActualQuotaAfterGroup  int
	MatchedTier            string
	RequestRules           []RequestRuleTrace
	CrossedTier            bool
	Clamp                  *Clamp
}
type ExpressionEvaluator interface {
	UsedVars(string) map[string]bool
	UsesFixedPricing(string) bool
	Compute(*TieredSnapshot, TokenParams, RequestInput) (TieredResult, error)
}

func NoteClamp(target **Clamp, value *Clamp) {
	if *target == nil {
		*target = value
	}
}
func FixedSettlement(snap *TieredSnapshot, result *TieredResult) bool {
	if result != nil {
		return result.BillingUnit == "request"
	}
	return snap != nil && snap.BillingMode == "tiered_expr" && snap.EstimatedBillingUnit == "request"
}
