package billing_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	tokenusage "github.com/QuantumNous/new-api/gateway/tokenusage"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	billing "github.com/QuantumNous/new-api/relay/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func calculateTextQuotaSummary(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) billing.TextCostSummary {
	return billing.New().TextCost(c, info, usage)
}
func calculateTextToolCallSurcharge(c *gin.Context, info *relaycommon.RelayInfo, summary *billing.TextCostSummary) int {
	info.PriceData.GroupRatioInfo.GroupRatio = summary.GroupRatio
	info.PriceData.BillingGroupRatio = 0
	result := billing.New().TextCost(c, info, &dto.Usage{})
	summary.ToolSurchargeItems = result.ToolSurchargeItems
	return result.Quota
}
func TestCalculateTextQuotaSummaryUnifiedForClaudeSemantic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         100,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	priceData := hosttypes.PriceData{
		ModelRatio:           1,
		CompletionRatio:      2,
		CacheRatio:           0.1,
		CacheCreationRatio:   1.25,
		CacheCreation5mRatio: 1.25,
		CacheCreation1hRatio: 2,
		GroupRatioInfo: hosttypes.GroupRatioInfo{
			GroupRatio: 1,
		},
	}

	chatRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}
	messageRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatClaude,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}

	chatSummary := calculateTextQuotaSummary(ctx, chatRelayInfo, usage)
	messageSummary := calculateTextQuotaSummary(ctx, messageRelayInfo, usage)

	require.Equal(t, messageSummary.Quota, chatSummary.Quota)
	require.Equal(t, messageSummary.CacheCreationTokens5m, chatSummary.CacheCreationTokens5m)
	require.Equal(t, messageSummary.CacheCreationTokens1h, chatSummary.CacheCreationTokens1h)
	require.True(t, chatSummary.IsClaudeUsageSemantic)
	require.Equal(t, 1488, chatSummary.Quota)
}

