package billing_test

import (
	subscriptionstorage "github.com/QuantumNous/new-api/internal/app/subscriptionstorage"

	apikeystorage "github.com/QuantumNous/new-api/internal/app/apikeystorage"

	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"

	fxstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/fx"

	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	tokenusage "github.com/QuantumNous/new-api/gateway/tokenusage"
	"github.com/QuantumNous/new-api/internal/contracttest"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"

	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
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
	httpclient.InitHttpClient()
	tokenusage.InitTokenEncoders()
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
			err = apikeystorage.Quota().DecreaseTokenQuota(1, billingKey, tc.drainToken)
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
	require.NoError(t, channelstore.AddAbilities(&(channel), nil))
	channelstore.InitChannelCache()
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
		subscriptionstorage.Cache().InvalidateSubscriptionPlanCache(plan.Id)
	}
	// Loading a fixed publication directly also keeps its version/timestamps
	// independent from how many earlier cases ran in this test process.
	require.NoError(t, env.DB.Create(&model.ModelCostFXRow{Source: "cbr-xml-daily.ru", EffectiveAt: 1700000000, FetchedAt: 1700000000, Version: 1, RatesJSON: `{"USD":100}`}).Error)
	require.NoError(t, fxstorage.LoadModelCostFX(context.Background(), "cbr-xml-daily.ru"))
	if useRedis {
		server := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
		t.Cleanup(func() { require.NoError(t, client.Close()) })
		common.RDB, common.RedisEnabled = client, true
		_, err := accountstorage.Cache().GetUserCache(env.UserID)
		require.NoError(t, err)
		_, err = apikeystorage.Reader().Read(billingKey, false)
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

// Cost cases reuse the lifecycle fixture and ledger reader. Each case runs in
// its own process: the production FX publication is monotonic and deliberately
// has no public API that rewinds its RAM state for the next test.
type billingCostCase struct {
	name, model, path, upstreamPath string
	request, response, usage        string
	expression, rates, afterRates   string
	channelType                     int
	ratio, completion, group        float64
	special, price                  *float64
	toolPrice                       *float64
	hold, quota, status             int
	stream, redis, playground       bool
	upstreamFailure                 bool
	emptyContent, noCount           bool
	errorCode                       string
	accountingRate                  float64
	other                           map[string]any
}

func TestBillingCostHTTPContract(t *testing.T) {
	runBillingCostContracts(t, false)
}

// origin/main does not register /v1/responses. These four cases exercise its
// existing gateway through a test-only route, not the production HTTP surface.
// No middleware is bypassed and the absent production route is asserted first.
func TestBillingCostGatewayContract(t *testing.T) {
	runBillingCostContracts(t, true)
}

func runBillingCostContracts(t *testing.T, gateway bool) {
	t.Helper()
	test, selector := "TestBillingCostHTTPContract", "WAVE2B2_COST_CASE"
	if gateway {
		test, selector = "TestBillingCostGatewayContract", "WAVE2B2_COST_GATEWAY"
	}
	selected := os.Getenv(selector)
	for _, tc := range billingCostCases() {
		if (tc.path == "/v1/responses") != gateway {
			continue
		}
		if selected != "" {
			if tc.name == selected {
				httpclient.InitHttpClient()
				tokenusage.InitTokenEncoders()
				runBillingCostRelayContract(t, tc)
				return
			}
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			runBillingCostProcess(t, test, selector, tc.name)
		})
	}
	if selected != "" {
		t.Fatalf("unknown isolated cost case %q for %s", selected, test)
	}
}

