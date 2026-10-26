package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	modelpkg "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"image"
	"image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errorStreamWriter struct {
	gin.ResponseWriter
}

func setStreamTestTimeout(t *testing.T) {
	t.Helper()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
}

func (w errorStreamWriter) Write([]byte) (int, error) {
	return 0, errors.New("client write failed")
}

func (w errorStreamWriter) WriteString(string) (int, error) {
	return 0, errors.New("client write failed")
}

const openRouterEmbeddedError = `{"choices":[],"error":{"code":502,"message":"Upstream error from Nvidia: Service temporarily overloaded","metadata":{"error_type":"provider_unavailable"}}}`

func newEmbeddedErrorTestRequest(body string, statusCode int) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, recorder, &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		RelayFormat: types.RelayFormatOpenAI,
	}
}

func assertEmbeddedOpenAIError(t *testing.T, relayError *types.NewAPIError, recorder *httptest.ResponseRecorder, statusCode int) {
	t.Helper()
	require.NotNil(t, relayError)
	require.Equal(t, statusCode, relayError.StatusCode)
	require.Equal(t, "upstream_error", relayError.RelayError.(types.OpenAIError).Type)
	require.Contains(t, relayError.RelayError.(types.OpenAIError).Message, "Upstream error from Nvidia: Service temporarily overloaded")
	require.Equal(t, float64(502), relayError.RelayError.(types.OpenAIError).Code)
	require.JSONEq(t, `{"error_type":"provider_unavailable"}`, string(relayError.Metadata))
	require.Empty(t, recorder.Body.String())
}

func TestOpenAIHandlersSurfaceEmbeddedOpenRouterErrors(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	tests := []struct {
		name   string
		handle func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{name: "chat", handle: OpenaiHandler},
		{name: "image", handle: OpenaiImageHandler},
		{name: "image JSON as stream", handle: openaiImageJSONAsStreamHandler},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newEmbeddedErrorTestRequest(openRouterEmbeddedError, http.StatusOK)
			usage, relayError := tt.handle(c, info, resp)

			require.Nil(t, usage)
			assertEmbeddedOpenAIError(t, relayError, recorder, http.StatusBadGateway)
		})
	}
}

func TestOpenAIEmbeddedErrorRecognition(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		hasError bool
	}{
		{name: "message", body: `{"error":{"message":"unavailable"}}`, hasError: true},
		{name: "code", body: `{"error":{"code":123}}`, hasError: true},
		{name: "type", body: `{"error":{"type":"server_error"}}`, hasError: true},
		{name: "empty object", body: `{"error":{}}`},
		{name: "null", body: `{"error":null}`},
		{name: "missing", body: `{}`},
		{name: "empty string code", body: `{"error":{"code":""}}`},
		{name: "numeric zero code", body: `{"error":{"code":0}}`, hasError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var response dto.SimpleResponse
			require.NoError(t, common.Unmarshal([]byte(tt.body), &response))
			assert.Equal(t, tt.hasError, hasOpenAIErrorContent(response.GetOpenAIError()))
		})
	}
}

func TestOpenAIEmbeddedErrorPreservesFailingStatusAndSuccessfulOutput(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	t.Run("failing upstream status", func(t *testing.T) {
		c, recorder, resp, info := newEmbeddedErrorTestRequest(`{"error":{"message":"unavailable"}}`, http.StatusServiceUnavailable)
		usage, relayError := OpenaiHandler(c, info, resp)

		require.Nil(t, usage)
		require.NotNil(t, relayError)
		require.Equal(t, http.StatusServiceUnavailable, relayError.StatusCode)
		require.Empty(t, recorder.Body.String())
	})

	t.Run("valid image response", func(t *testing.T) {
		body := `{"created":1710000000,"data":[{"url":"https://example.com/image"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`
		c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
		usage, relayError := OpenaiImageHandler(c, info, resp)

		require.Nil(t, relayError)
		require.NotNil(t, usage)
		require.Equal(t, body, recorder.Body.String())
	})
}

func TestOaiStreamHandlerSurfacesEmbeddedErrors(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	setStreamTestTimeout(t)

	t.Run("content and usage before error are retained", func(t *testing.T) {
		body := strings.Join([]string{
			`data: {"id":"chatcmpl","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
			``,
			`data: {"id":"chatcmpl","created":1,"model":"test","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`,
			``,
			`data: {"error":{"message":"upstream unavailable","code":"provider_unavailable"}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")
		c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
		info.RelayMode = relayconstant.RelayModeChatCompletions
		resp.Header.Set("Content-Type", "text/event-stream")

		usage, relayError := OaiStreamHandler(c, info, resp)

		require.Nil(t, relayError)
		require.Equal(t, 5, usage.TotalTokens)
		require.True(t, info.StreamStatus.HasErrors())
		require.Equal(t, 1, info.StreamStatus.TotalErrorCount())
		require.Equal(t, 1, strings.Count(recorder.Body.String(), `"content":"hello"`))
		require.Equal(t, 1, strings.Count(recorder.Body.String(), `"error"`))
		require.NotContains(t, recorder.Body.String(), "[DONE]")
		terminalError := strings.Index(recorder.Body.String(), `data: {"error":`)
		require.NotEqual(t, -1, terminalError)
		require.NotContains(t, recorder.Body.String()[terminalError:], `"content":"hello"`)
		require.NotContains(t, recorder.Body.String()[terminalError:], `"usage"`)
	})

	t.Run("latest usage before later chunks is retained", func(t *testing.T) {
		body := strings.Join([]string{
			`data: {"id":"chatcmpl","created":1,"model":"test","choices":[],"usage":{"prompt_tokens":200,"completion_tokens":300,"total_tokens":500}}`,
			``,
			`data: {"id":"chatcmpl","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"first"}}]}`,
			``,
			`data: {"id":"chatcmpl","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"second"}}]}`,
			``,
			`data: {"error":{"message":"upstream unavailable"}}`,
			``,
		}, "\n")
		c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
		info.RelayMode = relayconstant.RelayModeChatCompletions
		resp.Header.Set("Content-Type", "text/event-stream")

		usage, relayError := OaiStreamHandler(c, info, resp)

		require.Nil(t, relayError)
		require.Equal(t, 500, usage.TotalTokens)
		require.Equal(t, 1, strings.Count(recorder.Body.String(), `"content":"first"`))
		require.Equal(t, 1, strings.Count(recorder.Body.String(), `"content":"second"`))
		terminalError := strings.Index(recorder.Body.String(), `data: {"error":`)
		require.NotEqual(t, -1, terminalError)
		require.NotContains(t, recorder.Body.String()[terminalError:], `"content"`)
		require.NotContains(t, recorder.Body.String()[terminalError:], `"usage"`)
	})

	t.Run("terminal diagnostics are masked", func(t *testing.T) {
		body := "data: {\"error\":{\"message\":\"upstream api_key:credential\"}}\n"
		c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
		info.RelayMode = relayconstant.RelayModeChatCompletions
		resp.Header.Set("Content-Type", "text/event-stream")

		usage, relayError := OaiStreamHandler(c, info, resp)

		require.Nil(t, relayError)
		require.NotNil(t, usage)
		require.Equal(t, 1, info.StreamStatus.TotalErrorCount())
		require.NotContains(t, recorder.Body.String(), "credential")
		require.Len(t, info.StreamStatus.Errors, 1)
		require.NotContains(t, info.StreamStatus.Errors[0].Message, "credential")
		require.Contains(t, recorder.Body.String(), "***")
		require.Contains(t, info.StreamStatus.Errors[0].Message, "***")
	})

	t.Run("terminal error uses each client envelope", func(t *testing.T) {
		tests := []struct {
			name        string
			relayFormat types.RelayFormat
			contains    []string
		}{
			{name: "OpenAI", relayFormat: types.RelayFormatOpenAI, contains: []string{`data: {"error":`, `"upstream_error"`}},
			{name: "Claude", relayFormat: types.RelayFormatClaude, contains: []string{"event: error", `data: {"type":"error","error":`}},
			{name: "Gemini", relayFormat: types.RelayFormatGemini, contains: []string{`data: {"error":`}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				c, recorder, resp, info := newEmbeddedErrorTestRequest("data: {\"error\":{\"message\":\"upstream unavailable\",\"code\":\"provider_unavailable\"}}\n", http.StatusOK)
				info.RelayMode = relayconstant.RelayModeChatCompletions
				info.RelayFormat = tt.relayFormat
				info.ChannelSetting.ForceFormat = true
				info.ChannelSetting.ThinkingToContent = true
				resp.Header.Set("Content-Type", "text/event-stream")

				usage, relayError := OaiStreamHandler(c, info, resp)

				require.Nil(t, relayError)
				require.NotNil(t, usage)
				require.True(t, info.StreamStatus.HasErrors())
				require.Equal(t, 1, info.StreamStatus.TotalErrorCount())
				for _, expected := range tt.contains {
					require.Contains(t, recorder.Body.String(), expected)
				}
				require.NotContains(t, recorder.Body.String(), "[DONE]")
			})
		}
	})
}

func TestOaiStreamHandlerLeavesNonErrorsAndMalformedPayloadsUnchanged(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	setStreamTestTimeout(t)

	tests := []struct {
		name string
		data string
	}{
		{name: "top level message", data: `{"message":"not an error","choices":[]}`},
		{name: "empty error", data: `{"error":{},"choices":[]}`},
		{name: "null error", data: `{"error":null,"choices":[]}`},
		{name: "malformed", data: `{"choices":`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := "data: " + tt.data + "\n\ndata: [DONE]\n"
			c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
			info.RelayMode = relayconstant.RelayModeChatCompletions
			resp.Header.Set("Content-Type", "text/event-stream")

			usage, relayError := OaiStreamHandler(c, info, resp)

			if tt.name == "malformed" {
				require.NotNil(t, relayError)
				require.Nil(t, usage)
				require.True(t, info.StreamStatus.HasErrors())
				require.NotContains(t, recorder.Body.String(), tt.data)
			} else {
				require.Nil(t, relayError)
				require.NotNil(t, usage)
				require.False(t, info.StreamStatus.HasErrors())
				require.Contains(t, recorder.Body.String(), tt.data)
				require.Contains(t, recorder.Body.String(), "[DONE]")
			}
		})
	}
}

func TestOaiStreamHandlerRecordsErrorEventWriteFailure(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	setStreamTestTimeout(t)

	c, _, resp, info := newEmbeddedErrorTestRequest("data: {\"error\":{\"message\":\"upstream unavailable\"}}\n", http.StatusOK)
	info.RelayMode = relayconstant.RelayModeChatCompletions
	resp.Header.Set("Content-Type", "text/event-stream")
	c.Writer = errorStreamWriter{ResponseWriter: c.Writer}

	usage, relayError := OaiStreamHandler(c, info, resp)

	require.Nil(t, relayError)
	require.NotNil(t, usage)
	require.True(t, info.StreamStatus.HasErrors())
	require.GreaterOrEqual(t, info.StreamStatus.TotalErrorCount(), 2)
}

// newOpenRouterChatTextRequest binds a chat text hold to a persisted OpenRouter
// account so a missing cost can be deferred to the durable Task.
func newOpenRouterChatTextRequest(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo, *sonarAdmissionBilling) {
	t.Helper()
	previousDB, previousLogDB := modelpkg.DB, modelpkg.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousBatch, previousLog := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&modelpkg.Task{}, &modelpkg.User{}, &modelpkg.Token{}, &modelpkg.Channel{}, &modelpkg.Log{}))
	modelpkg.DB, modelpkg.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	t.Cleanup(func() {
		modelpkg.DB, modelpkg.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousRedis, previousBatch, previousLog
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&modelpkg.User{Id: 1, Username: "chat-text", Quota: 1000000}).Error)
	c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
	info.ChannelType, info.ChannelId, info.ChannelBaseUrl = constant.ChannelTypeOpenRouter, 1, "https://openrouter.test/api"
	info.ApiKey, info.UpstreamModelName, info.OriginModelName = "chat-text-account", "vendor/text", "text-alias"
	info.RelayMode, info.UserId, info.RequestId, info.BillingSource = relayconstant.RelayModeChatCompletions, 1, t.Name(), "wallet"
	info.OpenRouterCost = &relaycommon.OpenRouterCostState{ChatText: true, Sent: true, Basis: common.OpenRouterCostBasis{Version: "openrouter_cost_v1", EffectiveGroupRatio: 1, QuotaPerUnit: 1000, EstimatedUSD: .05}}
	channel := &modelpkg.Channel{Id: 1, Type: constant.ChannelTypeOpenRouter, Key: info.ApiKey, BaseURL: &info.ChannelBaseUrl}
	channel.SetSetting(info.ChannelSetting)
	require.NoError(t, db.Create(channel).Error)
	billing := &sonarAdmissionBilling{}
	require.NoError(t, billing.Reserve(50))
	info.Billing, info.FinalPreConsumedQuota = billing, 50
	info.SetEstimatePromptTokens(12)
	return c, recorder, resp, info, billing
}

func TestOpenRouterCostGateBeforeClientWrite(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
		cost       float64
		chatText   bool
		// deferred means the hold moved to a Task awaiting generation metadata.
		deferred bool
		upstream string
		// lookupDrift breaks Configure after Begin by moving the request origin.
		lookupDrift  bool
		billingState string
		// commitFailure fails the hold transfer after Begin persisted the Task.
		commitFailure bool
	}{
		{name: "cost only positive", body: `{"choices":[{"message":{"content":"ok"}}],"usage":{"cost":0.03}}`, valid: true, cost: .03},
		{name: "cost explicit zero", body: `{"choices":[{"message":{"content":"ok"}}],"usage":{"cost":0}}`, valid: true, cost: 0},
		{name: "missing cost", body: `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1}}`},
		{name: "null cost", body: `{"choices":[{"message":{"content":"ok"}}],"usage":{"cost":null}}`},
		{name: "string cost", body: `{"choices":[{"message":{"content":"ok"}}],"usage":{"cost":"0"}}`},
		{name: "chat text missing cost defers to generation", body: `{"id":"gen-text-missing","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1}}`, chatText: true, valid: true, deferred: true},
		{name: "chat text null cost defers to generation", body: `{"id":"gen-text-null","choices":[{"message":{"content":"ok"}}],"usage":{"cost":null}}`, chatText: true, valid: true, deferred: true},
		{name: "chat text lookup drift after Begin still delivers", body: `{"id":"gen-text-drift","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1}}`, chatText: true, valid: true, deferred: true, lookupDrift: true, billingState: "manual_reconciliation"},
		{name: "chat text failed hold transfer after Begin still delivers", body: `{"id":"gen-text-commit","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1}}`, chatText: true, valid: true, deferred: true, commitFailure: true, billingState: "reservation_unknown"},
		{name: "chat text handler failure after deferral holds for reconciliation", body: `{"id":"gen-text-broken","choices":"broken","usage":{"prompt_tokens":1}}`, chatText: true, deferred: true, billingState: "manual_reconciliation"},
		{name: "chat text missing cost without identity refunds", body: `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1}}`, chatText: true},
		{name: "chat text missing cost with invalid identity refunds", body: `{"id":"chatcmpl-foreign","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1}}`, chatText: true},
		{name: "chat text upstream error is surfaced", body: `{"id":"gen-text-error","choices":[],"error":{"code":502,"message":"Upstream error from Nvidia: Service temporarily overloaded","metadata":{"provider_name":"Nvidia"}}}`, chatText: true, upstream: "Upstream error from Nvidia: Service temporarily overloaded"},
		{name: "chat text valid cost ignores invalid identity", body: `{"id":"chatcmpl-foreign","choices":[{"message":{"content":"ok"}}],"usage":{"cost":0.03}}`, chatText: true, valid: true, cost: .03},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder, resp, info := newEmbeddedErrorTestRequest(tc.body, http.StatusOK)
			info.ChannelType = constant.ChannelTypeOpenRouter
			info.OpenRouterCost = &relaycommon.OpenRouterCostState{}
			info.SetEstimatePromptTokens(12)
			billing := &sonarAdmissionBilling{}
			if tc.chatText {
				c, recorder, resp, info, billing = newOpenRouterChatTextRequest(t, tc.body)
			}
			if tc.lookupDrift {
				info.ChannelBaseUrl = "https://openrouter-drift.test/api"
			}
			if tc.commitFailure {
				billing.err = errors.New("injected hold transfer failure")
			}
			if tc.billingState == "" {
				tc.billingState = "awaiting_cost"
			}
			usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
			assert.Equal(t, tc.deferred, service.HasOpenRouterSyncTask(info), "only a deferred Task keeps the hold; otherwise the controller refunds")
			if !tc.valid {
				require.NotNil(t, apiErr)
				if tc.upstream != "" {
					assert.Contains(t, apiErr.Error(), tc.upstream)
				}
				assert.Empty(t, recorder.Body.String())
				if !tc.deferred {
					assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
					assert.Empty(t, billing.settled)
					return
				}
				// The hold stays in the Task without an automatic charge.
				assert.Equal(t, []int{50}, billing.settled)
				task, exists, err := modelpkg.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
				require.NoError(t, err)
				require.True(t, exists)
				assert.Equal(t, 50, task.Quota)
				assert.Equal(t, "undelivered_chat_result", task.PrivateData.OpenRouterSync.LastError)
				assert.Equal(t, tc.billingState, service.OpenRouterSyncReconciliation(task)["billing_state"])
				return
			}
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, tc.cost, usage.(*dto.Usage).Cost)
			assert.Contains(t, recorder.Body.String(), "ok")
			if !tc.deferred {
				assert.Empty(t, billing.settled)
				return
			}
			assert.Equal(t, []int{50}, billing.settled, "the session hold moves into the Task unchanged")
			assert.Equal(t, info.OpenRouterCost.TaskID, recorder.Header().Get("X-Task-Id"))
			task, exists, err := modelpkg.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
			require.NoError(t, err)
			require.True(t, exists)
			assert.Equal(t, constant.TaskPlatformOpenRouterChatImage, task.Platform)
			assert.Equal(t, "chat", task.Action)
			assert.Equal(t, 50, task.Quota)
			assert.Equal(t, info.OpenRouterCost.GenerationID, task.PrivateData.UpstreamTaskID)
			assert.Equal(t, tc.billingState, service.OpenRouterSyncReconciliation(task)["billing_state"])
			if tc.commitFailure {
				// The poller parks it as interrupted_reservation_or_submission after the lease.
				assert.True(t, task.PrivateData.Reconciliation.ReservationPending)
				return
			}
			if tc.lookupDrift {
				assert.Empty(t, task.PrivateData.OpenRouterSync.MetadataURL)
				assert.Equal(t, "missing_generation_identity", service.OpenRouterSyncReconciliation(task)["reason_code"])
				return
			}
			assert.Equal(t, "https://openrouter.test/api/v1/generation", task.PrivateData.OpenRouterSync.MetadataURL)
		})
	}
}