func TestCalculateTextQuotaSummaryUsesSplitClaudeCacheCreationRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData: hosttypes.PriceData{
			ModelRatio:           1,
			CompletionRatio:      1,
			CacheRatio:           0,
			CacheCreationRatio:   1,
			CacheCreation5mRatio: 2,
			CacheCreation1hRatio: 3,
			GroupRatioInfo: hosttypes.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 0,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 10,
		},
		ClaudeCacheCreation5mTokens: 2,
		ClaudeCacheCreation1hTokens: 3,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 100 + remaining(5)*1 + 2*2 + 3*3 = 118
	require.Equal(t, 118, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesAnthropicUsageSemanticFromUpstreamUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: hosttypes.PriceData{
			ModelRatio:           1,
			CompletionRatio:      2,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo: hosttypes.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		UsageSemantic:    "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         100,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.True(t, summary.IsClaudeUsageSemantic)

	require.Equal(t, 1488, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesClaudeBillingUsageBeforeTopLevelUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: hosttypes.PriceData{
			ModelRatio:           1,
			CompletionRatio:      2,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo:       hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 999,
		TotalTokens:      1998,
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{
			InputTokens:              70,
			CacheReadInputTokens:     30,
			CacheCreationInputTokens: 20,
			OutputTokens:             7,
			CacheCreation: &dto.ClaudeCacheCreationUsage{
				Ephemeral5mInputTokens: 12,
				Ephemeral1hInputTokens: 8,
			},
		}),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, tokenusage.EffectiveBillingUsage(usage))

	require.True(t, summary.IsClaudeUsageSemantic)

	require.Equal(t, 70, summary.PromptTokens)
	require.Equal(t, 7, summary.CompletionTokens)
	require.Equal(t, 30, summary.CacheTokens)
	require.Equal(t, 20, summary.CacheCreationTokens)
	require.Equal(t, 12, summary.CacheCreationTokens5m)
	require.Equal(t, 8, summary.CacheCreationTokens1h)
	require.Equal(t, 118, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesGeminiBillingUsageBeforeTopLevelUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gemini-2.5-flash",
		PriceData: hosttypes.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			CacheRatio:      0.1,
			GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 999,
		TotalTokens:      1998,
		BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{
			PromptTokenCount:        100,
			ToolUsePromptTokenCount: 5,
			CandidatesTokenCount:    20,
			ThoughtsTokenCount:      3,
			TotalTokenCount:         128,
			CachedContentTokenCount: 7,
		}),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, tokenusage.EffectiveBillingUsage(usage))

	require.False(t, summary.IsClaudeUsageSemantic)

	require.Equal(t, 105, summary.PromptTokens)
	require.Equal(t, 23, summary.CompletionTokens)
	require.Equal(t, 7, summary.CacheTokens)

	require.Equal(t, 145, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesOpenAIBillingUsageBeforeTopLevelUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "gpt-4o",
		PriceData: hosttypes.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 999,
		TotalTokens:      1998,
		BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{
			PromptTokens:     80,
			CompletionTokens: 9,
			TotalTokens:      89,
		}),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, tokenusage.EffectiveBillingUsage(usage))

	require.False(t, summary.IsClaudeUsageSemantic)

	require.Equal(t, 80, summary.PromptTokens)
	require.Equal(t, 9, summary.CompletionTokens)

	require.Equal(t, 98, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesOpenAIResponsesInputTokenDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-4o",
		PriceData: hosttypes.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			CacheRatio:      0.25,
			GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	responsesUsage := &dto.Usage{
		InputTokens:  100,
		OutputTokens: 10,
		TotalTokens:  110,
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens: 40,
		},
	}
	convertedUsage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 10,
		TotalTokens:      110,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 40,
		},
		BillingUsage: dto.NewOpenAIResponsesBillingUsage(responsesUsage),
	}

	effectiveUsage := tokenusage.EffectiveBillingUsage(convertedUsage)
	require.Equal(t, 40, effectiveUsage.PromptTokensDetails.CachedTokens)
	require.Zero(t, convertedUsage.BillingUsage.OpenAIUsage.PromptTokensDetails.CachedTokens)

	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveUsage)
	require.Equal(t, 40, summary.CacheTokens)
	// 60 uncached input + 40*0.25 cached input + 10*2 output = 90.
	require.Equal(t, 90, summary.Quota)
}

func TestCalculateTextQuotaSummaryNormalizesOpenAIResponsesBillingUsageDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "gpt-5.6-sol",
		PriceData: hosttypes.PriceData{
			ModelRatio:         1,
			CompletionRatio:    2,
			CacheRatio:         0.5,
			CacheCreationRatio: 2,
			GroupRatioInfo:     hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	responsesDetails := dto.InputTokenDetails{
		CachedTokens:     80,
		CacheWriteTokens: 10,
		TextTokens:       100,
	}
	usage := &dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 999,
		BillingUsage: dto.NewOpenAIResponsesBillingUsage(&dto.Usage{
			InputTokens:        100,
			OutputTokens:       10,
			TotalTokens:        110,
			InputTokensDetails: &responsesDetails,
		}),
	}

	effectiveUsage := tokenusage.EffectiveBillingUsage(usage)
	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveUsage)

	require.Equal(t, dto.BillingUsageSourceOAIResponses, effectiveUsage.UsageSource)
	require.Equal(t, responsesDetails, effectiveUsage.PromptTokensDetails)
	require.Equal(t, 100, summary.PromptTokens)
	require.Equal(t, 10, summary.CompletionTokens)
	require.Equal(t, 80, summary.CacheTokens)
	require.Equal(t, 10, summary.CacheCreationTokens)
	// (100-80-10) + 80*0.5 + 10*2 + 10*2 = 90
	require.Equal(t, 90, summary.Quota)
}

func TestCalculateTextQuotaSummaryHandlesLegacyClaudeDerivedOpenAIUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: hosttypes.PriceData{
			ModelRatio:           1,
			CompletionRatio:      5,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo:       hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     62,
		CompletionTokens: 95,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3544,
		},
		ClaudeCacheCreation5mTokens: 586,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 62 + 3544*0.1 + 586*1.25 + 95*5 = 1624.9 => 1624
	require.Equal(t, 1624, summary.Quota)
}