func runBillingCostProcess(t *testing.T, test, selector, name string) {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), executable, "-test.run=^"+test+"$", "-test.count=1", "-test.timeout=90s", "-update="+flag.Lookup("update").Value.String())
	command.Env = append(os.Environ(), selector+"="+name)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func billingCostCases() []billingCostCase {
	const plain = `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}`
	const cache = `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":60}}`
	const audio = `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"text_tokens":20,"audio_tokens":80},"completion_tokens_details":{"text_tokens":5,"audio_tokens":15}}`
	const tier = `len <= 100 ? tier("short", p * 2 + c * 4) : tier("long", p * 4 + c * 8)`
	cases := []billingCostCase{
		{name: "ratio_completion_half_round", ratio: 1.25, completion: 2.5, usage: plain, hold: 125, quota: 188},
		{name: "ratio_stream", ratio: 1.25, completion: 2.5, usage: plain, stream: true, hold: 125, quota: 188},
		{name: "ratio_minimum_after_round", ratio: 0.001, usage: `{"prompt_tokens":1,"total_tokens":1}`, hold: 0, quota: 1},
		{name: "group_ratio", group: 1.5, usage: plain, hold: 150, quota: 180},
		{name: "user_group_override", group: 1.5, special: common.GetPointer(0.75), usage: plain, hold: 75, quota: 90},
		{name: "user_group_explicit_zero", special: common.GetPointer(0.0), usage: plain, hold: 0, quota: 0},
		{name: "model_price_rounding", price: common.GetPointer(0.000013), usage: plain, hold: 6, quota: 7},
		{name: "cache_read", usage: cache, hold: 100, quota: 66},
		{name: "cache_read_redis", usage: cache, redis: true, hold: 100, quota: 66},
		{name: "cache_read_and_write", usage: `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":20,"cache_write_tokens":30}}`, hold: 100, quota: 110, other: map[string]any{"cache_write_tokens": float64(30)}},
		{name: "cache_overlapping_prefix", usage: `{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30,"prompt_tokens_details":{"cached_tokens":8,"cache_write_tokens":9}}`, hold: 100, quota: 32},
		{name: "image_input_tokens", usage: `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"image_tokens":10}}`, hold: 100, quota: 130},
		{name: "embedding", path: "/v1/embeddings", request: `{"model":"billing-contract-model","input":"hello"}`, response: `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.25,0.5]}],"model":"billing-contract-model","usage":{"prompt_tokens":100,"total_tokens":100}}`, hold: 80, quota: 100},
		{name: "rerank", path: "/v1/rerank", request: `{"model":"billing-contract-model","query":"hello","documents":["hello","world"],"top_n":1}`, response: `{"results":[{"index":0,"relevance_score":0.9}],"usage":{"total_tokens":100}}`, hold: 80, quota: 100},
		{name: "responses_cache", path: "/v1/responses", request: `{"model":"billing-contract-model","input":"hello","max_output_tokens":20}`, response: `{"id":"resp-fixture","object":"response","created_at":1700000000,"status":"completed","model":"billing-contract-model","output":[],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":20,"cache_write_tokens":30}}}`, hold: 100, quota: 110},
		{name: "chat_audio_input_output", completion: 2, usage: audio, hold: 100, quota: 1130, other: map[string]any{"audio": true}},
		{name: "chat_audio_fixed_legacy_zero", price: common.GetPointer(0.00001), usage: audio, hold: 5, quota: 0, other: map[string]any{"audio": true}},
		{name: "speech_pcm", path: "/v1/audio/speech", request: `{"model":"billing-contract-model","input":"hello","voice":"alloy","response_format":"pcm"}`, response: strings.Repeat("\x00", 48), hold: 80, quota: 340, other: map[string]any{"audio_output": float64(17)}},
		{name: "transcription_usage", path: "/v1/audio/transcriptions", response: `{"text":"hello","usage":` + audio + `}`, completion: 2, hold: 80, quota: 1130},
		{name: "image_model_price_n", path: "/v1/images/generations", request: `{"model":"billing-contract-model","prompt":"hello","n":3,"size":"1024x1024"}`, response: `{"created":1700000000,"data":[{"b64_json":"AA=="},{"b64_json":"AA=="},{"b64_json":"AA=="}]}`, price: common.GetPointer(0.00001), hold: 15, quota: 15},
		{name: "image_actual_count_override", path: "/v1/images/generations", request: `{"model":"billing-contract-model","prompt":"hello","n":3,"size":"1024x1024"}`, response: `{"created":1700000000,"data":[{"b64_json":"AA=="}]}`, price: common.GetPointer(0.00001), hold: 15, quota: 5},
		{name: "tier_at_len_boundary", expression: tier, usage: plain, hold: 40, quota: 140, other: map[string]any{"matched_tier": "short"}},
		{name: "tier_above_len_boundary", expression: tier, usage: `{"prompt_tokens":101,"completion_tokens":20,"total_tokens":121}`, hold: 40, quota: 282, other: map[string]any{"matched_tier": "long"}},
		{name: "tier_cache_keeps_len", expression: `len <= 100 ? tier("short", p * 2 + c * 4 + cr * 0.2) : tier("long", p * 4 + c * 8 + cr * 0.4)`, usage: `{"prompt_tokens":101,"completion_tokens":20,"total_tokens":121,"prompt_tokens_details":{"cached_tokens":90}}`, hold: 40, quota: 120, other: map[string]any{"matched_tier": "long"}},
		{name: "fixed_expression", expression: `tier("request", fixed(0.000013))`, usage: plain, hold: 7, quota: 7, other: map[string]any{"billing_unit": "request", "fixed_price": 0.000013}},
		{name: "fixed_expression_zero", expression: `tier("request", fixed(0))`, usage: plain, hold: 0, quota: 0, other: map[string]any{"billing_unit": "request", "fixed_price": float64(0)}},
		{name: "fixed_expression_zero_tokens", expression: `tier("request", fixed(0.00002))`, usage: `{}`, emptyContent: true, hold: 10, quota: 10},
		{name: "fixed_expression_stream", expression: `tier("request", fixed(0.000013))`, usage: plain, stream: true, hold: 7, quota: 7},
		{name: "fixed_estimate_token_actual", expression: `len <= 100 ? tier("request", fixed(0.00002)) : tier("tokens", p * 2 + c * 4)`, usage: `{"prompt_tokens":101,"completion_tokens":20,"total_tokens":121}`, hold: 10, quota: 141, other: map[string]any{"billing_unit": "token", "matched_tier": "tokens"}},
		{name: "request_rule_header", expression: `tier("base", p * 2 + c * 4) * (header("x-cost-mode") == "fast" ? 1.5 : 1)`, usage: plain, hold: 60, quota: 210},
		{name: "fx_ratio", rates: `{"USD":137.5}`, usage: plain, hold: 137, quota: 165},
		{name: "fx_frozen_during_upstream", rates: `{"USD":137.5}`, afterRates: `{"USD":200}`, usage: plain, hold: 137, quota: 165},
		{name: "fx_model_price", rates: `{"USD":137.5}`, price: common.GetPointer(0.00002), usage: plain, hold: 13, quota: 14},
		{name: "fx_tiered_redis", rates: `{"USD":137.5}`, expression: tier, usage: plain, redis: true, hold: 55, quota: 193},
		{name: "fx_audio", rates: `{"USD":137.5}`, completion: 2, usage: audio, hold: 137, quota: 1554},
		{name: "zero_usage", usage: `{}`, emptyContent: true, noCount: true, hold: 100, quota: 0},
		{name: "missing_usage_fallback", emptyContent: true, noCount: true, hold: 100, quota: 0},
		{name: "admission_clamp", ratio: 1e20, usage: plain, hold: 0, quota: 0, status: 403, errorCode: "insufficient_user_quota"},
		{name: "admission_fx_without_usd", rates: `{"EUR":1}`, usage: plain, hold: 0, quota: 0, status: 503, errorCode: "model_price_error"},
		{name: "admission_fx_free_without_usd", special: common.GetPointer(0.0), rates: `{"EUR":1}`, usage: plain, hold: 0, quota: 0, status: 503, errorCode: "model_price_error"},
		{name: "admission_accounting_drift", accountingRate: 99, usage: plain, hold: 0, quota: 0, status: 503, errorCode: "model_price_error"},
		{name: "admission_tiered_clamp", expression: `tier("huge", c * 1e20)`, usage: plain, hold: 0, quota: 0, status: 403, errorCode: "insufficient_user_quota"},
		{name: "settlement_clamp", usage: `{"prompt_tokens":2147483647,"completion_tokens":20,"total_tokens":2147483667}`, hold: 100, quota: 2147483647},
		{name: "playground_fixed_expression", expression: `tier("request", fixed(0.00002))`, usage: plain, playground: true, hold: 10, quota: 10},
		{name: "alpha_search_zero_tokens", path: "/v1/alpha/search", channelType: constant.ChannelTypeNewAPI, request: `{"model":"billing-contract-model","query":"hello"}`, response: `{"results":[{"title":"fixture","url":"https://example.invalid/fixture"}]}`, hold: 80, quota: 5000},
		{name: "responses_tools", path: "/v1/responses", request: `{"model":"billing-contract-model","input":"hello","max_output_tokens":20,"tools":[{"type":"web_search_preview"},{"type":"file_search"}]}`, response: `{"id":"resp-fixture","object":"response","created_at":1700000000,"status":"completed","model":"billing-contract-model","output":[{"type":"web_search_call","id":"ws-1","status":"completed"},{"type":"web_search_call","id":"ws-2","status":"completed"},{"type":"file_search_call","id":"fs-1","status":"completed"}],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}`, hold: 100, quota: 11370},
		{name: "tiered_with_tools_single_round", path: "/v1/responses", request: `{"model":"billing-contract-model","input":"hello","max_output_tokens":20,"tools":[{"type":"web_search_preview"}]}`, response: `{"id":"resp-fixture","object":"response","created_at":1700000000,"status":"completed","model":"billing-contract-model","output":[{"type":"web_search_call","id":"ws-1","status":"completed"}],"usage":{"input_tokens":100,"output_tokens":1,"total_tokens":101}}`, expression: `tier("base", p * 0.01 + c * 0.01)`, toolPrice: common.GetPointer(0.001), hold: 0, quota: 1},
		{name: "tiered_request_multiplier_excludes_tools", path: "/v1/responses", request: `{"model":"billing-contract-model","input":"hello","max_output_tokens":20,"tools":[{"type":"web_search_preview"}]}`, response: `{"id":"resp-fixture","object":"response","created_at":1700000000,"status":"completed","model":"billing-contract-model","output":[{"type":"web_search_call","id":"ws-1","status":"completed"}],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}`, expression: `tier("base", p * 2 + c * 4) * (header("x-cost-mode") == "fast" ? 2 : 1)`, hold: 80, quota: 5280},
		{name: "fx_tool_display_legacy", path: "/v1/alpha/search", channelType: constant.ChannelTypeNewAPI, request: `{"model":"billing-contract-model","query":"hello"}`, response: `{"results":[]}`, rates: `{"USD":137.5}`, hold: 110, quota: 6875, redis: true},
	}
	claudeUsage := `{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":60,"cache_creation_input_tokens":50,"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":20}}`
	claudeResponse := `{"id":"msg-fixture","type":"message","role":"assistant","model":"billing-contract-model","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":` + claudeUsage + `}`
	cases = append(cases,
		billingCostCase{name: "claude_split_cache", path: "/v1/messages", channelType: constant.ChannelTypeAnthropic, request: `{"model":"billing-contract-model","max_tokens":20,"messages":[{"role":"user","content":"hello"}]}`, response: claudeResponse, hold: 100, quota: 204},
		billingCostCase{name: "claude_tier_len_includes_cache", path: "/v1/messages", channelType: constant.ChannelTypeAnthropic, request: `{"model":"billing-contract-model","max_tokens":20,"messages":[{"role":"user","content":"hello"}]}`, response: claudeResponse, expression: `len <= 200 ? tier("short", p * 2 + c * 4 + cr * 0.2 + cc * 2.5 + cc1h * 4) : tier("long", p * 4 + c * 8 + cr * 0.4 + cc * 5 + cc1h * 8)`, hold: 40, quota: 447, other: map[string]any{"matched_tier": "long"}},
		billingCostCase{name: "claude_web_search", path: "/v1/messages", channelType: constant.ChannelTypeAnthropic, request: `{"model":"billing-contract-model","max_tokens":20,"messages":[{"role":"user","content":"hello"}]}`, response: `{"id":"msg-fixture","type":"message","role":"assistant","model":"billing-contract-model","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":100,"output_tokens":20,"server_tool_use":{"web_search_requests":2}}}`, hold: 100, quota: 10120},
		billingCostCase{name: "gemini_audio_input", model: "gemini-2.5-flash", path: "/v1beta/models/gemini-2.5-flash:generateContent", channelType: constant.ChannelTypeGemini, request: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":20}}`, response: `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120,"promptTokensDetails":[{"modality":"TEXT","tokenCount":20},{"modality":"AUDIO","tokenCount":80}]}}`, hold: 100, quota: 80, other: map[string]any{"audio_input_seperate_price": true}},
		billingCostCase{name: "gemini_grounding", model: "gemini-2.5-flash", path: "/v1beta/models/gemini-2.5-flash:generateContent", channelType: constant.ChannelTypeGemini, request: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":20}}`, response: `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP","index":0,"groundingMetadata":{"webSearchQueries":["hello"]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120}}`, hold: 100, quota: 120},
	)
	// The native non-stream handler above does not mark Google Search calls.
	// Its stream handler does, so pin both existing paths without fixing either.
	cases = append(cases, billingCostCase{
		name: "gemini_grounding_stream", model: "gemini-2.5-flash", channelType: constant.ChannelTypeGemini,
		path: "/v1beta/models/gemini-2.5-flash:streamGenerateContent", stream: true,
		request:  `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":20}}`,
		response: "data: " + `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP","index":0,"groundingMetadata":{"webSearchQueries":["hello"]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120}}` + "\n\n",
		hold:     100, quota: 7120,
		other: map[string]any{"tool_surcharges": []any{map[string]any{"name": "google_search", "count": float64(1), "price": float64(14)}}},
	})
	// A zero complete discovery tariff makes the configured fixed reservation the
	// exact admission floor, independent of tokenizer estimates inside discovery.
	for _, cost := range []struct {
		name, raw string
		quota     int
	}{
		{"openrouter_actual_cost", "0.0002", 138},
		{"openrouter_explicit_zero", "0", 0},
	} {
		cases = append(cases, billingCostCase{name: cost.name, model: "fixture/cost-model", channelType: constant.ChannelTypeOpenRouter, rates: `{"USD":137.5}`, price: common.GetPointer(0.0001), usage: `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"cost":` + cost.raw + `}`, hold: 69, quota: cost.quota})
	}
	for _, unknown := range []struct{ name, usage string }{
		{"openrouter_null_cost", `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"cost":null}`},
		{"openrouter_missing_cost", `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}`},
	} {
		cases = append(cases, billingCostCase{name: unknown.name, model: "fixture/cost-model", channelType: constant.ChannelTypeOpenRouter, rates: `{"USD":137.5}`, price: common.GetPointer(0.0001), usage: unknown.usage, hold: 69, quota: 0, status: 502, errorCode: "bad_response_body", upstreamFailure: true})
	}
	for i := range cases {
		tc := &cases[i]
		if tc.model == "" {
			tc.model = billingModel
		}
		if tc.path == "" {
			tc.path = "/v1/chat/completions"
		}
		if tc.upstreamPath == "" {
			tc.upstreamPath = tc.path
		}
		if tc.channelType == constant.ChannelTypeGemini {
			tc.upstreamPath = "/v1beta/models/" + tc.model + ":generateContent"
			if tc.stream {
				tc.upstreamPath = "/v1beta/models/" + tc.model + ":streamGenerateContent"
			}
		}
		if tc.channelType == 0 {
			tc.channelType = constant.ChannelTypeOpenAI
		}
		if tc.status == 0 {
			tc.status = http.StatusOK
		}
		if tc.ratio == 0 {
			tc.ratio = 1
		}
		if tc.completion == 0 {
			tc.completion = 1
		}
		if tc.group == 0 {
			tc.group = 1
		}
		if tc.request == "" {
			tc.request = fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":20,"stream":%t,"stream_options":{"include_usage":true}}`, tc.model, tc.stream)
		}
		if tc.response == "" {
			tc.response = billingCostChatResponse(*tc)
		}
	}
	return cases
}