func TestOpenRouterStreamRequiresTerminalCost(t *testing.T) {
	setStreamTestTimeout(t)
	for _, tc := range []struct {
		name, frames string
		valid        bool
		cost         float64
		chatText     bool
		// billingState is the Task outcome that retains the streamed hold.
		billingState, generationID, upstream string
		// undelivered is content that must never reach the client.
		undelivered string
	}{
		{name: "final cost zero replaces provisional", frames: "data: {\"choices\":[],\"usage\":{\"cost\":0.1}}\n\ndata: {\"choices\":[],\"usage\":{\"cost\":0}}\n\ndata: [DONE]\n\n", valid: true},
		{name: "null final invalidates provisional", frames: "data: {\"choices\":[],\"usage\":{\"cost\":0.1}}\n\ndata: {\"choices\":[],\"usage\":{\"cost\":null}}\n\ndata: [DONE]\n\n"},
		{name: "disconnect without terminal", frames: "data: {\"choices\":[],\"usage\":{\"cost\":0.1}}\n\n"},
		{name: "chat text without terminal cost defers to generation", chatText: true, valid: true, billingState: "awaiting_cost", generationID: "gen-stream-text",
			frames: "data: {\"id\":\"gen-stream-text\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"id\":\"gen-stream-text\",\"choices\":[],\"usage\":{\"prompt_tokens\":1}}\n\ndata: [DONE]\n\n"},
		{name: "chat text without identity keeps streamed hold for reconciliation", chatText: true, valid: true, billingState: "manual_reconciliation",
			frames: "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":1}}\n\ndata: [DONE]\n\n"},
		{name: "chat text tool call without terminal cost keeps the hold", chatText: true, valid: true, billingState: "awaiting_cost", generationID: "gen-stream-tool",
			frames: "data: {\"id\":\"gen-stream-tool\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\ndata: {\"id\":\"gen-stream-tool\",\"choices\":[],\"usage\":{\"prompt_tokens\":1}}\n\ndata: [DONE]\n\n"},
		{name: "chat text role-only chunk then error refunds", chatText: true, upstream: "Upstream error from Nvidia",
			frames: "data: {\"id\":\"gen-stream-role\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\ndata: {\"id\":\"gen-stream-role\",\"choices\":[],\"error\":{\"code\":502,\"message\":\"Upstream error from Nvidia\"}}\n\n"},
		{name: "chat text role-only stream without usage or identity refunds", chatText: true,
			frames: "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\ndata: [DONE]\n\n"},
		{name: "chat text content inside the error chunk is not delivered and refunds", chatText: true, upstream: "Upstream error from Nvidia", undelivered: "lost-content",
			frames: "data: {\"id\":\"gen-stream-joint\",\"choices\":[{\"delta\":{\"content\":\"lost-content\"}}],\"error\":{\"code\":502,\"message\":\"Upstream error from Nvidia\"}}\n\n"},
		{name: "chat text reasoning then error keeps the hold", chatText: true, upstream: "Upstream error from Nvidia", billingState: "awaiting_cost", generationID: "gen-stream-reasoning",
			frames: "data: {\"id\":\"gen-stream-reasoning\",\"choices\":[{\"delta\":{\"reasoning\":\"thinking\"}}]}\n\ndata: {\"id\":\"gen-stream-reasoning\",\"choices\":[],\"error\":{\"code\":502,\"message\":\"Upstream error from Nvidia\"}}\n\n"},
		{name: "chat text upstream error before content refunds", chatText: true, upstream: "Upstream error from Nvidia",
			frames: "data: {\"id\":\"gen-stream-error\",\"choices\":[],\"error\":{\"code\":502,\"message\":\"Upstream error from Nvidia\"}}\n\n"},
		{name: "chat text upstream error after content keeps the hold", chatText: true, upstream: "Upstream error from Nvidia", billingState: "awaiting_cost", generationID: "gen-stream-partial",
			frames: "data: {\"id\":\"gen-stream-partial\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"id\":\"gen-stream-partial\",\"choices\":[],\"error\":{\"code\":502,\"message\":\"Upstream error from Nvidia\"}}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder, resp, info := newEmbeddedErrorTestRequest(tc.frames, http.StatusOK)
			info.OpenRouterCost = &relaycommon.OpenRouterCostState{}
			billing := &sonarAdmissionBilling{}
			if tc.chatText {
				c, recorder, resp, info, billing = newOpenRouterChatTextRequest(t, tc.frames)
			}
			resp.Header.Set("Content-Type", "text/event-stream")
			info.ChannelType = constant.ChannelTypeOpenRouter
			info.IsStream = true
			info.DisablePing = true
			usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
			assert.Equal(t, tc.billingState != "", service.HasOpenRouterSyncTask(info), "only a Task keeps the streamed hold; otherwise the controller refunds")
			if tc.undelivered != "" {
				assert.NotContains(t, recorder.Body.String(), tc.undelivered)
			}
			if tc.valid {
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Equal(t, tc.cost, usage.(*dto.Usage).Cost)
			} else {
				require.NotNil(t, apiErr)
				if tc.upstream != "" {
					assert.Contains(t, apiErr.Error(), tc.upstream)
				}
			}
			if tc.billingState == "" {
				assert.Empty(t, billing.settled)
				return
			}
			assert.Equal(t, []int{50}, billing.settled)
			task, exists, err := modelpkg.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
			require.NoError(t, err)
			require.True(t, exists)
			assert.Equal(t, "chat", task.Action)
			assert.Equal(t, 50, task.Quota)
			assert.Equal(t, tc.generationID, task.PrivateData.UpstreamTaskID)
			assert.Equal(t, tc.billingState, service.OpenRouterSyncReconciliation(task)["billing_state"])
		})
	}
}

func TestOpenRouterResponsesBillableTerminals(t *testing.T) {
	setStreamTestTimeout(t)
	for _, tc := range []struct {
		name, event, status, cost string
		valid                     bool
		want                      float64
	}{
		{"capped incomplete", "response.incomplete", "incomplete", `0.1`, true, .1},
		{"capped incomplete zero", "response.incomplete", "incomplete", `0`, true, 0},
		{"done without DONE marker", "response.done", "completed", `0.2`, true, .2},
		{"missing incomplete cost", "response.incomplete", "incomplete", ``, false, 0},
		{"null incomplete cost", "response.incomplete", "incomplete", `null`, false, 0},
		{"string incomplete cost", "response.incomplete", "incomplete", `"0"`, false, 0},
		{"failed event with cost", "response.failed", "failed", `0.1`, false, 0},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				costField := ""
				if tc.cost != "" {
					costField = `,"cost":` + tc.cost
				}
				response := fmt.Sprintf(`{"id":"resp_cost","object":"response","status":%q,"incomplete_details":{"reason":"max_output_tokens"},"output":[],"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18%s}}`, tc.status, costField)
				body := response
				if stream {
					body = "data: " + fmt.Sprintf(`{"type":%q,"response":%s}`, tc.event, response) + "\n\n"
				}
				c, recorder, resp, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
				info.ChannelType = constant.ChannelTypeOpenRouter
				info.RelayMode = relayconstant.RelayModeResponses
				info.RelayFormat = types.RelayFormatOpenAIResponses
				info.OpenRouterCost = &relaycommon.OpenRouterCostState{}
				info.IsStream = stream
				info.DisablePing = true
				if stream {
					resp.Header.Set("Content-Type", "text/event-stream")
				}
				usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
				if !tc.valid {
					require.NotNil(t, apiErr)
					assert.False(t, info.OpenRouterCost.Final)
					if !stream {
						assert.Empty(t, recorder.Body.String())
					}
					return
				}
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				actual := usage.(*dto.Usage)
				assert.Equal(t, tc.want, actual.Cost)
				assert.Equal(t, 11, actual.PromptTokens)
				assert.Equal(t, 7, actual.CompletionTokens)
				assert.Equal(t, 18, actual.TotalTokens)
				assert.True(t, info.OpenRouterCost.Final)
				assert.Contains(t, recorder.Body.String(), tc.status)
			})
		}
	}
}

type sonarAdmissionTransport struct {
	generation       string
	generationStatus int
	responseHeaders  http.Header
	metadata         string
	metadataStatus   int
	metadataError    error
	responseError    error
	response         string
	responseStatus   int
	gets             int
	posts            int
	body             []byte
	onPost           func(*http.Request)
}

