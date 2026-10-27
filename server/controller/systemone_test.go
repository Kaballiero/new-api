package controller

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
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSystemOneRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	httpclient.InitHttpClient()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMemory, previousRedis, previousBatch := common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled
	previousConsume, previousError := common.LogConsumeEnabled, constant.ErrorLogEnabled
	previousCount, previousRetries := constant.CountToken, common.RetryTimes
	previousPrices := ratio_setting.ModelPrice2JSONString()
	previousRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	previousUnit := common.QuotaPerUnit
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	savedConfig := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(k, v string) error { savedConfig[k] = v; return nil }))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = previousMemory, previousRedis, previousBatch
		common.LogConsumeEnabled, constant.ErrorLogEnabled = previousConsume, previousError
		constant.CountToken, common.RetryTimes = previousCount, previousRetries
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousRate
		common.QuotaPerUnit = previousUnit
		common.SetDatabaseTypes(previousMain, previousLog)
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
	})
	common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = false, false, false
	common.LogConsumeEnabled, constant.ErrorLogEnabled = true, false
	constant.CountToken, common.RetryTimes = false, 3
	common.QuotaPerUnit = 500000
	var dialector gorm.Dialector = sqlite.Open(":memory:")
	dialect := common.DatabaseTypeSQLite
	switch os.Getenv("DECISIONS_TEST_DIALECT") {
	case "mysql":
		require.NotEmpty(t, os.Getenv("TEST_MYSQL_DSN"))
		dialector = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
		dialect = common.DatabaseTypeMySQL
	case "postgres":
		require.NotEmpty(t, os.Getenv("TEST_POSTGRES_DSN"))
		dialector = postgres.Open(os.Getenv("TEST_POSTGRES_DSN"))
		dialect = common.DatabaseTypePostgreSQL
	case "", "sqlite":
	default:
		t.Fatal("unknown DECISIONS_TEST_DIALECT")
	}
	common.SetDatabaseTypes(dialect, dialect)
	database, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.ModelCostFXRow{}))
	model.DB, model.LOG_DB = database, database
	const upstreamModel = "vendor/decision-fixture"
	const requestBody = `{"model":"alias","state":{"z":1,"a":{"y":2,"b":3}},"questions":{"z":{"type":"choice","instructions":"route","criteria":{"z":"Z","a":"A"}},"a":{"type":"noul","instructions":"yes?"},"s":{"type":"score","instructions":"rank","criteria":[{"level":"low"},{"level":"high"}]}},"provider":{"only":["fixture"]}}`
	const answers = `"answers":{"z":{"type":"choice","choice":"z","probabilities":{"z":0.8,"a":0.2},"confidence":0.8},"a":{"type":"noul","noul":0.9},"s":{"type":"score","score":0.8,"legend":{"0":{"level":"low"},"1":{"level":"high"}}}}`
	for _, tc := range []struct {
		name, cost, capability, provider                                                                                string
		upstreamStatus, status, quota                                                                                   int
		missing, malformed, disconnect, timeout, noReserve, empty, unsupported, duplicate, override, frozen, configured bool
		nullProbability, oversizedUsage, missingID, mismatchID, metadata                                                bool
	}{
		{name: "typed result and raw order", cost: "0.02", status: 200, quota: 20000},
		{name: "OR metadata with required parameters", metadata: true, cost: "0.02", status: 200, quota: 20000},
		{name: "explicit zero", cost: "0", status: 200},
		{name: "missing cost", missing: true, status: 502},
		{name: "null cost", cost: "null", status: 502},
		{name: "negative cost", cost: "-1", status: 502},
		{name: "known cost broken answers", cost: "0.02", malformed: true, status: 502, quota: 20000},
		{name: "null probability known cost", nullProbability: true, cost: "0.02", status: 502, quota: 20000},
		{name: "oversized usage known cost", oversizedUsage: true, cost: "0.02", status: 502, quota: 20000},
		{name: "missing discovery model identity", missingID: true, status: 400},
		{name: "mismatched discovery model identity", mismatchID: true, status: 400},
		{name: "rate limit no replay", upstreamStatus: 429, status: 429},
		{name: "server error no replay", upstreamStatus: 500, status: 500},
		{name: "307 no replay", upstreamStatus: 307, status: 307},
		{name: "308 no replay", upstreamStatus: 308, status: 308},
		{name: "disconnect no replay", disconnect: true, status: 500},
		{name: "timeout no replay", timeout: true, status: 500},
		{name: "unknown quantity no reserve", noReserve: true, status: 400},
		{name: "empty endpoints", empty: true, status: 400},
		{name: "wrong capability", capability: "text", status: 400},
		{name: "provider excluded", provider: "other", status: 400},
		{name: "non OR channel", unsupported: true, status: 400},
		{name: "duplicate root model", duplicate: true, status: 400},
		{name: "final routing override", override: true, cost: "0.02", status: 200, quota: 20000},
		{name: "frozen group and quota", frozen: true, cost: "0.02", status: 200, quota: 20000},
		{name: "configured fixed floor", configured: true, cost: "0.02", status: 200, quota: 20000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, table := range []any{&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}} {
				require.NoError(t, database.Unscoped().Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(table).Error)
			}
			operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
			common.QuotaPerUnit = 500000
			seedNativeRouteFX(t, database)
			mode, expr := "{}", "{}"
			if tc.configured {
				mode = `{"alias":"tiered_expr"}`
				expr = `{"alias":"tier(\"reserve\", fixed(0.06))"}`
			}
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": mode, "billing_setting.billing_expr": expr, "group_ratio_setting.group_ratio": `{"default":2}`, "group_ratio_setting.group_group_ratio": `{}`}))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
			const initial = 1000000
			require.NoError(t, database.Create(&model.User{Id: 7, Username: "decision-owner", Group: "default", Quota: initial}).Error)
			require.NoError(t, database.Create(&model.Token{Id: 11, UserId: 7, Key: "fixture-token", Name: "decision-token", RemainQuota: initial}).Error)
			var calls atomic.Int32
			expectedReserve := 40000
			if tc.configured {
				expectedReserve = 60000
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer fixture-provider", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "GET" {
					assert.Equal(t, "/v1/models/"+upstreamModel+"/endpoints", r.URL.Path)
					cap := tc.capability
					if cap == "" {
						cap = "decisions"
					}
					endpoints := `[{"model_id":"vendor/decision-fixture","tag":"fixture","status":0,"context_length":32000,"max_completion_tokens":1000,"pricing":{"prompt":"0.000001","completion":"0"}}]`
					if tc.empty {
						endpoints = "[]"
					}
					identity := upstreamModel
					if tc.missingID {
						identity = ""
					}
					if tc.mismatchID {
						identity = "vendor/other-model"
					}
					fmt.Fprintf(w, `{"data":{"id":%q,"architecture":{"output_modalities":[%q]},"endpoints":%s}}`, identity, cap, endpoints)
					return
				}
				calls.Add(1)
				assert.Equal(t, "/alpha/decisions", r.URL.Path)
				body, readErr := io.ReadAll(r.Body)
				assert.NoError(t, readErr)
				assert.Contains(t, string(body), `"state":{"z":1,"a":{"y":2,"b":3}}`)
				assert.Contains(t, string(body), `"criteria":{"z":"Z","a":"A"}`)
				assert.Contains(t, string(body), `"model":"vendor/decision-fixture"`)
				if tc.override {
					assert.Contains(t, string(body), `"session_id":"override-session"`)
				}
				if tc.metadata {
					assert.Contains(t, string(body), `"session_id":"session-fixture"`)
					assert.Contains(t, string(body), `"trace":{"name":"trace-fixture"}`)
					assert.Contains(t, string(body), `"user":"user-fixture"`)
					assert.Contains(t, string(body), `"require_parameters":true`)
				}
				var user model.User
				var token model.Token
				assert.NoError(t, database.First(&user, 7).Error)
				assert.NoError(t, database.First(&token, 11).Error)
				assert.Equal(t, initial-expectedReserve, user.Quota)
				assert.Equal(t, initial-expectedReserve, token.RemainQuota)
				if tc.frozen {
					common.QuotaPerUnit = 1000000
					assert.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"group_ratio_setting.group_ratio": `{"default":9}`}))
				}
				if tc.disconnect {
					conn, _, hijackErr := w.(http.Hijacker).Hijack()
					if assert.NoError(t, hijackErr) {
						_ = conn.Close()
					}
					return
				}
				if tc.timeout {
					<-r.Context().Done()
					return
				}
				if tc.upstreamStatus != 0 {
					w.Header().Set("Location", "/alpha/decisions")
					w.WriteHeader(tc.upstreamStatus)
					io.WriteString(w, `{"error":{"message":"upstream rejected"}}`)
					return
				}
				result := answers
				if tc.nullProbability {
					result = strings.Replace(result, `"z":0.8`, `"z":null`, 1)
				}
				if tc.malformed {
					result = `"answers":{}`
				}
				cost := `,"cost":` + tc.cost
				if tc.missing {
					cost = ""
				}
				input := 12
				if tc.oversizedUsage {
					input = 2147483647
				}
				fmt.Fprintf(w, `{"model":%q,%s,"usage":{"input_tokens":%d,"output_tokens":3%s}}`, upstreamModel, result, input, cost)
			}))
			defer upstream.Close()
			setting := `{"openrouter_model_reserve_usd":{"vendor/decision-fixture":0.04}}`
			if tc.noReserve {
				setting = "{}"
			}
			if tc.configured {
				setting = `{"openrouter_model_reserve_usd":{"vendor/decision-fixture":0.04},"openrouter_reservation_source":{"vendor/decision-fixture":"configured_price"}}`
			}
			mapping := `{"alias":"vendor/decision-fixture"}`
			selected := model.Channel{Id: 21, Type: constant.ChannelTypeOpenRouter, Name: "fixture", Key: "fixture-provider", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, Models: "alias", Group: "default", Setting: &setting, ModelMapping: &mapping}
			if tc.unsupported {
				selected.Type = constant.ChannelTypeOpenAI
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"alias":0.04}`))
			}
			if tc.override {
				override := `{"session_id":"override-session","provider":{"only":["fixture"]}}`
				selected.ParamOverride = &override
			}
			require.NoError(t, database.Create(&selected).Error)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			body := requestBody
			if tc.metadata {
				body = strings.Replace(body, `"provider":{"only":["fixture"]}`, `"session_id":"session-fixture","trace":{"name":"trace-fixture"},"user":"user-fixture","provider":{"only":["fixture"],"require_parameters":true}`, 1)
			}
			if tc.provider != "" {
				body = strings.Replace(body, `"fixture"`, `"`+tc.provider+`"`, 1)
			}
			if tc.duplicate {
				body = strings.Replace(body, `"model":"alias"`, `"model":"wrong","model":"alias"`, 1)
			}
			c.Request = httptest.NewRequest("POST", "/v1/systemone", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			if tc.timeout {
				client := httpclient.GetHttpClient()
				previousTimeout := client.Timeout
				client.Timeout = 50 * time.Millisecond
				defer func() { client.Timeout = previousTimeout }()
			}
			common.SetContextKey(c, constant.ContextKeyUserId, 7)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUserQuota, initial)
			common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
			common.SetContextKey(c, constant.ContextKeyTokenId, 11)
			common.SetContextKey(c, constant.ContextKeyTokenKey, "fixture-token")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "alias")
			c.Set("username", "decision-owner")
			c.Set("token_name", "decision-token")
			require.Nil(t, middleware.SetupContextForSelectedChannel(c, &selected, "alias"))
			Relay(c, types.RelayFormatSystemOne)
			assert.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			expectedCalls := int32(1)
			if tc.status == 400 {
				expectedCalls = 0
			}
			assert.Equal(t, expectedCalls, calls.Load(), "at most one paid POST")
			if tc.status == 200 {
				assert.Contains(t, recorder.Body.String(), answers)
			}
			require.Eventually(t, func() bool {
				var u model.User
				var token model.Token
				if database.First(&u, 7).Error != nil || database.First(&token, 11).Error != nil {
					return false
				}
				return u.Quota == initial-tc.quota && token.RemainQuota == initial-tc.quota
			}, time.Second*3, time.Millisecond*10)
			var u model.User
			var token model.Token
			var ch model.Channel
			require.NoError(t, database.First(&u, 7).Error)
			require.NoError(t, database.First(&token, 11).Error)
			require.NoError(t, database.First(&ch, 21).Error)
			assert.Equal(t, tc.quota, u.UsedQuota)
			assert.Equal(t, tc.quota, token.UsedQuota)
			assert.Equal(t, int64(tc.quota), ch.UsedQuota)
			var logs []model.Log
			require.NoError(t, database.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			if tc.quota > 0 || tc.status == 200 {
				require.Len(t, logs, 1)
				assert.Equal(t, tc.quota, logs[0].Quota)
				assert.Contains(t, logs[0].Other, "openrouter_cost_basis")
			} else {
				assert.Empty(t, logs)
			}
		})
	}
}