func TestCalculateTextQuotaSummaryBillsOpenAICacheWriteTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-5.1",
		PriceData: hosttypes.PriceData{
			ModelRatio:         1,
			CompletionRatio:    2,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	t.Run("uncached remainder stays positive", func(t *testing.T) {
		usage := &dto.Usage{
			PromptTokens:     1473,
			CompletionTokens: 19,
			PromptTokensDetails: dto.InputTokenDetails{
				CacheWriteTokens: 1470,
			},
		}

		summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

		require.Equal(t, 1470, summary.CacheCreationTokens)
		// (1473-0-1470) + 1470*1.25 + 19*2 = 3 + 1837.5 + 38 = 1878.5 => 1879
		require.Equal(t, 1879, summary.Quota)
	})

	t.Run("uncached remainder clamps to zero", func(t *testing.T) {
		// Real OpenAI payload shape: cached_tokens + cache_write_tokens exceeds
		// prompt_tokens because both are unadjusted prefix counts. The negative
		// remainder must clamp to zero, never turn into a negative base charge.
		usage := &dto.Usage{
			PromptTokens:     3619,
			CompletionTokens: 36,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:     2921,
				CacheWriteTokens: 3616,
			},
		}

		summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

		require.Equal(t, 3619, summary.PromptTokens)
		require.Equal(t, 3616, summary.CacheCreationTokens)
		// max(3619-2921-3616, 0) + 2921*0.1 + 3616*1.25 + 36*2 = 4884.1 => 4884
		require.Equal(t, 4884, summary.Quota)
	})
}

func TestCalculateTextQuotaSummarySeparatesOpenRouterCacheReadFromPromptBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "openai/gpt-4.1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: hosttypes.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 2432,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// OpenRouter OpenAI-format display keeps prompt_tokens as total input,
	// but billing still separates normal input from cache read tokens.
	// quota = (2604 - 2432) + 2432*0.1 + 383 = 798.2 => 798
	require.Equal(t, 2604, summary.PromptTokens)
	require.Equal(t, 798, summary.Quota)
}

func TestCalculateTextQuotaSummarySeparatesOpenRouterCacheCreationFromPromptBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "openai/gpt-4.1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: hosttypes.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 100,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// prompt_tokens is still logged as total input, but cache creation is billed separately.
	// quota = (2604 - 100) + 100*1.25 + 383 = 3012
	require.Equal(t, 2604, summary.PromptTokens)
	require.Equal(t, 3012, summary.Quota)
}

func TestCalculateTextQuotaSummaryKeepsPrePRClaudeOpenRouterBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "anthropic/claude-3.7-sonnet",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: hosttypes.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 2432,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// Pre-PR PostClaudeConsumeQuota behavior for OpenRouter:
	// prompt = 2604 - 2432 = 172
	// quota = 172 + 2432*0.1 + 383 = 798.2 => 798
	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, 172, summary.PromptTokens)
	require.Equal(t, 798, summary.Quota)
}

func TestTryTieredSettleRecordsClampOnOverflow(t *testing.T) {
	// exprOutput = p * 1e12; quotaBeforeGroup = p*1e12 / 1e6 * 5e5 far exceeds
	// the supported single-request range and must saturate.
	exprStr := `tier("base", p * 1000000000000)`
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "overflow-model",
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:  "tiered_expr",
			ExprString:   exprStr,
			ExprHash:     billingexpr.ExprHashString(exprStr),
			GroupRatio:   1,
			QuotaPerUnit: 500_000,
		},
	}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	summary := billing.New().TextCost(ctx, relayInfo, &dto.Usage{PromptTokens: 1_000_000_000, TotalTokens: 1_000_000_000})
	ok, quota, result := summary.TieredApplied, summary.Quota, summary.TieredResult

	require.True(t, ok)
	require.NotNil(t, result)
	require.Equal(t, common.MaxQuota, quota, "oversized settlement must clamp, never wrap negative")
	require.NotNil(t, relayInfo.QuotaClamp, "clamp must be recorded on RelayInfo for admin auditing")
	require.Equal(t, common.QuotaClampOverflow, relayInfo.QuotaClamp.Kind)
}