func (s *sonarAdmissionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body := s.metadata
	switch request.Method {
	case http.MethodGet:
		s.gets++
		if request.URL.Path == "/api/v1/generation" {
			status := http.StatusOK
			if s.generationStatus != 0 {
				status = s.generationStatus
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(s.generation)), Request: request}, nil
		}
		if s.metadataError != nil {
			return nil, s.metadataError
		}
		if s.metadataStatus != 0 {
			return &http.Response{StatusCode: s.metadataStatus, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}
	case http.MethodPost:
		s.posts++
		if s.onPost != nil {
			s.onPost(request)
		}
		var err error
		s.body, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		if s.responseError != nil {
			return nil, s.responseError
		}
		body = `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"cost":0.47545,"cost_details":{"upstream_inference_cost":0.001032}}}`
		if s.response != "" {
			body = s.response
		}
	default:
		return nil, errors.New("unexpected request method")
	}
	headers := http.Header{"Content-Type": []string{"application/json"}}
	if request.Method == http.MethodPost && s.responseHeaders != nil {
		headers = s.responseHeaders
	}
	status := http.StatusOK
	if request.Method == http.MethodPost && s.responseStatus != 0 {
		status = s.responseStatus
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

type sonarAdmissionBilling struct {
	reserveCalls int
	reserved     int
	settled      []int
	err          error
}

func (s *sonarAdmissionBilling) Reserve(quota int) error {
	s.reserveCalls++
	if s.err != nil {
		return s.err
	}
	s.reserved = quota
	return nil
}
func (s *sonarAdmissionBilling) GetPreConsumedQuota() int { return s.reserved }
func (s *sonarAdmissionBilling) Settle(quota int) error {
	s.settled = append(s.settled, quota)
	return s.err
}
func (s *sonarAdmissionBilling) Refund(*gin.Context) {}
func (s *sonarAdmissionBilling) NeedsRefund() bool   { return s.reserved > 0 }

func TestOpenRouterSonarApproximateReserveAdmission(t *testing.T) {
	const model = "perplexity/sonar-deep-research"
	for _, tc := range []struct {
		name, model, extra, endpoint, rateKey string
		rate                                  any
		reserve                               *float64
		insufficient, knownAboveReserve       bool
		general, hostedSearch                 bool
		policyModel                           string
		endpointModel                         string
		missingCost                           bool
		nonOpenRouter                         bool
		operation                             string
		wantError                             string
		zeroReasoning                         bool
		reserveUse                            openrouter.ReserveUse
	}{
		{name: "general Sonar overrides legacy", general: true, reserve: common.GetPointer(2.0)},
		{name: "general missing terminal cost remains unknown", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), missingCost: true},
		{name: "general non OpenRouter rejected", model: "other/reasoner", general: true, nonOpenRouter: true, reserve: common.GetPointer(2.0), wantError: "one nonstream text chat response"},
		{name: "general batch rejected", model: "other/reasoner", general: true, operation: "batch_chat/completions", reserve: common.GetPointer(2.0), wantError: "one nonstream text chat response"},
		{name: "general Responses rejected", model: "other/reasoner", general: true, operation: "responses", reserve: common.GetPointer(2.0), wantError: "one nonstream text chat response"},

		{name: "general second model", model: "other/reasoner", endpoint: "other-provider", general: true, reserve: common.GetPointer(2.0)},
		{name: "general exact ID does not match alias", model: "other/reasoner-alias", policyModel: "other/reasoner", general: true, reserve: common.GetPointer(2.0), wantError: "unsupported billable dimension internal_reasoning"},
		{name: "general channel absent remains closed", model: "other/reasoner", general: true, wantError: "unsupported billable dimension internal_reasoning"},
		{name: "general invalid reserve", model: "other/reasoner", general: true, reserve: common.GetPointer(0.0), wantError: "invalid OpenRouter"},
		{name: "general huge reserve", model: "other/reasoner", general: true, reserve: common.GetPointer(1e100), wantError: "quota"},
		{name: "general insufficient reserve", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), insufficient: true, wantError: "insufficient test quota"},
		{name: "general known subtotal above minimum", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), rateKey: "completion", rate: "1", knownAboveReserve: true},
		{name: "general wrong endpoint model rejected", model: "other/reasoner", endpointModel: "other/different", general: true, reserve: common.GetPointer(2.0), wantError: "endpoint model does not match"},
		{name: "general missing prompt price", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), rateKey: "prompt", rate: nil, wantError: "invalid OpenRouter endpoint price"},
		{name: "general missing completion price", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), rateKey: "completion", rate: nil, wantError: "invalid OpenRouter endpoint price"},

		{name: "general missing tariff", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), rateKey: "internal_reasoning", rate: nil, wantError: "invalid OpenRouter endpoint price"},
		{name: "general unknown dimension", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), rateKey: "research_step", rate: "1", wantError: "unsupported billable dimension research_step"},
		{name: "general stream rejected", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), extra: `,"stream":true`, wantError: "one nonstream text chat response"},
		{name: "disabled by default", wantError: "research reserve policy is disabled"},
		{name: "zero reasoning rate cannot enable policy", rateKey: "internal_reasoning", rate: "0", wantError: "research reserve policy is disabled"},
		{name: "enabled channel hold and full total cost", reserve: common.GetPointer(2.0)},
		{name: "known subtotal exceeds approximate hold", reserve: common.GetPointer(2.0), rateKey: "completion", rate: "1", knownAboveReserve: true},
		{name: "client cannot enable policy", extra: `,"openrouter_sonar_research_reserve_usd":2`, wantError: "research reserve policy is disabled"},
		{name: "other model remains closed", model: "other/reasoner", reserve: common.GetPointer(2.0), wantError: "unsupported billable dimension internal_reasoning"},
		{name: "another provider remains closed", endpoint: "other-provider", reserve: common.GetPointer(2.0), wantError: "requires the Perplexity endpoint"},
		{name: "unknown charge remains closed", reserve: common.GetPointer(2.0), rateKey: "research_step", rate: "0.01", wantError: "unsupported billable dimension research_step"},
		{name: "null reasoning rate", reserve: common.GetPointer(2.0), rateKey: "internal_reasoning", rate: nil, wantError: "invalid OpenRouter endpoint price"},
		{name: "negative search rate", reserve: common.GetPointer(2.0), rateKey: "web_search", rate: "-1", wantError: "invalid OpenRouter endpoint price"},
		{name: "zero reserve rejected", reserve: common.GetPointer(0.0), wantError: "invalid OpenRouter approximate reserve"},
		{name: "negative reserve rejected", reserve: common.GetPointer(-2.0), wantError: "invalid OpenRouter approximate reserve"},
		{name: "nan reserve rejected", reserve: common.GetPointer(math.NaN()), wantError: "invalid OpenRouter approximate reserve"},
		{name: "infinite reserve rejected", reserve: common.GetPointer(math.Inf(1)), wantError: "invalid OpenRouter approximate reserve"},
		{name: "oversized reserve rejected", reserve: common.GetPointer(1e100), wantError: "quota"},
		{name: "insufficient reserve stops before paid post", reserve: common.GetPointer(2.0), insufficient: true, wantError: "insufficient test quota"},
		{name: "stream rejected", reserve: common.GetPointer(2.0), extra: `,"stream":true`, wantError: "one nonstream text chat response"},
		{name: "multiple outputs rejected", reserve: common.GetPointer(2.0), extra: `,"n":2`, wantError: "one nonstream text chat response"},
		{name: "fallback models rejected", reserve: common.GetPointer(2.0), extra: `,"models":["other/reasoner"]`, wantError: "one nonstream text chat response"},
		{name: "tools rejected", reserve: common.GetPointer(2.0), extra: `,"tools":[{"type":"function","function":{"name":"f"}}]`, wantError: "one nonstream text chat response"},
		{name: "hosted search hold adds to token estimate", reserve: common.GetPointer(2.0), extra: `,"web_search_options":{}`, hostedSearch: true},
		{name: "general hosted search hold adds to token estimate", model: "other/reasoner", general: true, reserve: common.GetPointer(2.0), extra: `,"web_search_options":{}`, hostedSearch: true},
		{name: "general hosted search without channel hold", model: "other/reasoner", general: true, extra: `,"web_search_options":{}`, wantError: "hosted-search quantity is unpriced"},
		{name: "general priced native search reserve is add-on", model: "other/reasoner", general: true, zeroReasoning: true, reserve: common.GetPointer(2.0), hostedSearch: true, reserveUse: openrouter.ReserveAddOn},
		{name: "general priced reserve without search is not applied", model: "other/reasoner", general: true, zeroReasoning: true, rateKey: "web_search", rate: "0", reserve: common.GetPointer(2.0), reserveUse: openrouter.ReserveNotApplied},
		{name: "preset rejected", reserve: common.GetPointer(2.0), extra: `,"preset":"some-preset"`, wantError: "unpriced execution dimensions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestedModel := tc.model
			if requestedModel == "" {
				requestedModel = model
			}
			tag := tc.endpoint
			if tag == "" {
				tag = "perplexity"
			}
			prices := map[string]any{"prompt": "0.000002", "completion": "0.000008", "internal_reasoning": "0.000003", "web_search": "0.005"}
			if tc.zeroReasoning {
				prices["internal_reasoning"] = "0"
			}
			if tc.reserveUse == openrouter.ReserveNotApplied && !tc.zeroReasoning {
				tc.reserveUse = openrouter.ReserveWholeHold
			}
			if tc.rateKey != "" {
				prices[tc.rateKey] = tc.rate
			}
			// Discovery includes fallback metadata; policy must reject the final
			// models[] before reservation rather than merely checking its primary.
			metadata, err := common.Marshal(map[string]any{"data": map[string]any{"endpoints": []any{map[string]any{"tag": tag, "model_id": tc.endpointModel, "context_length": 128000, "max_completion_tokens": 115200, "pricing": prices}}}})
			require.NoError(t, err)
			transport := &sonarAdmissionTransport{metadata: string(metadata)}
			if tc.missingCost {
				transport.response = `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`
			}
			client, err := service.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
			require.NoError(t, err)
			oldTransport := client.Transport
			client.Transport = transport
			t.Cleanup(func() { client.Transport = oldTransport })
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"Reply OK."}],"max_tokens":128%s}`, requestedModel, tc.extra)
			c, recorder, _, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
			info.ChannelType = constant.ChannelTypeOpenRouter
			info.ChannelBaseUrl = "https://openrouter.test/api"
			info.RequestURLPath = "/v1/chat/completions"
			info.RelayMode = relayconstant.RelayModeChatCompletions
			info.ChannelSetting.OpenRouterSonarResearchReserveUSD = tc.reserve
			if tc.general {
				info.ChannelSetting.OpenRouterSonarResearchReserveUSD = nil
				if tc.reserve != nil {
					policyModel := tc.policyModel
					if policyModel == "" {
						policyModel = requestedModel
					}
					info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{policyModel: *tc.reserve}
					if requestedModel == model {
						info.ChannelSetting.OpenRouterSonarResearchReserveUSD = common.GetPointer(1.0)
					}
				}
			}

			info.PriceData.BillingGroupRatio = 1.5
			billing := &sonarAdmissionBilling{}
			if tc.insufficient {
				billing.err = errors.New("insufficient test quota")
			}
			info.Billing = billing
			adaptor := &Adaptor{}
			if tc.nonOpenRouter || tc.operation != "" {
				operation := tc.operation
				if operation == "" {
					operation = "chat/completions"
				}
				if tc.nonOpenRouter {
					info.ChannelType = constant.ChannelTypeOpenAI
				}
				var discovered struct {
					Data struct {
						Endpoints []openrouter.PricedEndpoint `json:"endpoints"`
					} `json:"data"`
				}
				require.NoError(t, common.Unmarshal(metadata, &discovered))
				estimator := openrouter.NewRequestEstimator()
				estimator.Models[requestedModel] = discovered.Data.Endpoints
				_, err := estimator.Estimate(info, []byte(body), operation)
				require.ErrorContains(t, err, tc.wantError)
				assert.Zero(t, transport.posts)
				return
			}
			info.OriginModelName = "public-alias"
			info.UpstreamModelName = requestedModel
			response, err := adaptor.DoRequest(c, info, strings.NewReader(body))
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				var apiErr *types.NewAPIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
				assert.Equal(t, types.ErrorCodeModelPriceError, apiErr.GetErrorCode())
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.Equal(t, 0, transport.posts)
				if info.OpenRouterCost != nil {
					assert.False(t, info.OpenRouterCost.Sent)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, transport.gets)
			assert.Equal(t, 1, transport.posts)
			assert.JSONEq(t, body, string(transport.body), "pricing and paid transport must inspect the same final body")
			require.NotNil(t, info.OpenRouterCost)
			assert.True(t, info.OpenRouterCost.Sent)
			assert.True(t, info.OpenRouterCost.ChatText, "a plain chat completion may defer a missing cost to its generation")
			switch tc.reserveUse {
			case openrouter.ReserveWholeHold:
				require.NotNil(t, info.OpenRouterCost.ApproximateReserveUSD)
				assert.Equal(t, 2.0, *info.OpenRouterCost.ApproximateReserveUSD)
				assert.Equal(t, requestedModel, info.OpenRouterCost.ApproximateReserveModel)
				assert.Nil(t, info.OpenRouterCost.AddOnReserveUSD)
			case openrouter.ReserveAddOn:
				assert.Nil(t, info.OpenRouterCost.ApproximateReserveUSD)
				require.NotNil(t, info.OpenRouterCost.AddOnReserveUSD)
				assert.Equal(t, 2.0, *info.OpenRouterCost.AddOnReserveUSD)
				assert.Equal(t, requestedModel, info.OpenRouterCost.AddOnReserveModel)
			default:
				assert.Nil(t, info.OpenRouterCost.ApproximateReserveUSD)
				assert.Nil(t, info.OpenRouterCost.AddOnReserveUSD)
			}
			if tc.reserveUse == openrouter.ReserveNotApplied {
				assert.Less(t, info.OpenRouterCost.Basis.EstimatedUSD, 0.01, "an unused reserve does not raise the token hold")
			} else if tc.knownAboveReserve {
				assert.Greater(t, info.OpenRouterCost.Basis.EstimatedUSD, 128.0)
				assert.Greater(t, billing.reserved, 1500000)
			} else if tc.hostedSearch {
				assert.Greater(t, info.OpenRouterCost.Basis.EstimatedUSD, 2.0, "search hold is added to the token estimate")
				assert.Less(t, info.OpenRouterCost.Basis.EstimatedUSD, 2.01)
				assert.Greater(t, billing.reserved, 1500000)
			} else {
				assert.Equal(t, 2.0, info.OpenRouterCost.Basis.EstimatedUSD)
				assert.Equal(t, 1500000, billing.reserved)
			}
			assert.Equal(t, billing.reserved, info.FinalPreConsumedQuota)
			assert.True(t, info.ForcePreConsume)
			// Changes after admission cannot reprice the authoritative total.
			info.PriceData.BillingGroupRatio = 9
			*tc.reserve = 7
			if tc.general {
				info.ChannelSetting.OpenRouterModelReserveUSD[requestedModel] = 7
			}
			usage, apiErr := adaptor.DoResponse(c, response.(*http.Response), info)
			if tc.missingCost {
				require.NotNil(t, apiErr)
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.False(t, info.OpenRouterCost.Final)
				assert.False(t, info.OpenRouterCost.Valid)
				assert.Empty(t, recorder.Body.String())
				return
			}
			require.Nil(t, apiErr)
			assert.Equal(t, .47545, usage.(*dto.Usage).Cost)
			quota, clamp, err := info.OpenRouterCost.Basis.Quota(info.OpenRouterCost.CostUSD)
			require.NoError(t, err)
			require.Nil(t, clamp)
			assert.Equal(t, 356588, quota, "total usage.cost, not upstream_inference_cost or local surcharges")
			quota, clamp, err = info.OpenRouterCost.Basis.Quota(3)
			require.NoError(t, err)
			require.Nil(t, clamp)
			assert.Equal(t, 2250000, quota, "full excess is charged, not capped to the approximate hold")
			if tc.reserveUse == openrouter.ReserveWholeHold {
				assert.Equal(t, 2.0, *info.OpenRouterCost.ApproximateReserveUSD)
			}
			assert.Contains(t, recorder.Body.String(), "ok")
		})
	}
}

func TestOpenRouterModelReserveSettings(t *testing.T) {
	for _, tc := range []struct {
		model  string
		source dto.OpenRouterReservationSource
		valid  bool
	}{
		{"other/text", dto.OpenRouterReservationConfiguredPrice, true},
		{"other/text", dto.OpenRouterReservationUpstream, true},
		{"other/*", dto.OpenRouterReservationConfiguredPrice, false},
		{" other/text", dto.OpenRouterReservationConfiguredPrice, false},
		{"other/text", "unknown", false},
		{"other/text", "", false},
	} {
		settings := dto.ChannelSettings{OpenRouterReservationSource: map[string]dto.OpenRouterReservationSource{tc.model: tc.source}}
		if !tc.valid {
			require.Error(t, settings.ValidateOpenRouterReserves())
			continue
		}
		require.NoError(t, settings.ValidateOpenRouterReserves())
		encoded, err := common.Marshal(settings)
		require.NoError(t, err)
		var restored dto.ChannelSettings
		require.NoError(t, common.Unmarshal(encoded, &restored))
		assert.Equal(t, tc.source, restored.OpenRouterReservationSource[tc.model])
	}
	for _, tc := range []struct {
		name, model string
		reserve     float64
		invalid     bool
	}{
		{name: "second exact model", model: "other/reasoner", reserve: 2},
		{name: "zero", model: "other/reasoner", invalid: true},
		{name: "negative", model: "other/reasoner", reserve: -1, invalid: true},
		{name: "infinite", model: "other/reasoner", reserve: math.Inf(1), invalid: true},
		{name: "wildcard", model: "other/*", reserve: 2, invalid: true},
		{name: "whitespace", model: " other/reasoner", reserve: 2, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := dto.ChannelSettings{OpenRouterModelReserveUSD: map[string]float64{tc.model: tc.reserve}}
			if tc.invalid {
				require.Error(t, settings.ValidateOpenRouterReserves())
				return
			}
			require.NoError(t, settings.ValidateOpenRouterReserves())
			reserve := settings.OpenRouterReserveUSD(tc.model)
			require.NotNil(t, reserve)
			settings.OpenRouterModelReserveUSD[tc.model] = 7
			assert.Equal(t, 2.0, *reserve)
			assert.Nil(t, settings.OpenRouterReserveUSD(tc.model+"-alias"))
			otherChannel := dto.ChannelSettings{}
			assert.Nil(t, otherChannel.OpenRouterReserveUSD(tc.model))
		})
	}
}

func TestOpenRouterConfiguredPriceReservation(t *testing.T) {
	const modelID, alias = "other/local-reserve-text", "local-reserve-alias"
	for _, tc := range []struct {
		name, expression, extra, rateKey, rate, wantError string
		price, ratio, minimum                             float64
		source                                            dto.OpenRouterReservationSource
		noPolicy, wrongPolicy, noPrice, useRatio          bool
		missingCost, insufficient, knownAboveLocal        bool
		zeroTariffs                                       bool
	}{
		{name: "fixed configured price", price: 2},
		{name: "configured price preserves minimum", price: 2, minimum: 3},
		{name: "known upstream cost wins", price: 2, rateKey: "completion", rate: "1", knownAboveLocal: true},
		{name: "expression uses effective payload", expression: `param("max_tokens") == 128 ? tier("effective", fixed(2)) : tier("other", fixed(7))`},
		{name: "token expression uses USD per million", expression: `tier("tokens", c * 15625)`},
		{name: "expression receives request headers", expression: `header("x-reserve") == "yes" ? tier("header", fixed(2)) : tier("other", fixed(7))`},
		{name: "legacy ratio convention", useRatio: true, ratio: 5000},
		{name: "unknown reasoning accepted by positive estimate", price: 2, rateKey: "internal_reasoning", rate: "0.000003"},
		{name: "zero local does not erase upstream", price: 0},
		{name: "confirmed zero preserved", price: 0, zeroTariffs: true},
		{name: "zero cannot cover unknown quantity", rateKey: "internal_reasoning", rate: "0.000003", wantError: "requires a positive reserve"},
		{name: "disabled by default", price: 2, noPolicy: true},
		{name: "explicit upstream", price: 2, source: dto.OpenRouterReservationUpstream},
		{name: "policy keyed by final ID not alias", price: 2, wrongPolicy: true},
		{name: "invalid source rejected", price: 2, source: "invalid", wantError: "invalid OpenRouter reservation source"},
		{name: "missing configured price", noPrice: true, wantError: "has no configured reservation price"},
		{name: "negative configured price", price: -1, wantError: "invalid configured reservation price"},
		{name: "huge configured price", price: 1e100, wantError: "quota"},
		{name: "cannot recursively use provider cost", expression: `tier("provider", provider_cost * 1000000)`, wantError: "requires a validated provider cost estimate"},
		{name: "unmeasured audio expression rejected", expression: `p * 2 + ao * 50`, wantError: "unavailable ao quantity"},
		{name: "unmeasured task expression rejected", expression: `u("seconds") * 2`, wantError: "unavailable u quantity"},
		{name: "unsupported quantity uses explicit local reserve", price: 2, rateKey: "research_step", rate: "1"},
		{name: "unavailable tariff uses explicit local reserve", price: 2, rateKey: "prompt", rate: "null"},
		{name: "default source cannot cover missing tariff", price: 2, noPolicy: true, rateKey: "prompt", rate: "null", wantError: "estimate unavailable"},
		{name: "upstream source cannot cover unknown quantity", price: 2, source: dto.OpenRouterReservationUpstream, rateKey: "render_step", rate: "1", wantError: "estimate unavailable"},
		{name: "stream stays closed", price: 2, extra: `,"stream":true`, wantError: "one nonstream text chat response"},
		{name: "nonstream audio output stays closed", price: 2, extra: `,"modalities":["text","audio"]`, wantError: "audio output requires streaming"},
		{name: "tools stay closed", price: 2, extra: `,"tools":[{"type":"function"}]`, wantError: "one nonstream text chat response"},
		{name: "multiple responses stay closed", price: 2, extra: `,"n":2`, wantError: "one nonstream text chat response"},
		{name: "fallback model cannot disable selected policy", price: 2, extra: `,"models":["other/fallback"]`, wantError: "only supports one nonstream text chat response"},
		{name: "insufficient funds before paid POST", price: 2, insufficient: true, wantError: "insufficient test quota"},
		{name: "missing actual cost never becomes estimate", price: 2, missingCost: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldPrices, oldRatios := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString()
			settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
			oldModes, oldExprs := settings.BillingMode, settings.BillingExpr
			t.Cleanup(func() {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
				settings.BillingMode, settings.BillingExpr = oldModes, oldExprs
			})
			settings.BillingMode, settings.BillingExpr = map[string]string{}, map[string]string{}
			if tc.expression != "" {
				settings.BillingMode[alias], settings.BillingExpr[alias] = billing_setting.BillingModeTieredExpr, tc.expression
			} else if !tc.noPrice {
				encoded, err := common.Marshal(map[string]float64{alias: tc.price})
				require.NoError(t, err)
				if tc.useRatio {
					encoded, err = common.Marshal(map[string]float64{alias: tc.ratio})
					require.NoError(t, err)
					require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(encoded)))
				} else {
					require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(encoded)))
				}
			}
			prices := map[string]any{"prompt": "0.000002", "completion": "0.000008"}
			if tc.zeroTariffs {
				prices["prompt"], prices["completion"] = "0", "0"
			}
			if tc.rateKey != "" {
				prices[tc.rateKey] = tc.rate
				if tc.rate == "null" {
					prices[tc.rateKey] = nil
				}
			}
			metadata, err := common.Marshal(map[string]any{"data": map[string]any{"id": modelID, "endpoints": []any{map[string]any{"tag": "other-provider", "model_id": modelID, "max_completion_tokens": 128, "pricing": prices}}}})
			require.NoError(t, err)
			transport := &sonarAdmissionTransport{metadata: string(metadata)}
			if tc.missingCost {
				transport.response = `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{}}`
			}
			client, err := service.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
			require.NoError(t, err)
			oldTransport := client.Transport
			client.Transport = transport
			t.Cleanup(func() { client.Transport = oldTransport })
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"Reply OK."}],"max_tokens":128%s}`, modelID, tc.extra)
			c, recorder, _, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
			info.ChannelType, info.ChannelBaseUrl = constant.ChannelTypeOpenRouter, "https://openrouter.test/api"
			info.RequestURLPath, info.RelayMode = "/v1/chat/completions", relayconstant.RelayModeChatCompletions
			info.OriginModelName, info.UpstreamModelName = alias, modelID
			info.RequestHeaders = map[string]string{"X-Reserve": "yes"}
			info.PriceData.BillingGroupRatio = 1.5
			if !tc.noPolicy {
				policyID, source := modelID, tc.source
				if tc.wrongPolicy {
					policyID = alias
				}
				if source == "" {
					source = dto.OpenRouterReservationConfiguredPrice
				}
				info.ChannelSetting.OpenRouterReservationSource = map[string]dto.OpenRouterReservationSource{policyID: source}
			}
			if tc.minimum > 0 {
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{modelID: tc.minimum}
			}
			billing := &sonarAdmissionBilling{}
			if tc.insufficient {
				billing.err = errors.New("insufficient test quota")
			}
			info.Billing = billing
			adaptor := &Adaptor{}
			response, err := adaptor.DoRequest(c, info, strings.NewReader(body))
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.Zero(t, transport.posts)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, transport.posts)
			assert.JSONEq(t, body, string(transport.body))
			require.NotNil(t, info.OpenRouterCost)
			assert.Nil(t, info.TieredBillingSnapshot)
			assert.Nil(t, info.BillingRequestInput)
			if tc.noPolicy || tc.wrongPolicy || tc.source == dto.OpenRouterReservationUpstream {
				assert.Nil(t, info.OpenRouterCost.ConfiguredReserveUSD)
				assert.Less(t, info.OpenRouterCost.Basis.EstimatedUSD, 2.0)
			} else {
				require.NotNil(t, info.OpenRouterCost.ConfiguredReserveUSD)
				assert.Equal(t, alias, info.OpenRouterCost.ConfiguredReserveModel)
				assert.GreaterOrEqual(t, info.OpenRouterCost.Basis.EstimatedUSD, *info.OpenRouterCost.ConfiguredReserveUSD)
				assert.GreaterOrEqual(t, info.OpenRouterCost.Basis.EstimatedUSD, tc.minimum)
				if tc.expression != "" {
					assert.Equal(t, 2.0, *info.OpenRouterCost.ConfiguredReserveUSD)
					assert.NotEmpty(t, info.OpenRouterCost.ConfiguredReserveExprHash)
				} else if !tc.useRatio {
					assert.Equal(t, tc.price, *info.OpenRouterCost.ConfiguredReserveUSD)
				} else {
					assert.Greater(t, *info.OpenRouterCost.ConfiguredReserveUSD, 1.0)
				}
				if !tc.knownAboveLocal && !tc.useRatio && !tc.zeroTariffs && tc.price > 0 {
					assert.Equal(t, max(tc.price, tc.minimum), info.OpenRouterCost.Basis.EstimatedUSD)
				}
			}
			if tc.knownAboveLocal {
				assert.Greater(t, info.OpenRouterCost.Basis.EstimatedUSD, 128.0)
			}
			info.PriceData.BillingGroupRatio = 9
			usage, apiErr := adaptor.DoResponse(c, response.(*http.Response), info)
			if tc.missingCost {
				require.NotNil(t, apiErr)
				assert.False(t, info.OpenRouterCost.Final)
				assert.Empty(t, recorder.Body.String())
				return
			}
			require.Nil(t, apiErr)
			assert.Equal(t, .47545, usage.(*dto.Usage).Cost)
			quota, clamp, err := info.OpenRouterCost.Basis.Quota(3)
			require.NoError(t, err)
			require.Nil(t, clamp)
			assert.Equal(t, 2250000, quota, "actual may exceed the hold, using the frozen ratio once")
		})
	}
}

