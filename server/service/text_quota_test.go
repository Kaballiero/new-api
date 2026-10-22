package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
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
			require.Equal(t, tt.want, deliveryFailureReason(ctx, info))
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
	usage := EffectiveBillingUsage(&dto.Usage{BillingUsage: billingUsage})

	require.Equal(t, 8, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 5, usage.PromptTokensDetails.CachedCreationTokens)
	require.Equal(t, 6, usage.PromptTokensDetails.CacheWriteTokens)
	require.Equal(t, 12, usage.PromptTokensDetails.TextTokens)
	require.Equal(t, 7, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 9, usage.PromptTokensDetails.AudioTokens)
	require.Zero(t, billingUsage.OpenAIUsage.PromptTokensDetails.CachedCreationTokens)
}

func TestUsageFromOpenAIBillingUsageFallsBackToPromptCacheHitTokens(t *testing.T) {
	usage := EffectiveBillingUsage(&dto.Usage{
		BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{
			PromptTokens:         100,
			CompletionTokens:     10,
			PromptCacheHitTokens: 35,
		}),
	})

	require.Equal(t, 35, usage.PromptTokensDetails.CachedTokens)
}

func TestUsageBillingPathForLog(t *testing.T) {
	require.Equal(t, usageBillingPathAnthropic, usageBillingPathForLog(true, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	}))
	invalidBillingUsage := &dto.Usage{
		PromptTokens: 1,
		BillingUsage: &dto.BillingUsage{
			Source:   dto.BillingUsageSourceClaudeMessages,
			Semantic: dto.BillingUsageSemanticAnthropic,
		},
	}
	require.Equal(t, usageBillingPathLocal, usageBillingPathForLog(true, invalidBillingUsage))
	require.Equal(t, usageBillingPathUpstream, usageBillingPathForLog(false, invalidBillingUsage))
	require.Equal(t, usageBillingPathUpstream, usageBillingPathForLog(false, &dto.Usage{}))
	require.Equal(t, usageBillingPathOpenAI, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{PromptTokens: 1}),
	}))
	require.Equal(t, usageBillingPathAnthropic, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	}))
	require.Equal(t, usageBillingPathGemini, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{PromptTokenCount: 1}),
	}))
	require.Equal(t, usageBillingPathGeminiEstimated, usageBillingPathForLog(true, &dto.Usage{
		BillingUsage: dto.NewEstimatedGeminiChatBillingUsage(&dto.Usage{PromptTokens: 1}),
	}))
}

func TestAppendUsageBillingPathForLogWritesAdminInfo(t *testing.T) {
	other := model.NewLogOther()
	appendUsageBillingPathForLog(other, true, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	})

	adminInfo, ok := other.Snapshot()["admin_info"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, usageBillingPathAnthropic, adminInfo["usage_billing_path"])

	other = model.NewLogOther()
	appendUsageBillingPathForLog(other, true, nil)
	adminInfo, ok = other.Snapshot()["admin_info"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, usageBillingPathLocal, adminInfo["usage_billing_path"])
}

func TestAppendToolSurchargeLogInfoWritesOnlyStructuredFields(t *testing.T) {
	items := []ToolSurchargeItem{
		{Name: dto.BuildInToolWebSearch, Count: 2, Price: 10},
		{Name: dto.BuildInToolImageGeneration, Count: 1, Price: 150},
	}
	other := model.NewLogOther()

	appendToolSurchargeLogInfo(other, items)

	fields := other.Snapshot()
	assert.Equal(t, items, fields["tool_surcharges"])
	assert.NotContains(t, fields, "web_search")
	assert.NotContains(t, fields, "web_search_call_count")
	assert.NotContains(t, fields, "web_search_price")
	assert.NotContains(t, fields, "file_search")
	assert.NotContains(t, fields, "image_generation_call")
	assert.NotContains(t, fields, "image_generation_call_price")
}
