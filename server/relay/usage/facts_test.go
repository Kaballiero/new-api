package usage_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	tokenusage "github.com/QuantumNous/new-api/gateway/tokenusage"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	usagegateway "github.com/QuantumNous/new-api/relay/usage"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDeliveryFailureReason(t *testing.T) {
	newContext := func(t *testing.T) *gin.Context {
		t.Helper()
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		return ctx
	}
	streamStatus := func(reason relaycommon.StreamEndReason, streamError error) *relaycommon.StreamStatus {
		status := relaycommon.NewStreamStatus()
		status.SetEndReason(reason, streamError)
		return status
	}

	tests := []struct {
		name  string
		setup func(*gin.Context, *relaycommon.RelayInfo)
		want  string
	}{
		{name: "absent stream status"},
		{name: "done", setup: func(_ *gin.Context, info *relaycommon.RelayInfo) {
			info.StreamStatus = streamStatus(relaycommon.StreamEndReasonDone, nil)
		}},
		{name: "EOF", setup: func(_ *gin.Context, info *relaycommon.RelayInfo) {
			info.StreamStatus = streamStatus(relaycommon.StreamEndReasonEOF, nil)
		}},
		{name: "clean handler stop", setup: func(_ *gin.Context, info *relaycommon.RelayInfo) {
			info.StreamStatus = streamStatus(relaycommon.StreamEndReasonHandlerStop, nil)
		}},
		{name: "timeout", setup: func(_ *gin.Context, info *relaycommon.RelayInfo) {
			info.StreamStatus = streamStatus(relaycommon.StreamEndReasonTimeout, nil)
		}, want: "stream_timeout"},
		{name: "client gone", setup: func(_ *gin.Context, info *relaycommon.RelayInfo) {
			info.StreamStatus = streamStatus(relaycommon.StreamEndReasonClientGone, errors.New("client connection closed"))
		}, want: "stream_client_gone"},
		{name: "scanner error", setup: func(_ *gin.Context, info *relaycommon.RelayInfo) {
			info.StreamStatus = streamStatus(relaycommon.StreamEndReasonScannerErr, errors.New("scanner failed"))
		}, want: "stream_scanner_error"},
		{name: "soft error after done", setup: func(_ *gin.Context, info *relaycommon.RelayInfo) {
			info.StreamStatus = streamStatus(relaycommon.StreamEndReasonDone, nil)
			info.StreamStatus.RecordError("upstream failed")
		}, want: "stream_error"},
		{name: "soft error after EOF", setup: func(_ *gin.Context, info *relaycommon.RelayInfo) {
			info.StreamStatus = streamStatus(relaycommon.StreamEndReasonEOF, nil)
			info.StreamStatus.RecordError("upstream failed")
		}, want: "stream_error"},
		{name: "canceled nonstream", setup: func(ctx *gin.Context, _ *relaycommon.RelayInfo) {
			requestContext, cancel := context.WithCancel(ctx.Request.Context())
			ctx.Request = ctx.Request.WithContext(requestContext)
			cancel()
		}, want: "context_canceled"},
		{name: "deadline first", setup: func(ctx *gin.Context, info *relaycommon.RelayInfo) {
			requestContext, cancel := context.WithDeadline(ctx.Request.Context(), time.Now().Add(-time.Second))
			ctx.Request = ctx.Request.WithContext(requestContext)
			cancel()
			info.StreamStatus = streamStatus(relaycommon.StreamEndReasonTimeout, nil)
		}, want: "context_deadline_exceeded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newContext(t)
			info := &relaycommon.RelayInfo{}
			if tt.setup != nil {
				tt.setup(ctx, info)
			}
			require.Equal(t, tt.want, usagegateway.DeliveryFailure(ctx, info))
		})
	}
}

func TestUsageFromOpenAIBillingUsageNormalizesCacheDetailsWithoutOverwritingCanonicalValues(t *testing.T) {
	responsesUsage := &dto.Usage{
		InputTokens:          100,
		OutputTokens:         10,
		PromptCacheHitTokens: 55,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 8,
			TextTokens:   12,
		},
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens:         40,
			CachedCreationTokens: 5,
			CacheWriteTokens:     6,
			TextTokens:           60,
			ImageTokens:          7,
			AudioTokens:          9,
		},
	}

	billingUsage := dto.NewOpenAIResponsesBillingUsage(responsesUsage)
	usage := tokenusage.EffectiveBillingUsage(&dto.Usage{BillingUsage: billingUsage})

	require.Equal(t, 8, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 5, usage.PromptTokensDetails.CachedCreationTokens)
	require.Equal(t, 6, usage.PromptTokensDetails.CacheWriteTokens)
	require.Equal(t, 12, usage.PromptTokensDetails.TextTokens)
	require.Equal(t, 7, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 9, usage.PromptTokensDetails.AudioTokens)
	require.Zero(t, billingUsage.OpenAIUsage.PromptTokensDetails.CachedCreationTokens)
}

func TestUsageFromOpenAIBillingUsageFallsBackToPromptCacheHitTokens(t *testing.T) {
	usage := tokenusage.EffectiveBillingUsage(&dto.Usage{
		BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{
			PromptTokens:         100,
			CompletionTokens:     10,
			PromptCacheHitTokens: 35,
		}),
	})

	require.Equal(t, 35, usage.PromptTokensDetails.CachedTokens)
}

func TestUsageBillingPathForLog(t *testing.T) {
	require.Equal(t, "billing-usage-anthropic", usagegateway.UsagePath(true, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	}))
	invalidBillingUsage := &dto.Usage{
		PromptTokens: 1,
		BillingUsage: &dto.BillingUsage{
			Source:   dto.BillingUsageSourceClaudeMessages,
			Semantic: dto.BillingUsageSemanticAnthropic,
		},
	}
	require.Equal(t, "local", usagegateway.UsagePath(true, invalidBillingUsage))
	require.Equal(t, "upstream", usagegateway.UsagePath(false, invalidBillingUsage))
	require.Equal(t, "upstream", usagegateway.UsagePath(false, &dto.Usage{}))
	require.Equal(t, "billing-usage-openai", usagegateway.UsagePath(false, &dto.Usage{
		BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{PromptTokens: 1}),
	}))
	require.Equal(t, "billing-usage-anthropic", usagegateway.UsagePath(false, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	}))
	require.Equal(t, "billing-usage-gemini", usagegateway.UsagePath(false, &dto.Usage{
		BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{PromptTokenCount: 1}),
	}))
	require.Equal(t, "billing-usage-gemini-estimated", usagegateway.UsagePath(true, &dto.Usage{
		BillingUsage: dto.NewEstimatedGeminiChatBillingUsage(&dto.Usage{PromptTokens: 1}),
	}))
}

func TestAppendUsageBillingPathForLogWritesAdminInfo(t *testing.T) {
	other := new(usagerecord.LogOther)
	other.SetAdmin("usage_billing_path", usagegateway.UsagePath(true, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	}))

	adminInfo, ok := other.Snapshot()["admin_info"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "billing-usage-anthropic", adminInfo["usage_billing_path"])

	other = new(usagerecord.LogOther)
	other.SetAdmin("usage_billing_path", usagegateway.UsagePath(true, nil))
	adminInfo, ok = other.Snapshot()["admin_info"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "local", adminInfo["usage_billing_path"])
}
