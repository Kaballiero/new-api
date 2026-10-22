package billing_test

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/contracttest"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const billingModel = "billing-contract-model"
const billingKey = "billingcontractkey"

// All amounts are quota units. The production ratio path reserves 80 + the
// request's 20 max_tokens; provider usage determines a final charge of 50/150.
// Token counting is disabled explicitly, so the fixture needs no remote codec.
type billingCase struct {
	name                      string
	wallet, token, actual     int
	status                    int
	stream, unlimited         bool
	playground, upstreamError bool
	trusted                   bool
	preference                string
	subscriptionTotal         int64
	overflow                  bool
	subscriptionFunding       bool
	walletHeld, tokenHeld     int64
	drainToken                int
}

type billingLedger struct {
	Wallet           int   `json:"wallet"`
	UserUsed         int   `json:"user_used_quota"`
	RequestCount     int   `json:"request_count"`
	WalletHeld       int64 `json:"wallet_batch_held_quota"`
	TokenRemain      int   `json:"token_remain_quota"`
	TokenUsed        int   `json:"token_used_quota"`
	TokenHeld        int64 `json:"token_batch_held_quota"`
	SubscriptionUsed int64 `json:"subscription_used"`
	ChannelUsed      int64 `json:"channel_used_quota"`
}

// TestBillingHTTPContract records only on origin/main with -update. Each request
// traverses the real relay router to an HTTP provider, and snapshots both the
// hold observed by that provider and the final ledgers/consumption log rows.
func TestBillingHTTPContract(t *testing.T) {
	service.InitHttpClient()
	service.InitTokenEncoders()
	cases := []billingCase{
		{name: "nonstream_delta_charge", wallet: 1000, token: 1000, actual: 150, status: 200},
		{name: "nonstream_delta_return", wallet: 1000, token: 1000, actual: 50, status: 200},
		{name: "stream_delta_charge", wallet: 1000, token: 1000, actual: 150, status: 200, stream: true},
		{name: "stream_delta_return", wallet: 1000, token: 1000, actual: 50, status: 200, stream: true},
		{name: "upstream_error_full_refund", wallet: 1000, token: 1000, status: 400, upstreamError: true},
		{name: "wallet_empty", wallet: 0, token: 1000, status: 403},
		{name: "wallet_below_hold", wallet: 99, token: 1000, status: 403},
		{name: "limited_token_below_hold", wallet: 1000, token: 99, status: 403},
		{name: "token_exhausted_at_auth", wallet: 1000, token: 0, status: 401},
		{name: "unlimited_token_accounting", wallet: 1000, token: 0, actual: 150, status: 200, unlimited: true},
		{name: "terminal_wallet_and_token_debt", wallet: 120, token: 120, actual: 150, status: 200},
		{name: "token_exhausted_midway", wallet: 1000, token: 200, actual: 150, status: 200, drainToken: 100},
		{name: "subscription_delta_charge", wallet: 1000, token: 1000, actual: 150, status: 200, preference: "subscription_only", subscriptionTotal: 1000, subscriptionFunding: true},
		{name: "subscription_delta_return", wallet: 1000, token: 1000, actual: 50, status: 200, preference: "subscription_only", subscriptionTotal: 1000, subscriptionFunding: true},
		{name: "subscription_first_no_subscription", wallet: 1000, token: 1000, actual: 50, status: 200, preference: "subscription_first"},
		{name: "subscription_first_overflow_allowed", wallet: 1000, token: 1000, actual: 50, status: 200, preference: "subscription_first", subscriptionTotal: 50, overflow: true},
		{name: "subscription_first_overflow_denied", wallet: 1000, token: 1000, status: 403, preference: "subscription_first", subscriptionTotal: 50},
		{name: "wallet_first_subscription_fallback", wallet: 99, token: 1000, actual: 50, status: 200, preference: "wallet_first", subscriptionTotal: 1000, subscriptionFunding: true},
		{name: "wallet_first_uses_wallet", wallet: 1000, token: 1000, actual: 50, status: 200, preference: "wallet_first", subscriptionTotal: 1000},
		{name: "wallet_only_ignores_subscription", wallet: 1000, token: 1000, actual: 50, status: 200, preference: "wallet_only", subscriptionTotal: 1000},
		{name: "subscription_only_no_fallback", wallet: 1000, token: 1000, status: 403, preference: "subscription_only"},
		{name: "invalid_preference_subscription_first", wallet: 1000, token: 1000, actual: 50, status: 200, preference: "invalid", subscriptionTotal: 1000, subscriptionFunding: true},
		{name: "trusted_limited_token", wallet: 10000000, token: 10000000, actual: 50, status: 200, trusted: true},
		{name: "trusted_unlimited_token", wallet: 10000000, token: 0, actual: 50, status: 200, trusted: true, unlimited: true},
		{name: "trust_wallet_threshold_strict", wallet: 5000000, token: 10000000, actual: 50, status: 200},
		{name: "trust_token_threshold_strict", wallet: 10000000, token: 5000000, actual: 50, status: 200},
		{name: "subscription_never_trusted", wallet: 10000000, token: 10000000, actual: 50, status: 200, preference: "subscription_only", subscriptionTotal: 10000000, subscriptionFunding: true},
		{name: "wallet_batch_hold_blocks_admission", wallet: 1000, token: 1000, walletHeld: 950, status: 403},
		{name: "token_batch_hold_blocks_admission", wallet: 1000, token: 1000, tokenHeld: 950, status: 403},
		{name: "playground_charge", wallet: 1000, token: 1000, actual: 50, status: 200, playground: true},
		{name: "playground_refund", wallet: 1000, token: 1000, status: 400, playground: true, upstreamError: true},
	}
	for _, cache := range []string{"database", "redis"} {
		t.Run(cache, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					runBillingHTTPContract(t, tc, cache == "redis", cache+"_"+tc.name)
				})
			}
		})
	}
}

