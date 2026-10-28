package domain

import "github.com/shopspring/decimal"

type TextCost struct {
	PromptTokens           int
	CompletionTokens       int
	TotalTokens            int
	CacheTokens            int
	CacheCreationTokens    int
	CacheCreationTokens5m  int
	CacheCreationTokens1h  int
	ImageTokens            int
	AudioTokens            int
	ModelName              string
	CompletionRatio        float64
	CacheRatio             float64
	ImageRatio             float64
	ModelRatio             float64
	GroupRatio             float64
	BillingGroupRatio      float64
	ModelPrice             float64
	CacheCreationRatio     float64
	CacheCreationRatio5m   float64
	CacheCreationRatio1h   float64
	Quota                  int
	IsClaudeUsageSemantic  bool
	UsageSemantic          string
	AudioInputPrice        float64
	ToolSurchargeItems     []ToolSurchargeItem
	ToolCallSurchargeQuota decimal.Decimal
	FixedPriceBilling      bool
	ProviderCostBilling    bool
	Clamp                  *Clamp
	TieredResult           *TieredResult
	TieredApplied          bool
	AudioDisplayQuota      int
	CacheWriteTokens       int
}

func (s *TextCost) HasBillableUsage() bool {
	return s.FixedPriceBilling || s.ProviderCostBilling || s.TotalTokens > 0 || !s.ToolCallSurchargeQuota.IsZero()
}

func CacheWriteTokensTotal(summary TextCost) int {
	if summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0 {
		splitCacheWriteTokens := summary.CacheCreationTokens5m + summary.CacheCreationTokens1h
		if summary.CacheCreationTokens > splitCacheWriteTokens {
			return summary.CacheCreationTokens
		}
		return splitCacheWriteTokens
	}
	return summary.CacheCreationTokens
}
