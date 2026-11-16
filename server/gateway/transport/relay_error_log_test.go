package transport

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	fxstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/fx"
	usageadapter "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	notificationsadapter "github.com/QuantumNous/new-api/internal/adapter/notifications"
	fxstate "github.com/QuantumNous/new-api/internal/adapter/pricing/fx"
	"github.com/QuantumNous/new-api/internal/contracttest/fxseed"
	pricingdomainfx "github.com/QuantumNous/new-api/internal/pricing/domain"
	usagedomain "github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Initialize translations for this test binary, which does not run main.
func init() {
	if err := i18n.Init(); err != nil {
		panic(err)
	}
}

func TestProcessChannelErrorUsesSnapshotWithoutLeakingChannelMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	previousErrorLogEnabled := constant.ErrorLogEnabled

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Log{}))
	model.DB, model.LOG_DB = database, database
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		constant.ErrorLogEnabled = previousErrorLogEnabled
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, database.Create(&model.User{Id: 7, Username: "log-owner", Group: "default"}).Error)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("id", 7)
	ctx.Set("username", "log-owner")
	ctx.Set("token_name", "test-token")
	ctx.Set("token_id", 11)
	ctx.Set("original_model", "gpt-test")
	ctx.Set("group", "default")
	ctx.Set("channel_id", 202)
	ctx.Set("channel_name", "mutable-context-channel")
	ctx.Set("channel_type", 9)
	ctx.Set("use_channel", []string{"101"})
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now().Add(-time.Second))

	channelSnapshot := types.ChannelError{
		ChannelId:   101,
		ChannelType: 1,
		ChannelName: "snapshot-channel",
		AutoBan:     false,
	}
	apiErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)

	ProcessChannelError(ctx, channelSnapshot, apiErr, nil)

	var stored model.Log
	require.NoError(t, database.First(&stored).Error)
	assert.Equal(t, channelSnapshot.ChannelId, stored.ChannelId)
	storedOther, err := common.StrToMap(stored.Other)
	require.NoError(t, err)
	assert.Equal(t, float64(http.StatusBadGateway), storedOther["status_code"])
	for _, key := range []string{"channel_id", "channel_name", "channel_type"} {
		assert.NotContains(t, storedOther, key)
	}
	adminInfo, ok := storedOther["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"101"}, adminInfo["use_channel"])

	logs, total, err := (usageadapter.Store{}).ListOwnLogs(usagedomain.LogQuery{CountLimit: usagedomain.MaxLogSearchCount,
		UserID: 7, Kind: usagerecord.LogTypeError, StartTimestamp: 0, EndTimestamp: 0, ModelName: "",
		TokenName: "", Start: 0, Count: 10, Group: "", RequestID: "", UpstreamRequestID: ""},
		usagedomain.LogVisibility(usagedomain.AudienceUser))
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	assert.Equal(t, channelSnapshot.ChannelId, logs[0].ChannelId)
	assert.Empty(t, logs[0].ChannelName)
	userOther, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, userOther, "admin_info")
	for _, key := range []string{"channel_id", "channel_name", "channel_type"} {
		assert.NotContains(t, userOther, key)
	}
}

func TestRelayPricingAdmissionPresentersAreSafeAndLocal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousCustomRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	previousModelRatios := ratio_setting.ModelRatio2JSONString()
	previousSpecialRatios := ratio_setting.GroupGroupRatio2JSONString()
	previousCountToken, previousRetries := constant.CountToken, common.RetryTimes
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.ModelCostFXRow{}, &model.User{}))
	require.NoError(t, database.Create(&model.User{Id: 7, Username: "presenter-user", Group: "vip", Quota: 1_000_000}).Error)
	model.DB, model.LOG_DB = database, database
	constant.CountToken, common.RetryTimes = false, 2
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"presenter-clamp":1e20}`))
	fxseed.SeedNativeRouteFX(t, database)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousCustomRate
		constant.CountToken, common.RetryTimes = previousCountToken, previousRetries
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(previousSpecialRatios))
	})

	for _, testCase := range []struct {
		name        string
		format      types.RelayFormat
		body        string
		wantClaude  bool
		customRate  float64
		wantStatus  int
		wantMessage string
	}{
		{name: "ordinary clamp", format: types.RelayFormatOpenAI, body: `{"model":"presenter-clamp","messages":[{"role":"user","content":"x"}]}`, customRate: 100, wantStatus: http.StatusForbidden, wantMessage: "insufficient quota"},
		{name: "Claude clamp", format: types.RelayFormatClaude, body: `{"model":"presenter-clamp","max_tokens":1,"messages":[{"role":"user","content":"x"}]}`, wantClaude: true, customRate: 100, wantStatus: http.StatusForbidden, wantMessage: "insufficient quota"},
		{name: "ordinary FX configuration", format: types.RelayFormatOpenAI, body: `{"model":"presenter-clamp","messages":[{"role":"user","content":"x"}]}`, customRate: 1, wantStatus: http.StatusServiceUnavailable, wantMessage: "model cost accounting configuration is invalid"},
		{name: "Claude FX configuration", format: types.RelayFormatClaude, body: `{"model":"presenter-clamp","max_tokens":1,"messages":[{"role":"user","content":"x"}]}`, wantClaude: true, customRate: 1, wantStatus: http.StatusServiceUnavailable, wantMessage: "model cost accounting configuration is invalid"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = testCase.customRate
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(testCase.body))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, common.RequestIdKey, "presenter-"+strings.ReplaceAll(testCase.name, " ", "-"))
			common.SetContextKey(c, constant.ContextKeyUserId, 7)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUserQuota, 1_000_000)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "presenter-clamp")

			Relay(c, testCase.format)

			require.Equal(t, testCase.wantStatus, recorder.Code)
			assert.Empty(t, c.GetStringSlice("use_channel"), "admission must stop before channel selection or upstream dispatch")
			assert.Contains(t, recorder.Body.String(), testCase.wantMessage)
			assert.NotContains(t, recorder.Body.String(), "QuotaRound")
			assert.NotContains(t, recorder.Body.String(), "1e20")
			if testCase.wantClaude {
				assert.Contains(t, recorder.Body.String(), `"type":"error"`)
			} else {
				assert.Contains(t, recorder.Body.String(), `"error"`)
			}
		})
	}

	for _, testCase := range []struct {
		name, message string
		customRate    float64
	}{
		{name: "upgraded realtime clamp", customRate: 100, message: "insufficient quota"},
		{name: "upgraded realtime FX configuration", customRate: 1, message: "model cost accounting configuration is invalid"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = testCase.customRate
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				c, _ := gin.CreateTestContext(w)
				c.Request = req
				common.SetContextKey(c, common.RequestIdKey, "presenter-"+strings.ReplaceAll(testCase.name, " ", "-"))
				common.SetContextKey(c, constant.ContextKeyUserId, 7)
				common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
				common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
				common.SetContextKey(c, constant.ContextKeyUserQuota, 1_000_000)
				common.SetContextKey(c, constant.ContextKeyOriginalModel, "presenter-clamp")
				Relay(c, types.RelayFormatOpenAIRealtime)
			}))
			t.Cleanup(server.Close)
			client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
			_, payload, err := client.ReadMessage()
			require.NoError(t, err)
			assert.Contains(t, string(payload), `"type":"error"`)
			assert.Contains(t, string(payload), testCase.message)
			assert.NotContains(t, string(payload), "QuotaRound")
			assert.NotContains(t, string(payload), "1e20")
		})
	}

	t.Run("special zero free pricing still rejects missing USD basis", func(t *testing.T) {
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"presenter-free":0}`))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"special-zero":0}}`))
		newContext := func() (*gin.Context, *httptest.ResponseRecorder) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"presenter-free","messages":[{"role":"user","content":"x"}]}`))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, common.RequestIdKey, "presenter-special-zero")
			common.SetContextKey(c, constant.ContextKeyUserId, 7)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "vip")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "special-zero")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "special-zero")
			common.SetContextKey(c, constant.ContextKeyUserQuota, 1_000_000)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "presenter-free")
			return c, recorder
		}

		baseline, baselineRecorder := newContext()
		Relay(baseline, types.RelayFormatOpenAI)
		assert.NotEqual(t, http.StatusServiceUnavailable, baselineRecorder.Code, "valid USD basis must pass pricing before the expected no-channel rejection")
		assert.Empty(t, baseline.GetStringSlice("use_channel"))

		current, err := fxstate.CurrentModelCostFX("cbr-xml-daily.ru")
		require.NoError(t, err)
		require.NoError(t, fxstorage.SaveModelCostFX(context.Background(), pricingdomainfx.ModelCostFXSnapshot{
			Source: current.Source, EffectiveAt: current.EffectiveAt + 1, FetchedAt: current.FetchedAt + 1,
			Rates: map[string]float64{"EUR": 1},
		}, current.Version))
		require.NoError(t, fxstorage.LoadModelCostFX(context.Background(), "cbr-xml-daily.ru"))
		t.Cleanup(func() { fxseed.SeedNativeRouteFX(t, database) })

		unavailable, unavailableRecorder := newContext()
		Relay(unavailable, types.RelayFormatOpenAI)
		require.Equal(t, http.StatusServiceUnavailable, unavailableRecorder.Code)
		assert.Contains(t, unavailableRecorder.Body.String(), "model cost FX basis is invalid")
		assert.NotContains(t, unavailableRecorder.Body.String(), "QuotaRound")
		assert.Empty(t, unavailable.GetStringSlice("use_channel"), "missing USD must reject free/special-zero pricing before channel or upstream")
		var user model.User
		require.NoError(t, database.First(&user, 7).Error)
		assert.Equal(t, 1_000_000, user.Quota, "free baseline and missing-USD rejection must not consume funding")
	})
}