func runBillingHTTPContract(t *testing.T, tc billingCase, useRedis bool, golden string) {
	t.Helper()
	env := newBillingEnv(t, tc, useRedis)
	observed := make(chan struct {
		ledger billingLedger
		body   []byte
		err    error
	}, 1)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) != 1 {
			http.Error(w, "unexpected paid retry", http.StatusInternalServerError)
			return
		}
		ledger, err := readBillingLedger(env.DB, tc.subscriptionTotal > 0)
		body, bodyErr := io.ReadAll(r.Body)
		if err == nil {
			err = bodyErr
		}
		if err == nil && (r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer contract-upstream-key") {
			err = fmt.Errorf("unexpected upstream method, path or authorization")
		}
		if err == nil && tc.drainToken != 0 {
			err = model.DecreaseTokenQuota(1, billingKey, tc.drainToken)
		}
		observed <- struct {
			ledger billingLedger
			body   []byte
			err    error
		}{ledger, body, err}
		if err != nil {
			http.Error(w, "fixture failed", http.StatusInternalServerError)
			return
		}
		if tc.upstreamError {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"fixture upstream rejected","type":"invalid_request_error","code":"fixture_rejected"}}`)
			return
		}
		if !tc.stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"id":"chatcmpl-fixture","object":"chat.completion","created":1700000000,"model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":%d,"completion_tokens":10,"total_tokens":%d}}`, billingModel, tc.actual-10, tc.actual)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-fixture\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"},\"finish_reason\":null}]}\n\n", billingModel)
		w.(http.Flusher).Flush()
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-fixture\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":10,\"total_tokens\":%d}}\n\ndata: [DONE]\n\n", billingModel, tc.actual-10, tc.actual)
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(upstream.Close)
	channel := model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Key: "contract-upstream-key", Status: common.ChannelStatusEnabled, Name: "billing-contract", BaseURL: &upstream.URL, Models: billingModel, Group: "default", AutoBan: common.GetPointer(0)}
	require.NoError(t, env.DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	model.InitChannelCache()
	env.MountRelay()

	path, bearer := "/v1/chat/completions", "sk-"+billingKey
	if tc.playground {
		path, bearer = "/pg/chat/completions", env.User
	}
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":20,"stream":%t,"stream_options":{"include_usage":true}}`, billingModel, tc.stream)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Language", "zh-CN")
	request.Header.Set("Authorization", "Bearer "+bearer)
	recorder := env.Serve(request)
	require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
	wantCalls := int32(0)
	var hold any
	if tc.status == http.StatusOK || tc.upstreamError {
		wantCalls = 1
		select {
		case observation := <-observed:
			require.NoError(t, observation.err)
			var sent map[string]any
			require.NoError(t, common.Unmarshal(observation.body, &sent))
			assert.Equal(t, billingModel, sent["model"])
			assert.Equal(t, tc.stream, sent["stream"] == true)
			expectedHold := billingLedger{Wallet: tc.wallet, TokenRemain: tc.token, WalletHeld: tc.walletHeld, TokenHeld: tc.tokenHeld}
			if !tc.trusted {
				if tc.subscriptionFunding {
					expectedHold.SubscriptionUsed = 100
				} else {
					expectedHold.Wallet -= 100
				}
				if !tc.playground {
					expectedHold.TokenRemain -= 100
					expectedHold.TokenUsed = 100
				}
			}
			require.Equal(t, expectedHold, observation.ledger, "provider must observe the exact hold before it responds")
			hold = observation.ledger
		default:
			t.Fatal("relay returned without the expected upstream observation")
		}
	}
	require.Equal(t, wantCalls, calls.Load(), "admission failures never submit and paid calls are not replayed")

	expected := billingLedger{Wallet: tc.wallet, TokenRemain: tc.token, WalletHeld: tc.walletHeld, TokenHeld: tc.tokenHeld}
	if tc.status == http.StatusOK {
		if tc.subscriptionFunding {
			expected.SubscriptionUsed = int64(tc.actual)
		} else {
			expected.Wallet -= tc.actual
		}
		if !tc.playground {
			expected.TokenRemain -= tc.actual + tc.drainToken
			expected.TokenUsed = tc.actual + tc.drainToken
		}
		expected.UserUsed, expected.RequestCount, expected.ChannelUsed = tc.actual, 1, int64(tc.actual)
	}
	// Refund and cache deltas use the production pool; wait for their observable
	// effects before reading state or restoring process-wide dependencies.
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		assert.Zero(check, gopool.WorkerCount(), "refund/cache jobs must finish before the fixture is restored")
		ledger, err := readBillingLedger(env.DB, tc.subscriptionTotal > 0)
		if assert.NoError(check, err) {
			assert.Equal(check, expected, ledger)
		}
		if useRedis {
			assertBillingCache(check, expected, tc.playground)
		}
	}, 5*time.Second, 10*time.Millisecond)
	final, err := readBillingLedger(env.DB, tc.subscriptionTotal > 0)
	require.NoError(t, err)

	var records []model.SubscriptionPreConsumeRecord
	require.NoError(t, env.DB.Order("id").Find(&records).Error)
	if tc.subscriptionFunding {
		require.Len(t, records, 1)
		assert.Equal(t, int64(100), records[0].PreConsumed)
		assert.Equal(t, "consumed", records[0].Status)
	} else {
		require.Empty(t, records)
	}
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type IN ?", []int{model.LogTypeConsume, model.LogTypeError}).Order("id").Find(&logs).Error)
	wantLogs, wantErrors := 0, 0
	if tc.status == http.StatusOK {
		wantLogs = 1
	}
	if tc.upstreamError {
		wantErrors = 1
	}
	require.Len(t, logs, wantLogs+wantErrors)
	consumeCount, errorCount := 0, 0
	logValues := make([]any, 0, len(logs))
	for _, row := range logs {
		if row.Type == model.LogTypeConsume {
			consumeCount++
			assert.Equal(t, tc.actual, row.Quota)
		} else {
			errorCount++
			assert.Zero(t, row.Quota)
		}
		assert.Equal(t, tc.stream, row.IsStream)
		assert.GreaterOrEqual(t, row.UseTime, 0)
		var value map[string]any
		raw, err := common.Marshal(row)
		require.NoError(t, err)
		require.NoError(t, common.Unmarshal(raw, &value))
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(row.Other, &other))
		if row.Type == model.LogTypeConsume {
			frt, ok := other["frt"].(float64)
			require.True(t, ok, "frt must be a numeric duration")
			require.False(t, math.IsNaN(frt) || math.IsInf(frt, 0))
			if tc.stream {
				assert.GreaterOrEqual(t, frt, float64(0))
			} else {
				// Non-stream text never calls SetFirstResponseTime: its existing
				// sentinel is exactly StartTime-1s, rather than an elapsed duration.
				assert.Equal(t, float64(-1000), frt)
			}
			other["frt"] = "__duration__"
		}
		value["use_time"] = "__duration__"
		value["other"] = other
		logValues = append(logValues, value)
	}
	assert.Equal(t, wantLogs, consumeCount)
	assert.Equal(t, wantErrors, errorCount)
	var responseBody any
	if tc.stream && tc.status == http.StatusOK {
		assert.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
		assert.Contains(t, recorder.Body.String(), "data: [DONE]")
		responseBody = recorder.Body.String()
	} else {
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &responseBody))
	}
	headers := recorder.Header().Clone()
	for i := range headers["Date"] {
		headers["Date"][i] = "__timestamp__"
	}
	state := map[string]any{
		"hold_at_upstream": hold, "final": final,
		"subscription_records": records, "relay_logs": logValues,
		"upstream_calls": calls.Load(),
		"response":       map[string]any{"status": recorder.Code, "headers": headers, "body": responseBody},
	}
	// GoldenValue normalizes generic JSON, not structs; decode once so the
	// timestamp mask also reaches subscription records and ledger snapshots.
	raw, err := common.Marshal(state)
	require.NoError(t, err)
	var snapshot any
	require.NoError(t, common.Unmarshal(raw, &snapshot))
	contracttest.GoldenValue(t, golden, snapshot, "created_at", "updated_at")
}

