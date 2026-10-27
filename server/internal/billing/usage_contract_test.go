package billing_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	tokenusage "github.com/QuantumNous/new-api/gateway/tokenusage"
	"github.com/QuantumNous/new-api/internal/contracttest"
	"github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Only recording gaps are added here. The immutable billing oracle supplies
// fixtures and raw-duration normalization without copying its accounting rules.
// Every case gets a process because FX publication cannot be rewound in RAM.
type usageHTTPCase struct {
	name                                  string
	disableConsume, export, disableErrors bool
	upstreamError, stream, retry          bool
	emptyTokenName, recordIP              bool
	subscription, playground, separateLog bool
	channelTest                           bool
}

func TestUsageHTTPContract(t *testing.T) {
	cases := []usageHTTPCase{
		{name: "http_consume_disabled", disableConsume: true},
		{name: "http_consume_disabled_export_enabled", disableConsume: true, export: true},
		{name: "http_export_enabled", export: true},
		{name: "http_upstream_error_raw", upstreamError: true},
		{name: "http_stream_upstream_error_raw", upstreamError: true, stream: true},
		{name: "http_error_logging_disabled", upstreamError: true, disableErrors: true},
		{name: "http_retry_then_success", retry: true},
		{name: "http_empty_token_name", emptyTokenName: true},
		{name: "http_ip_enabled", recordIP: true},
		{name: "http_subscription_raw", subscription: true},
		{name: "http_playground_consume_disabled", playground: true, disableConsume: true},
		{name: "http_separate_log_db_consume", separateLog: true, export: true, recordIP: true},
		{name: "http_separate_log_db_error", separateLog: true, upstreamError: true, recordIP: true},
		{name: "http_channel_test", channelTest: true},
	}
	selected := os.Getenv("WAVE2C_USAGE_CASE")
	for _, tc := range cases {
		if selected == "" {
			t.Run(tc.name, func(t *testing.T) {
				runBillingCostProcess(t, "TestUsageHTTPContract", "WAVE2C_USAGE_CASE", tc.name)
			})
			continue
		}
		if tc.name == selected {
			httpclient.InitHttpClient()
			tokenusage.InitTokenEncoders()
			runUsageHTTPContract(t, tc)
			return
		}
	}
	if selected != "" {
		t.Fatalf("unknown isolated HTTP usage case %q", selected)
	}
}