func TestOpenRouterChatImageReservationAndResponse(t *testing.T) {
	previousDB, previousLogDB := modelpkg.DB, modelpkg.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousBatch, previousLog := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&modelpkg.Task{}, &modelpkg.User{}, &modelpkg.Token{}, &modelpkg.Channel{}, &modelpkg.Log{}))
	modelpkg.DB, modelpkg.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	require.NoError(t, db.Create(&modelpkg.User{Id: 1, Username: "chat-image-transport", Quota: 1000000000}).Error)
	t.Cleanup(func() {
		modelpkg.DB, modelpkg.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousRedis, previousBatch, previousLog
		require.NoError(t, sqlDB.Close())
	})
	const alias = "chat-image-client-alias"
	service.InitTokenEncoders()
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	pngData := base64.StdEncoding.EncodeToString(pngBytes.Bytes())
	oldPrices := ratio_setting.ModelPrice2JSONString()
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	oldModes, oldExprs := settings.BillingMode, settings.BillingExpr
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		settings.BillingMode, settings.BillingExpr = oldModes, oldExprs
	})
	for _, tc := range []struct {
		name, model, extra, imageRate, otherKey, otherRate string
		cost, wantError, metadata, badExpression           string
		metadataStatus                                     int
		noPrices, noLocal, rawBody, secondEndpoint         bool
		metadataFailure                                    bool
		local, minimum                                     float64
		noPolicy, wrongPolicy, missingRate, expression     bool
		forceFormat, convertedFormat, responses, reject    bool
		bodyID, headerID                                   string
		httpError, disconnected                            bool
	}{
		{name: "Gemini metadata and raw image", model: "google/gemini-2.5-flash-image", local: 2, cost: "3", otherKey: "audio", otherRate: "0.000001"},
		{name: "GPT image survives force format", model: "openai/gpt-5-image", local: 2, cost: "3", forceFormat: true},
		{name: "configured expression sees image size", local: 2, cost: "3", expression: true},
		{name: "explicit minimum wins", local: 2, minimum: 4, cost: "3"},
		{name: "known upstream subtotal wins", local: 2, cost: "3", otherKey: "request", otherRate: "5"},
		{name: "actual explicit zero", local: 2, cost: "0", forceFormat: true},
		{name: "zero image rate preserves known subtotal", local: 0, imageRate: "0", cost: "0"},
		{name: "positive minimum covers unknown quantity", local: 0, minimum: 2, cost: "3"},
		{name: "channel reserve only actual cost", metadata: `{"data":{"id":"other/image-model","architecture":{"output_modalities":["text","image"]},"endpoints":[{"tag":"image-provider","model_id":"other/image-model","max_completion_tokens":128,"pricing":null}]}}`, noPolicy: true, noLocal: true, minimum: 2, noPrices: true, cost: "3"},
		{name: "channel reserve only explicit zero", metadata: `{"data":{"id":"other/image-model","architecture":{"output_modalities":["text","image"]},"endpoints":[{"tag":"image-provider","model_id":"other/image-model","max_completion_tokens":128,"pricing":{"prompt":"0.0000003","completion":"0.0000025","image_output":"0.00003"}}]}}`, noPolicy: true, noLocal: true, minimum: 2, cost: "0"},
		{name: "channel reserve only unknown cost retains hold", metadata: `{"data":{"id":"other/image-model","architecture":{"output_modalities":["text","image"]},"endpoints":[{"tag":"image-provider","model_id":"other/image-model","max_completion_tokens":128,"pricing":{"prompt":"0.0000003","completion":"0.0000025","image_output":"0.00003"}}]}}`, noPolicy: true, noLocal: true, minimum: 2, bodyID: "gen-shared-reserve-unknown"},
		{name: "positive image quantity policy required", local: 0, cost: "3", wantError: "requires a positive reserve"},
		{name: "no implicit image enablement", local: 2, noPolicy: true, wantError: "image output requires configured_price"},
		{name: "policy uses final model not alias", local: 2, wrongPolicy: true, wantError: "image output requires configured_price"},
		{name: "missing image tariff falls back to local formula", local: 2, missingRate: true, expression: true, cost: "3"},
		{name: "null image tariff falls back to local price", local: 2, imageRate: "null", cost: "3"},
		{name: "invalid image tariff falls back to local price", local: 2, imageRate: "-1", cost: "3"},
		{name: "unmeasured dimension falls back to local price", local: 2, otherKey: "render_step", otherRate: "1", cost: "3"},
		{name: "invalid inactive tariff falls back to local price", local: 2, otherKey: "audio", otherRate: "null", cost: "3"},
		{name: "stream remains closed", local: 2, extra: `,"stream":true`, wantError: "one nonstream"},
		{name: "multiple results remain closed", local: 2, extra: `,"n":2`, wantError: "one nonstream"},
		{name: "zero count invalid", local: 2, extra: `,"n":0`, wantError: "invalid OpenRouter output count"},
		{name: "excessive count invalid", local: 2, extra: `,"n":1000000`, wantError: "invalid OpenRouter output count"},
		{name: "tools remain closed", local: 2, extra: `,"tools":[{"type":"function"}]`, wantError: "one nonstream"},
		{name: "input image remains closed", local: 2, extra: `,"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]`, wantError: "one nonstream"},
		{name: "replayed assistant images remain closed", local: 2, extra: `,"messages":[{"role":"assistant","content":null,"images":[{"image_url":{"url":"data:image/png;base64,abc"}}]}]`, wantError: "assistant image inputs are unpriced"},
		{name: "converted output format remains closed", local: 2, convertedFormat: true, wantError: "OpenAI Chat format"},
		{name: "Responses remains closed", local: 2, responses: true, wantError: "one nonstream"},
		{name: "insufficient quota before paid dispatch", local: 2, reject: true, wantError: "insufficient image test quota"},
		{name: "missing terminal cost", local: 2, cost: "", forceFormat: true},
		{name: "null terminal cost", local: 2, cost: "null"},
		{name: "missing cost retains body identity", local: 2, bodyID: "gen-chat-body"},
		{name: "HTTP error retains body identity", local: 2, bodyID: "gen-chat-error", httpError: true},
		{name: "header body identity conflict", local: 2, cost: "3", bodyID: "gen-chat-body", headerID: "gen-chat-header"},
		{name: "client disconnect retains original attempt", local: 2, cost: "3", bodyID: "gen-chat-disconnect", disconnected: true},
		{name: "negative terminal cost", local: 2, cost: "-1"},
		{name: "no prices uses formula and minimum", local: 2, minimum: 4, noPrices: true, expression: true, cost: "3"},
		{name: "unknown metadata plus zero cannot reserve", noPrices: true, wantError: "requires a positive reserve"},
		{name: "zero local plus positive minimum covers unknown", minimum: 2, noPrices: true, cost: "3"},
		{name: "missing local price never falls back", noPrices: true, noLocal: true, wantError: "has no configured reservation price"},
		{name: "invalid local formula never falls back", noPrices: true, badExpression: `u("unknown") * 2`, wantError: "unavailable u quantity"},
		{name: "incomplete candidate retains known candidate floor", local: 2, missingRate: true, secondEndpoint: true, cost: "3"},
		{name: "empty pricing array still has a route", local: 2, metadata: `{"data":{"id":"other/image-model","endpoints":[{"tag":"image-provider","pricing":[]}]}}`, cost: "3"},
		{name: "empty endpoints is unavailable model", local: 2, metadata: `{"data":{"id":"other/image-model","endpoints":[]}}`, wantError: "no available endpoints"},
		{name: "discovery identity mismatch never fallback", local: 2, metadata: `{"data":{"id":"other/wrong","endpoints":[{"tag":"image-provider"}]}}`, wantError: "discovery model mismatch"},
		{name: "route identity mismatch never fallback", local: 2, metadata: `{"data":{"id":"other/image-model","endpoints":[{"tag":"image-provider","model_id":"other/wrong"}]}}`, wantError: "endpoint model does not match"},
		{name: "negative status retains local monetary fallback", local: 2, metadata: `{"data":{"id":"other/image-model","endpoints":[{"tag":"image-provider","status":-1}]}}`, cost: "3"},
		{name: "missing default cap cannot waive context limit", local: 2, metadata: `{"data":{"id":"other/image-model","endpoints":[{"tag":"image-provider","context_length":1}]}}`, extra: `,"max_tokens":null`, wantError: "no compatible"},
		{name: "negative endpoint limits never fallback", local: 2, metadata: `{"data":{"id":"other/image-model","endpoints":[{"tag":"image-provider","context_length":-1}]}}`, wantError: "invalid OpenRouter endpoint token limits"},
		{name: "missing image capability never fallback", local: 2, metadata: `{"data":{"id":"other/image-model","architecture":{"output_modalities":["text"]},"endpoints":[{"tag":"image-provider","status":-1}]}}`, wantError: "does not support requested output modality"},
		{name: "provider filter never fallback", local: 2, noPrices: true, rawBody: true, extra: `,"provider":{"only":["absent-provider"]}`, wantError: "no compatible"},
		{name: "explicit available provider remains eligible", local: 2, noPrices: true, rawBody: true, extra: `,"provider":{"order":["absent-provider","image-provider"],"allow_fallbacks":false}`, cost: "3"},
		{name: "provider outside explicit order never fallback", local: 2, noPrices: true, rawBody: true, extra: `,"provider":{"order":["absent-provider"],"allow_fallbacks":false}`, wantError: "no compatible"},
		{name: "all explicit providers contribute known floor", local: 2, secondEndpoint: true, rawBody: true, extra: `,"provider":{"order":["image-provider","other-provider"],"allow_fallbacks":false}`, cost: "3"},
		{name: "audio output config never fallback", local: 2, noPrices: true, extra: `,"audio":{"voice":"alloy","format":"wav"}`, wantError: "audio output remains unsupported"},
		{name: "implicit audio model never fallback", local: 2, metadata: `{"data":{"id":"other/image-model","architecture":{"output_modalities":["audio"]},"endpoints":[{"tag":"image-provider"}]}}`, extra: `,"modalities":[]`, wantError: "explicit text/image modalities"},
		{name: "required parameters never fallback", local: 2, noPrices: true, rawBody: true, extra: `,"provider":{"require_parameters":true}`, wantError: "no compatible"},
		{name: "unknown max price cannot waive constraint", local: 2, noPrices: true, rawBody: true, extra: `,"provider":{"max_price":{"prompt":1}}`, wantError: "invalid OpenRouter endpoint price"},
		{name: "invalid output limit never fallback", local: 2, noPrices: true, rawBody: true, extra: `,"max_completion_tokens":-1`, wantError: "invalid OpenRouter output cap"},
		{name: "metadata authentication failure never fallback", local: 2, noPrices: true, metadataStatus: 401, wantError: "discovery HTTP 401"},
		{name: "metadata access failure never fallback", local: 2, noPrices: true, metadataStatus: 403, wantError: "discovery HTTP 403"},
		{name: "metadata model missing never fallback", local: 2, noPrices: true, metadataStatus: 404, wantError: "discovery HTTP 404"},
		{name: "metadata server failure never fallback", local: 2, noPrices: true, metadataStatus: 503, wantError: "discovery HTTP 503"},
		{name: "metadata transport failure never fallback", local: 2, noPrices: true, metadataFailure: true, wantError: "pricing discovery failed"},
		{name: "malformed discovery never fallback", local: 2, metadata: `{"data":`, wantError: "invalid OpenRouter endpoint discovery metadata"},
		{name: "fallback missing cost never artifact success", local: 2, noPrices: true, cost: "", forceFormat: true},
		{name: "fallback null cost never artifact success", local: 2, noPrices: true, cost: "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := tc.model
			if model == "" {
				model = "other/image-model"
			}
			settings.BillingMode, settings.BillingExpr = map[string]string{}, map[string]string{}
			prices, err := common.Marshal(map[string]float64{alias: tc.local})
			require.NoError(t, err)
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(prices)))
			if tc.noLocal {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
			}
			if tc.badExpression != "" {
				settings.BillingMode[alias], settings.BillingExpr[alias] = billing_setting.BillingModeTieredExpr, tc.badExpression
			}
			if tc.expression {
				settings.BillingMode[alias] = billing_setting.BillingModeTieredExpr
				settings.BillingExpr[alias] = `param("image_config.image_size") == "2K" ? tier("2k", fixed(2)) : tier("other", fixed(7))`
			}
			imageRate := tc.imageRate
			if imageRate == "" {
				imageRate = "0.00003"
			}
			tariff := map[string]any{"prompt": "0.0000003", "completion": "0.0000025", "image_output": imageRate}
			if tc.missingRate {
				delete(tariff, "image_output")
			} else if imageRate == "null" {
				tariff["image_output"] = nil
			}
			if tc.otherKey != "" {
				tariff[tc.otherKey] = tc.otherRate
				if tc.otherRate == "null" {
					tariff[tc.otherKey] = nil
				}
			}
			var publishedPrices any = tariff
			if tc.noPrices {
				publishedPrices = nil
			}
			endpoints := []any{map[string]any{"tag": "image-provider", "model_id": model, "max_completion_tokens": 128, "pricing": publishedPrices}}
			if tc.secondEndpoint {
				endpoints = append(endpoints, map[string]any{"tag": "other-provider", "model_id": model, "max_completion_tokens": 128, "pricing": map[string]any{"prompt": "0.0000003", "completion": "0.0000025", "image_output": "0.00003", "request": "5"}})
			}
			metadata, err := common.Marshal(map[string]any{"data": map[string]any{"id": model, "endpoints": endpoints}})
			require.NoError(t, err)
			if tc.metadata != "" {
				metadata = []byte(tc.metadata)
			}
			costField := ""
			if tc.cost != "" {
				costField = `"cost":` + tc.cost
			}
			transport := &sonarAdmissionTransport{metadata: string(metadata), response: fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":null,"images":[{"image_url":{"url":"data:image/png;base64,%s"}}]},"finish_reason":"stop"}],"usage":{%s}}`, pngData, costField)}
			if tc.bodyID != "" {
				transport.response = `{"id":"` + tc.bodyID + `",` + strings.TrimPrefix(transport.response, "{")
			}
			if tc.headerID != "" {
				transport.responseHeaders = http.Header{"X-Generation-Id": []string{tc.headerID}, "Content-Type": []string{"application/json"}}
			}
			if tc.httpError {
				transport.responseStatus, transport.response = 502, `{"id":"`+tc.bodyID+`","error":{"message":"upstream unavailable"}}`
			}
			transport.metadataStatus = tc.metadataStatus
			if tc.metadataFailure {
				transport.metadataError = errors.New("injected metadata connection failure")
			}
			client, err := service.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
			require.NoError(t, err)
			oldTransport := client.Transport
			client.Transport = transport
			t.Cleanup(func() { client.Transport = oldTransport })
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"Draw a dot."}],"max_tokens":128,"modalities":["text","image"],"image_config":{"image_size":"2K","aspect_ratio":"1:1"}%s}`, model, tc.extra)
			c, recorder, _, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
			if tc.disconnected {
				ctx, cancel := context.WithCancel(c.Request.Context())
				defer cancel()
				c.Request = c.Request.WithContext(ctx)
				transport.onPost = func(req *http.Request) {
					cancel()
					assert.NoError(t, req.Context().Err())
					assert.Nil(t, req.GetBody)
				}
			}
			info.ChannelType, info.ChannelBaseUrl = constant.ChannelTypeOpenRouter, "https://openrouter.test/api"
			info.RequestURLPath, info.RelayMode = "/v1/chat/completions", relayconstant.RelayModeChatCompletions
			info.OriginModelName, info.UpstreamModelName = alias, model
			info.PriceData.BillingGroupRatio = 1.5
			info.ChannelSetting.ForceFormat = tc.forceFormat
			if tc.responses {
				info.RelayMode = relayconstant.RelayModeResponses
			}
			if tc.convertedFormat {
				info.RelayFormat = types.RelayFormatClaude
			}
			if !tc.noPolicy {
				policyModel := model
				if tc.wrongPolicy {
					policyModel = alias
				}
				info.ChannelSetting.OpenRouterReservationSource = map[string]dto.OpenRouterReservationSource{policyModel: dto.OpenRouterReservationConfiguredPrice}
			}
			if tc.minimum > 0 {
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{model: tc.minimum}
			}
			info.UserId, info.ChannelId, info.BillingSource = 1, 1, "wallet"
			info.ApiKey, info.RequestId = "chat-image-account", t.Name()
			channel := &modelpkg.Channel{Id: 1, Type: constant.ChannelTypeOpenRouter, Key: info.ApiKey, BaseURL: &info.ChannelBaseUrl}
			channel.SetSetting(info.ChannelSetting)
			require.NoError(t, db.Save(channel).Error)
			billing := &sonarAdmissionBilling{}
			if tc.reject {
				billing.err = errors.New("insufficient image test quota")
			}
			info.Billing = billing
			adaptor := &Adaptor{}
			finalBody := []byte(body)
			if !tc.rawBody {
				var request dto.GeneralOpenAIRequest
				require.NoError(t, common.Unmarshal([]byte(body), &request))
				converted, err := adaptor.ConvertOpenAIRequest(c, info, &request)
				require.NoError(t, err)
				finalBody, err = common.Marshal(converted)
				require.NoError(t, err)
			}
			response, err := adaptor.DoRequest(c, info, strings.NewReader(string(finalBody)))
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.Zero(t, transport.posts)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, transport.posts)
			if tc.httpError {
				raw, err := io.ReadAll(response.(*http.Response).Body)
				require.NoError(t, err)
				require.NoError(t, response.(*http.Response).Body.Close())
				assert.Equal(t, transport.response, string(raw))
				task, exists, err := modelpkg.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
				require.NoError(t, err)
				require.True(t, exists)
				assert.Equal(t, tc.bodyID, task.PrivateData.UpstreamTaskID)
				assert.False(t, task.PrivateData.Reconciliation.UpstreamCostConfirmed)
				assert.Equal(t, billing.reserved, task.Quota)
				return
			}
			assert.JSONEq(t, string(finalBody), string(transport.body))
			if tc.noPolicy {
				assert.Nil(t, info.OpenRouterCost.ConfiguredReserveUSD)
			} else {
				require.NotNil(t, info.OpenRouterCost.ConfiguredReserveUSD)
				assert.Equal(t, tc.local, *info.OpenRouterCost.ConfiguredReserveUSD)
			}
			assert.GreaterOrEqual(t, info.OpenRouterCost.Basis.EstimatedUSD, max(tc.local, tc.minimum))
			if tc.otherKey == "request" || tc.secondEndpoint {
				assert.Greater(t, info.OpenRouterCost.Basis.EstimatedUSD, 5.0)
			}
			assert.Nil(t, info.TieredBillingSnapshot)
			assert.Nil(t, info.BillingRequestInput)
			info.PriceData.BillingGroupRatio = 9
			settings.BillingMode[alias] = billing_setting.BillingModeTieredExpr
			settings.BillingExpr[alias] = `tier("changed-after-send", fixed(999))`
			usage, apiErr := adaptor.DoResponse(c, response.(*http.Response), info)
			if tc.headerID != "" && tc.headerID != tc.bodyID {
				require.NotNil(t, apiErr)
				assert.Empty(t, recorder.Body.String(), "reject identity disagreement before delivering a successful result")
				task, exists, err := modelpkg.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
				require.NoError(t, err)
				require.True(t, exists)
				assert.True(t, task.PrivateData.OpenRouterSync.IdentityConflict)
				assert.Equal(t, tc.headerID, task.PrivateData.UpstreamTaskID)
				assert.Equal(t, billing.reserved, task.Quota)
				return
			}
			if tc.cost == "" || tc.cost == "null" || tc.cost == "-1" {
				require.Nil(t, apiErr, "valid image delivery is independent of unknown cost")
				assert.Contains(t, recorder.Body.String(), pngData)
				assert.True(t, info.OpenRouterCost.Sent)
				assert.False(t, info.OpenRouterCost.Final)
				assert.Equal(t, 1, transport.posts, "unknown cost is never replayed")
				task, exists, err := modelpkg.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
				require.NoError(t, err)
				require.True(t, exists)
				assert.True(t, task.PrivateData.OpenRouterSync.ResultReady)
				assert.True(t, task.PrivateData.Reconciliation.Required)
				assert.False(t, task.PrivateData.Reconciliation.UpstreamCostConfirmed)
				assert.Equal(t, billing.reserved, task.Quota)
				assert.Equal(t, tc.bodyID, task.PrivateData.UpstreamTaskID)
				return
			}
			require.Nil(t, apiErr)
			assert.True(t, info.OpenRouterCost.Final)
			assert.True(t, info.OpenRouterCost.Valid)
			assert.Equal(t, info.OpenRouterCost.CostUSD, usage.(*dto.Usage).Cost)
			quota, clamp, err := info.OpenRouterCost.Basis.Quota(info.OpenRouterCost.CostUSD)
			require.NoError(t, err)
			require.Nil(t, clamp)
			if tc.cost == "0" {
				assert.Zero(t, quota)
			} else {
				assert.Equal(t, 2250000, quota, "full actual cost, including excess, uses captured group ratio once")
			}
			var result struct {
				Choices []struct {
					Message struct {
						Images []struct {
							ImageURL struct {
								URL string `json:"url"`
							} `json:"image_url"`
						} `json:"images"`
					} `json:"message"`
				} `json:"choices"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
			require.Len(t, result.Choices, 1)
			require.Len(t, result.Choices[0].Message.Images, 1)
			encoded, ok := strings.CutPrefix(result.Choices[0].Message.Images[0].ImageURL.URL, "data:image/png;base64,")
			require.True(t, ok)
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			require.NoError(t, err)
			image, err := png.Decode(strings.NewReader(string(decoded)))
			require.NoError(t, err)
			assert.Equal(t, 1, image.Bounds().Dx())
			assert.Equal(t, 1, image.Bounds().Dy())
		})
	}
}