func newBillingEnv(t *testing.T, tc billingCase, useRedis bool) *contracttest.Env {
	t.Helper()
	previousMemory, previousBatch := common.MemoryCacheEnabled, common.BatchUpdateEnabled
	previousLogs, previousExport := common.LogConsumeEnabled, common.DataExportEnabled
	previousCount, previousRetries := constant.CountToken, common.RetryTimes
	previousStreamingTimeout := constant.StreamingTimeout
	previousPreconsume, previousReminder := common.PreConsumedQuota, common.QuotaRemindThreshold
	previousUnit, previousRedis, previousRDB := common.QuotaPerUnit, common.RedisEnabled, common.RDB
	previousErrorLogs, previousModelRate := constant.ErrorLogEnabled, setting.ModelRequestRateLimitEnabled
	previousPerformance := common.GetPerformanceMonitorConfig()
	previousModelPrice, previousModels := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString()
	previousCompletion := ratio_setting.CompletionRatio2JSONString()
	savedConfig := make(map[string]string)
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { savedConfig[key] = value; return nil }))
	t.Cleanup(func() {
		common.MemoryCacheEnabled, common.BatchUpdateEnabled = previousMemory, previousBatch
		common.LogConsumeEnabled, common.DataExportEnabled = previousLogs, previousExport
		constant.CountToken, common.RetryTimes = previousCount, previousRetries
		constant.StreamingTimeout = previousStreamingTimeout
		common.PreConsumedQuota, common.QuotaRemindThreshold = previousPreconsume, previousReminder
		common.QuotaPerUnit, common.RedisEnabled, common.RDB = previousUnit, previousRedis, previousRDB
		constant.ErrorLogEnabled, setting.ModelRequestRateLimitEnabled = previousErrorLogs, previousModelRate
		common.SetPerformanceMonitorConfig(previousPerformance)
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousModelPrice))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModels))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(previousCompletion))
	})
	common.MemoryCacheEnabled, common.BatchUpdateEnabled = false, false
	common.LogConsumeEnabled, common.DataExportEnabled = true, false
	constant.CountToken, common.RetryTimes = false, 2
	// The fixture does not call common.Init, which sets this production default.
	constant.StreamingTimeout = 300
	constant.ErrorLogEnabled, setting.ModelRequestRateLimitEnabled = true, false
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{})
	common.PreConsumedQuota, common.QuotaRemindThreshold, common.QuotaPerUnit = 80, -1000000000, 500000
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{}`, "billing_setting.billing_expr": `{}`,
		"group_ratio_setting.group_ratio": `{"default":1}`, "group_ratio_setting.group_group_ratio": `{}`,
		"global.chat_completions_to_responses_policy": `{"enabled":false}`,
		"fetch_setting.enable_ssrf_protection":        "false",
		"channel_affinity_setting.enabled":            "false",
	}))
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"billing-contract-model":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"billing-contract-model":1}`))
	env := contracttest.New(t)
	preference := tc.preference
	if preference == "" {
		preference = "wallet_only"
	}
	userSetting, err := common.Marshal(dto.UserSetting{BillingPreference: preference, Language: "zh-CN"})
	require.NoError(t, err)
	require.NoError(t, env.DB.Model(&model.User{}).Where("id = ?", env.UserID).Updates(map[string]any{"quota": tc.wallet, "batch_held_quota": tc.walletHeld, "setting": string(userSetting)}).Error)
	token := model.Token{Id: 1, UserId: env.UserID, Key: billingKey, Name: "billing-contract-token", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: tc.token, UnlimitedQuota: tc.unlimited, BatchHeldQuota: tc.tokenHeld}
	require.NoError(t, env.DB.Create(&token).Error)
	if tc.subscriptionTotal > 0 {
		plan := model.SubscriptionPlan{Id: 1, Title: "billing-contract-plan", Enabled: true, QuotaResetPeriod: "never", TotalAmount: tc.subscriptionTotal, AllowWalletOverflow: common.GetPointer(tc.overflow)}
		require.NoError(t, env.DB.Create(&plan).Error)
		subscription := model.UserSubscription{Id: 1, UserId: env.UserID, PlanId: plan.Id, AmountTotal: tc.subscriptionTotal, StartTime: 1700000000, EndTime: 4102444800, Status: "active", AllowWalletOverflow: tc.overflow}
		require.NoError(t, env.DB.Create(&subscription).Error)
		model.InvalidateSubscriptionPlanCache(plan.Id)
	}
	// Loading a fixed publication directly also keeps its version/timestamps
	// independent from how many earlier cases ran in this test process.
	require.NoError(t, env.DB.Create(&model.ModelCostFXRow{Source: "cbr-xml-daily.ru", EffectiveAt: 1700000000, FetchedAt: 1700000000, Version: 1, RatesJSON: `{"USD":100}`}).Error)
	require.NoError(t, model.LoadModelCostFX(context.Background(), "cbr-xml-daily.ru"))
	if useRedis {
		server := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
		t.Cleanup(func() { require.NoError(t, client.Close()) })
		common.RDB, common.RedisEnabled = client, true
		_, err := model.GetUserCache(env.UserID)
		require.NoError(t, err)
		_, err = model.GetTokenByKey(billingKey, false)
		require.NoError(t, err)
	}
	// This cleanup runs before the Redis client and database cleanups, even if
	// an earlier assertion fails while a production refund job is still queued.
	t.Cleanup(func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 5*time.Second, 10*time.Millisecond)
	})
	return env
}

