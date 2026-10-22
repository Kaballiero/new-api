package calculate_text_cost

import (
	"math"
	"sort"

	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/shopspring/decimal"
)

type Prices struct {
	Tool         func(string, string) float64
	InputAudio   func(string) float64
	DefaultRatio func(string) (float64, bool)
	QuotaUnit    func() float64
}
type Usecase struct {
	math   domain.QuotaMath
	engine domain.ExpressionEvaluator
	prices Prices
	warn   func(string)
}

func NewUsecase(math domain.QuotaMath, engine domain.ExpressionEvaluator, prices Prices, warn func(string)) Usecase {
	return Usecase{math: math, engine: engine, prices: prices, warn: warn}
}

type Input struct {
	Usage                                             *domain.Usage
	Prices                                            domain.Prices
	ModelName, Semantic                               string
	Estimate                                          int
	NativeCost, LegacyClaudeDerived, OpenRouterClaude bool
	Provider                                          domain.ProviderCost
	Tools                                             []domain.ToolCall
	Snapshot                                          *domain.TieredSnapshot
	Request                                           domain.RequestInput
	Reserved                                          int
	PriorClamp                                        *domain.Clamp
}

func (u Usecase) customRatio(model string, current float64) bool {
	ratio, ok := u.prices.DefaultRatio(model)
	return !ok || ratio != current
}
func (u Usecase) Execute(in Input) domain.TextCost {
	summary := u.summarize(in)
	usage := in.Usage
	if usage == nil && in.Snapshot != nil && u.engine.UsesFixedPricing(in.Snapshot.ExprString) {
		usage = &domain.Usage{PromptTokens: summary.PromptTokens, CompletionTokens: summary.CompletionTokens, TotalTokens: summary.TotalTokens}
	}
	if usage != nil && !in.Provider.Present {
		var used map[string]bool
		if in.Snapshot != nil {
			used = u.engine.UsedVars(in.Snapshot.ExprString)
		}
		applied, quota, result, _ := domain.SettleTiered(u.engine, in.Snapshot, in.Request, in.Reserved, domain.BuildTieredTokenParams(usage, summary.IsClaudeUsageSemantic, used), false)
		if applied {
			summary.TieredApplied = true
			summary.TieredResult = result
			if result != nil {
				domain.NoteClamp(&summary.Clamp, result.Clamp)
			}
			summary.Quota = u.composeTieredQuota(in.Snapshot, &summary, quota, result)
			summary.FixedPriceBilling = domain.FixedSettlement(in.Snapshot, result)
			if summary.FixedPriceBilling {
				summary.AudioInputPrice = 0
			}
		}
	}
	for i, item := range summary.ToolSurchargeItems {
		value := decimal.NewFromFloat(item.Price).Mul(decimal.NewFromInt(int64(item.Count))).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(u.prices.QuotaUnit()))
		summary.ToolSurchargeItems[i].DisplayQuota, _ = u.math.Decimal(value)
	}
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		value := decimal.NewFromFloat(summary.AudioInputPrice).Div(decimal.NewFromInt(1000000)).Mul(decimal.NewFromInt(int64(summary.AudioTokens))).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(u.prices.QuotaUnit()))
		summary.AudioDisplayQuota, _ = u.math.Decimal(value)
	}
	summary.CacheWriteTokens = domain.CacheWriteTokensTotal(summary)
	return summary
}
func (u Usecase) summarize(in Input) domain.TextCost {
	usage := in.Usage
	summary := domain.TextCost{
		ProviderCostBilling:  in.NativeCost,
		Clamp:                in.PriorClamp,
		ModelName:            in.ModelName,
		CompletionRatio:      in.Prices.CompletionRatio,
		CacheRatio:           in.Prices.CacheRatio,
		ImageRatio:           in.Prices.ImageRatio,
		ModelRatio:           in.Prices.ModelRatio,
		GroupRatio:           in.Prices.GroupRatio,
		BillingGroupRatio:    in.Prices.BillingGroupRatio,
		ModelPrice:           in.Prices.ModelPrice,
		CacheCreationRatio:   in.Prices.CacheCreationRatio,
		CacheCreationRatio5m: in.Prices.CacheCreation5mRatio,
		CacheCreationRatio1h: in.Prices.CacheCreation1hRatio,
		UsageSemantic:        in.Semantic,
	}
	summary.IsClaudeUsageSemantic = summary.UsageSemantic == "anthropic"

	if usage == nil {
		usage = &domain.Usage{
			PromptTokens:     in.Estimate,
			CompletionTokens: 0,
			TotalTokens:      in.Estimate,
		}
	}

	summary.PromptTokens = usage.PromptTokens
	summary.CompletionTokens = usage.CompletionTokens
	summary.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	summary.CacheTokens = usage.PromptTokensDetails.CachedTokens
	summary.CacheCreationTokens = usage.PromptTokensDetails.CacheCreationTokensTotal()
	summary.CacheCreationTokens5m = usage.ClaudeCacheCreation5mTokens
	summary.CacheCreationTokens1h = usage.ClaudeCacheCreation1hTokens
	summary.ImageTokens = usage.PromptTokensDetails.ImageTokens
	summary.AudioTokens = usage.PromptTokensDetails.AudioTokens
	if state := in.Provider; state.Present {
		summary.ProviderCostBilling = true
		if state.Final && state.Valid {
			quota, clamp, err := u.math.Provider(state.Basis, state.CostUSD)
			if err == nil {
				summary.Quota = quota
				summary.Clamp = clamp
			}
		}
		return summary
	}
	legacyClaudeDerived := in.LegacyClaudeDerived
	isOpenRouterClaudeBilling := in.OpenRouterClaude

	if isOpenRouterClaudeBilling {
		summary.PromptTokens -= summary.CacheTokens
		isUsingCustomSettings := in.Prices.UsePrice || u.customRatio(summary.ModelName, in.Prices.ModelRatio)
		if summary.CacheCreationTokens == 0 && in.Prices.CacheCreationRatio != 1 && usage.Cost != 0 && !isUsingCustomSettings {
			maybeCacheCreationTokens := domain.CalcOpenRouterCacheCreateTokens(*usage, in.Prices, u.prices.QuotaUnit(), u.math)
			if maybeCacheCreationTokens >= 0 && summary.PromptTokens >= maybeCacheCreationTokens {
				summary.CacheCreationTokens = maybeCacheCreationTokens
			}
		}
		summary.PromptTokens -= summary.CacheCreationTokens
	}

	dPromptTokens := decimal.NewFromInt(int64(summary.PromptTokens))
	dCacheTokens := decimal.NewFromInt(int64(summary.CacheTokens))
	dImageTokens := decimal.NewFromInt(int64(summary.ImageTokens))
	dAudioTokens := decimal.NewFromInt(int64(summary.AudioTokens))
	dCompletionTokens := decimal.NewFromInt(int64(summary.CompletionTokens))
	dCachedCreationTokens := decimal.NewFromInt(int64(summary.CacheCreationTokens))
	dCompletionRatio := decimal.NewFromFloat(summary.CompletionRatio)
	dCacheRatio := decimal.NewFromFloat(summary.CacheRatio)
	dImageRatio := decimal.NewFromFloat(summary.ImageRatio)
	dModelRatio := decimal.NewFromFloat(summary.ModelRatio)
	billingGroupRatio := summary.BillingGroupRatio
	if billingGroupRatio == 0 && summary.GroupRatio != 0 {
		billingGroupRatio = summary.GroupRatio
	}
	dGroupRatio := decimal.NewFromFloat(billingGroupRatio)
	dModelPrice := decimal.NewFromFloat(summary.ModelPrice)
	dCacheCreationRatio := decimal.NewFromFloat(summary.CacheCreationRatio)
	dCacheCreationRatio5m := decimal.NewFromFloat(summary.CacheCreationRatio5m)
	dCacheCreationRatio1h := decimal.NewFromFloat(summary.CacheCreationRatio1h)
	dQuotaPerUnit := decimal.NewFromFloat(u.prices.QuotaUnit())

	ratio := dModelRatio.Mul(dGroupRatio)
	summary.ToolCallSurchargeQuota = u.toolSurcharge(in.Tools, &summary)

	var audioInputQuota decimal.Decimal
	if !in.Prices.UsePrice {
		baseTokens := dPromptTokens

		var cachedTokensWithRatio decimal.Decimal
		if !dCacheTokens.IsZero() {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dCacheTokens)
			}
			cachedTokensWithRatio = dCacheTokens.Mul(dCacheRatio)
		}

		var cachedCreationTokensWithRatio decimal.Decimal
		hasSplitCacheCreationTokens := summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0
		if !dCachedCreationTokens.IsZero() || hasSplitCacheCreationTokens {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dCachedCreationTokens)
				cachedCreationTokensWithRatio = dCachedCreationTokens.Mul(dCacheCreationRatio)
			} else {
				remaining := max(summary.CacheCreationTokens-summary.CacheCreationTokens5m-summary.CacheCreationTokens1h, 0)
				cachedCreationTokensWithRatio = decimal.NewFromInt(int64(remaining)).Mul(dCacheCreationRatio)
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens5m)).Mul(dCacheCreationRatio5m))
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens1h)).Mul(dCacheCreationRatio1h))
			}
		}

		var imageTokensWithRatio decimal.Decimal
		if !dImageTokens.IsZero() {
			baseTokens = baseTokens.Sub(dImageTokens)
			imageTokensWithRatio = dImageTokens.Mul(dImageRatio)
		}

		if !dAudioTokens.IsZero() {
			summary.AudioInputPrice = u.prices.InputAudio(summary.ModelName)
			if summary.AudioInputPrice > 0 {
				baseTokens = baseTokens.Sub(dAudioTokens)
				audioInputQuota = decimal.NewFromFloat(summary.AudioInputPrice).
					Div(decimal.NewFromInt(1000000)).Mul(dAudioTokens).Mul(dGroupRatio).Mul(dQuotaPerUnit)
			}
		}

		// OpenAI cache-write usage reports unadjusted prefix counts, so
		// cached_tokens + cache_write_tokens can exceed prompt_tokens and the
		// remainder can go negative. Clamp at zero so overlap never turns into
		// a negative base charge.
		if baseTokens.IsNegative() {
			baseTokens = decimal.Zero
		}

		promptQuota := baseTokens.Add(cachedTokensWithRatio).Add(imageTokensWithRatio).Add(cachedCreationTokensWithRatio)
		completionQuota := dCompletionTokens.Mul(dCompletionRatio)
		quotaCalculateDecimal := promptQuota.Add(completionQuota).Mul(ratio)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(audioInputQuota)
		quotaCalculateDecimal = in.Prices.ApplyOtherRatiosToDecimal(quotaCalculateDecimal)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.ToolCallSurchargeQuota)

		if !ratio.IsZero() && quotaCalculateDecimal.LessThanOrEqual(decimal.Zero) {
			quotaCalculateDecimal = decimal.NewFromInt(1)
		}
		quota, clamp := u.math.Decimal(quotaCalculateDecimal)
		summary.Quota = quota
		domain.NoteClamp(&summary.Clamp, clamp)
	} else {
		quotaCalculateDecimal := dModelPrice.Mul(dQuotaPerUnit).Mul(dGroupRatio)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(audioInputQuota)
		quotaCalculateDecimal = in.Prices.ApplyOtherRatiosToDecimal(quotaCalculateDecimal)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.ToolCallSurchargeQuota)
		quota, clamp := u.math.Decimal(quotaCalculateDecimal)
		summary.Quota = quota
		domain.NoteClamp(&summary.Clamp, clamp)
	}

	if !summary.HasBillableUsage() {
		summary.Quota = 0
	} else if !ratio.IsZero() && summary.Quota == 0 {
		summary.Quota = 1
	}

	return summary
}