func TestOpenRouterAudioReservationAndAuthoritativeCost(t *testing.T) {
	previousDB, previousLogDB := modelpkg.DB, modelpkg.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousBatch, previousLog := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&modelpkg.Task{}, &modelpkg.User{}, &modelpkg.Token{}, &modelpkg.Channel{}, &modelpkg.Log{}))
	modelpkg.DB, modelpkg.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	require.NoError(t, db.Create(&modelpkg.User{Id: 1, Username: "audio-transport", Quota: 10000000}).Error)
	require.NoError(t, db.Create(&modelpkg.Channel{Id: 1}).Error)
	t.Cleanup(func() {
		modelpkg.DB, modelpkg.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousRedis, previousBatch, previousLog
		_ = sqlDB.Close()
	})
	artifactRoot := t.TempDir()
	require.NoError(t, os.Chmod(artifactRoot, 0700))
	t.Cleanup(func() { require.NoError(t, service.InitTaskArtifactStore()) })
	t.Setenv(system_setting.TaskArtifactStoreModeEnv, "filesystem")
	t.Setenv(system_setting.TaskArtifactStoreRootEnv, artifactRoot)
	require.NoError(t, service.InitTaskArtifactStore())
	const alias = "audio-client-alias"
	const model = "vendor/audio-model"
	const chunk = "data: {\"choices\":[{\"delta\":{\"audio\":{\"data\":\"UklGRg==\",\"transcript\":\"hello\"}}}]}\n\n"
	service.InitTokenEncoders()
	oldPrices := ratio_setting.ModelPrice2JSONString()
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	oldModes, oldExprs := settings.BillingMode, settings.BillingExpr
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		settings.BillingMode, settings.BillingExpr = oldModes, oldExprs
	})
	for _, tc := range []struct {
		name, kind, cost, extra, expression, wantError                                               string
		noSource, zeroLocal, minimum, unknownCapability, insufficient, disconnected, storageDisabled bool
		endpointStatus                                                                               int
		missingCost, noDone, provisional, noGenerationID, noLocal                                    bool
		channelReserve                                                                               *float64
	}{
		{name: "Chat audio preserves delta with force format", kind: "chat", cost: "3"},
		{name: "Chat audio without local storage", kind: "chat", storageDisabled: true, cost: "3"},
		{name: "client disconnect still records paid completion", kind: "chat", cost: "3", disconnected: true},
		{name: "music with zero token tariff still holds", kind: "music", cost: "0.04"},
		{name: "music zero configured price cannot imply free", kind: "music", zeroLocal: true, wantError: "positive reserve"},
		{name: "explicit minimum covers music", kind: "music", zeroLocal: true, minimum: true, cost: "0.04"},
		{name: "SSE explicit zero is authoritative", kind: "chat", cost: "0"},
		{name: "Chat audio is opt in", kind: "chat", noSource: true, wantError: "requires configured_price"},
		{name: "Chat audio cannot use nonstream protocol", kind: "chat", extra: `,"stream":false`, wantError: "requires streaming"},
		{name: "negative status music settles authoritative cost", kind: "music", endpointStatus: -5, cost: "0.04"},
		{name: "audio capability must be confirmed", kind: "chat", endpointStatus: -5, unknownCapability: true, wantError: "capability is missing"},
		{name: "audio tools remain closed", kind: "chat", extra: `,"tools":[{"type":"function"}]`, wantError: "without routing or tools"},
		{name: "audio input media remains closed", kind: "chat", extra: `,"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"UklGRg==","format":"wav"}}]}]`, wantError: "duration quantity is unpriced"},
		{name: "reserve fails before paid submission", kind: "chat", insufficient: true, wantError: "insufficient audio quota"},
		{name: "missing stream cost records completion with retained hold", kind: "chat", missingCost: true},
		{name: "null stream cost retains hold", kind: "chat", cost: "null", missingCost: true},
		{name: "invalid stream cost retains hold", kind: "chat", cost: "-1", missingCost: true},
		{name: "unknown stream cost without identity fails", kind: "chat", missingCost: true, noGenerationID: true},
		{name: "speech without identity fails", kind: "tts", noGenerationID: true},
		{name: "unknown transcription without identity fails", kind: "stt", missingCost: true, noGenerationID: true},
		{name: "provisional cost cannot replace terminal cost", kind: "chat", provisional: true, missingCost: true},
		{name: "cost without DONE is incomplete", kind: "chat", cost: "3", noDone: true, missingCost: true},
		{name: "TTS receives raw audio with retained hold", kind: "tts", cost: "3"},
		{name: "TTS numeric playback speed", kind: "tts", cost: "3", extra: `,"speed":1.2`},
		{name: "TTS expression sees final request", kind: "tts", cost: "3", expression: `param("voice") == "alloy" ? tier("voice", fixed(2)) : tier("other", fixed(8))`},
		{name: "TTS no source rejects before submission", kind: "tts", noSource: true, wantError: "requires configured_price"},
		{name: "STT without price or channel opt in fails closed", kind: "stt", noSource: true, noLocal: true, wantError: "requires configured_price"},
		{name: "TTS channel reserve without global price", kind: "tts", noSource: true, noLocal: true, channelReserve: common.GetPointer(0.3)},
		{name: "STT channel reserve settles above hold", kind: "stt", cost: "3", noSource: true, noLocal: true, channelReserve: common.GetPointer(0.3)},
		{name: "STT channel reserve settles below hold", kind: "stt", cost: "0.04", noSource: true, noLocal: true, channelReserve: common.GetPointer(0.3)},
		{name: "STT channel reserve retains unknown cost", kind: "stt", missingCost: true, noSource: true, noLocal: true, channelReserve: common.GetPointer(0.3)},
		{name: "channel reserve does not waive configured price", kind: "stt", noLocal: true, channelReserve: common.GetPointer(0.3), wantError: "no configured reservation price"},
		{name: "configured price exceeds channel reserve", kind: "stt", cost: "3", channelReserve: common.GetPointer(0.3)},
		{name: "channel reserve exceeds configured price", kind: "stt", cost: "3", channelReserve: common.GetPointer(4.0)},
		{name: "zero channel reserve rejected", kind: "tts", noSource: true, channelReserve: common.GetPointer(0.0), wantError: "invalid OpenRouter approximate reserve"},
		{name: "negative channel reserve rejected", kind: "stt", noSource: true, channelReserve: common.GetPointer(-1.0), wantError: "invalid OpenRouter approximate reserve"},
		{name: "NaN channel reserve rejected", kind: "tts", noSource: true, channelReserve: common.GetPointer(math.NaN()), wantError: "invalid OpenRouter approximate reserve"},
		{name: "infinite channel reserve rejected", kind: "stt", noSource: true, channelReserve: common.GetPointer(math.Inf(1)), wantError: "invalid OpenRouter approximate reserve"},
		{name: "oversize channel hold rejected", kind: "tts", noSource: true, channelReserve: common.GetPointer(1e20), wantError: "quota conversion"},
		{name: "negative status TTS uses channel reserve", kind: "tts", noSource: true, endpointStatus: -5, channelReserve: common.GetPointer(0.3), cost: "0.04"},
		{name: "missing capability cannot use channel reserve", kind: "stt", noSource: true, unknownCapability: true, channelReserve: common.GetPointer(0.3), wantError: "capability is missing"},
		{name: "Chat audio channel reserve without global price", kind: "chat", cost: "3", noSource: true, noLocal: true, channelReserve: common.GetPointer(0.3)},
		{name: "Chat audio channel reserve retains unknown cost", kind: "chat", missingCost: true, noSource: true, noLocal: true, channelReserve: common.GetPointer(0.3)},
		{name: "Chat audio channel reserve requires streaming", kind: "chat", extra: `,"stream":false`, noSource: true, noLocal: true, channelReserve: common.GetPointer(0.3), wantError: "requires streaming"},
		{name: "multipart STT final usage cost", kind: "stt", cost: "3"},
		{name: "multipart STT confirmed zero", kind: "stt", cost: "0"},
		{name: "STT expression sees outgoing form", kind: "stt", cost: "3", expression: `param("language") == "en" ? tier("english", fixed(2)) : tier("other", fixed(8))`},
		{name: "STT missing cost persists transcript with retained hold", kind: "stt", missingCost: true},
		{name: "STT rejects unmeasured token expression", kind: "stt", expression: `tier("tokens", p * 10)`, wantError: "unavailable p quantity"},
		{name: "STT text format has no usage envelope", kind: "stt", extra: "text", wantError: "json or verbose_json"},
		{name: "STT unsupported form field rejected", kind: "stt", extra: "provider", wantError: "unsupported provider field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.storageDisabled {
				t.Cleanup(func() { require.NoError(t, service.InitTaskArtifactStore()) })
				t.Setenv(system_setting.TaskArtifactStoreModeEnv, "upstream")
				require.NoError(t, service.InitTaskArtifactStore())
			}
			price := 2.0
			if tc.zeroLocal {
				price = 0
			}
			priceMap := map[string]float64{alias: price}
			if tc.noLocal {
				priceMap = map[string]float64{}
			}
			prices, err := common.Marshal(priceMap)
			require.NoError(t, err)
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(prices)))
			settings.BillingMode, settings.BillingExpr = map[string]string{}, map[string]string{}
			if tc.expression != "" {
				settings.BillingMode[alias], settings.BillingExpr[alias] = billing_setting.BillingModeTieredExpr, tc.expression
			}
			modality := "audio"
			mode, format, path := relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI, "/v1/chat/completions"
			stream := true
			body := `{"model":"vendor/audio-model","messages":[{"role":"user","content":"hello"}],"modalities":["text","audio"],"audio":{"voice":"alloy","format":"wav"},"max_tokens":128,"stream":true` + tc.extra + `}`
			if tc.kind == "music" {
				body = `{"model":"vendor/audio-model","messages":[{"role":"user","content":"play a tune"}],"modalities":["audio"],"max_tokens":128,"stream":true}`
			}
			if tc.kind == "tts" {
				mode, format, path, modality, stream = relayconstant.RelayModeAudioSpeech, types.RelayFormatOpenAIAudio, "/v1/audio/speech", "speech", false
				body = `{"model":"vendor/audio-model","input":"hello","voice":"alloy","response_format":"mp3"` + tc.extra + `}`
			}
			contentType := "application/json"
			if tc.kind == "stt" {
				mode, format, path, modality, stream = relayconstant.RelayModeAudioTranscription, types.RelayFormatOpenAIAudio, "/v1/audio/transcriptions", "transcription", false
				var b bytes.Buffer
				writer := multipart.NewWriter(&b)
				require.NoError(t, writer.WriteField("model", model))
				require.NoError(t, writer.WriteField("language", "en"))
				responseFormat := "verbose_json"
				if tc.extra == "text" {
					responseFormat = "text"
				}
				require.NoError(t, writer.WriteField("response_format", responseFormat))
				if tc.extra == "provider" {
					require.NoError(t, writer.WriteField("provider", `{"only":["absent"]}`))
				}
				file, err := writer.CreateFormFile("file", "sample.wav")
				require.NoError(t, err)
				_, err = file.Write([]byte("RIFF audio fixture"))
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				body, contentType = b.String(), writer.FormDataContentType()
			}
			status := tc.endpointStatus
			var modalities any = []string{"text", modality}
			if tc.unknownCapability {
				modalities = nil
			}
			metadata, err := common.Marshal(map[string]any{"data": map[string]any{"id": model, "architecture": map[string]any{"output_modalities": modalities}, "endpoints": []any{map[string]any{"tag": "audio-provider", "model_id": model, "status": status, "max_completion_tokens": 128, "pricing": map[string]string{"prompt": "0", "completion": "0"}}}}})
			require.NoError(t, err)
			cost := `"cost":` + tc.cost
			if tc.cost == "" {
				cost = ""
			}
			transport := &sonarAdmissionTransport{metadata: string(metadata), responseHeaders: http.Header{"Content-Type": []string{"application/json"}, "X-Generation-Id": []string{"gen-audio-fixture"}}}
			if tc.kind == "tts" {
				transport.response, transport.responseHeaders = "RIFF upstream audio", http.Header{"Content-Type": []string{"audio/mpeg"}, "X-Generation-Id": []string{"gen-tts-fixture"}}
			} else if tc.kind == "stt" {
				transport.response = `{"text":"hello transcript","usage":{` + cost + `}}`
			} else {
				transport.responseHeaders = http.Header{"Content-Type": []string{"text/event-stream"}, "X-Generation-Id": []string{"gen-audio-fixture"}}
				transport.response = chunk
				if tc.provisional {
					transport.response += "data: {\"choices\":[],\"usage\":{\"cost\":0.1}}\n\n"
				}
				transport.response += "data: {\"choices\":[],\"usage\":{" + cost + "}}\n\n"
				if !tc.noDone {
					transport.response += "data: [DONE]\n\n"
				}
			}
			if tc.noGenerationID {
				transport.responseHeaders.Del("X-Generation-Id")
			}
			client, err := service.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
			require.NoError(t, err)
			oldTransport := client.Transport
			client.Transport = transport
			t.Cleanup(func() { client.Transport = oldTransport })
			c, recorder, _, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
			c.Request.Header.Set("Content-Type", contentType)
			var paidContext context.Context
			if tc.disconnected {
				ctx, cancel := context.WithCancel(c.Request.Context())
				defer cancel()
				c.Request = c.Request.WithContext(ctx)
				transport.onPost = func(req *http.Request) {
					paidContext = req.Context()
					cancel()
					require.NoError(t, req.Context().Err(), "paid request owns a bounded independent context")
					assert.Nil(t, req.GetBody, "paid request cannot replay")
				}
			}
			info.UserId, info.ChannelId, info.BillingSource = 1, 1, "wallet"
			info.ChannelType, info.ChannelBaseUrl = constant.ChannelTypeOpenRouter, "https://openrouter.test/api"
			info.OriginModelName, info.UpstreamModelName = alias, model
			if tc.noLocal {
				info.OriginModelName = model
			}
			info.RelayMode, info.RelayFormat, info.RequestURLPath, info.IsStream = mode, format, path, stream
			if strings.Contains(tc.extra, `"stream":false`) {
				info.IsStream = false
			}
			info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent = true, true
			info.PriceData.BillingGroupRatio = 1.5
			if !tc.noSource {
				info.ChannelSetting.OpenRouterReservationSource = map[string]dto.OpenRouterReservationSource{model: dto.OpenRouterReservationConfiguredPrice}
			}
			if tc.minimum {
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{model: 2}
			}
			if tc.channelReserve != nil {
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{model: *tc.channelReserve}
			}
			billing := &sonarAdmissionBilling{}
			if tc.insufficient {
				billing.err = errors.New("insufficient audio quota")
			}
			info.Billing = billing
			adaptor := &Adaptor{}
			response, err := adaptor.DoRequest(c, info, strings.NewReader(body))
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.Zero(t, transport.posts)
				assert.Empty(t, recorder.Body.String())
				return
			}
			require.NoError(t, err)
			require.NotNil(t, info.OpenRouterCost)
			reserve := price
			if tc.noSource {
				reserve = 0
			}
			if tc.minimum {
				reserve = max(reserve, 2)
			}
			if tc.channelReserve != nil {
				reserve = max(reserve, *tc.channelReserve)
				assert.Equal(t, tc.channelReserve, info.OpenRouterCost.ApproximateReserveUSD)
			}
			wantHold := common.QuotaRound(reserve * common.QuotaPerUnit * 1.5)
			require.Equal(t, wantHold, billing.reserved)
			if tc.noLocal {
				assert.Nil(t, info.OpenRouterCost.ConfiguredReserveUSD)
			}
			assert.Equal(t, 1, billing.reserveCalls, "audio intent reserves once")
			assert.Equal(t, body, string(transport.body), "reservation must not rewrite the paid order")
			assert.Equal(t, 1, transport.posts)
			assert.Nil(t, info.TieredBillingSnapshot)
			if tc.channelReserve != nil {
				info.ChannelSetting.OpenRouterModelReserveUSD[model] = 99
				assert.Equal(t, tc.channelReserve, info.OpenRouterCost.ApproximateReserveUSD, "admitted reserve is a snapshot")
			}
			info.PriceData.BillingGroupRatio = 9
			settings.BillingExpr[alias] = `tier("changed", fixed(999))`
			usage, apiErr := adaptor.DoResponse(c, response.(*http.Response), info)
			if tc.disconnected {
				require.ErrorIs(t, paidContext.Err(), context.Canceled, "closing response releases detached deadline")
			}
			if tc.noDone || tc.noGenerationID {
				require.NotNil(t, apiErr)
				assert.Empty(t, recorder.Body.String(), "unknown cost cannot publish successful audio/transcript")
				assert.False(t, info.OpenRouterCost.Final)
				assert.Equal(t, 1, transport.posts, "never replay a paid submission")
				return
			}
			require.Nil(t, apiErr)
			task, exists, err := modelpkg.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
			require.NoError(t, err)
			require.True(t, exists)
			require.NotNil(t, task.PrivateData.OpenRouterSyncState())
			assert.True(t, task.PrivateData.OpenRouterSyncState().ResultReady)
			assert.Equal(t, info.OpenRouterCost.TaskID, recorder.Header().Get("X-Task-Id"))
			assert.Empty(t, task.PrivateData.OpenRouterSyncState().ResultSHA256)
			files, err := os.ReadDir(artifactRoot)
			require.NoError(t, err)
			assert.Empty(t, files, "synchronous responses must not write artifact files")
			assert.Equal(t, 1, transport.posts)
			if tc.kind == "tts" || tc.missingCost {
				assert.False(t, info.OpenRouterCost.Final)
				if tc.kind == "tts" {
					assert.Equal(t, "RIFF upstream audio", recorder.Body.String())
				} else if tc.kind == "stt" {
					assert.Contains(t, recorder.Body.String(), "hello transcript")
				} else {
					assert.Contains(t, recorder.Body.String(), "data: [DONE]")
				}
				assert.Equal(t, wantHold, task.Quota, "unknown cost retains the captured hold")
				assert.False(t, task.PrivateData.Reconciliation.Finalized)
				assert.Equal(t, 1, transport.gets, "delivery never waits for a cost GET")
				return
			}
			require.True(t, info.OpenRouterCost.Final)
			assert.Equal(t, info.OpenRouterCost.CostUSD, usage.(*dto.Usage).Cost)
			quota, clamp, err := info.OpenRouterCost.Basis.Quota(info.OpenRouterCost.CostUSD)
			require.NoError(t, err)
			require.Nil(t, clamp)
			switch tc.cost {
			case "3":
				assert.Equal(t, 2250000, quota)
			case "0":
				assert.Zero(t, quota)
			case "0.04":
				assert.Equal(t, 30000, quota)
			}
			if tc.kind == "stt" {
				assert.Equal(t, quota, task.Quota, "actual cost replaces the hold using frozen multipliers")
				assert.True(t, task.PrivateData.Reconciliation.Finalized)
				assert.Contains(t, recorder.Body.String(), "hello transcript")
			} else {
				assert.Contains(t, recorder.Body.String(), chunk)
				assert.Contains(t, recorder.Body.String(), "data: [DONE]")
				assert.NotContains(t, recorder.Body.String(), `"cost"`)
			}
		})
	}
}

