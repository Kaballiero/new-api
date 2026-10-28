package billing

import (
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

type TextCostSummary struct {
	PromptTokens     int
	CompletionTokens int

	CacheTokens           int
	CacheCreationTokens   int
	CacheCreationTokens5m int
	CacheCreationTokens1h int
	ImageTokens           int
	AudioTokens           int
	ModelName             string
	TokenName             string
	UseTimeSeconds        int64
	CompletionRatio       float64
	CacheRatio            float64
	ImageRatio            float64
	ModelRatio            float64
	GroupRatio            float64

	ModelPrice            float64
	CacheCreationRatio    float64
	CacheCreationRatio5m  float64
	CacheCreationRatio1h  float64
	Quota                 int
	IsClaudeUsageSemantic bool

	AudioInputPrice    float64
	ToolSurchargeItems []domain.ToolSurchargeItem

	TieredResult                        *billingexpr.TieredResult
	TieredApplied, BillableUsage        bool
	AudioDisplayQuota, CacheWriteTokens int
}

type AudioCostResult struct {
	Quota        int
	TieredResult *billingexpr.TieredResult
	Billable     bool
}