func TestOpenRouterNativeImageRelayAccounting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	httpclient.InitHttpClient()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMemory, previousRedis, previousBatch := common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled
	previousConsume, previousError := common.LogConsumeEnabled, constant.ErrorLogEnabled
	previousCount, previousRetries := constant.CountToken, common.RetryTimes
	previousPrices := ratio_setting.ModelPrice2JSONString()
	previousRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	savedConfig := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { savedConfig[key] = value; return nil }))
	common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = false, false, false
	common.LogConsumeEnabled, constant.ErrorLogEnabled = true, true
	constant.CountToken, common.RetryTimes = false, 3
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = previousMemory, previousRedis, previousBatch
		common.LogConsumeEnabled, constant.ErrorLogEnabled = previousConsume, previousError
		constant.CountToken, common.RetryTimes = previousCount, previousRetries
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousRate
		common.SetDatabaseTypes(previousMain, previousLog)
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
	})
	var bitmap bytes.Buffer
	require.NoError(t, png.Encode(&bitmap, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	encoded := base64.StdEncoding.EncodeToString(bitmap.Bytes())
	const defaultImageModel = "black-forest-labs/flux.2-pro"
	for _, tc := range []struct {
		name, cost, expression, model, metadata string
		status, quota, reserved                 int
		unknown, transport, stream, legacy      bool
	}{
		{name: "Muse documented price with empty discovery", model: "meta/muse-image", metadata: `{"id":"meta/muse-image","endpoints":[]}`, cost: "0.01", status: 200, quota: 10000, reserved: 10000},
		{name: "Krea Turbo documented price with empty pricing", model: "krea/krea-2-medium-turbo", metadata: `{"id":"krea/krea-2-medium-turbo","endpoints":[{"provider_tag":"krea","supported_parameters":{"n":{"type":"range","min":1,"max":4}},"pricing":[]}]}`, cost: "0.015", status: 200, quota: 15000, reserved: 15000},
		{name: "actual below estimate", cost: "0.02", status: 200, quota: 20000, reserved: 40000},
		{name: "actual above estimate", cost: "0.06", status: 200, quota: 60000, reserved: 40000},
		{name: "explicit zero cost", cost: "0", status: 200, reserved: 40000},
		{name: "administrator expression", cost: "0.02", expression: `tier("admin", provider_cost * 2000000)`, status: 200, quota: 20000, reserved: 40000},
		{name: "administrator fixed image price", cost: "0.02", legacy: true, status: 200, quota: 20000, reserved: 40000},
		{name: "stream completed image", cost: "0.02", stream: true, status: 200, quota: 20000, reserved: 40000},
		{name: "upstream rejection", status: 429, reserved: 40000},
		{name: "missing actual cost", status: 502, reserved: 40000, unknown: true},
		{name: "interrupted paid request", status: 502, reserved: 40000, unknown: true, transport: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imageModel := tc.model
			if imageModel == "" {
				imageModel = defaultImageModel
			}
			database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := database.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			require.NoError(t, database.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.ModelCostFXRow{}))
			model.DB, model.LOG_DB = database, database
			fxseed.SeedNativeRouteFX(t, database)
			mode, expr := `{}`, `{}`
			if tc.expression != "" {
				mode = fmt.Sprintf(`{%q:"tiered_expr"}`, imageModel)
				expr = fmt.Sprintf(`{%q:%q}`, imageModel, tc.expression)
			}
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_mode": mode, "billing_setting.billing_expr": expr,
				"group_ratio_setting.group_ratio": `{"default":2}`, "group_ratio_setting.group_group_ratio": `{}`,
			}))
			prices := `{}`
			if tc.legacy {
				prices = fmt.Sprintf(`{%q:0.03}`, imageModel)
			}
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prices))
			const initial = 1000000
			require.NoError(t, database.Create(&model.User{Id: 7, Username: "image-owner", Group: "default", Quota: initial}).Error)
			require.NoError(t, database.Create(&model.Token{Id: 11, UserId: 7, Key: "fixture-token", Name: "image-token", RemainQuota: initial}).Error)
			var paidCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer fixture-provider", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					assert.Equal(t, "/v1/images/models/"+imageModel+"/endpoints", r.URL.Path)
					metadata := tc.metadata
					if metadata == "" {
						metadata = fmt.Sprintf(`{"id":%q,"endpoints":[{"provider_tag":"fixture-cheap","supported_parameters":{"n":{"type":"range","min":1,"max":4}},"pricing":[{"billable":"output_image","unit":"image","cost_usd":0.02}]},{"provider_tag":"fixture-expensive","supported_parameters":{"n":{"type":"range","min":1,"max":4}},"pricing":[{"billable":"output_image","unit":"image","cost_usd":0.04}]}]}`, imageModel)
					}
					_, _ = io.WriteString(w, metadata)
					return
				}
				paidCalls.Add(1)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v1/images", r.URL.Path)
				if tc.metadata != "" {
					var payload map[string]any
					assert.NoError(t, common.DecodeJson(r.Body, &payload))
					assert.Equal(t, imageModel, payload["model"])
					assert.NotContains(t, payload, "input_references")
					assert.NotContains(t, payload, "size")
					if tc.model == "meta/muse-image" {
						assert.Equal(t, map[string]any{"model": imageModel, "prompt": "a red square", "provider": map[string]any{"allow_fallbacks": false}}, payload)
					} else {
						assert.Equal(t, map[string]any{"only": []any{"krea"}, "allow_fallbacks": false}, payload["provider"])
					}
				}

				if tc.metadata == "" {
					var payload map[string]any
					require.NoError(t, common.DecodeJson(r.Body, &payload))
					assert.Equal(t, map[string]any{"only": []any{"fixture-cheap"}, "allow_fallbacks": false}, payload["provider"], "cheapest endpoint stays pinned while max compatible tariff is reserved")
				}
				var user model.User
				var token model.Token
				assert.NoError(t, database.First(&user, 7).Error)
				assert.NoError(t, database.First(&token, 11).Error)
				assert.Equal(t, initial-tc.reserved, user.Quota, "wallet reserved before provider dispatch")
				assert.Equal(t, initial-tc.reserved, token.RemainQuota, "token reserved before provider dispatch")
				if tc.transport {
					connection, _, hijackErr := w.(http.Hijacker).Hijack()
					if assert.NoError(t, hijackErr) {
						_ = connection.Close()
					}
					return
				}
				w.Header().Set("X-Request-Id", "provider-image-fixture")
				if tc.status == 429 {
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = io.WriteString(w, `{"error":{"code":429,"message":"rate limited","metadata":{"raw":"fixture-provider secret"}}}`)
					return
				}
				usage := ""
				if tc.cost != "" {
					usage = `,"usage":{"cost":` + tc.cost + `}`
				}
				_, _ = io.WriteString(w, fmt.Sprintf(`{"created":123,"data":[{"b64_json":%q,"media_type":"image/png"}]%s}`, encoded, usage))
			}))
			t.Cleanup(upstream.Close)
			selected := model.Channel{Id: 21, Type: constant.ChannelTypeOpenRouter, Name: "fixture-image", Key: "fixture-provider", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, Models: imageModel, Group: "default"}
			require.NoError(t, database.Create(&selected).Error)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(fmt.Sprintf(`{"model":%q,"prompt":"a red square","response_format":"url","stream":%t}`, imageModel, tc.stream)))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, common.RequestIdKey, "image-relay-"+strings.ReplaceAll(tc.name, " ", "-"))
			common.SetContextKey(c, constant.ContextKeyUserId, 7)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUserQuota, initial)
			common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
			common.SetContextKey(c, constant.ContextKeyTokenId, 11)
			common.SetContextKey(c, constant.ContextKeyTokenKey, "fixture-token")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, imageModel)
			c.Set("username", "image-owner")
			c.Set("token_name", "image-token")
			require.Nil(t, middleware.SetupContextForSelectedChannel(c, &selected, imageModel))
			Relay(c, types.RelayFormatOpenAIImage)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			assert.Equal(t, int32(1), paidCalls.Load(), "never replay paid generation even when retries are enabled")
			if tc.status == http.StatusOK && tc.stream {
				assert.Contains(t, recorder.Body.String(), `"type":"image_generation.completed"`)
				assert.Contains(t, recorder.Body.String(), `"created_at":123`)
				assert.Contains(t, recorder.Body.String(), encoded)
			} else if tc.status == http.StatusOK {
				var response dto.ImageResponse
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
				require.Len(t, response.Data, 1)
				decoded, err := base64.StdEncoding.DecodeString(response.Data[0].B64Json)
				require.NoError(t, err)
				_, format, err := image.Decode(bytes.NewReader(decoded))
				require.NoError(t, err)
				assert.Equal(t, "png", format)
			} else {
				assert.Contains(t, recorder.Body.String(), `"error"`)
				assert.NotContains(t, recorder.Body.String(), "fixture-provider")
			}
			require.Eventually(t, func() bool {
				var user model.User
				var token model.Token
				if database.First(&user, 7).Error != nil || database.First(&token, 11).Error != nil {
					return false
				}
				return user.Quota == initial-tc.quota && token.RemainQuota == initial-tc.quota
			}, 5*time.Second, 10*time.Millisecond, "wallet/token must settle actual cost or refund full reservation")
			var user model.User
			var token model.Token
			var channel model.Channel
			require.NoError(t, database.First(&user, 7).Error)
			require.NoError(t, database.First(&token, 11).Error)
			require.NoError(t, database.First(&channel, 21).Error)
			assert.Equal(t, tc.quota, user.UsedQuota)
			assert.Equal(t, tc.quota, token.UsedQuota)
			assert.Equal(t, int64(tc.quota), channel.UsedQuota)
			var logs []model.Log
			require.NoError(t, database.Find(&logs).Error)
			require.Len(t, logs, 1, "one terminal consume/error audit per generation")
			assert.Equal(t, 21, logs[0].ChannelId)
			assert.Equal(t, 11, logs[0].TokenId)
			assert.Equal(t, tc.quota, logs[0].Quota)
			if !tc.transport {
				assert.Equal(t, "provider-image-fixture", logs[0].UpstreamRequestId)
			}
			assert.NotContains(t, logs[0].Content, "fixture-provider secret")
			other, err := common.StrToMap(logs[0].Other)
			require.NoError(t, err)
			admin, ok := other["admin_info"].(map[string]any)
			require.True(t, ok)
			if tc.unknown {
				assert.Equal(t, "unknown", admin["image_financial_outcome"])
				assert.Equal(t, "refund_without_retry", admin["image_error_policy"])
				assert.Equal(t, usagerecord.LogTypeError, logs[0].Type)
			} else if tc.status == http.StatusOK {
				assert.Contains(t, admin, "image_actual_cost_usd")
				assert.Equal(t, usagerecord.LogTypeConsume, logs[0].Type)
				assert.NotContains(t, admin, "image_financial_outcome")
			}
		})
	}
}

func TestOpenRouterGeminiNativeRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	httpclient.InitHttpClient()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMemory, previousRedis, previousBatch := common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled
	previousConsume, previousError := common.LogConsumeEnabled, constant.ErrorLogEnabled
	previousCount, previousRetries := constant.CountToken, common.RetryTimes
	previousPrices := ratio_setting.ModelPrice2JSONString()
	previousRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	previousUnit, previousTimeout := common.QuotaPerUnit, constant.StreamingTimeout
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	savedConfig := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { savedConfig[key] = value; return nil }))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = previousMemory, previousRedis, previousBatch
		common.LogConsumeEnabled, constant.ErrorLogEnabled = previousConsume, previousError
		constant.CountToken, common.RetryTimes = previousCount, previousRetries
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousRate
		common.QuotaPerUnit, constant.StreamingTimeout = previousUnit, previousTimeout
		common.SetDatabaseTypes(previousMain, previousLog)
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
	})
	common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = false, false, false
	common.LogConsumeEnabled, constant.ErrorLogEnabled = true, false
	constant.CountToken, common.RetryTimes = false, 3
	common.QuotaPerUnit, constant.StreamingTimeout = 500000, 30
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	const geminiModel = "vendor/gemini-fixture"
	const textRequest = `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":16,"temperature":0}}`
	for _, tc := range []struct {
		name, model, action, body, cost string
		upstreamStatus, status          int
		quota                           int
		stream, rejected, streamError   bool
		envelope                        *types.GeminiError
	}{
		{name: "generateContent settles terminal cost", action: ":generateContent", body: textRequest, cost: "0.02", status: 200, quota: 20000},
		{name: "streamGenerateContent settles terminal cost", action: ":streamGenerateContent?alt=sse", body: textRequest, cost: "0.02", stream: true, status: 200, quota: 20000},
		{name: "alt=sse on generateContent streams", action: ":generateContent?alt=sse", body: textRequest, cost: "0.02", stream: true, status: 200, quota: 20000},
		{name: "model id containing embed is served as chat", model: "vendor/embed-chat", action: ":generateContent", body: textRequest, cost: "0.02", status: 200, quota: 20000},
		{name: "embedded stream error yields one Gemini error frame and refunds", action: ":streamGenerateContent?alt=sse", body: textRequest, stream: true, streamError: true, status: 200},
		{name: "upstream rejection uses Gemini envelope and refunds", action: ":generateContent", body: textRequest, upstreamStatus: 429, status: 429, envelope: &types.GeminiError{Code: 429, Status: "RESOURCE_EXHAUSTED"}},
		{name: "missing terminal cost refunds with Gemini envelope", action: ":generateContent", body: textRequest, status: 502, envelope: &types.GeminiError{Code: 502, Status: "UNAVAILABLE"}},
		{name: "hosted tool rejected before reservation", action: ":generateContent", body: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"tools":[{"googleSearch":{}}]}`, rejected: true, status: 400,
			envelope: &types.GeminiError{Code: 400, Status: "INVALID_ARGUMENT", Message: `tool "googleSearch" is not supported for OpenRouter channels; only functionDeclarations are supported`}},
		{name: "media-only systemInstruction is rejected, not dropped", action: ":generateContent", body: `{"systemInstruction":{"parts":[{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]},"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, rejected: true, status: 400,
			envelope: &types.GeminiError{Code: 400, Status: "INVALID_ARGUMENT", Message: "systemInstruction non-text parts are not supported for OpenRouter channels; only text parts are supported"}},
		{name: "countTokens never reaches a paid Chat call", action: ":countTokens", body: textRequest, rejected: true, status: 400,
			envelope: &types.GeminiError{Code: 400, Status: "INVALID_ARGUMENT", Message: "OpenRouter operation lacks a validated cost reservation contract"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modelName := geminiModel
			if tc.model != "" {
				modelName = tc.model
			}
			database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := database.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			require.NoError(t, database.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.ModelCostFXRow{}))
			model.DB, model.LOG_DB = database, database
			fxseed.SeedNativeRouteFX(t, database)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_mode": `{}`, "billing_setting.billing_expr": `{}`,
				"group_ratio_setting.group_ratio": `{"default":2}`, "group_ratio_setting.group_group_ratio": `{}`,
			}))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
			const initial = 1000000
			require.NoError(t, database.Create(&model.User{Id: 7, Username: "gemini-owner", Group: "default", Quota: initial}).Error)
			require.NoError(t, database.Create(&model.Token{Id: 11, UserId: 7, Key: "fixture-token", Name: "gemini-token", RemainQuota: initial}).Error)
			var gets, posts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer fixture-provider", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					gets.Add(1)
					assert.Equal(t, "/v1/models/"+modelName+"/endpoints", r.URL.Path)
					_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"endpoints":[{"tag":"fixture","context_length":128000,"max_completion_tokens":128,"pricing":{"prompt":"0.000001","completion":"0.000002"}}]}}`, modelName)
					return
				}
				posts.Add(1)
				assert.Equal(t, "/v1/chat/completions", r.URL.Path, "Gemini native is served by the priced OpenRouter Chat endpoint")
				var payload map[string]any
				assert.NoError(t, common.DecodeJson(r.Body, &payload))
				assert.Equal(t, modelName, payload["model"])
				assert.Equal(t, []any{map[string]any{"role": "user", "content": "hi"}}, payload["messages"])
				assert.Equal(t, float64(16), payload["max_tokens"])
				assert.Equal(t, float64(0), payload["temperature"], "explicit zero survives conversion")
				assert.Equal(t, map[string]any{"include": true}, payload["usage"])
				assert.Equal(t, tc.stream, payload["stream"])
				var user model.User
				assert.NoError(t, database.First(&user, 7).Error)
				assert.Less(t, user.Quota, initial, "wallet reserved before provider dispatch")
				if tc.upstreamStatus != 0 {
					w.WriteHeader(tc.upstreamStatus)
					_, _ = io.WriteString(w, `{"error":{"code":429,"message":"rate limited"}}`)
					return
				}
				usage := `"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5`
				if tc.cost != "" {
					usage += `,"cost":` + tc.cost
				}
				usage += `}`
				if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, `data: {"id":"gen-gemini","object":"chat.completion.chunk","created":1,"model":"vendor/gemini-fixture","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`+"\n\n")
					if tc.streamError {
						_, _ = io.WriteString(w, `data: {"id":"gen-gemini","choices":[],"error":{"code":502,"message":"Upstream error from Nvidia"}}`+"\n\n")
						return
					}
					_, _ = io.WriteString(w, `data: {"id":"gen-gemini","object":"chat.completion.chunk","created":1,"model":"vendor/gemini-fixture","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
					_, _ = io.WriteString(w, `data: {"id":"gen-gemini","object":"chat.completion.chunk","created":1,"model":"vendor/gemini-fixture","choices":[],`+usage+`}`+"\n\ndata: [DONE]\n\n")
					return
				}
				_, _ = io.WriteString(w, `{"id":"gen-gemini","object":"chat.completion","created":1,"model":"vendor/gemini-fixture","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],`+usage+`}`)
			}))
			t.Cleanup(upstream.Close)
			selected := model.Channel{Id: 21, Type: constant.ChannelTypeOpenRouter, Name: "fixture-gemini", Key: "fixture-provider", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, Models: modelName, Group: "default"}
			require.NoError(t, database.Create(&selected).Error)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/"+modelName+tc.action, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, common.RequestIdKey, "gemini-relay")
			common.SetContextKey(c, constant.ContextKeyUserId, 7)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUserQuota, initial)
			common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
			common.SetContextKey(c, constant.ContextKeyTokenId, 11)
			common.SetContextKey(c, constant.ContextKeyTokenKey, "fixture-token")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, modelName)
			c.Set("username", "gemini-owner")
			c.Set("token_name", "gemini-token")
			require.Nil(t, middleware.SetupContextForSelectedChannel(c, &selected, modelName))

			Relay(c, types.RelayFormatGemini)

			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			if tc.rejected {
				assert.Zero(t, posts.Load())
				assert.Zero(t, gets.Load(), "rejection precedes discovery and reservation")
			} else {
				assert.Equal(t, int32(1), posts.Load(), "never replay a paid generation")
			}
			switch {
			case tc.streamError:
				frames := recorder.Body.String()
				assert.Contains(t, frames, `"text":"hello"`)
				assert.Equal(t, 1, strings.Count(frames, `"error"`), "exactly one error frame: %s", frames)
				errorFrame := frames[strings.Index(frames, `data: {"error":`):]
				var body struct {
					Error types.GeminiError `json:"error"`
				}
				require.NoError(t, common.UnmarshalJsonStr(strings.TrimSpace(strings.TrimPrefix(errorFrame, "data: ")), &body), errorFrame)
				assert.Equal(t, 502, body.Error.Code)
				assert.Equal(t, "UNAVAILABLE", body.Error.Status)
				assert.Contains(t, body.Error.Message, "Upstream error from Nvidia")
				assert.NotContains(t, frames, "usage.cost")
			case tc.envelope != nil:
				var body struct {
					Error types.GeminiError `json:"error"`
				}
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body), recorder.Body.String())
				assert.Equal(t, tc.envelope.Code, body.Error.Code)
				assert.Equal(t, tc.envelope.Status, body.Error.Status)
				assert.Contains(t, body.Error.Message, tc.envelope.Message)
				assert.NotContains(t, recorder.Body.String(), "fixture-provider")
			case tc.stream:
				frames := recorder.Body.String()
				assert.Contains(t, frames, `"text":"hello"`)
				assert.Contains(t, frames, `"usageMetadata":{"promptTokenCount":3,"toolUsePromptTokenCount":0,"candidatesTokenCount":2,"totalTokenCount":5`, "terminal usage frame")
			default:
				var response dto.GeminiChatResponse
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
				require.Len(t, response.Candidates, 1)
				require.Len(t, response.Candidates[0].Content.Parts, 1)
				assert.Equal(t, "hello", response.Candidates[0].Content.Parts[0].Text)
				assert.Equal(t, 3, response.UsageMetadata.PromptTokenCount)
				assert.Equal(t, 2, response.UsageMetadata.CandidatesTokenCount)
				assert.Equal(t, 5, response.UsageMetadata.TotalTokenCount)
			}
			require.Eventually(t, func() bool {
				var user model.User
				var token model.Token
				if database.First(&user, 7).Error != nil || database.First(&token, 11).Error != nil {
					return false
				}
				return user.Quota == initial-tc.quota && token.RemainQuota == initial-tc.quota
			}, 5*time.Second, 10*time.Millisecond, "wallet/token settle the terminal usage.cost or refund the whole reservation")
		})
	}
}