func TestOpenRouterSyncProviderFinalPayload(t *testing.T) {
	const prefs = `{"only":["openai"],"allow_fallbacks":false}`
	for _, operation := range []struct {
		name, path, body string
		mode             int
		format           types.RelayFormat
	}{
		{"chat", "/v1/chat/completions", `{"model":"vendor/provider-test","messages":[{"role":"user","content":"OK"}],"max_tokens":8}`, relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI},
		{"messages converted to Chat", "/v1/messages", `{"model":"vendor/provider-test","messages":[{"role":"user","content":"OK"}],"max_tokens":8}`, relayconstant.RelayModeUnknown, types.RelayFormatClaude},
		{"embeddings", "/v1/embeddings", `{"model":"vendor/provider-test","input":"OK"}`, relayconstant.RelayModeEmbeddings, types.RelayFormatEmbedding},
		{"responses", "/v1/responses", `{"model":"vendor/provider-test","input":"OK","max_output_tokens":8}`, relayconstant.RelayModeResponses, types.RelayFormatOpenAIResponses},
	} {
		for _, variant := range []struct {
			name, extra, want            string
			override                     map[string]any
			conflict, nonOR, unavailable bool
		}{
			{name: "absent"},
			{name: "top level", extra: `,"provider":` + prefs, want: prefs},
			{name: "legacy nested", extra: `,"extra_body":{"provider":` + prefs + `}`, want: prefs},
			{name: "matching aliases", extra: `,"provider":` + prefs + `,"extra_body":{"provider":` + prefs + `}`, want: prefs},
			{name: "admin replacement", extra: `,"extra_body":{"provider":` + prefs + `}`, override: map[string]any{"provider": map[string]any{"only": []string{"azure"}, "allow_fallbacks": false}}, want: `{"only":["azure"],"allow_fallbacks":false}`},
			{name: "legacy admin move", extra: `,"extra_body":{"provider":` + prefs + `}`, override: map[string]any{"operations": []any{map[string]any{"mode": "move", "from": "extra_body.provider", "to": "provider"}}}, want: prefs},
			{name: "legacy admin copy", extra: `,"extra_body":{"provider":` + prefs + `}`, override: map[string]any{"operations": []any{map[string]any{"mode": "copy", "from": "extra_body.provider", "to": "provider"}}}, want: prefs},
			{name: "unavailable provider before paid dispatch", extra: `,"provider":{"only":["missing"],"allow_fallbacks":false}`, unavailable: true},
			{name: "conflict before reservation", extra: `,"provider":` + prefs + `,"extra_body":{"provider":{"only":["azure"]}}`, conflict: true},
			{name: "non OR preserves own provider", extra: `,"provider":"vendor-specific"`, nonOR: true},
		} {
			t.Run(operation.name+"/"+variant.name, func(t *testing.T) {
				transport := &sonarAdmissionTransport{metadata: `{"data":{"id":"vendor/provider-test","endpoints":[{"tag":"openai","context_length":128000,"max_completion_tokens":128,"pricing":{"prompt":"0.000001","completion":"0.000002"}},{"tag":"azure","context_length":128000,"max_completion_tokens":128,"pricing":{"prompt":"0.000003","completion":"0.000004"}}]}}`}
				client, err := service.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
				require.NoError(t, err)
				previous := client.Transport
				client.Transport = transport
				t.Cleanup(func() { client.Transport = previous })
				original := strings.TrimSuffix(operation.body, "}") + variant.extra + "}"
				c, _, _, info := newEmbeddedErrorTestRequest(original, http.StatusOK)
				c.Request = httptest.NewRequest(http.MethodPost, operation.path, strings.NewReader(original))
				c.Request.Header.Set("Content-Type", "application/json")
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
				info.ChannelType = constant.ChannelTypeOpenRouter
				if variant.nonOR {
					info.ChannelType = constant.ChannelTypeOpenAI
				}
				info.ChannelBaseUrl, info.RequestURLPath = "https://openrouter.test/api", operation.path
				info.RelayMode, info.RelayFormat = operation.mode, operation.format
				info.UpstreamModelName, info.OriginModelName = "vendor/provider-test", "vendor/provider-test"
				info.ParamOverride = variant.override
				info.PriceData.BillingGroupRatio = 1
				billing := &sonarAdmissionBilling{}
				info.Billing = billing
				// Model the protocol DTO round trip that loses provider in these APIs.
				parsed := []byte(operation.body)
				if strings.Contains(variant.extra, "extra_body") {
					var fields map[string]any
					require.NoError(t, common.Unmarshal([]byte(original), &fields))
					delete(fields, "provider")
					parsed, err = common.Marshal(fields)
					require.NoError(t, err)
				}
				if variant.nonOR {
					parsed = []byte(original)
				}
				final, err := relaycommon.ApplyRequestOverrides(c, parsed, info)
				if variant.conflict {
					require.ErrorContains(t, err, "conflicting")
					assert.Zero(t, billing.reserveCalls)
					assert.Zero(t, transport.posts)
					return
				}
				require.NoError(t, err)
				response, err := (&Adaptor{}).DoRequest(c, info, bytes.NewReader(final))
				if variant.unavailable {
					require.ErrorContains(t, err, "no compatible")
					assert.Zero(t, billing.reserveCalls)
					assert.Zero(t, transport.posts)
					return
				}
				require.NoError(t, err)
				require.NoError(t, response.(*http.Response).Body.Close())
				assert.Equal(t, 1, transport.posts)
				var sent map[string]any
				require.NoError(t, common.Unmarshal(transport.body, &sent))
				if variant.nonOR {
					assert.Equal(t, "vendor-specific", sent["provider"])
					assert.Zero(t, billing.reserveCalls)
					assert.Zero(t, transport.gets)
				} else if variant.want == "" {
					assert.NotContains(t, sent, "provider")
				} else {
					actual, err := common.Marshal(sent["provider"])
					require.NoError(t, err)
					assert.JSONEq(t, variant.want, string(actual))
					assert.NotContains(t, sent, "extra_body")
					assert.Positive(t, billing.reserved)
				}
			})
		}
	}
}

