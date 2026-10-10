package controller

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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

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

	processChannelError(ctx, channelSnapshot, apiErr, nil)

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

	logs, total, err := model.GetUserLogs(7, model.LogTypeError, 0, 0, "", "", 0, 10, "", "", "")
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
	seedNativeRouteFX(t, database)
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

		current, err := model.CurrentModelCostFX("cbr-xml-daily.ru")
		require.NoError(t, err)
		require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{
			Source: current.Source, EffectiveAt: current.EffectiveAt + 1, FetchedAt: current.FetchedAt + 1,
			Rates: map[string]float64{"EUR": 1},
		}, current.Version))
		require.NoError(t, model.LoadModelCostFX(context.Background(), "cbr-xml-daily.ru"))
		t.Cleanup(func() { seedNativeRouteFX(t, database) })

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
	service.InitHttpClient()
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
		{name: "administrator expression", cost: "0.02", expression: `tier("admin", provider_cost * 2000000)`, status: 200, quota: 40000, reserved: 80000},
		{name: "administrator fixed image price", cost: "0.02", legacy: true, status: 200, quota: 30000, reserved: 30000},
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
			seedNativeRouteFX(t, database)
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
						metadata = fmt.Sprintf(`{"id":%q,"endpoints":[{"provider_tag":"fixture","supported_parameters":{"n":{"type":"range","min":1,"max":4}},"pricing":[{"billable":"output_image","unit":"image","cost_usd":0.04}]}]}`, imageModel)
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
				assert.Equal(t, model.LogTypeError, logs[0].Type)
			} else if tc.status == http.StatusOK {
				assert.Contains(t, admin, "image_actual_cost_usd")
				assert.Equal(t, model.LogTypeConsume, logs[0].Type)
				assert.NotContains(t, admin, "image_financial_outcome")
			}
		})
	}
}