func (u Usecase) collectToolSurchargeItem(items []domain.ToolSurchargeItem, name string, count int, modelName string) []domain.ToolSurchargeItem {
	if count <= 0 {
		return items
	}
	price := u.prices.Tool(name, modelName)
	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return items
	}
	return append(items, domain.ToolSurchargeItem{
		Name:  name,
		Count: count,
		Price: price,
	})
}

func (u Usecase) mergeToolSurchargeItems(items []domain.ToolSurchargeItem) []domain.ToolSurchargeItem {
	if len(items) == 0 {
		return nil
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].Price < items[j].Price
		}
		return items[i].Name < items[j].Name
	})

	merged := items[:0]
	for _, item := range items {
		lastIndex := len(merged) - 1
		if lastIndex >= 0 &&
			merged[lastIndex].Name == item.Name &&
			merged[lastIndex].Price == item.Price {
			if item.Count > math.MaxInt-merged[lastIndex].Count {
				u.warn("tool surcharge call count overflow for " + item.Name)
				merged[lastIndex].Count = math.MaxInt
			} else {
				merged[lastIndex].Count += item.Count
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func (u Usecase) toolSurcharge(tools []domain.ToolCall, summary *domain.TextCost) decimal.Decimal {
	billingGroupRatio := summary.BillingGroupRatio
	if billingGroupRatio == 0 && summary.GroupRatio != 0 {
		billingGroupRatio = summary.GroupRatio
	}
	dGroupRatio := decimal.NewFromFloat(billingGroupRatio)
	dQuotaPerUnit := decimal.NewFromFloat(u.prices.QuotaUnit())

	var items []domain.ToolSurchargeItem

	for _, tool := range tools {
		items = u.collectToolSurchargeItem(items, tool.Name, tool.Count, summary.ModelName)
	}

	summary.ToolSurchargeItems = u.mergeToolSurchargeItems(items)
	var surcharge decimal.Decimal
	for _, item := range summary.ToolSurchargeItems {
		surcharge = surcharge.Add(decimal.NewFromFloat(item.Price).
			Mul(decimal.NewFromInt(int64(item.Count))).
			Div(decimal.NewFromInt(1000)).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit))
	}

	return surcharge
}

func (u Usecase) composeTieredQuota(snap *domain.TieredSnapshot, summary *domain.TextCost, tieredQuota int, tieredResult *domain.TieredResult) int {
	if summary.ToolCallSurchargeQuota.IsZero() {
		return tieredQuota
	}

	if tieredResult != nil {
		if snap != nil {
			quota, clamp := u.math.Decimal(decimal.NewFromFloat(tieredResult.ActualQuotaBeforeGroup).
				Mul(decimal.NewFromFloat(snap.GroupRatio)).
				Add(summary.ToolCallSurchargeQuota))
			domain.NoteClamp(&summary.Clamp, clamp)
			return quota
		}
	}

	// Saturate the final sum, not just the surcharge: tieredQuota can be near
	// MaxQuota and adding the surcharge could push the total past the
	// single-request quota policy bound.
	total, clamp := u.math.Decimal(
		decimal.NewFromInt(int64(tieredQuota)).Add(summary.ToolCallSurchargeQuota),
	)
	domain.NoteClamp(&summary.Clamp, clamp)
	return total
}