// TestTryTieredSettleNoClampInRange confirms an in-range settlement leaves
// RelayInfo.QuotaClamp nil.
func TestTryTieredSettleNoClampInRange(t *testing.T) {
	exprStr := `tier("base", p * 2 + c * 10)`
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "in-range-model",
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:  "tiered_expr",
			ExprString:   exprStr,
			ExprHash:     billingexpr.ExprHashString(exprStr),
			GroupRatio:   1,
			QuotaPerUnit: 500_000,
		},
	}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	summary := billing.New().TextCost(ctx, relayInfo, &dto.Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500})
	ok, result := summary.TieredApplied, summary.TieredResult

	require.True(t, ok)
	require.NotNil(t, result)
	require.Nil(t, relayInfo.QuotaClamp, "in-range settlement must not record a clamp")
}

func TestCalculateTextQuotaSummaryFixedPriceAppliesImageCountOnceAndAllowsOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	priceData := hosttypes.PriceData{
		ModelPrice: 0.12,
		UsePrice:   true,
		GroupRatioInfo: hosttypes.GroupRatioInfo{
			GroupRatio: 1,
		},
	}
	priceData.AddOtherRatio("n", 3)
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "dall-e-3",
		PriceData:       priceData,
		StartTime:       time.Now(),
	}
	usage := &dto.Usage{PromptTokens: 1, TotalTokens: 1}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.Equal(t, 180000, summary.Quota)

	// An adaptor-reported actual count replaces the requested count rather
	// than multiplying it a second time.
	relayInfo.PriceData.AddOtherRatio("n", 2)
	summary = calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.Equal(t, 120000, summary.Quota)
}

func TestCalculateTextToolCallSurchargeGeneralizedBuiltInTools(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	operation_setting.SetToolPriceForTest("my_fn", 5.0)
	t.Cleanup(func() {
		operation_setting.DeleteToolPriceForTest("my_fn")
	})

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {CallCount: 2},
				"my_fn":                         {CallCount: 3},
				"unpriced":                      {CallCount: 5},
			},
		},
	}
	summary := &billing.TextCostSummary{
		ModelName:  "o1",
		GroupRatio: 1,
	}

	surcharge := calculateTextToolCallSurcharge(ctx, relayInfo, summary)
	expected := decimal.NewFromFloat((10.0*2 + 5.0*3) / 1000).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	assert.Equal(t, common.QuotaFromDecimal(expected), surcharge)
	require.Len(t, summary.ToolSurchargeItems, 2)
	assert.Equal(t, "my_fn", summary.ToolSurchargeItems[0].Name)
	assert.Equal(t, 3, summary.ToolSurchargeItems[0].Count)
	assert.Equal(t, 5.0, summary.ToolSurchargeItems[0].Price)
	assert.Equal(t, dto.BuildInToolWebSearchPreview, summary.ToolSurchargeItems[1].Name)
	assert.Equal(t, 2, summary.ToolSurchargeItems[1].Count)
	assert.Equal(t, 10.0, summary.ToolSurchargeItems[1].Price)
}

