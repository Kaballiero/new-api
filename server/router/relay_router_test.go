package router

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/contracttest/fxseed"
	"github.com/QuantumNous/new-api/model"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestListModelsSupportsOpenAIAndGeminiAuthentication(t *testing.T) {
	setupRelayRouterTestDB(t)

	user := model.User{
		Username: "models-user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		UserId:         user.Id,
		Key:            "modelstestkey",
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}).Error)

	engine := gin.New()
	SetRelayRouter(engine)

	tests := []struct {
		name           string
		path           string
		headerName     string
		expectedObject string
		expectedField  string
	}{
		{
			name:           "OpenAI bearer token",
			path:           "/v1/models",
			headerName:     "Authorization",
			expectedObject: "list",
			expectedField:  "data",
		},
		{
			name:          "Gemini API key header",
			path:          "/v1/models",
			headerName:    "x-goog-api-key",
			expectedField: "models",
		},
		{
			name:          "Gemini API key query",
			path:          "/v1/models?key=modelstestkey",
			expectedField: "models",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.headerName != "" {
				value := "modelstestkey"
				if test.headerName == "Authorization" {
					value = "Bearer " + value
				}
				request.Header.Set(test.headerName, value)
			}

			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			var payload map[string]any
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
			assert.Contains(t, payload, test.expectedField)
			assert.NotContains(t, payload, "error")
			if test.expectedObject != "" {
				assert.Equal(t, test.expectedObject, payload["object"])
			}
		})
	}
}

func setupRelayRouterTestDB(t *testing.T) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	originalIsMasterNode := common.IsMasterNode
	originalRedisEnabled := common.RedisEnabled
	originalSQLitePath := common.SQLitePath
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	common.IsMasterNode = false
	common.RedisEnabled = false
	common.SQLitePath = fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	model.LOG_DB = model.DB
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Token{}, &channelrecord.Ability{}))

	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		common.IsMasterNode = originalIsMasterNode
		common.RedisEnabled = originalRedisEnabled
		common.SQLitePath = originalSQLitePath
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})
}

func TestDecisionRoutesApplyProviderRoutingAndSettle(t *testing.T) {
	setupRelayRouterTestDB(t)
	httpclient.InitHttpClient()
	require.NoError(t, i18n.Init())
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Log{}, &model.ModelCostFXRow{}, &model.UserSubscription{}))
	previousMemory, previousBatch, previousConsume := common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	previousCount, previousRetries, previousUnit := constant.CountToken, common.RetryTimes, common.QuotaPerUnit
	previousRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	previousPrices := ratio_setting.ModelPrice2JSONString()
	savedConfig := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(k, v string) error { savedConfig[k] = v; return nil }))
	t.Cleanup(func() {
		common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousMemory, previousBatch, previousConsume
		constant.CountToken, common.RetryTimes, common.QuotaPerUnit = previousCount, previousRetries, previousUnit
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousRate
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
	})
	common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	constant.CountToken, common.RetryTimes, common.QuotaPerUnit = false, 0, 500000
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": "{}", "billing_setting.billing_expr": "{}", "group_ratio_setting.group_ratio": `{"default":2}`, "group_ratio_setting.group_group_ratio": `{}`}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	fxseed.SeedNativeRouteFX(t, model.DB)

	const upstreamModel = "vendor/decision-fixture"
	const answers = `"answers":{"a":{"type":"noul","noul":0.9}}`
	var posts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			fmt.Fprintf(w, `{"data":{"id":%q,"architecture":{"output_modalities":["decisions"]},"endpoints":[{"model_id":%q,"tag":"fixture","status":0,"pricing":{"prompt":"0.000001","completion":"0"}}]}}`, upstreamModel, upstreamModel)
			return
		}
		posts.Add(1)
		assert.Equal(t, "/alpha/decisions", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Contains(t, string(body), `"model":"vendor/decision-fixture"`)
		fmt.Fprintf(w, `{"model":%q,%s,"usage":{"input_tokens":12,"output_tokens":3,"cost":0.02}}`, upstreamModel, answers)
	}))
	defer upstream.Close()

	engine := gin.New()
	SetRelayRouter(engine)
	const initial = 1000000
	const requestBody = `{"model":"alias","state":{},"questions":{"a":{"type":"noul","instructions":"yes?"}},"provider":{"only":["fixture"]}}`
	for _, path := range []string{"/v1/decisions", "/v1/systemone"} {
		for _, tc := range []struct {
			name        string
			channelType int
			body        string
			status      int
			quota       int
			errorCode   string
		}{
			{name: "OpenRouter channel settles", channelType: constant.ChannelTypeOpenRouter, body: requestBody, status: http.StatusOK, quota: 20000},
			// Provider preferences restrict selection to OpenRouter-compatible channel types.
			{name: "provider excludes non OpenRouter channel", channelType: constant.ChannelTypeOpenAI, body: requestBody, status: http.StatusServiceUnavailable, errorCode: string(types.ErrorCodeModelNotFound)},
			{name: "unsupported provider field rejected", channelType: constant.ChannelTypeOpenRouter, body: strings.Replace(requestBody, `"only":["fixture"]`, `"bogus":true`, 1), status: http.StatusBadRequest, errorCode: "provider_routing"},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				for _, table := range []any{&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Log{}} {
					require.NoError(t, model.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(table).Error)
				}
				posts.Store(0)
				require.NoError(t, model.DB.Create(&model.User{Id: 7, Username: "decision-owner", Status: common.UserStatusEnabled, Group: "default", Quota: initial}).Error)
				require.NoError(t, model.DB.Create(&model.Token{Id: 11, UserId: 7, Key: "decisionkey", Name: "decision-token", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: initial}).Error)
				setting := `{"openrouter_model_reserve_usd":{"vendor/decision-fixture":0.04}}`
				mapping := `{"alias":"vendor/decision-fixture"}`
				channel := model.Channel{Id: 21, Type: tc.channelType, Name: "fixture", Key: "fixture-provider", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, Models: "alias", Group: "default", Setting: &setting, ModelMapping: &mapping}
				require.NoError(t, channelstore.NewChannelPersistence().Insert(&channel))

				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(tc.body))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer decisionkey")
				engine.ServeHTTP(recorder, request)

				require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
				if tc.status != http.StatusOK {
					assert.Equal(t, int32(0), posts.Load())
					var payload struct {
						Error struct {
							Code string `json:"code"`
						} `json:"error"`
					}
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
					assert.Equal(t, tc.errorCode, payload.Error.Code)
					return
				}
				assert.Equal(t, int32(1), posts.Load())
				assert.Contains(t, recorder.Body.String(), answers)
				require.Eventually(t, func() bool {
					var user model.User
					var token model.Token
					if model.DB.First(&user, 7).Error != nil || model.DB.First(&token, 11).Error != nil {
						return false
					}
					return user.Quota == initial-tc.quota && token.RemainQuota == initial-tc.quota
				}, 3*time.Second, 10*time.Millisecond)
				var logs []model.Log
				require.NoError(t, model.DB.Where("type = ?", usagerecord.LogTypeConsume).Find(&logs).Error)
				require.Len(t, logs, 1)
				assert.Equal(t, tc.quota, logs[0].Quota)
			})
		}
	}
}