// An OpenRouter reserve the wallet or the API key does not cover answers every
// client format as the pre-consume shortfall of any other channel does: 403
// with the billing code and message, before any paid call and without retry.
// A genuine pricing failure stays 400 model_price_error.
func TestOpenRouterReserveShortfallAnswersLikeOtherChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	httpclient.InitHttpClient()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMemory, previousRedis, previousBatch := common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled
	previousConsume, previousError := common.LogConsumeEnabled, constant.ErrorLogEnabled
	previousCount, previousRetries := constant.CountToken, common.RetryTimes
	previousPrices := ratio_setting.ModelPrice2JSONString()
	general := operation_setting.GetGeneralSetting()
	previousRate, previousDisplay := general.CustomCurrencyExchangeRate, general.QuotaDisplayType
	previousUnit := common.QuotaPerUnit
	previousMain, previousLog, previousKey := common.MainDatabaseType(), common.LogDatabaseType(), model.KeyColumn
	savedConfig := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { savedConfig[key] = value; return nil }))
	t.Cleanup(func() {
		model.DB, model.LOG_DB, model.KeyColumn = previousDB, previousLogDB, previousKey
		common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = previousMemory, previousRedis, previousBatch
		common.LogConsumeEnabled, constant.ErrorLogEnabled = previousConsume, previousError
		constant.CountToken, common.RetryTimes = previousCount, previousRetries
		general.CustomCurrencyExchangeRate, general.QuotaDisplayType = previousRate, previousDisplay
		common.QuotaPerUnit = previousUnit
		common.SetDatabaseTypes(previousMain, previousLog)
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
	})
	common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = false, false, false
	common.LogConsumeEnabled, constant.ErrorLogEnabled = true, false
	constant.CountToken, common.RetryTimes = false, 3
	common.QuotaPerUnit = 500000
	general.CustomCurrencyExchangeRate, general.QuotaDisplayType = 100, operation_setting.QuotaDisplayTypeTokens
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	model.KeyColumn = "`key`"
	const textModel = "vendor/text-fixture"
	const imageModel = "black-forest-labs/flux.2-pro"
	const funded = 1000000
	type clientFormat struct {
		format      types.RelayFormat
		path, body  string
		model       string
		channelType int
	}
	chat := clientFormat{types.RelayFormatOpenAI, "/v1/chat/completions", `{"model":"vendor/text-fixture","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`, textModel, constant.ChannelTypeOpenRouter}
	responses := clientFormat{types.RelayFormatOpenAIResponses, "/v1/responses", `{"model":"vendor/text-fixture","input":"hi","max_output_tokens":16}`, textModel, constant.ChannelTypeOpenRouter}
	messages := clientFormat{types.RelayFormatClaude, "/v1/messages", `{"model":"vendor/text-fixture","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, textModel, constant.ChannelTypeOpenRouter}
	gemini := clientFormat{types.RelayFormatGemini, "/v1beta/models/vendor/text-fixture:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":16}}`, textModel, constant.ChannelTypeOpenRouter}
	images := clientFormat{types.RelayFormatOpenAIImage, "/v1/images/generations", `{"model":"black-forest-labs/flux.2-pro","prompt":"a red square"}`, imageModel, constant.ChannelTypeOpenRouter}
	// The same chat request on a fixed-price OpenAI channel is the reference
	// pre-consume shortfall that OpenRouter must match.
	otherChannel := clientFormat{types.RelayFormatOpenAI, "/v1/chat/completions", chat.body, textModel, constant.ChannelTypeOpenAI}
	for _, tc := range []struct {
		name           string
		client         clientFormat
		walletQuota    int
		tokenQuota     int
		metadataStatus int
		status         int
		body           string
	}{
		{name: "chat/wallet", client: chat, walletQuota: 5, tokenQuota: funded, status: 403,
			body: `{"error":{"message":"Insufficient balance: the request needs 70, the balance is 5 (request id: shortfall)","type":"new_api_error","param":"","code":"insufficient_user_quota"}}`},
		{name: "chat/token", client: chat, walletQuota: funded, tokenQuota: 5, status: 403,
			body: `{"error":{"message":"Insufficient API key spending limit: the request needs 70, the API key has 5 left (request id: shortfall)","type":"new_api_error","param":"","code":"pre_consume_token_quota_failed"}}`},
		{name: "responses/wallet", client: responses, walletQuota: 5, tokenQuota: funded, status: 403,
			body: `{"error":{"message":"Insufficient balance: the request needs 54, the balance is 5 (request id: shortfall)","type":"new_api_error","param":"","code":"insufficient_user_quota"}}`},
		{name: "responses/token", client: responses, walletQuota: funded, tokenQuota: 5, status: 403,
			body: `{"error":{"message":"Insufficient API key spending limit: the request needs 54, the API key has 5 left (request id: shortfall)","type":"new_api_error","param":"","code":"pre_consume_token_quota_failed"}}`},
		{name: "messages/wallet", client: messages, walletQuota: 5, tokenQuota: funded, status: 403,
			body: `{"type":"error","error":{"type":"new_api_error","message":"Insufficient balance: the request needs 70, the balance is 5 (request id: shortfall)"}}`},
		{name: "messages/token", client: messages, walletQuota: funded, tokenQuota: 5, status: 403,
			body: `{"type":"error","error":{"type":"new_api_error","message":"Insufficient API key spending limit: the request needs 70, the API key has 5 left (request id: shortfall)"}}`},
		{name: "gemini/wallet", client: gemini, walletQuota: 5, tokenQuota: funded, status: 403,
			body: `{"error":{"code":403,"message":"Insufficient balance: the request needs 74, the balance is 5 (request id: shortfall)","status":"PERMISSION_DENIED"}}`},
		{name: "gemini/token", client: gemini, walletQuota: funded, tokenQuota: 5, status: 403,
			body: `{"error":{"code":403,"message":"Insufficient API key spending limit: the request needs 74, the API key has 5 left (request id: shortfall)","status":"PERMISSION_DENIED"}}`},
		{name: "images/wallet", client: images, walletQuota: 5, tokenQuota: funded, status: 403,
			body: `{"error":{"message":"Failed to reserve quota, remaining user quota: 5, required quota: 40000 (request id: shortfall)","type":"new_api_error","param":"","code":"insufficient_user_quota"}}`},
		{name: "images/token", client: images, walletQuota: funded, tokenQuota: 5, status: 403,
			body: `{"error":{"message":"token quota is not enough, token remain quota: 5, need quota: 40000 (request id: shortfall)","type":"new_api_error","param":"","code":"pre_consume_token_quota_failed"}}`},
		{name: "other channel/wallet", client: otherChannel, walletQuota: 5, tokenQuota: funded, status: 403,
			body: `{"error":{"message":"Failed to reserve quota, remaining user quota: 5, required quota: 30000 (request id: shortfall)","type":"new_api_error","param":"","code":"insufficient_user_quota"}}`},
		{name: "other channel/token", client: otherChannel, walletQuota: funded, tokenQuota: 5, status: 403,
			body: `{"error":{"message":"token quota is not enough, token remain quota: 5, need quota: 30000 (request id: shortfall)","type":"new_api_error","param":"","code":"pre_consume_token_quota_failed"}}`},
		{name: "chat/pricing error", client: chat, walletQuota: funded, tokenQuota: funded, metadataStatus: 404, status: 400,
			body: `{"error":{"message":"OpenRouter pricing discovery HTTP 404 (request id: shortfall)","type":"new_api_error","param":"","code":"model_price_error"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := database.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			require.NoError(t, database.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.ModelCostFXRow{}))
			model.DB, model.LOG_DB = database, database
			fxseed.SeedNativeRouteFX(t, database)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_mode": `{}`, "billing_setting.billing_expr": `{}`,
				"group_ratio_setting.group_ratio": `{"default":2}`, "group_ratio_setting.group_group_ratio": `{}`,
			}))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"vendor/text-fixture":0.03}`))
			require.NoError(t, database.Create(&model.User{Id: 7, Username: "shortfall-owner", Group: "default", Quota: tc.walletQuota}).Error)
			require.NoError(t, database.Create(&model.Token{Id: 11, UserId: 7, Key: "fixture-token", Name: "shortfall-token", RemainQuota: tc.tokenQuota}).Error)
			var posts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodGet {
					posts.Add(1)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if tc.metadataStatus != 0 {
					w.WriteHeader(tc.metadataStatus)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/v1/images/") {
					_, _ = fmt.Fprintf(w, `{"id":%q,"endpoints":[{"provider_tag":"fixture","supported_parameters":{"n":{"type":"range","min":1,"max":4}},"pricing":[{"billable":"output_image","unit":"image","cost_usd":0.04}]}]}`, imageModel)
					return
				}
				_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"endpoints":[{"tag":"fixture","context_length":128000,"max_completion_tokens":128,"pricing":{"prompt":"0.000001","completion":"0.000002"}}]}}`, textModel)
			}))
			t.Cleanup(upstream.Close)
			selected := model.Channel{Id: 21, Type: tc.client.channelType, Name: "fixture-channel", Key: "fixture-provider", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, Models: tc.client.model, Group: "default"}
			require.NoError(t, database.Create(&selected).Error)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tc.client.path, strings.NewReader(tc.client.body))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, common.RequestIdKey, "shortfall")
			common.SetContextKey(c, constant.ContextKeyUserId, 7)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUserQuota, tc.walletQuota)
			common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
			common.SetContextKey(c, constant.ContextKeyTokenId, 11)
			common.SetContextKey(c, constant.ContextKeyTokenKey, "fixture-token")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, tc.client.model)
			c.Set("username", "shortfall-owner")
			c.Set("token_name", "shortfall-token")
			require.Nil(t, middleware.SetupContextForSelectedChannel(c, &selected, tc.client.model))

			Relay(c, tc.client.format)

			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			assert.JSONEq(t, tc.body, recorder.Body.String())
			assert.Zero(t, posts.Load(), "the refusal precedes the paid call")
			assert.LessOrEqual(t, len(c.GetStringSlice("use_channel")), 1, "a refusal is not retried on another channel")
			var user model.User
			var token model.Token
			require.NoError(t, database.First(&user, 7).Error)
			require.NoError(t, database.First(&token, 11).Error)
			assert.Equal(t, tc.walletQuota, user.Quota, "a refusal holds nothing")
			assert.Equal(t, tc.tokenQuota, token.RemainQuota, "a refusal holds nothing")
		})
	}
}

// relayErrorLog collects error-level log lines written while a relay runs.
type relayErrorLog struct {
	mu    sync.Mutex
	lines bytes.Buffer
}

func (l *relayErrorLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lines.Write(p)
}

func (l *relayErrorLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lines.String()
}

// abandonedStreamBody is an upstream stream whose client disconnects before
// any content arrives: the first read closes the client request, and the read
// returns only when the relay closes the body.
type abandonedStreamBody struct {
	disconnectClient context.CancelFunc
	closed           chan struct{}
	closeOnce        sync.Once
}

func (b *abandonedStreamBody) Read([]byte) (int, error) {
	b.disconnectClient()
	<-b.closed
	return 0, io.EOF
}

func (b *abandonedStreamBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

type openRouterFixtureTransport func(*http.Request) (*http.Response, error)

func (f openRouterFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A client abort and a failure new-api decides itself never count against the
// OpenRouter channel: no channel error is logged or recorded and automatic
// disabling never sees them, even with their status in the disable ranges.
// An abort refunds the reservation and, on any channel, is not retried. Real
// upstream failures still count, even with a code new-api also uses locally.
func TestOpenRouterLocalOutcomesAreNotChannelErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	httpclient.InitHttpClient()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousConsume, previousError := common.LogConsumeEnabled, constant.ErrorLogEnabled
	previousCount, previousRetries := constant.CountToken, common.RetryTimes
	previousAutoDisable, previousDisableRanges := common.AutomaticDisableChannelEnabled, operation_setting.AutomaticDisableStatusCodesToString()
	previousPrices := ratio_setting.ModelPrice2JSONString()
	general := operation_setting.GetGeneralSetting()
	previousRate, previousDisplay := general.CustomCurrencyExchangeRate, general.QuotaDisplayType
	previousUnit, previousTimeout := common.QuotaPerUnit, constant.StreamingTimeout
	previousMain, previousLog, previousKey, previousGroup := common.MainDatabaseType(), common.LogDatabaseType(), model.KeyColumn, model.GroupColumn
	// Only the keys the cases load are restored: a finished relay may still read
	// the rest from a background goroutine.
	changedConfig := []string{"billing_setting.billing_mode", "billing_setting.billing_expr", "group_ratio_setting.group_ratio", "group_ratio_setting.group_group_ratio"}
	savedConfig := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if slices.Contains(changedConfig, key) {
			savedConfig[key] = value
		}
		return nil
	}))
	client, err := httpclient.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
	require.NoError(t, err)
	previousTransport := client.Transport
	common.LogWriterMu.RLock()
	previousErrorWriter := gin.DefaultErrorWriter
	common.LogWriterMu.RUnlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousErrorWriter
		common.LogWriterMu.Unlock()
		client.Transport = previousTransport
		model.DB, model.LOG_DB, model.KeyColumn, model.GroupColumn = previousDB, previousLogDB, previousKey, previousGroup
		common.LogConsumeEnabled, constant.ErrorLogEnabled = previousConsume, previousError
		constant.CountToken, common.RetryTimes = previousCount, previousRetries
		common.AutomaticDisableChannelEnabled = previousAutoDisable
		require.NoError(t, operation_setting.AutomaticDisableStatusCodesFromString(previousDisableRanges))
		general.CustomCurrencyExchangeRate, general.QuotaDisplayType = previousRate, previousDisplay
		common.QuotaPerUnit, constant.StreamingTimeout = previousUnit, previousTimeout
		common.SetDatabaseTypes(previousMain, previousLog)
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
	})
	for _, cache := range []*bool{&common.MemoryCacheEnabled, &common.RedisEnabled, &common.BatchUpdateEnabled} {
		if *cache {
			*cache = false
			t.Cleanup(func() { *cache = true })
		}
	}
	// A finished relay records its sample in a background goroutine, which must
	// end before the globals it reads are restored. The notifier of automatic
	// disabling keeps one worker for good, so it starts before the baseline.
	notificationsadapter.MemoryCounter{}.Check(0, "relay-outcome-baseline")
	idleWorkers := gopool.WorkerCount()
	t.Cleanup(func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() <= idleWorkers }, 5*time.Second, 10*time.Millisecond)
	})
	common.LogConsumeEnabled, constant.ErrorLogEnabled = true, true
	constant.CountToken, common.RetryTimes = false, 3
	common.AutomaticDisableChannelEnabled = true
	require.NoError(t, operation_setting.AutomaticDisableStatusCodesFromString("401,403,502"))
	common.QuotaPerUnit, constant.StreamingTimeout = 500000, 30
	general.CustomCurrencyExchangeRate, general.QuotaDisplayType = 100, operation_setting.QuotaDisplayTypeTokens
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	model.KeyColumn, model.GroupColumn = "`key`", "`group`"
	const textModel = "vendor/text-fixture"
	const funded = 1000000
	const channelError = "channel error (channel #"
	type clientFormat struct {
		format     types.RelayFormat
		path, body string
	}
	chat := clientFormat{types.RelayFormatOpenAI, "/v1/chat/completions", `{"model":"vendor/text-fixture","messages":[{"role":"user","content":"hi"}],"max_tokens":16,"stream":true}`}
	messages := clientFormat{types.RelayFormatClaude, "/v1/messages", `{"model":"vendor/text-fixture","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`}
	responses := clientFormat{types.RelayFormatOpenAIResponses, "/v1/responses", `{"model":"vendor/text-fixture","input":"hi","max_output_tokens":16,"stream":true}`}
	gemini := clientFormat{types.RelayFormatGemini, "/v1beta/models/vendor/text-fixture:streamGenerateContent?alt=sse", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":16}}`}
	unsupportedTool := clientFormat{types.RelayFormatGemini, gemini.path, `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"tools":[{"googleSearch":{}}]}`}
	nonstreamChat := clientFormat{types.RelayFormatOpenAI, "/v1/chat/completions", `{"model":"vendor/text-fixture","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`}
	for _, tc := range []struct {
		name                    string
		client                  clientFormat
		walletQuota, tokenQuota int
		// channelType defaults to OpenRouter; another type is priced by modelPrice
		// and has a second channel in the group to retry on.
		channelType int
		modelPrice  string
		// abort disconnects the client before the upstream stream carries content;
		// abortBeforeHeaders disconnects it while the paid call awaits headers.
		abort, abortBeforeHeaders bool
		// upstreamStatus answers the paid call with upstreamCode, the status by
		// default; discoveryStatus answers the pricing lookup.
		upstreamStatus, discoveryStatus int
		upstreamCode                    string
		// groupRatio overrides the default group ratio 2.
		groupRatio string
		// status and body are what a connected client receives.
		status       int
		body         string
		channelError bool
		autoDisabled bool
	}{
		{name: "chat/client abort before content", client: chat, walletQuota: funded, tokenQuota: funded, abort: true},
		{name: "messages/client abort before content", client: messages, walletQuota: funded, tokenQuota: funded, abort: true},
		{name: "responses/client abort before content", client: responses, walletQuota: funded, tokenQuota: funded, abort: true},
		{name: "gemini/client abort before content", client: gemini, walletQuota: funded, tokenQuota: funded, abort: true},
		{name: "other channel/client abort is not retried", client: nonstreamChat, channelType: constant.ChannelTypeOpenAI, modelPrice: `{"vendor/text-fixture":0.03}`, walletQuota: funded, tokenQuota: funded, abortBeforeHeaders: true},
		{name: "wallet reserve shortfall", client: chat, walletQuota: 5, tokenQuota: funded, status: http.StatusForbidden},
		{name: "API key reserve shortfall", client: chat, walletQuota: funded, tokenQuota: 5, status: http.StatusForbidden},
		{name: "saturated reserve", client: chat, walletQuota: funded, tokenQuota: funded, groupRatio: "1e15", status: http.StatusForbidden,
			body: `{"error":{"message":"insufficient quota","type":"new_api_error","param":"","code":"insufficient_user_quota"}}`},
		{name: "request conversion refusal", client: unsupportedTool, walletQuota: funded, tokenQuota: funded, status: http.StatusBadRequest},
		{name: "upstream 401", client: chat, walletQuota: funded, tokenQuota: funded, upstreamStatus: http.StatusUnauthorized, status: http.StatusUnauthorized, channelError: true, autoDisabled: true},
		{name: "upstream 403", client: chat, walletQuota: funded, tokenQuota: funded, upstreamStatus: http.StatusForbidden, status: http.StatusForbidden, channelError: true, autoDisabled: true},
		{name: "upstream 502", client: chat, walletQuota: funded, tokenQuota: funded, upstreamStatus: http.StatusBadGateway, status: http.StatusBadGateway, channelError: true, autoDisabled: true},
		{name: "upstream 400 with a local error code", client: chat, walletQuota: funded, tokenQuota: funded, upstreamStatus: http.StatusBadRequest, upstreamCode: `"invalid_request"`, status: http.StatusBadRequest, channelError: true},
		{name: "pricing discovery 401", client: chat, walletQuota: funded, tokenQuota: funded, discoveryStatus: http.StatusUnauthorized, status: http.StatusBadRequest, channelError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := database.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			require.NoError(t, database.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Log{}, &model.ModelCostFXRow{}))
			model.DB, model.LOG_DB = database, database
			fxseed.SeedNativeRouteFX(t, database)
			groupRatio := "2"
			if tc.groupRatio != "" {
				groupRatio = tc.groupRatio
			}
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_mode": `{}`, "billing_setting.billing_expr": `{}`,
				"group_ratio_setting.group_ratio": `{"default":` + groupRatio + `}`, "group_ratio_setting.group_group_ratio": `{}`,
			}))
			modelPrice, channelType := `{}`, constant.ChannelTypeOpenRouter
			if tc.channelType != 0 {
				modelPrice, channelType = tc.modelPrice, tc.channelType
			}
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(modelPrice))
			require.NoError(t, database.Create(&model.User{Id: 7, Username: "outcome-owner", Group: "default", Quota: tc.walletQuota}).Error)
			require.NoError(t, database.Create(&model.Token{Id: 11, UserId: 7, Key: "fixture-token", Name: "outcome-token", RemainQuota: tc.tokenQuota}).Error)
			ctx, disconnectClient := context.WithCancel(context.Background())
			t.Cleanup(disconnectClient)
			var paidCalls atomic.Int32
			client.Transport = openRouterFixtureTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPost {
					paidCalls.Add(1)
				}
				if tc.abortBeforeHeaders {
					disconnectClient()
					return nil, context.Canceled
				}
				upstreamCode := strconv.Itoa(tc.upstreamStatus)
				if tc.upstreamCode != "" {
					upstreamCode = tc.upstreamCode
				}
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Request: r}
				switch {
				case r.Method == http.MethodGet && tc.discoveryStatus != 0:
					response.StatusCode = tc.discoveryStatus
					response.Body = io.NopCloser(strings.NewReader(`{"error":{"code":401,"message":"No auth credentials found"}}`))
				case r.Method == http.MethodGet:
					response.Body = io.NopCloser(strings.NewReader(`{"data":{"id":"vendor/text-fixture","endpoints":[{"tag":"fixture","context_length":128000,"max_completion_tokens":128,"pricing":{"prompt":"0.000001","completion":"0.000002"}}]}}`))
				case tc.abort:
					response.Header.Set("Content-Type", "text/event-stream")
					response.Body = &abandonedStreamBody{disconnectClient: disconnectClient, closed: make(chan struct{})}
				default:
					response.StatusCode = tc.upstreamStatus
					response.Body = io.NopCloser(strings.NewReader(fmt.Sprintf(`{"error":{"code":%s,"message":"upstream refused the request"}}`, upstreamCode)))
				}
				return response, nil
			})
			errorLog := &relayErrorLog{}
			common.LogWriterMu.Lock()
			gin.DefaultErrorWriter = errorLog
			common.LogWriterMu.Unlock()
			autoBan := 1
			baseURL := "https://openrouter.fixture"
			selected := model.Channel{Id: 21, Type: channelType, Name: "fixture-channel", Key: "fixture-provider", BaseURL: &baseURL, Status: common.ChannelStatusEnabled, Models: textModel, Group: "default", AutoBan: &autoBan}
			require.NoError(t, database.Create(&selected).Error)
			if tc.channelType != 0 {
				fallback := model.Channel{Id: 22, Type: channelType, Name: "fallback-channel", Key: "fixture-provider", BaseURL: &baseURL, Status: common.ChannelStatusEnabled, Models: textModel, Group: "default", AutoBan: &autoBan}
				require.NoError(t, database.Create(&fallback).Error)
				for _, id := range []int{selected.Id, fallback.Id} {
					require.NoError(t, database.Create(&model.Ability{Group: "default", Model: textModel, ChannelId: id, Enabled: true}).Error)
				}
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, tc.client.path, strings.NewReader(tc.client.body))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, common.RequestIdKey, "outcome")
			common.SetContextKey(c, constant.ContextKeyUserId, 7)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUserQuota, tc.walletQuota)
			common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
			common.SetContextKey(c, constant.ContextKeyTokenId, 11)
			common.SetContextKey(c, constant.ContextKeyTokenKey, "fixture-token")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, textModel)
			c.Set("username", "outcome-owner")
			c.Set("token_name", "outcome-token")
			require.Nil(t, middleware.SetupContextForSelectedChannel(c, &selected, textModel))

			Relay(c, tc.client.format)

			if tc.status != 0 {
				require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			}
			if tc.body != "" {
				assert.JSONEq(t, tc.body, recorder.Body.String())
			}
			if tc.abortBeforeHeaders {
				assert.Equal(t, int32(1), paidCalls.Load(), "a client that left is not retried on another channel")
			}
			require.Eventually(t, func() bool {
				var user model.User
				var token model.Token
				if database.First(&user, 7).Error != nil || database.First(&token, 11).Error != nil {
					return false
				}
				return user.Quota == tc.walletQuota && token.RemainQuota == tc.tokenQuota
			}, 5*time.Second, 10*time.Millisecond, "a failed request charges nothing and returns any reservation")
			var errorLogs int64
			require.NoError(t, database.Model(&model.Log{}).Where("type = ?", usagerecord.LogTypeError).Count(&errorLogs).Error)
			assert.Equal(t, tc.channelError, strings.Contains(errorLog.String(), channelError), errorLog.String())
			if !tc.channelError {
				assert.Zero(t, errorLogs, "only a channel failure records an error log")
			}
			if tc.autoDisabled {
				require.Eventually(t, func() bool {
					var channel model.Channel
					return database.First(&channel, selected.Id).Error == nil && channel.Status == common.ChannelStatusAutoDisabled
				}, 5*time.Second, 10*time.Millisecond, "a real upstream failure feeds automatic disabling")
				return
			}
			var channel model.Channel
			require.NoError(t, database.First(&channel, selected.Id).Error)
			assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
		})
	}
}