func TestCalculateTextToolCallSurchargeKeepsSearchPreviewFallbackWithCustomFunctions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	operation_setting.SetToolPriceForTest("my_fn", 5)
	t.Cleanup(func() {
		operation_setting.DeleteToolPriceForTest("my_fn")
	})

	relayInfo := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeChatCompletions,
		OriginModelName: "gpt-4o-search-preview",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				"my_fn": {CallCount: 1},
			},
		},
	}
	summary := &billing.TextCostSummary{
		ModelName:  relayInfo.OriginModelName,
		GroupRatio: 1,
	}

	surcharge := calculateTextToolCallSurcharge(ctx, relayInfo, summary)

	require.Len(t, summary.ToolSurchargeItems, 2)
	assert.Equal(t, "my_fn", summary.ToolSurchargeItems[0].Name)
	assert.Equal(t, dto.BuildInToolWebSearchPreview, summary.ToolSurchargeItems[1].Name)
	expected := decimal.NewFromFloat((5.0 + 25.0) / 1000).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	assert.Equal(t, common.QuotaFromDecimal(expected), surcharge)
}

func TestCalculateTextToolCallSurchargeDoesNotInferSearchForResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-4o-search-preview",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{},
		},
	}
	summary := &billing.TextCostSummary{
		ModelName:  relayInfo.OriginModelName,
		GroupRatio: 1,
	}

	surcharge := calculateTextToolCallSurcharge(ctx, relayInfo, summary)

	assert.Zero(t, surcharge)
	assert.Empty(t, summary.ToolSurchargeItems)
}

func TestCalculateTextToolCallSurchargeMergesSameNameAndPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("claude_web_search_requests", 3)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearch: {CallCount: 2},
			},
		},
	}
	summary := &billing.TextCostSummary{ModelName: relayInfo.OriginModelName, GroupRatio: 1}

	surcharge := calculateTextToolCallSurcharge(ctx, relayInfo, summary)

	require.Len(t, summary.ToolSurchargeItems, 1)
	assert.Equal(t, dto.BuildInToolWebSearch, summary.ToolSurchargeItems[0].Name)
	assert.Equal(t, 5, summary.ToolSurchargeItems[0].Count)
	assert.Equal(t, 10.0, summary.ToolSurchargeItems[0].Price)
	expected := decimal.NewFromFloat(10.0 * 5 / 1000).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	assert.Equal(t, common.QuotaFromDecimal(expected), surcharge)
}

func TestCalculateTextQuotaSummaryZeroTokensStillBillsToolSurcharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {CallCount: 1},
			},
		},
	}
	relayInfo.PriceData.GroupRatioInfo.GroupRatio = 1

	usage := &dto.Usage{} // zero tokens, mirrors alpha search
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.Zero(t, summary.PromptTokens)
	require.Zero(t, summary.CompletionTokens)
	assert.True(t, summary.BillableUsage)
	assert.Greater(t, summary.Quota, 0, "quota must not be zeroed out for a zero-token web search request")
	expected := common.QuotaFromFloat(10.0 / 1000 * common.QuotaPerUnit)
	assert.Equal(t, expected, summary.Quota)
}

func TestCalculateTextQuotaSummaryDoesNotApplyRequestMultipliersToToolSurcharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: hosttypes.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {CallCount: 1},
			},
		},
	}
	relayInfo.PriceData.AddOtherRatio("n", 3)

	summary := calculateTextQuotaSummary(ctx, relayInfo, &dto.Usage{})

	expected := decimal.NewFromFloat(10.0 / 1000).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	assert.Equal(t, common.QuotaFromDecimal(expected), summary.Quota)
}

func TestCalculateTextToolCallSurchargeGeminiGoogleSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("gemini_google_search_call", true)

	relayInfo := &relaycommon.RelayInfo{OriginModelName: "gemini-2.5-flash"}
	summary := &billing.TextCostSummary{ModelName: "gemini-2.5-flash", GroupRatio: 1}

	surcharge := calculateTextToolCallSurcharge(ctx, relayInfo, summary)
	expected := decimal.NewFromFloat(14.0 / 1000).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	assert.Equal(t, common.QuotaFromDecimal(expected), surcharge)
	require.Len(t, summary.ToolSurchargeItems, 1)
	assert.Equal(t, dto.BuildInToolGoogleSearch, summary.ToolSurchargeItems[0].Name)
	assert.Equal(t, 1, summary.ToolSurchargeItems[0].Count)
	assert.Equal(t, 14.0, summary.ToolSurchargeItems[0].Price)
}