func runUsageHTTPContract(t *testing.T, tc usageHTTPCase) {
	t.Helper()
	const balance, actual = 1000, 50
	fixture := billingCase{wallet: balance, token: balance}
	if tc.subscription {
		fixture.preference, fixture.subscriptionTotal = "subscription_only", 1000
	}
	env := newBillingEnv(t, fixture, false)
	defer waitForUsageJobs(t)
	configureBillingCost(t, env, billingCostCase{model: billingModel, ratio: 1, completion: 1, group: 1})
	common.LogConsumeEnabled, common.DataExportEnabled = !tc.disableConsume, tc.export
	constant.ErrorLogEnabled = !tc.disableErrors
	common.RetryTimes = 1
	require.NoError(t, operation_setting.AutomaticRetryStatusCodesFromString("500"))
	isolateUsageExport(t)
	if tc.recordIP {
		var user model.User
		require.NoError(t, env.DB.First(&user, env.UserID).Error)
		settings := user.GetSetting()
		settings.RecordIpLog = true
		encoded, err := common.Marshal(settings)
		require.NoError(t, err)
		require.NoError(t, env.DB.Model(&user).Update("setting", string(encoded)).Error)
	}
	if tc.emptyTokenName {
		require.NoError(t, env.DB.Model(&model.Token{}).Where("id = ?", 1).Update("name", "").Error)
	}
	if tc.channelTest {
		// origin/main uses a fixed Chinese test label. The migrated channel
		// module already localizes it, so pin the recipient's language to zh-CN.
		require.NoError(t, env.DB.Model(&model.User{}).Where("id = ?", 3).Update("setting", `{"language":"zh-CN"}`).Error)
	}
	if tc.separateLog {
		// New deliberately resets LOG_SQL_DSN. Select the separate log database
		// afterwards through the production initializer, not by swapping a handle.
		mainPath := common.SQLitePath
		common.SQLitePath = filepath.Join(t.TempDir(), "usage-log.db") + "?_pragma=busy_timeout(30000)&_pragma=synchronous(OFF)"
		t.Setenv("LOG_SQL_DSN", "local")
		require.NoError(t, model.InitLogDB())
		common.SQLitePath = mainPath
		logDB := model.LOG_DB
		require.NotSame(t, env.DB, logDB)
		t.Cleanup(func() {
			connection, err := logDB.DB()
			require.NoError(t, err)
			require.NoError(t, connection.Close())
		})
	}

	type observation struct {
		Ledger   billingLedger `json:"ledger"`
		LogTypes []int         `json:"log_types"`
		Error    error         `json:"-"`
	}
	observed := make(chan observation, 2)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		ledger, err := readBillingLedger(env.DB, tc.subscription)
		logTypes := make([]int, 0)
		if err == nil {
			err = model.LOG_DB.Model(&model.Log{}).Where("type IN ?", []int{model.LogTypeConsume, model.LogTypeError}).Order("id").Pluck("type", &logTypes).Error
		}
		if err == nil && (request.Method != http.MethodPost || request.URL.Path != "/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer contract-upstream-key") {
			err = fmt.Errorf("unexpected provider request %s %s", request.Method, request.URL.Path)
		}
		if call > 2 {
			http.Error(w, "unexpected extra submission", http.StatusInternalServerError)
			return
		}
		observed <- observation{Ledger: ledger, LogTypes: logTypes, Error: err}
		if err != nil {
			http.Error(w, "fixture observation failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "usage-provider-request")
		if tc.upstreamError || tc.retry && call == 1 {
			status := http.StatusBadRequest
			if tc.retry {
				status = http.StatusInternalServerError
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":{"message":"usage fixture rejected","type":"invalid_request_error","code":"usage_rejected"}}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":"chatcmpl-usage","object":"chat.completion","created":1700000000,"model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":40,"completion_tokens":10,"total_tokens":50}}`, billingModel)
	}))
	t.Cleanup(upstream.Close)
	channel := model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Key: "contract-upstream-key", Status: common.ChannelStatusEnabled, Name: "usage-contract", BaseURL: &upstream.URL, Models: billingModel, Group: "default", AutoBan: common.GetPointer(0)}
	require.NoError(t, env.DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	model.InitChannelCache()
	env.MountRelay()

	path, method, bearer := "/v1/chat/completions", http.MethodPost, "sk-"+billingKey
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":20,"stream":%t,"stream_options":{"include_usage":true}}`, billingModel, tc.stream)
	if tc.playground {
		path, bearer = "/pg/chat/completions", env.User
	}
	// Channel tests create their own Gin context internally. Their log's
	// generated request ID is characterized, rather than supplied by this route.
	var channelUpdateDone chan struct{}
	if tc.channelTest {
		path, method, bearer, body = "/api/channel/test/1?model="+billingModel, http.MethodGet, env.Root, ""
		channelUpdateDone = make(chan struct{}, 1)
		const callback = "usage_channel_test_response_time"
		require.NoError(t, env.DB.Callback().Update().After("gorm:commit_or_rollback_transaction").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "channels" {
				select {
				case channelUpdateDone <- struct{}{}:
				default:
				}
			}
		}))
		t.Cleanup(func() { require.NoError(t, env.DB.Callback().Update().Remove(callback)) })
	}
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Language", "zh-CN")
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.RemoteAddr = "198.51.100.7:1234"
	recorder := env.Serve(request)
	status, wantCalls := http.StatusOK, 1
	if tc.upstreamError {
		status = http.StatusBadRequest
	}
	if tc.retry {
		wantCalls = 2
	}
	require.Equal(t, status, recorder.Code, recorder.Body.String())
	require.EqualValues(t, wantCalls, calls.Load())
	if tc.channelTest {
		select {
		case <-channelUpdateDone:
		case <-time.After(5 * time.Second):
			t.Fatal("channel response-time update did not complete before cleanup")
		}
	}
	observations := make([]observation, 0, wantCalls)
	for index := range wantCalls {
		select {
		case observation := <-observed:
			require.NoError(t, observation.Error)
			hold := billingLedger{Wallet: balance, TokenRemain: balance}
			if !tc.channelTest {
				if tc.subscription {
					hold.SubscriptionUsed = 100
				} else {
					hold.Wallet -= 100
				}
				if !tc.playground {
					hold.TokenRemain, hold.TokenUsed = balance-100, 100
				}
			}
			require.Equal(t, hold, observation.Ledger)
			if index == 1 {
				require.Equal(t, []int{model.LogTypeError}, observation.LogTypes, "error recording precedes the retry")
			} else {
				require.Empty(t, observation.LogTypes)
			}
			observations = append(observations, observation)
		default:
			t.Fatal("missing provider observation")
		}
	}

	expected := billingLedger{Wallet: balance, TokenRemain: balance}
	if !tc.upstreamError && !tc.channelTest {
		expected.UserUsed, expected.RequestCount, expected.ChannelUsed = actual, 1, actual
		if tc.subscription {
			expected.SubscriptionUsed = actual
		} else {
			expected.Wallet -= actual
		}
		if !tc.playground {
			expected.TokenRemain, expected.TokenUsed = balance-actual, actual
		}
	}
	waitForUsageLedger(t, env, tc.subscription, expected)
	wantTypes := make([]int, 0, 2)
	if (tc.upstreamError || tc.retry) && !tc.disableErrors {
		wantTypes = append(wantTypes, model.LogTypeError)
	}
	if !tc.upstreamError && !tc.disableConsume {
		wantTypes = append(wantTypes, model.LogTypeConsume)
	}
	logs := usageLogSnapshot(t, wantTypes, actual, tc.channelTest)
	for _, row := range logs {
		if tc.channelTest {
			assert.EqualValues(t, 3, row["user_id"])
			assert.Equal(t, row["content"], row["token_name"])
			assert.NotEmpty(t, row["content"])
		} else {
			assert.Equal(t, "billing-contract-request", row["request_id"])
			if tc.recordIP {
				assert.Equal(t, "198.51.100.7", row["ip"])
			} else {
				assert.Empty(t, row["ip"])
			}
			if tc.emptyTokenName {
				assert.Empty(t, row["token_name"])
			}
		}
	}
	if tc.separateLog {
		var count int64
		require.NoError(t, env.DB.Model(&model.Log{}).Where("type IN ?", []int{model.LogTypeConsume, model.LogTypeError}).Count(&count).Error)
		require.Zero(t, count, "recording must not fall back to the primary database")
	}
	wantExport := 0
	if tc.export && !tc.disableConsume && !tc.upstreamError {
		wantExport = 1
	}
	exported := usageExportSnapshot(t, wantExport, actual)
	if wantExport == 1 {
		require.Len(t, logs, 1)
		createdAt := int64(logs[0]["created_at"].(float64))
		assert.Equal(t, createdAt-createdAt%3600, exported[0].CreatedAt)
	}
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	if tc.channelTest {
		assert.Equal(t, true, response["success"])
		data, ok := response["data"].(map[string]any)
		require.True(t, ok)
		elapsed, ok := data["time"].(float64)
		require.True(t, ok)
		require.GreaterOrEqual(t, elapsed, float64(0))
		data["time"] = "__duration__"
	}
	// Date is an HTTP-server timestamp, never financial evidence.
	headers := recorder.Header().Clone()
	for index := range headers["Date"] {
		headers["Date"][index] = "__timestamp__"
	}
	if tc.channelTest {
		requestID := headers.Get(common.RequestIdKey)
		require.Regexp(t, `^[0-9]{23}[A-Za-z0-9]{16}$`, requestID)
		_, err := time.Parse("20060102150405", requestID[:14])
		require.NoError(t, err)
		headers.Set(common.RequestIdKey, "__generated_request_id__")
	}
	usageGolden(t, tc.name, map[string]any{
		"upstream_observations": observations, "final": expected, "relay_logs": logs, "export": exported,
		"upstream_calls": calls.Load(), "separate_log_db": tc.separateLog,
		"response": map[string]any{"status": recorder.Code, "headers": headers, "body": response},
	})
}

