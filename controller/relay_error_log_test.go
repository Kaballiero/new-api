package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
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