func billingCostChatResponse(tc billingCostCase) string {
	content := "hello"
	if tc.emptyContent {
		content = ""
	}
	usage := ""
	if tc.usage != "" {
		usage = `,"usage":` + tc.usage
	}
	if !tc.stream {
		return fmt.Sprintf(`{"id":"chatcmpl-fixture","object":"chat.completion","created":1700000000,"model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]%s}`, tc.model, content, usage)
	}
	return fmt.Sprintf("data: {\"id\":\"chatcmpl-fixture\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chatcmpl-fixture\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]%s}\n\ndata: [DONE]\n\n", tc.model, content, tc.model, usage)
}

func configureBillingCost(t *testing.T, env *contracttest.Env, tc billingCostCase) {
	t.Helper()
	// A saturated terminal debit can create legacy debt. Keep this fixture from
	// invoking email/webhook transports even in that case.
	common.QuotaRemindThreshold = math.MinInt
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(fmt.Sprintf(`{%q:%g}`, tc.model, tc.ratio)))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(fmt.Sprintf(`{%q:%g}`, tc.model, tc.completion)))
	require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(fmt.Sprintf(`{%q:0.1}`, tc.model)))
	require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(fmt.Sprintf(`{%q:1.25}`, tc.model)))
	require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(fmt.Sprintf(`{%q:2}`, tc.model)))
	require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(fmt.Sprintf(`{%q:10}`, tc.model)))
	require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(fmt.Sprintf(`{%q:2}`, tc.model)))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(fmt.Sprintf(`{"default":%g}`, tc.group)))
	if tc.special != nil {
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(fmt.Sprintf(`{"default":{"default":%g}}`, *tc.special)))
	}
	if tc.price != nil {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(fmt.Sprintf(`{%q:%g}`, tc.model, *tc.price)))
	}
	if tc.expression != "" {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"billing_setting.billing_mode": fmt.Sprintf(`{%q:"tiered_expr"}`, tc.model),
			"billing_setting.billing_expr": fmt.Sprintf(`{%q:%q}`, tc.model, tc.expression),
		}))
	}
	// Explicitly fix operator overrides, rather than relying on a host's config.
	operation_setting.LoadToolPricesFromJSONString(`{"web_search":10,"web_search_preview":10,"file_search":2.5,"google_search":14}`)
	if tc.toolPrice != nil {
		operation_setting.SetToolPriceForTest("web_search_preview", *tc.toolPrice)
	}
	if tc.rates != "" {
		require.NoError(t, publishBillingCostFX(env.DB, tc.rates, 2))
	}
	if tc.accountingRate != 0 {
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = tc.accountingRate
	}
}