func TestUsageBoundaryContract(t *testing.T) {
	cases := []string{
		"boundary_delivery_stream_failure", "boundary_delivery_canceled",
		"boundary_delivery_error_disabled", "boundary_delivery_consume_disabled",
		"boundary_realtime_ratio", "boundary_realtime_fixed", "boundary_realtime_zero",
		"boundary_realtime_settle_failure", "boundary_realtime_subscription", "boundary_realtime_cache_discount_unavailable",
		"boundary_no_request", "boundary_gizmo4", "boundary_gizmo4o", "boundary_rich_metadata",
	}
	selected := os.Getenv("WAVE2C_USAGE_CASE")
	for _, name := range cases {
		if selected == "" {
			t.Run(name, func(t *testing.T) {
				runBillingCostProcess(t, "TestUsageBoundaryContract", "WAVE2C_USAGE_CASE", name)
			})
			continue
		}
		if name == selected {
			runUsageBoundaryContract(t, name)
			return
		}
	}
	if selected != "" {
		t.Fatalf("unknown isolated boundary usage case %q", selected)
	}
}

// These paths require post-response/cancellation/session facts that cannot be
// injected by a normal chat HTTP request. Exercise the existing service facade
// on both origin/main and the refactored branch; no new route is installed.
func runUsageBoundaryContract(t *testing.T, name string) {
	t.Helper()
	const balance, hold = 1000, 10
	realtime := strings.HasPrefix(name, "boundary_realtime_")
	subscription := name == "boundary_realtime_subscription"
	fixture := billingCase{wallet: balance, token: balance}
	if subscription {
		fixture.preference, fixture.subscriptionTotal = "subscription_only", 1000
	}
	env := newBillingEnv(t, fixture, false)
	defer waitForUsageJobs(t)
	modelName := billingModel
	if name == "boundary_gizmo4" {
		modelName = "gpt-4-gizmo-fixture"
	}
	if name == "boundary_gizmo4o" {
		modelName = "gpt-4o-gizmo-fixture"
	}
	configureBillingCost(t, env, billingCostCase{model: modelName, ratio: 1, completion: 1, group: 1})
	isolateUsageExport(t)
	channel := model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Name: "usage-boundary", Status: common.ChannelStatusEnabled}
	require.NoError(t, env.DB.Create(&channel).Error)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("Accept-Language", "zh-CN")
	ctx.Set(common.RequestIdKey, "usage-boundary-request")
	ctx.Set(common.UpstreamRequestIdKey, "usage-boundary-provider")
	ctx.Set("username", "contract-user")
	ctx.Set("token_name", "billing-contract-token")
	ctx.Set("token_quota", balance)
	start := time.Now()
	info := &relaycommon.RelayInfo{
		UserId: env.UserID, TokenId: 1, TokenKey: billingKey, UserQuota: balance,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 1, ChannelType: constant.ChannelTypeOpenAI},
		OriginModelName: modelName, UsingGroup: "default", UserGroup: "default", RequestId: "usage-boundary-request",
		RelayFormat: relaytypes.RelayFormatOpenAI, StartTime: start, FirstResponseTime: start.Add(-time.Second),
		RequestURLPath: "/v1/fallback?private=not-recorded", ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
		PriceData:   hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}},
	}
	if subscription {
		info.UserSetting.BillingPreference = "subscription_only"
	}
	if realtime {
		info.RelayFormat, info.IsStream = relaytypes.RelayFormatOpenAIRealtime, true
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
		info.FirstResponseTime = start
	}
	if name == "boundary_realtime_fixed" {
		info.PriceData.UsePrice, info.PriceData.ModelPrice = true, 0.00002
	}
	require.Nil(t, service.PreConsumeBilling(ctx, hold, info))
	before, err := readBillingLedger(env.DB, subscription)
	require.NoError(t, err)
	expectedBefore := billingLedger{Wallet: balance - hold, TokenRemain: balance - hold, TokenUsed: hold}
	if subscription {
		expectedBefore.Wallet, expectedBefore.SubscriptionUsed = balance, hold
	}
	require.Equal(t, expectedBefore, before)

	quota, wantCount := 5, 1
	wantTypes := []int{model.LogTypeConsume}
	settlementFailed := name == "boundary_realtime_settle_failure"
	if realtime {
		usage := &dto.RealtimeUsage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8,
			InputTokenDetails:  dto.InputTokenDetails{TextTokens: 3, AudioTokens: 2},
			OutputTokenDetails: dto.OutputTokenDetails{TextTokens: 1, AudioTokens: 2}}
		quota = 64 // 3 text + 1 output text + 2*10 audio + 2*10*2 output audio.
		if name == "boundary_realtime_fixed" {
			quota = 10
		}
		if name == "boundary_realtime_zero" {
			usage, quota, wantCount = &dto.RealtimeUsage{}, 0, 0
		}
		if name == "boundary_realtime_cache_discount_unavailable" {
			usage.CacheDiscountUnavailable, usage.CacheDiscountUnavailableReason = true, "cached_token_details_missing"
		}
		injected := errors.New("usage fixture terminal token failure")
		var failureHits int
		if settlementFailed {
			// Observe counters inside the failing terminal leg, rather than
			// inferring write order from the final state alone.
			const callback = "usage_terminal_token_failure"
			require.NoError(t, env.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table != "tokens" {
					return
				}
				failureHits++
				var user model.User
				var channel model.Channel
				if err := tx.Session(&gorm.Session{NewDB: true}).First(&user, 1).Error; err != nil {
					tx.AddError(err)
					return
				}
				if err := tx.Session(&gorm.Session{NewDB: true}).First(&channel, 1).Error; err != nil {
					tx.AddError(err)
					return
				}
				assert.Equal(t, quota, user.UsedQuota)
				assert.Equal(t, 1, user.RequestCount)
				assert.EqualValues(t, quota, channel.UsedQuota)
				tx.AddError(injected)
			}))
			t.Cleanup(func() { require.NoError(t, env.DB.Callback().Update().Remove(callback)) })
		}
		err = service.PostWssConsumeQuota(ctx, info, modelName, usage, "usage fixture")
		if settlementFailed {
			require.ErrorIs(t, err, injected)
			require.Equal(t, 1, failureHits)
			wantTypes = nil
		} else {
			require.NoError(t, err)
		}
	} else {
		usage := &dto.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}
		switch name {
		case "boundary_delivery_stream_failure", "boundary_delivery_error_disabled", "boundary_delivery_consume_disabled":
			info.IsStream, info.StreamStatus = true, relaycommon.NewStreamStatus()
			info.FirstResponseTime = start
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, errors.New("fixture delivery closed"))
			wantTypes = append(wantTypes, model.LogTypeError)
			if name == "boundary_delivery_error_disabled" {
				constant.ErrorLogEnabled, wantTypes = false, []int{model.LogTypeConsume}
			}
			if name == "boundary_delivery_consume_disabled" {
				common.LogConsumeEnabled, wantTypes = false, []int{model.LogTypeError}
			}
		case "boundary_delivery_canceled":
			requestContext, cancel := context.WithCancel(ctx.Request.Context())
			ctx.Request = ctx.Request.WithContext(requestContext)
			cancel()
			wantTypes = append(wantTypes, model.LogTypeError)
		case "boundary_no_request":
			ctx.Request = nil
		case "boundary_rich_metadata":
			info.ReasoningEffort = "high"
			info.IsModelMapped, info.UpstreamModelName = true, "fixture-upstream-model"
			info.RequestConversionChain = []relaytypes.RelayFormat{relaytypes.RelayFormatOpenAI, relaytypes.RelayFormatClaude, relaytypes.RelayFormatOpenAIResponses}
			info.ParamOverrideAudit = []string{"set temperature=0", "delete metadata.fixture"}
			info.RecordConversionDiagnostics(ctx, []relaytypes.ConversionDiagnostic{{Code: "fixture_loss", Path: "request.tools", Message: "fixture conversion note", Severity: relaytypes.ConversionDiagnosticWarning, From: relaytypes.RelayFormatOpenAI, To: relaytypes.RelayFormatClaude}})
			common.SetContextKey(ctx, constant.ContextKeySystemPromptOverride, true)
			common.SetContextKey(ctx, constant.ContextKeyChannelIsMultiKey, true)
			common.SetContextKey(ctx, constant.ContextKeyChannelMultiKeyIndex, 0)
			common.SetContextKey(ctx, constant.ContextKeyLocalCountTokens, true)
			common.SetContextKey(ctx, constant.ContextKeyAdminRejectReason, "fixture admin-only reason")
			ctx.Set("use_channel", []string{"1", "2"})
			usage = &dto.Usage{BillingUsage: dto.NewOpenAIResponsesBillingUsage(&dto.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5})}
		}
		service.PostTextConsumeQuota(ctx, info, usage, []string{"usage fixture"})
	}

	expected := billingLedger{Wallet: balance - quota, TokenRemain: balance - quota, TokenUsed: quota, UserUsed: quota, RequestCount: wantCount, ChannelUsed: int64(quota)}
	if subscription {
		expected.Wallet, expected.SubscriptionUsed = balance, int64(quota)
	}
	if settlementFailed {
		expected.TokenRemain, expected.TokenUsed = balance-hold, hold
	}
	waitForUsageLedger(t, env, subscription, expected)
	logs := usageLogSnapshot(t, wantTypes, quota, false)
	for _, row := range logs {
		assert.Equal(t, "usage-boundary-request", row["request_id"])
		assert.Equal(t, "usage-boundary-provider", row["upstream_request_id"])
		if name == "boundary_no_request" {
			assert.Equal(t, "/v1/fallback", row["other"].(map[string]any)["request_path"])
		}
		if strings.HasPrefix(name, "boundary_delivery_") {
			other := row["other"].(map[string]any)
			assert.Equal(t, "failed", other["delivery_status"])
			if name == "boundary_delivery_canceled" {
				assert.Equal(t, "context_canceled", other["delivery_reason"])
			} else {
				assert.Equal(t, "stream_client_gone", other["delivery_reason"])
			}
		}
	}
	if len(logs) == 2 {
		assert.Equal(t, logs[0]["other_raw"], logs[1]["other_raw"], "delivery row reuses consumption facts")
	}
	usageGolden(t, name, map[string]any{"hold_before_boundary": before, "final": expected, "relay_logs": logs, "settlement_failed": settlementFailed, "export": usageExportSnapshot(t, 0, quota)})
}