func TestOpenRouterRerankProviderRequiresReservationContract(t *testing.T) {
	const model = "vendor/rerank"
	const prefs = `{"only":["cohere"],"allow_fallbacks":false}`
	for _, tc := range []struct{ name, extra string }{
		{"absent", ""},
		{"provider", `,"provider":` + prefs},
		{"nested provider", `,"extra_body":{"provider":` + prefs + `}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &sonarAdmissionTransport{metadata: `{"data":{"id":"vendor/rerank","architecture":{"output_modalities":["rerank"]},"endpoints":[{"tag":"cohere","model_id":"vendor/rerank","pricing":{"prompt":"0.000001","completion":"0"}}]}}`}
			client, err := service.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
			require.NoError(t, err)
			previous := client.Transport
			client.Transport = transport
			t.Cleanup(func() { client.Transport = previous })
			original := `{"model":"vendor/rerank","query":"hi","documents":["hi"]` + tc.extra + `}`
			c, _, _, info := newEmbeddedErrorTestRequest(original, http.StatusOK)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/rerank", strings.NewReader(original))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info.ChannelType, info.RelayMode, info.RelayFormat = constant.ChannelTypeOpenRouter, relayconstant.RelayModeRerank, types.RelayFormatRerank
			info.ChannelBaseUrl, info.RequestURLPath = "https://openrouter.test/api", "/v1/rerank"
			info.UpstreamModelName, info.OriginModelName = model, model
			billing := &sonarAdmissionBilling{}
			info.Billing = billing
			body, err := relaycommon.ApplyRequestOverrides(c, []byte(`{"model":"vendor/rerank","query":"hi","documents":["hi"]}`), info)
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, common.Unmarshal(body, &fields))
			if tc.extra == "" {
				assert.NotContains(t, fields, "provider")
			} else {
				actual, err := common.Marshal(fields["provider"])
				require.NoError(t, err)
				assert.JSONEq(t, prefs, string(actual))
				assert.NotContains(t, fields, "extra_body")
			}
			// Exercise the fixture even before Rerank admission is enabled. A
			// discovery/transport failure must never satisfy the contract check.
			require.NoError(t, openrouter.NewRequestEstimator().Discover(c, info, &Adaptor{}, model))
			assert.Equal(t, 1, transport.gets)
			// No configured price or explicit reserve: enabling Rerank may move
			// the financial rejection past discovery, but must not permit a POST.
			response, err := (&Adaptor{}).DoRequest(c, info, bytes.NewReader(body))
			require.Error(t, err)
			require.Contains(t, []string{
				"OpenRouter operation lacks a validated cost reservation contract",
				"OpenRouter rerank requires configured_price or an explicit approximate channel reserve and Rerank format",
			}, err.Error())
			assert.Nil(t, response)
			assert.Zero(t, transport.posts)
			assert.Zero(t, billing.reserveCalls)
			assert.Nil(t, info.OpenRouterCost)
		})
	}
}

func TestOpenRouterAudioProviderConversion(t *testing.T) {
	const policy = `{"zdr":true,"data_collection":"deny","options":{"openai":{"language":"en"}}}`
	for _, kind := range []string{"speech", "transcriptions"} {
		for _, tc := range []struct {
			name, prefs, want string
			strict, nested    bool
			override          map[string]any
		}{
			{name: "policy and options", prefs: `{"zdr":true,"data_collection":"deny","options":{"openai":{"language":"en"}}}`},
			{name: "admin policy options replacement", prefs: policy, override: map[string]any{"provider": map[string]any{"zdr": false, "data_collection": "allow", "options": map[string]any{"openai": map[string]any{"language": "fr"}}}}, want: `{"zdr":false,"data_collection":"allow","options":{"openai":{"language":"fr"}}}`},
			{name: "legacy nested policy", prefs: policy, nested: true},
			{name: "legacy admin move", prefs: policy, nested: true, override: map[string]any{"operations": []any{map[string]any{"mode": "move", "from": "extra_body.provider", "to": "provider"}}}},
			{name: "strict selection rejected", prefs: `{"only":["openai"],"allow_fallbacks":false}`, strict: true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				path := "/v1/audio/" + kind
				want := tc.want
				if want == "" {
					want = tc.prefs
				}
				c, _, _, info := newEmbeddedErrorTestRequest("", http.StatusOK)
				info.ChannelType, info.RelayFormat = constant.ChannelTypeOpenRouter, types.RelayFormatOpenAIAudio
				info.RelayMode = relayconstant.RelayModeAudioSpeech
				info.ParamOverride = tc.override
				request := dto.AudioRequest{Model: "vendor/audio", Input: "hello", Voice: common.GetPointer("alloy")}
				if kind == "speech" {
					routing := `"provider":` + tc.prefs
					if tc.nested {
						routing = `"extra_body":{"provider":` + tc.prefs + `}`
					}
					body := `{"model":"vendor/audio","input":"hello","voice":"alloy",` + routing + `}`
					c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
					c.Request.Header.Set("Content-Type", "application/json")
				} else {
					info.RelayMode = relayconstant.RelayModeAudioTranscription
					var body bytes.Buffer
					writer := multipart.NewWriter(&body)
					require.NoError(t, writer.WriteField("model", request.Model))
					if tc.nested {
						require.NoError(t, writer.WriteField("extra_body", `{"provider":`+tc.prefs+`}`))
					} else {
						require.NoError(t, writer.WriteField("provider", tc.prefs))
					}
					part, err := writer.CreateFormFile("file", "audio.wav")
					require.NoError(t, err)
					_, err = part.Write([]byte("RIFF fixture"))
					require.NoError(t, err)
					require.NoError(t, writer.Close())
					c.Request = httptest.NewRequest(http.MethodPost, path, &body)
					c.Request.Header.Set("Content-Type", writer.FormDataContentType())
				}
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
				adaptor := &Adaptor{}
				converted, err := adaptor.ConvertAudioRequest(c, info, request)
				if kind == "transcriptions" && tc.strict {
					require.ErrorContains(t, err, "unsupported provider field")
					return
				}
				require.NoError(t, err)
				body, err := io.ReadAll(converted)
				require.NoError(t, err)
				if tc.strict {
					_, err = openrouter.PrepareRequest(c, info, adaptor, bytes.NewReader(body), "audio/"+kind)
					require.ErrorContains(t, err, "unsupported provider field")
					assert.Nil(t, info.OpenRouterCost, "rejected before reserve or submission")
					return
				}
				if kind == "speech" {
					var sent map[string]any
					require.NoError(t, common.Unmarshal(body, &sent))
					prefs, err := common.Marshal(sent["provider"])
					require.NoError(t, err)
					assert.JSONEq(t, want, string(prefs))
				} else {
					upload := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
					upload.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
					require.NoError(t, upload.ParseMultipartForm(1<<20))
					t.Cleanup(func() { require.NoError(t, upload.MultipartForm.RemoveAll()) })
					assert.JSONEq(t, want, upload.FormValue("provider"))
					assert.Empty(t, upload.FormValue("extra_body"))
					assert.Len(t, upload.MultipartForm.Value["provider"], 1)
					assert.Len(t, upload.MultipartForm.File["file"], 1)
				}
			})
		}
	}
}

// Local upstream fixtures exercise the real reservation, dispatch, completion and
// wallet/token settlement chain; they do not establish live provider support.
func TestOpenRouterRerankDurableReservation(t *testing.T) {
	previousLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 100
	t.Cleanup(func() { constant.TaskQueryLimit = previousLimit })
	previousDB, previousLogDB := modelpkg.DB, modelpkg.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousBatch, previousLog := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	dialect := os.Getenv("FX_TASK_DB_DIALECT")
	openDB := func(dsn string) (*gorm.DB, error) {
		switch dialect {
		case "mysql":
			return gorm.Open(mysql.Open(dsn), &gorm.Config{})
		case "postgres":
			return gorm.Open(postgres.Open(dsn), &gorm.Config{})
		default:
			return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		}
	}
	mainDSN, logDSN := os.Getenv("FX_TASK_MAIN_DSN"), os.Getenv("FX_TASK_LOG_DSN")
	databaseType := common.DatabaseTypeSQLite
	if dialect == "" || dialect == "sqlite" {
		mainDSN, logDSN = ":memory:", ":memory:"
	} else {
		require.NotEmpty(t, mainDSN)
		require.NotEmpty(t, logDSN)
		if dialect == "mysql" {
			databaseType = common.DatabaseTypeMySQL
		} else {
			require.Equal(t, "postgres", dialect)
			databaseType = common.DatabaseTypePostgreSQL
		}
	}
	db, err := openDB(mainDSN)
	require.NoError(t, err)
	logDB, err := openDB(logDSN)
	require.NoError(t, err)
	for _, database := range []*gorm.DB{db, logDB} {
		sqlDB, err := database.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	}
	var version string
	query := "SELECT sqlite_version()"
	if dialect == "mysql" || dialect == "postgres" {
		query = "SELECT version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("real %s database version: %s", databaseType, version)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&modelpkg.Task{}, &modelpkg.User{}, &modelpkg.Token{}, &modelpkg.Channel{}))
		require.NoError(t, logDB.AutoMigrate(&modelpkg.Log{}))
	}
	modelpkg.DB, modelpkg.LOG_DB = db, logDB
	common.SetDatabaseTypes(databaseType, databaseType)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	t.Cleanup(func() {
		modelpkg.DB, modelpkg.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousRedis, previousBatch, previousLog
	})
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0700))
	t.Cleanup(func() { require.NoError(t, service.InitTaskArtifactStore()) })
	t.Setenv(system_setting.TaskArtifactStoreModeEnv, "filesystem")
	t.Setenv(system_setting.TaskArtifactStoreRootEnv, root)
	require.NoError(t, service.InitTaskArtifactStore())
	oldPrices := ratio_setting.ModelPrice2JSONString()
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	oldModes, oldExprs := settings.BillingMode, settings.BillingExpr
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		settings.BillingMode, settings.BillingExpr = oldModes, oldExprs
	})
	for i, tc := range []struct {
		name, family, cost, extra, wantError, metadata, crash                                                              string
		reserve                                                                                                            *float64
		configured, missingPrice, expression, noCapability, noID, nonOR, timeout, upstreamError, storageDisabled, deadline bool
		endpointStatus                                                                                                     int
	}{
		{name: "token family below hold", family: "token", reserve: common.GetPointer(.02), cost: "0.01"},
		{name: "search family above hold", family: "search", reserve: common.GetPointer(.02), cost: "0.03"},
		{name: "authoritative zero", reserve: common.GetPointer(.02), cost: "0"},
		{name: "zero catalog prices require opt in", wantError: "requires configured_price"},
		{name: "zero hold", reserve: common.GetPointer(0.0), wantError: "invalid OpenRouter approximate reserve"},
		{name: "negative hold", reserve: common.GetPointer(-1.0), wantError: "invalid OpenRouter approximate reserve"},
		{name: "NaN hold", reserve: common.GetPointer(math.NaN()), wantError: "invalid OpenRouter approximate reserve"},
		{name: "infinite hold", reserve: common.GetPointer(math.Inf(1)), wantError: "invalid OpenRouter approximate reserve"},
		{name: "oversized hold", reserve: common.GetPointer(1e20), wantError: "quota conversion"},
		{name: "configured override", configured: true, cost: "0.01"},
		{name: "configured and channel floor", configured: true, reserve: common.GetPointer(.04), cost: "0.01"},
		{name: "configured source is not waived", configured: true, missingPrice: true, reserve: common.GetPointer(.02), wantError: "no configured reservation price"},
		{name: "unmeasured token expression rejected", configured: true, expression: true, wantError: "unavailable p quantity"},
		{name: "unknown capability", reserve: common.GetPointer(.02), endpointStatus: -1, noCapability: true, wantError: "rerank output capability"},
		{name: "negative status settles authoritative cost", reserve: common.GetPointer(.02), endpointStatus: -1, cost: "0.01"},
		{name: "final provider constraint", reserve: common.GetPointer(.02), extra: `,"provider":{"only":["absent"]}`, wantError: "no compatible"},
		{name: "Chat cap is not a bound", reserve: common.GetPointer(.02), extra: `,"max_output_tokens":1`, wantError: "Chat output bounds"},
		{name: "stream closed", reserve: common.GetPointer(.02), extra: `,"stream":true`, wantError: "without routing or tools"},
		{name: "missing cost awaits metadata", reserve: common.GetPointer(.02)},
		{name: "missing cost without identity holds manually", reserve: common.GetPointer(.02), noID: true},
		{name: "null cost retains hold", reserve: common.GetPointer(.02), cost: "null"},
		{name: "negative cost retains hold", reserve: common.GetPointer(.02), cost: "-1"},
		{name: "string cost retains hold", reserve: common.GetPointer(.02), cost: `"0.01"`},
		{name: "malformed usage retains hold", reserve: common.GetPointer(.02), cost: "malformed"},
		{name: "missing metadata retains hold", reserve: common.GetPointer(.02), metadata: `{"data":{}}`},
		{name: "metadata confirms cost", reserve: common.GetPointer(.02), metadata: `{"data":{"id":"gen-rerank","model":"vendor/reranker","api_type":"rerank","cancelled":false,"generation_time":1,"total_cost":0.01}}`},
		{name: "wrong metadata operation retains hold", reserve: common.GetPointer(.02), metadata: `{"data":{"id":"gen-rerank","model":"vendor/reranker","api_type":"tts","cancelled":false,"generation_time":1,"total_cost":0}}`},
		{name: "crash after dispatch retains hold", reserve: common.GetPointer(.02), crash: "dispatch"},
		{name: "crash before reserve is unknown", reserve: common.GetPointer(.02), crash: "intent"},
		{name: "malformed metadata retains hold", reserve: common.GetPointer(.02), metadata: `broken`},
		{name: "wrong metadata identity retains hold", reserve: common.GetPointer(.02), metadata: `{"data":{"id":"other","total_cost":0}}`},
		{name: "upstream error retains hold", reserve: common.GetPointer(.02), upstreamError: true},
		{name: "deadline after dispatch retains hold", reserve: common.GetPointer(.02), deadline: true},
		{name: "transport disconnect retains hold", reserve: common.GetPointer(.02), timeout: true},
		{name: "rerank without local storage", reserve: common.GetPointer(.02), storageDisabled: true, cost: "0.01"},
		{name: "non OpenRouter unchanged", nonOR: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.storageDisabled {
				t.Cleanup(func() { require.NoError(t, service.InitTaskArtifactStore()) })
				t.Setenv(system_setting.TaskArtifactStoreModeEnv, "upstream")
				require.NoError(t, service.InitTaskArtifactStore())
			}
			id := i + 1
			require.NoError(t, db.Create(&modelpkg.User{Id: id, Username: fmt.Sprintf("rerank-%d", id), AffCode: fmt.Sprint(id), Quota: 10000000}).Error)
			key := fmt.Sprintf("rerank-key-%d", id)
			require.NoError(t, db.Create(&modelpkg.Token{Id: id, UserId: id, Key: key, RemainQuota: 10000000, Status: common.TokenStatusEnabled}).Error)
			require.NoError(t, db.Create(&modelpkg.Channel{Id: id}).Error)
			const model = "vendor/reranker"
			prices := `{}`
			if tc.configured && !tc.missingPrice {
				prices = `{"rerank-alias":0.03}`
			}
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prices))
			settings.BillingMode, settings.BillingExpr = map[string]string{}, map[string]string{}
			if tc.expression {
				settings.BillingMode["rerank-alias"], settings.BillingExpr["rerank-alias"] = billing_setting.BillingModeTieredExpr, `tier("tokens", p * 10)`
			}
			posts, gets := 0, 0
			responseBody := `{"id":"gen-rerank","provider":"fixture","results":[{"index":0,"relevance_score":0.9}],"usage":{"total_tokens":42,"search_units":1`
			if tc.cost != "" {
				responseBody += `,"cost":` + tc.cost
			}
			responseBody += `}}`
			if tc.noID {
				responseBody = strings.ReplaceAll(responseBody, `"id":"gen-rerank",`, "")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					gets++
					if r.URL.Path == "/v1/generation" {
						assert.Equal(t, "gen-rerank", r.URL.Query().Get("id"))
						_, _ = io.WriteString(w, tc.metadata)
						return
					}
					assert.Equal(t, "/v1/models/vendor/reranker/endpoints", r.URL.Path)
					modality, status := "rerank", tc.endpointStatus
					if tc.noCapability {
						modality = "text"
					}
					// Both published unit families deliberately retain zero catalog token rates.
					pricing := `"prompt":"0","completion":"0"`
					if tc.family == "search" {
						pricing += `,"search":"0.001"`
					}
					if tc.family == "token" {
						pricing = `"prompt":"0.00000002","completion":"0"`
					}
					_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"architecture":{"output_modalities":[%q]},"endpoints":[{"tag":"fixture","status":%d,"model_id":%q,"pricing":{%s}}]}}`, model, modality, status, model, pricing)
					return
				}
				posts++
				assert.Equal(t, "/v1/rerank", r.URL.Path)
				if tc.timeout {
					conn, _, err := w.(http.Hijacker).Hijack()
					require.NoError(t, err)
					_ = conn.Close()
					return
				}
				if tc.upstreamError {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"error":{"message":"unavailable"}}`)
					return
				}
				_, _ = io.WriteString(w, responseBody)
			}))
			defer server.Close()
			body := `{"model":"vendor/reranker","query":"test","documents":["one","two"],"top_n":1` + tc.extra + `}`
			c, recorder, _, info := newEmbeddedErrorTestRequest(body, http.StatusOK)
			info.UserId, info.TokenId, info.TokenKey, info.RequestId = id, id, key, fmt.Sprintf("rerank-request-%d", id)
			info.ChannelType, info.ChannelId, info.ChannelBaseUrl, info.UpstreamModelName = constant.ChannelTypeOpenRouter, id, server.URL, model
			info.RelayMode, info.RelayFormat, info.RequestURLPath = relayconstant.RelayModeRerank, types.RelayFormatRerank, "/v1/rerank"
			info.OriginModelName = "rerank-alias"
			info.UserSetting.BillingPreference = "wallet_only"
			info.PriceData.BillingGroupRatio = 1.5
			if tc.configured {
				info.ChannelSetting.OpenRouterReservationSource = map[string]dto.OpenRouterReservationSource{model: dto.OpenRouterReservationConfiguredPrice}
			}
			if tc.reserve != nil {
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{model: *tc.reserve}
			}
			if tc.nonOR {
				info.ChannelType = constant.ChannelTypeOpenAI
			}
			var deadlineTransport *sonarAdmissionTransport
			if tc.deadline {
				client, err := service.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
				require.NoError(t, err)
				previousTransport := client.Transport
				deadlineTransport = &sonarAdmissionTransport{metadata: `{"data":{"id":"vendor/reranker","architecture":{"output_modalities":["rerank"]},"endpoints":[{"tag":"fixture","model_id":"vendor/reranker","pricing":{"prompt":"0","completion":"0"}}]}}`, responseError: context.DeadlineExceeded}
				client.Transport = deadlineTransport
				t.Cleanup(func() { client.Transport = previousTransport })
			}
			adaptor := &Adaptor{}
			if tc.crash == "intent" {
				info.OpenRouterCost = &relaycommon.OpenRouterCostState{Basis: common.OpenRouterCostBasis{Version: "openrouter_cost_v1", EffectiveGroupRatio: 1.5, QuotaPerUnit: common.QuotaPerUnit, EstimatedUSD: .02}}
				require.NoError(t, service.BeginOpenRouterSyncTask(c, info, 15000))
				task, exists, err := modelpkg.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
				require.NoError(t, err)
				require.True(t, exists)
				task.PrivateData.OpenRouterSyncState().LeaseUntil = 1
				require.NoError(t, task.UpdateOpenRouterSync(task.PrivateData.OpenRouterSyncState().Revision))
				service.RunTaskPollingOnce(t.Context(), nil)
				task, exists, err = modelpkg.GetUniqueByOnlyTaskId(task.TaskID)
				require.NoError(t, err)
				require.True(t, exists)
				assert.Equal(t, modelpkg.TaskStatus(modelpkg.TaskStatusFailure), task.Status)
				assert.True(t, task.PrivateData.Reconciliation.Required)
				assert.Zero(t, posts)
				var user modelpkg.User
				require.NoError(t, db.First(&user, id).Error)
				assert.Equal(t, 10000000, user.Quota)
				return
			}
			response, err := adaptor.DoRequest(c, info, strings.NewReader(body))
			if deadlineTransport != nil {
				posts = deadlineTransport.posts
				require.ErrorContains(t, err, "do request failed")
			}
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.Zero(t, posts)
				return
			}
			if tc.nonOR {
				require.NoError(t, err)
				_, apiErr := adaptor.DoResponse(c, response.(*http.Response), info)
				require.Nil(t, apiErr)
				assert.Nil(t, info.OpenRouterCost)
				assert.Zero(t, gets)
				assert.Contains(t, recorder.Body.String(), `"results"`)
				return
			}
			require.NotNil(t, info.OpenRouterCost)
			require.True(t, info.OpenRouterCost.Sent)
			hold := .03
			if !tc.configured {
				hold = *tc.reserve
			} else if tc.reserve != nil {
				hold = max(hold, *tc.reserve)
			}
			wantHold := common.QuotaRound(hold * common.QuotaPerUnit * 1.5)
			assert.Equal(t, wantHold, info.FinalPreConsumedQuota)
			require.NotNil(t, info.Billing)
			assert.False(t, info.Billing.NeedsRefund(), "durable Task owns hold before dispatch")
			info.PriceData.BillingGroupRatio = 9 // Admission freezes the settlement basis.
			info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{model: 99}
			if tc.crash == "dispatch" {
				require.NoError(t, err)
				require.NoError(t, response.(*http.Response).Body.Close())
				task, exists, err := modelpkg.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
				require.NoError(t, err)
				require.True(t, exists)
				task.PrivateData.OpenRouterSyncState().LeaseUntil = 1
				require.NoError(t, task.UpdateOpenRouterSync(task.PrivateData.OpenRouterSyncState().Revision))
				service.RunTaskPollingOnce(t.Context(), nil)
			} else if tc.timeout || tc.deadline {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				_, apiErr := adaptor.DoResponse(c, response.(*http.Response), info)
				if tc.upstreamError || tc.noID || tc.cost == "null" || tc.cost == "-1" || strings.HasPrefix(tc.cost, `"`) || tc.cost == "malformed" {
					require.NotNil(t, apiErr)
					assert.Empty(t, recorder.Body.String())
				} else {
					require.Nil(t, apiErr)
					assert.Contains(t, recorder.Body.String(), `"results"`)
					assert.Contains(t, recorder.Body.String(), `"search_units":1`)
				}
			}
			task, exists, err := modelpkg.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
			require.NoError(t, err)
			require.True(t, exists)
			assert.Equal(t, "rerank", task.Action)
			assert.Equal(t, constant.TaskPlatformOpenRouterRerank, task.Platform)
			assert.Nil(t, task.PrivateData.OpenRouterAudio)
			require.NotNil(t, task.PrivateData.OpenRouterSync)
			if task.PrivateData.OpenRouterSync.ResultReady {
				assert.Equal(t, "application/json", task.PrivateData.OpenRouterSync.ResultMimeType)
			}
			info.TokenId++
			require.ErrorContains(t, service.FailOpenRouterSyncTask(info, "foreign_owner"), "identity changed")
			info.TokenId--
			stale := *task
			stale.Platform = constant.TaskPlatformOpenRouterAudio
			require.Error(t, stale.UpdateOpenRouterSync(stale.PrivateData.OpenRouterSyncState().Revision), "platform cannot be rewritten by a stale claimant")
			target := wantHold
			if info.OpenRouterCost.Final {
				target = common.QuotaRound(info.OpenRouterCost.CostUSD * common.QuotaPerUnit * 1.5)
				assert.True(t, task.PrivateData.Reconciliation.Finalized)
			} else {
				assert.False(t, task.PrivateData.Reconciliation.Finalized)
			}
			assert.Equal(t, target, task.Quota)
			var user modelpkg.User
			var token modelpkg.Token
			require.NoError(t, db.First(&user, id).Error)
			require.NoError(t, db.First(&token, id).Error)
			assert.Equal(t, 10000000-target, user.Quota)
			assert.Equal(t, 10000000-target, token.RemainQuota)
			if tc.metadata != "" {
				service.RunTaskPollingOnce(t.Context(), nil)
				saved, exists, err := modelpkg.GetUniqueByOnlyTaskId(task.TaskID)
				require.NoError(t, err)
				require.True(t, exists)
				if tc.name == "metadata confirms cost" {
					assert.True(t, saved.PrivateData.Reconciliation.Finalized)
					assert.Equal(t, 7500, saved.Quota)
				} else {
					assert.False(t, saved.PrivateData.Reconciliation.Finalized)
					assert.Equal(t, wantHold, saved.Quota)
				}
			}
			assert.Equal(t, 1, posts, "no paid replay")
		})
	}
}