func TestCalculateTextToolCallSurchargeGeminiFunctionCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	operation_setting.SetToolPriceForTest("gemini_surcharge_fn", 5.0)
	t.Cleanup(func() {
		operation_setting.DeleteToolPriceForTest("gemini_surcharge_fn")
	})

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "gemini-2.5-flash",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				"gemini_surcharge_fn": {CallCount: 2},
			},
		},
	}
	summary := &billing.TextCostSummary{ModelName: "gemini-2.5-flash", GroupRatio: 1}

	surcharge := calculateTextToolCallSurcharge(ctx, relayInfo, summary)
	expected := decimal.NewFromFloat(5.0 * 2 / 1000).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	assert.Equal(t, common.QuotaFromDecimal(expected), surcharge)
	require.Len(t, summary.ToolSurchargeItems, 1)
	assert.Equal(t, "gemini_surcharge_fn", summary.ToolSurchargeItems[0].Name)
	assert.Equal(t, 2, summary.ToolSurchargeItems[0].Count)
	assert.Equal(t, 5.0, summary.ToolSurchargeItems[0].Price)

}

func TestCalculateTextToolCallSurchargeImageGenerationDefaultPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	t.Cleanup(func() {
		operation_setting.DeleteToolPriceForTest(dto.BuildInToolImageGeneration)
	})

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.1",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {CallCount: 2},
			},
		},
	}
	summary := &billing.TextCostSummary{ModelName: "gpt-5.1", GroupRatio: 1.5}

	surcharge := calculateTextToolCallSurcharge(ctx, relayInfo, summary)
	expected := decimal.NewFromFloat(150.0).
		Mul(decimal.NewFromInt(2)).
		Div(decimal.NewFromInt(1000)).
		Mul(decimal.NewFromFloat(1.5)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	assert.Equal(t, common.QuotaFromDecimal(expected), surcharge)
	require.Len(t, summary.ToolSurchargeItems, 1)
	assert.Equal(t, dto.BuildInToolImageGeneration, summary.ToolSurchargeItems[0].Name)
	assert.Equal(t, 2, summary.ToolSurchargeItems[0].Count)
	assert.Equal(t, 150.0, summary.ToolSurchargeItems[0].Price)
}

func TestCalculateTextToolCallSurchargeImageGenerationExplicitZeroDisables(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	operation_setting.SetToolPriceForTest(dto.BuildInToolImageGeneration, 0)
	t.Cleanup(func() {
		operation_setting.DeleteToolPriceForTest(dto.BuildInToolImageGeneration)
	})

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.1",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {CallCount: 3},
			},
		},
	}
	summary := &billing.TextCostSummary{ModelName: "gpt-5.1", GroupRatio: 1}

	surcharge := calculateTextToolCallSurcharge(ctx, relayInfo, summary)
	assert.Zero(t, surcharge)
	assert.Empty(t, summary.ToolSurchargeItems)
}

func TestCalculateTextQuotaSummaryImageGenerationUsesStructuredSurcharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	t.Cleanup(func() {
		operation_setting.DeleteToolPriceForTest(dto.BuildInToolImageGeneration)
	})

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.1",
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {CallCount: 1},
			},
		},
	}
	relayInfo.PriceData.GroupRatioInfo.GroupRatio = 1
	relayInfo.PriceData.ModelRatio = 1
	relayInfo.PriceData.CompletionRatio = 1

	usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.Len(t, summary.ToolSurchargeItems, 1)
	assert.Equal(t, dto.BuildInToolImageGeneration, summary.ToolSurchargeItems[0].Name)
	assert.Equal(t, 1, summary.ToolSurchargeItems[0].Count)
	assert.Equal(t, 150.0, summary.ToolSurchargeItems[0].Price)

	expectedSurcharge := decimal.NewFromFloat(150.0 / 1000).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	assert.Equal(t, common.QuotaFromDecimal(expectedSurcharge.Add(decimal.NewFromInt(15))), summary.Quota)
}