func waitForUsageLedger(t *testing.T, env *contracttest.Env, subscription bool, expected billingLedger) {
	t.Helper()
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		assert.Zero(check, gopool.WorkerCount(), "queued effects must finish before DB handles or globals are restored")
		actual, err := readBillingLedger(env.DB, subscription)
		if assert.NoError(check, err) {
			assert.Equal(check, expected, actual)
		}
	}, 5*time.Second, 10*time.Millisecond)
}

func waitForUsageJobs(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 5*time.Second, 10*time.Millisecond,
		"queued effects must finish even when an earlier assertion fails")
}

func usageLogSnapshot(t *testing.T, types []int, quota int, generatedRequestID bool) []map[string]any {
	t.Helper()
	var rows []model.Log
	require.NoError(t, model.LOG_DB.Where("type IN ?", []int{model.LogTypeConsume, model.LogTypeError}).Order("id").Find(&rows).Error)
	require.Len(t, rows, len(types))
	result := make([]map[string]any, 0, len(rows))
	for index, row := range rows {
		require.Equal(t, types[index], row.Type)
		if row.Type == model.LogTypeConsume {
			assert.Equal(t, quota, row.Quota)
		} else {
			assert.Zero(t, row.Quota)
			assert.Zero(t, row.PromptTokens)
			assert.Zero(t, row.CompletionTokens)
		}
		require.GreaterOrEqual(t, row.UseTime, 0)
		raw, err := common.Marshal(row)
		require.NoError(t, err)
		var value map[string]any
		require.NoError(t, common.Unmarshal(raw, &value))
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(row.Other, &other))
		firstResponse, hasFirstResponse := other["frt"]
		if row.Type == model.LogTypeConsume {
			require.True(t, hasFirstResponse, "consumption rows retain their first-response duration")
		}
		if hasFirstResponse {
			frt, ok := firstResponse.(float64)
			require.True(t, ok)
			require.True(t, frt == -1000 || frt >= 0, "known sentinel or nonnegative elapsed duration")
			other["frt"] = "__duration__"
		}
		if elapsed, exists := other["use_time"]; exists {
			duration, ok := elapsed.(float64)
			require.True(t, ok)
			require.GreaterOrEqual(t, duration, float64(0))
			other["use_time"] = "__duration__"
		}
		value["use_time"], value["other"], value["other_raw"] = "__duration__", other, normalizeBillingCostOther(t, row.Other)
		if generatedRequestID {
			// The inner channel-test context has no RequestId middleware.
			require.Regexp(t, `^[0-9]{23}[A-Za-z0-9]{16}$`, row.RequestId)
			_, err := time.Parse("20060102150405", row.RequestId[:14])
			require.NoError(t, err)
			value["request_id"] = "__generated_request_id__"
		}
		result = append(result, value)
	}
	return result
}