func publishBillingCostFX(db *gorm.DB, rates string, version int64) error {
	if err := db.Model(&model.ModelCostFXRow{}).Where("source = ?", "cbr-xml-daily.ru").Updates(map[string]any{
		"rates_json": rates, "version": version, "effective_at": int64(1700000000) + version, "fetched_at": int64(1700000000) + version,
	}).Error; err != nil {
		return err
	}
	return fxstorage.LoadModelCostFX(context.Background(), "cbr-xml-daily.ru")
}

func runBillingCostRelayContract(t *testing.T, tc billingCostCase) {
	t.Helper()
	const balance = 2_000_000 // Below the trust threshold, including unlimited keys.
	env := newBillingEnv(t, billingCase{wallet: balance, token: balance}, tc.redis)
	configureBillingCost(t, env, tc)
	type observation struct {
		Ledger billingLedger `json:"ledger"`
		Path   string        `json:"path"`
	}
	observed := make(chan observation, 1)
	errors := make(chan error, 4)
	var paidCalls, metadataCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && tc.channelType == constant.ChannelTypeOpenRouter {
			metadataCalls.Add(1)
			if r.URL.Path != "/v1/models/fixture/cost-model/endpoints" {
				errors <- fmt.Errorf("unexpected discovery path %q", r.URL.Path)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"id":"fixture/cost-model","architecture":{"output_modalities":["text"]},"endpoints":[{"tag":"fixture","context_length":100000,"supported_parameters":["max_tokens"],"pricing":{"prompt":"0","completion":"0","input_cache_read":"0","input_cache_write":"0"}}]}}`)
			return
		}
		if paidCalls.Add(1) != 1 || r.Method != http.MethodPost || r.URL.Path != tc.upstreamPath {
			errors <- fmt.Errorf("unexpected submission %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		ledger, err := readBillingLedger(env.DB, false)
		if err != nil {
			errors <- err
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		observed <- observation{ledger, r.URL.Path}
		if tc.afterRates != "" {
			if err := publishBillingCostFX(env.DB, tc.afterRates, 3); err != nil {
				errors <- err
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if tc.stream {
			w.Header().Set("Content-Type", "text/event-stream")
		}
		if tc.path == "/v1/audio/speech" {
			w.Header().Set("Content-Type", "audio/pcm")
		}
		_, _ = io.WriteString(w, tc.response)
		if tc.stream {
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(upstream.Close)
	channel := model.Channel{Id: 1, Type: tc.channelType, Key: "contract-upstream-key", Status: common.ChannelStatusEnabled, Name: "billing-contract", BaseURL: &upstream.URL, Models: tc.model, Group: "default", AutoBan: common.GetPointer(0)}
	if tc.channelType == constant.ChannelTypeOpenRouter {
		channel.Setting = common.GetPointer(`{"openrouter_reservation_source":{"fixture/cost-model":"configured_price"}}`)
	}
	require.NoError(t, env.DB.Create(&channel).Error)
	require.NoError(t, channelstore.AddAbilities(&(channel), nil))
	channelstore.InitChannelCache()
	env.MountRelay()
	path, bearer := tc.path, "sk-"+billingKey
	if tc.playground {
		path, bearer = "/pg/chat/completions", env.User
	}
	requestBody := []byte(tc.request)
	contentType := "application/json"
	if tc.path == "/v1/audio/transcriptions" {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.SetBoundary("billing-cost-boundary"))
		require.NoError(t, writer.WriteField("model", tc.model))
		require.NoError(t, writer.WriteField("response_format", "json"))
		file, err := writer.CreateFormFile("file", "fixture.wav")
		require.NoError(t, err)
		_, err = file.Write([]byte("fixture audio bytes; token estimation is disabled"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		requestBody, contentType = body.Bytes(), writer.FormDataContentType()
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept-Language", "zh-CN")
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("X-Cost-Mode", "fast")
	gateway := tc.path == "/v1/responses"
	if gateway {
		// This registration is local to the named gateway test. Pin the real
		// router's 404 before entering the existing controller/relay pipeline.
		missingRequest := request.Clone(request.Context())
		missingRequest.Body = io.NopCloser(bytes.NewReader(requestBody))
		missing := env.Serve(missingRequest)
		require.Equal(t, http.StatusNotFound, missing.Code)
		require.Zero(t, paidCalls.Load())
		env.Route(http.MethodPost, tc.path,
			middleware.RouteTag("relay"), middleware.SystemPerformanceCheck(),
			middleware.TokenAuth(), middleware.ModelRequestRateLimit(), middleware.Distribute(),
			func(c *gin.Context) { controller.Relay(c, relaytypes.RelayFormatOpenAIResponses) })
	}
	recorder := env.Serve(request)
	require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
	wantCalls := int32(1)
	if tc.status != http.StatusOK && !tc.upstreamFailure {
		wantCalls = 0
	}
	require.Equal(t, wantCalls, paidCalls.Load(), "admission errors do not send and paid requests never replay")
	var hold any
	if wantCalls > 0 {
		select {
		case observation := <-observed:
			expected := billingLedger{Wallet: balance - tc.hold, TokenRemain: balance - tc.hold, TokenUsed: tc.hold}
			if tc.playground {
				expected.TokenRemain, expected.TokenUsed = balance, 0
			}
			require.Equal(t, expected, observation.Ledger, "exact reservation at upstream time")
			hold = observation
		default:
			t.Fatal("missing upstream ledger observation")
		}
	}
	if tc.channelType == constant.ChannelTypeOpenRouter {
		require.EqualValues(t, 1, metadataCalls.Load())
	} else {
		require.Zero(t, metadataCalls.Load())
	}
	expected := billingLedger{Wallet: balance - tc.quota, TokenRemain: balance - tc.quota, TokenUsed: tc.quota}
	if tc.playground {
		expected.TokenRemain, expected.TokenUsed = balance, 0
	}
	if tc.status == http.StatusOK && !tc.noCount {
		expected.UserUsed, expected.RequestCount, expected.ChannelUsed = tc.quota, 1, int64(tc.quota)
	}
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		assert.Zero(check, gopool.WorkerCount())
		actual, err := readBillingLedger(env.DB, false)
		if assert.NoError(check, err) {
			assert.Equal(check, expected, actual)
		}
		if tc.redis {
			assertBillingCache(check, expected, tc.playground)
		}
	}, 5*time.Second, 10*time.Millisecond)
	final, err := readBillingLedger(env.DB, false)
	require.NoError(t, err)
	logs := billingCostLogSnapshot(t, tc)
	var response any
	if tc.stream || tc.path == "/v1/audio/speech" && tc.status == http.StatusOK {
		response = recorder.Body.String()
	} else {
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	}
	if tc.errorCode != "" {
		assert.Equal(t, tc.errorCode, gjson.GetBytes(recorder.Body.Bytes(), "error.code").String())
	}
	headers := recorder.Header().Clone()
	for i := range headers["Date"] {
		headers["Date"][i] = "__timestamp__"
	}
	raw, err := common.Marshal(map[string]any{
		"hold_at_upstream": hold, "final": final, "relay_logs": logs,
		"upstream_calls": paidCalls.Load(), "metadata_calls": metadataCalls.Load(),
		"response": map[string]any{"status": recorder.Code, "headers": headers, "body": response},
	})
	require.NoError(t, err)
	var snapshot any
	require.NoError(t, common.Unmarshal(raw, &snapshot))
	prefix := "cost_"
	if gateway {
		prefix = "cost_gateway_"
	}
	contracttest.GoldenValue(t, prefix+tc.name, snapshot, "created_at", "updated_at")
}

func billingCostLogSnapshot(t *testing.T, tc billingCostCase) []any {
	t.Helper()
	var rows []model.Log
	require.NoError(t, model.LOG_DB.Where("type IN ?", []int{model.LogTypeConsume, model.LogTypeError}).Order("id").Find(&rows).Error)
	// These admission failures return before processChannelError; they have no
	// persisted error row. Successful requests log even a zero charge.
	wantRows := 1
	if tc.status != http.StatusOK && !tc.upstreamFailure {
		wantRows = 0
	}
	require.Len(t, rows, wantRows)
	values := make([]any, 0, len(rows))
	for _, row := range rows {
		if tc.status == http.StatusOK {
			require.Equal(t, model.LogTypeConsume, row.Type)
			require.Equal(t, tc.quota, row.Quota)
		} else {
			require.Equal(t, model.LogTypeError, row.Type)
			require.Zero(t, row.Quota)
		}
		require.GreaterOrEqual(t, row.UseTime, 0)
		var value map[string]any
		raw, err := common.Marshal(row)
		require.NoError(t, err)
		require.NoError(t, common.Unmarshal(raw, &value))
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(row.Other, &other))
		for key, expected := range tc.other {
			assert.Equal(t, expected, other[key], "consume other.%s", key)
		}
		if row.Type == model.LogTypeConsume {
			frt, ok := other["frt"].(float64)
			require.True(t, ok, "frt is a numeric duration")
			require.False(t, math.IsNaN(frt) || math.IsInf(frt, 0))
			if tc.stream {
				require.GreaterOrEqual(t, frt, float64(0))
			} else {
				require.Equal(t, float64(-1000), frt)
			}
			other["frt"] = "__duration__"
		}
		if elapsed, present := other["use_time"]; present {
			duration, ok := elapsed.(float64)
			require.True(t, ok)
			require.GreaterOrEqual(t, duration, float64(0))
			other["use_time"] = "__duration__"
		}
		value["other_raw"] = normalizeBillingCostOther(t, row.Other)
		value["other"] = other
		value["use_time"] = "__duration__"
		values = append(values, value)
	}
	return values
}

// Replace only a top-level duration's numeric token. All other raw bytes,
// including key order, string escaping, numeric spellings and nested frt, stay.
func normalizeBillingCostOther(t *testing.T, raw string) string {
	t.Helper()
	for _, key := range []string{"frt", "use_time"} {
		value := gjson.Get(raw, key)
		if !value.Exists() {
			continue
		}
		require.Equal(t, gjson.Number, value.Type)
		require.False(t, math.IsNaN(value.Num) || math.IsInf(value.Num, 0))
		if key == "frt" {
			require.True(t, value.Num == -1000 || value.Num >= 0, "known sentinel or nonnegative elapsed duration")
		}
		require.Greater(t, value.Index, 0)
		require.Equal(t, value.Raw, raw[value.Index:value.Index+len(value.Raw)])
		if key == "use_time" {
			require.GreaterOrEqual(t, value.Num, float64(0))
		}
		raw = raw[:value.Index] + `"__duration__"` + raw[value.Index+len(value.Raw):]
	}
	return raw
}

func TestBillingCostRawOtherNormalization(t *testing.T) {
	const raw = ` {"z":1e-6,"frt":-1000.00,"nested":{"frt":17},"text":"\\u4e2d","use_time":2} `
	assert.Equal(t, ` {"z":1e-6,"frt":"__duration__","nested":{"frt":17},"text":"\\u4e2d","use_time":"__duration__"} `, normalizeBillingCostOther(t, raw))
}

// Providers construct a non-nil usage before PostText. Invalid expressions
// cannot pass normal admission. These legacy API inputs therefore have named
// boundary contracts rather than a fake HTTP middleware that bypasses admission.
func TestBillingCostBoundaryContract(t *testing.T) {
	cases := []struct {
		name, expr, estimatedUnit string
		nilUsage                  bool
		hold, estimate, quota     int
		searchCalls               int
	}{
		{name: "nil_usage_estimate", nilUsage: true, hold: 37, estimate: 23, quota: 23},
		{name: "nil_usage_fixed", expr: `tier("request", fixed(0.00002))`, estimatedUnit: "request", nilUsage: true, hold: 10, estimate: 23, quota: 10},
		{name: "tiered_error_retains_hold", expr: "invalid +-+ expr", hold: 37, quota: 37},
		{name: "tiered_error_uses_estimate_without_hold", expr: "invalid +-+ expr", hold: 0, quota: 78},
		{name: "tiered_error_adds_tool_fee", expr: "invalid +-+ expr", hold: 37, searchCalls: 2, quota: 10037},
	}
	selected := os.Getenv("WAVE2B2_COST_BOUNDARY")
	if selected == "" {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				runBillingCostProcess(t, "TestBillingCostBoundaryContract", "WAVE2B2_COST_BOUNDARY", tc.name)
			})
		}
		return
	}
	for _, tc := range cases {
		if tc.name != selected {
			continue
		}
		const balance = 2_000_000
		env := newBillingEnv(t, billingCase{wallet: balance, token: balance}, false)
		costCase := billingCostCase{model: billingModel, ratio: 1, completion: 1, group: 1, status: http.StatusOK, quota: tc.quota}
		configureBillingCost(t, env, costCase)
		channel := model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Name: "billing-boundary", Status: common.ChannelStatusEnabled}
		require.NoError(t, env.DB.Create(&channel).Error)
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		ctx.Request.Header.Set("Accept-Language", "zh-CN")
		ctx.Set("token_name", "billing-contract-token")
		ctx.Set("claude_web_search_requests", tc.searchCalls)
		start := time.Now()
		info := &relaycommon.RelayInfo{
			UserId: env.UserID, TokenId: 1, TokenKey: billingKey, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1, ChannelType: constant.ChannelTypeOpenAI},
			OriginModelName: billingModel, UsingGroup: "default", UserGroup: "default", RequestId: "billing-boundary-request",
			RelayFormat: relaytypes.RelayFormatOpenAI, StartTime: start, FirstResponseTime: start.Add(-time.Second),
			PriceData: hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}},
		}
		info.UserSetting.BillingPreference = "wallet_only"
		info.SetEstimatePromptTokens(tc.estimate)
		if tc.expr != "" {
			info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{
				BillingMode: "tiered_expr", ExprString: tc.expr, ExprHash: billingexpr.ExprHashString(tc.expr), ExprVersion: 1,
				GroupRatio: 1, QuotaPerUnit: 500000, EstimatedQuotaBeforeGroup: 78, EstimatedQuotaAfterGroup: 78,
				EstimatedTier: "estimate", EstimatedBillingUnit: billingexpr.BillingUnit(tc.estimatedUnit),
			}
		}
		require.Nil(t, service.PreConsumeBilling(ctx, tc.hold, info))
		hold, err := readBillingLedger(env.DB, false)
		require.NoError(t, err)
		require.Equal(t, billingLedger{Wallet: balance - tc.hold, TokenRemain: balance - tc.hold, TokenUsed: tc.hold}, hold)
		var usage *dto.Usage
		if !tc.nilUsage {
			usage = &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
		}
		service.PostTextConsumeQuota(ctx, info, usage, nil)
		expected := billingLedger{Wallet: balance - tc.quota, TokenRemain: balance - tc.quota, TokenUsed: tc.quota, UserUsed: tc.quota, RequestCount: 1, ChannelUsed: int64(tc.quota)}
		require.EventuallyWithT(t, func(check *assert.CollectT) {
			assert.Zero(check, gopool.WorkerCount())
			actual, err := readBillingLedger(env.DB, false)
			if assert.NoError(check, err) {
				assert.Equal(check, expected, actual)
			}
		}, 5*time.Second, 10*time.Millisecond)
		actual, err := readBillingLedger(env.DB, false)
		require.NoError(t, err)
		logs := billingCostLogSnapshot(t, costCase)
		// Direct boundary calls have no request-ID middleware. The log hook
		// generates this row ID; normalize only its value, after checking shape.
		for _, item := range logs {
			row, ok := item.(map[string]any)
			require.True(t, ok)
			requestID, ok := row["request_id"].(string)
			require.True(t, ok, "generated request_id must remain present as a string")
			require.Regexp(t, `^[0-9]{23}[A-Za-z0-9]{16}$`, requestID)
			_, err := time.Parse("20060102150405", requestID[:14])
			require.NoError(t, err, "request_id must start with a valid UTC timestamp")
			row["request_id"] = "__generated_request_id__"
		}
		raw, err := common.Marshal(map[string]any{"hold_before_boundary": hold, "final": actual, "relay_logs": logs})
		require.NoError(t, err)
		var snapshot any
		require.NoError(t, common.Unmarshal(raw, &snapshot))
		contracttest.GoldenValue(t, "cost_boundary_"+tc.name, snapshot, "created_at", "updated_at")
		return
	}
	t.Fatalf("unknown isolated boundary case %q", selected)
}

// The synchronous OpenRouter path always installs OpenRouterCost and returns
// before legacy cache inference. Keep the historical public calculation pinned
// here instead of erasing that state to manufacture an HTTP-reachable branch.
func TestBillingCostLegacyCacheInference(t *testing.T) {
	previous := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = previous })
	price := hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, CacheRatio: 0.1, CacheCreationRatio: 1.25}
	usage := dto.Usage{PromptTokens: 100, CompletionTokens: 20, Cost: 0.000196, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 30}}
	assert.Equal(t, 20, service.CalcOpenRouterCacheCreateTokens(usage, price))
	usage.Cost = math.Inf(1)
	assert.Equal(t, -1, service.CalcOpenRouterCacheCreateTokens(usage, price))
	price.CacheCreationRatio = 1
	assert.Zero(t, service.CalcOpenRouterCacheCreateTokens(usage, price))
}