func readBillingLedger(db *gorm.DB, hasSubscription bool) (billingLedger, error) {
	var ledger billingLedger
	var user model.User
	if err := db.First(&user, 1).Error; err != nil {
		return ledger, err
	}
	var token model.Token
	if err := db.First(&token, 1).Error; err != nil {
		return ledger, err
	}
	var channel model.Channel
	if err := db.First(&channel, 1).Error; err != nil {
		return ledger, err
	}
	ledger = billingLedger{Wallet: user.Quota, UserUsed: user.UsedQuota, RequestCount: user.RequestCount, WalletHeld: user.BatchHeldQuota, TokenRemain: token.RemainQuota, TokenUsed: token.UsedQuota, TokenHeld: token.BatchHeldQuota, ChannelUsed: channel.UsedQuota}
	if hasSubscription {
		var subscription model.UserSubscription
		if err := db.First(&subscription, 1).Error; err != nil {
			return ledger, err
		}
		ledger.SubscriptionUsed = subscription.AmountUsed
	}
	return ledger, nil
}

func assertBillingCache(t assert.TestingT, expected billingLedger, playground bool) {
	ctx := context.Background()
	quota, err := common.RDB.HGet(ctx, "user:1", "Quota").Int()
	if assert.NoError(t, err) {
		assert.Equal(t, expected.Wallet, quota)
	}
	if !playground {
		key := "token:" + common.GenerateHMAC(billingKey)
		remain, err := common.RDB.HGet(ctx, key, "RemainQuota").Int()
		if assert.NoError(t, err) {
			assert.Equal(t, expected.TokenRemain, remain)
		}
		used, err := common.RDB.HGet(ctx, key, "UsedQuota").Int()
		if assert.NoError(t, err) {
			assert.Equal(t, expected.TokenUsed, used)
		}
	}
}