// The adaptor-injected usage control must not make require_parameters reject
// every endpoint, while genuinely unsupported parameters still exclude one.
func TestOpenRouterRequireParametersAdmitsAdaptorUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitTokenEncoders()
	var endpoints []openrouter.PricedEndpoint
	require.NoError(t, common.UnmarshalJsonStr(`[
		{"tag":"openai","model_id":"openai/gpt-4o-mini","quantization":"unknown","context_length":128000,"max_completion_tokens":16384,"supported_parameters":["max_tokens","temperature","top_p","tools","response_format"],"pricing":{"prompt":"0.00000015","completion":"0.0000006"}},
		{"tag":"azure","model_id":"openai/gpt-4o-mini","quantization":"unknown","context_length":128000,"max_completion_tokens":16384,"supported_parameters":["temperature","top_p","tools","response_format"],"pricing":{"prompt":"0.000001","completion":"0.000002"}}
	]`, &endpoints))
	var fp8 openrouter.PricedEndpoint
	require.NoError(t, common.UnmarshalJsonStr(`{"tag":"deepinfra","model_id":"openai/gpt-4o-mini","quantization":"fp8","context_length":128000,"max_completion_tokens":16384,"supported_parameters":["max_tokens"],"pricing":{"prompt":"0.000001","completion":"0.000002"}}`, &fp8))
	for _, tc := range []struct {
		name, provider, wantError string
		withFP8                   bool
		want                      float64
	}{
		{"usage admitted and unsupported max_tokens excluded", `{"require_parameters":true}`, "", false, 0.00021},
		{"only provider with require_parameters", `{"only":["openai"],"require_parameters":true}`, "", false, 0.00021},
		{"really requested parameter names exclusion", `{"only":["azure"],"require_parameters":true}`, "OpenRouter request has no compatible priced endpoint: provider.require_parameters excluded endpoints that do not support: max_tokens", false, 0},
		{"unmatched quantization names endpoint values", `{"quantizations":["fp16"]}`, "OpenRouter request has no compatible priced endpoint: no OpenRouter endpoint matches the requested quantizations; endpoints report: unknown", false, 0},
		{"matching quantization excluded by another filter stays generic", `{"quantizations":["fp8"],"max_price":{"prompt":0.1}}`, "OpenRouter request has no compatible priced endpoint", true, 0},
		{"supported endpoint excluded by another filter stays generic", `{"require_parameters":true,"max_price":{"prompt":0.1}}`, "OpenRouter request has no compatible priced endpoint", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _, info := newEmbeddedErrorTestRequest("", http.StatusOK)
			info.ChannelType = constant.ChannelTypeOpenRouter
			info.OriginModelName, info.UpstreamModelName = "openai/gpt-4o-mini", "openai/gpt-4o-mini"
			info.SetEstimatePromptTokens(1000)
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"openai/gpt-4o-mini","messages":[{"role":"user","content":"Hello"}],"max_tokens":100}`, &request))
			converted, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, &request)
			require.NoError(t, err)
			convertedBody, err := common.Marshal(converted)
			require.NoError(t, err)
			// Provider preferences reach the final body through request overrides.
			var final map[string]json.RawMessage
			require.NoError(t, common.Unmarshal(convertedBody, &final))
			require.JSONEq(t, `{"include":true}`, string(final["usage"]))
			final["provider"] = json.RawMessage(tc.provider)
			body, err := common.Marshal(final)
			require.NoError(t, err)

			estimator := openrouter.NewRequestEstimator()
			estimator.Models["openai/gpt-4o-mini"] = endpoints
			if tc.withFP8 {
				estimator.Models["openai/gpt-4o-mini"] = append(slices.Clone(endpoints), fp8)
			}
			cost, err := estimator.Estimate(info, body, "chat/completions")
			if tc.wantError != "" {
				require.EqualError(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-12)
		})
	}
}