func isolateUsageExport(t *testing.T) {
	t.Helper()
	previousNode := common.NodeName
	common.NodeName = "usage-contract-node"
	model.CacheQuotaDataLock.Lock()
	previous := model.CacheQuotaData
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = previous
		model.CacheQuotaDataLock.Unlock()
		common.NodeName = previousNode
	})
}

func usageExportSnapshot(t *testing.T, count, quota int) []model.QuotaData {
	t.Helper()
	model.CacheQuotaDataLock.Lock()
	defer model.CacheQuotaDataLock.Unlock()
	require.Len(t, model.CacheQuotaData, count)
	rows := make([]model.QuotaData, 0, count)
	for _, row := range model.CacheQuotaData {
		require.Equal(t, 1, row.Count)
		require.Equal(t, quota, row.Quota)
		require.Equal(t, 50, row.TokenUsed)
		require.Zero(t, row.CreatedAt%3600)
		assert.Equal(t, "usage-contract-node", row.NodeName)
		assert.Equal(t, "contract-user", row.Username)
		assert.Equal(t, billingModel, row.ModelName)
		assert.Equal(t, "default", row.UseGroup)
		assert.Equal(t, 1, row.UserID)
		assert.Equal(t, 1, row.TokenID)
		assert.Equal(t, 1, row.ChannelID)
		rows = append(rows, *row)
	}
	return rows
}

func usageGolden(t *testing.T, name string, state map[string]any) {
	t.Helper()
	raw, err := common.Marshal(state)
	require.NoError(t, err)
	var snapshot any
	require.NoError(t, common.Unmarshal(raw, &snapshot))
	contracttest.GoldenValue(t, "usage_"+name, snapshot, "created_at", "updated_at")
}
