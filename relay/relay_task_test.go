package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskModel2DtoNormalizesLegacyAction(t *testing.T) {
	task := &model.Task{Action: "firstTailGenerate"}

	dtoTask := TaskModel2Dto(task)

	assert.Equal(t, constant.TaskActionFirstTailToVideo, dtoTask.Action)
	assert.Equal(t, "firstTailGenerate", task.Action)
}

const mappingOrderSubmitPlugin = `
export const meta = {apiVersion:1,key:"maporder",name:"Map Order",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl+"/submit", method:"POST", body:{upstreamModel: ctx.upstreamModel, model: ctx.model}, action:"text_to_video"};
}
export function parseSubmitResponse(){return {taskId:"1"};}
export function buildQueryRequest(){return {url:"https://provider.example"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`

const mappingOrderRewritePlugin = `
export const meta = {apiVersion:1,key:"maporder-rw",name:"Map Order RW",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl+"/submit", method:"POST", body:{upstreamModel: ctx.upstreamModel}, rewriteModel:"rewritten"};
}
export function parseSubmitResponse(){return {taskId:"1"};}
export function buildQueryRequest(){return {url:"https://provider.example"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`

func pinMappingOrderPlugin(t *testing.T, c *gin.Context, source string) {
	t.Helper()
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: plugin})
}

func newTaskSubmitContext(t *testing.T, originalModel, mapping string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, originalModel)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://provider.example")
	if mapping != "" {
		c.Set("model_mapping", mapping)
	}
	c.Set("task_request", map[string]any{"prompt": "p"})
	return c, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
}

func TestRelayTaskSubmitMapsBeforeValidateWhenOriginSet(t *testing.T) {
	const mapping = `{"alias-model":"mid-model","mid-model":"declared-model"}`

	c, info := newTaskSubmitContext(t, "alias-model", mapping)
	pinMappingOrderPlugin(t, c, mappingOrderSubmitPlugin)
	info.OriginModelName = "alias-model"

	_, taskErr := RelayTaskSubmit(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "model_price_error", taskErr.Code)
	assert.Equal(t, "alias-model", info.OriginModelName)
	assert.Equal(t, "declared-model", info.UpstreamModelName)
	assert.True(t, info.IsModelMapped)
}

func TestRelayTaskSubmitDeclaredNameWithoutMappingIsUnchanged(t *testing.T) {
	c, info := newTaskSubmitContext(t, "declared-model", "")
	pinMappingOrderPlugin(t, c, mappingOrderSubmitPlugin)
	info.OriginModelName = "declared-model"

	_, taskErr := RelayTaskSubmit(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "model_price_error", taskErr.Code)
	assert.Equal(t, "declared-model", info.OriginModelName)
	assert.Equal(t, "declared-model", info.UpstreamModelName)
	assert.False(t, info.IsModelMapped)
}

func TestRelayTaskSubmitDoesNotApplyMappingTwice(t *testing.T) {
	c, info := newTaskSubmitContext(t, "alias-model", `{"alias-model":"declared-model"}`)
	pinMappingOrderPlugin(t, c, mappingOrderRewritePlugin)
	info.OriginModelName = "alias-model"

	_, taskErr := RelayTaskSubmit(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "model_price_error", taskErr.Code)
	assert.Equal(t, "rewritten", info.UpstreamModelName, "late mapping would overwrite rewriteModel with the chain tail")
	assert.Equal(t, "alias-model", info.OriginModelName)
}

func TestRelayTaskSubmitEmptyOriginKeepsLateMapping(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(mappingOrderSubmitPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	synthesized := service.CoverTaskActionToModelName(constant.TaskPlatform(plugin.Meta.Key), "text_to_video")
	c, info := newTaskSubmitContext(t, "pre-validate-upstream",
		`{"pre-validate-upstream":"should-not-apply-early","`+synthesized+`":"legacy-tail"}`)
	c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: plugin})
	info.OriginModelName = ""

	_, taskErr := RelayTaskSubmit(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "model_price_error", taskErr.Code)
	assert.Equal(t, synthesized, info.OriginModelName)
	assert.Equal(t, "legacy-tail", info.UpstreamModelName)
	assert.True(t, info.IsModelMapped)
}

const billingFallbackPlugin = `
export const meta = {apiVersion:1,key:"bill-fallback",name:"Bill Fallback",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl+"/submit", method:"POST", body:{upstreamModel: ctx.upstreamModel, model: ctx.model}, action:"text_to_video"};
}
export function parseSubmitResponse(){return {taskId:"1"};}
export function buildQueryRequest(){return {url:"https://provider.example"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`

func saveBillingConfig(t *testing.T) {
	t.Helper()
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
}

func TestRelayTaskSubmitAliasBillingIdentityAndExprFallback(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousCustomRate := settings.CustomCurrencyExchangeRate
	settings.CustomCurrencyExchangeRate = 100
	t.Cleanup(func() { settings.CustomCurrencyExchangeRate = previousCustomRate })

	seedRelayTaskTestFX(t)
	const mapping = `{"alias-model":"declared-model"}`
	const aliasExpr = `tier("alias", 2)`
	const tailExpr = `tier("tail", 3)`

	tests := []struct {
		name       string
		modes      map[string]string
		exprs      map[string]string
		wantTiered bool
		wantExpr   string
	}{
		{
			name:       "alias own tiered wins",
			modes:      map[string]string{"alias-model": "tiered_expr", "declared-model": "tiered_expr"},
			exprs:      map[string]string{"alias-model": aliasExpr, "declared-model": tailExpr},
			wantTiered: true,
			wantExpr:   aliasExpr,
		},
		{
			name:       "fallback uses tail expr",
			modes:      map[string]string{"declared-model": "tiered_expr"},
			exprs:      map[string]string{"declared-model": tailExpr},
			wantTiered: true,
			wantExpr:   tailExpr,
		},
		{
			name:       "neither tiered uses ordinary pricing",
			wantTiered: false,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			saveBillingConfig(t)
			if len(testCase.modes) > 0 {
				modeJSON, marshalErr := common.Marshal(testCase.modes)
				require.NoError(t, marshalErr)
				exprJSON, marshalErr := common.Marshal(testCase.exprs)
				require.NoError(t, marshalErr)
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
					"billing_setting.billing_mode": string(modeJSON),
					"billing_setting.billing_expr": string(exprJSON),
				}))
				if testCase.wantExpr == aliasExpr {
					require.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode("alias-model"))
				} else {
					require.Equal(t, billing_setting.BillingModeRatio, billing_setting.GetBillingMode("alias-model"))
					require.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode("declared-model"))
				}
			}

			c, info := newTaskSubmitContext(t, "alias-model", mapping)
			c.Set("group", "default")
			info.UserGroup = "default"
			info.UsingGroup = "default"
			pinMappingOrderPlugin(t, c, billingFallbackPlugin)
			info.OriginModelName = "alias-model"

			_, taskErr := RelayTaskSubmit(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, "alias-model", info.OriginModelName)
			assert.Equal(t, "declared-model", info.UpstreamModelName)
			assert.True(t, info.IsModelMapped)

			task := model.InitTask(constant.TaskPlatform("bill-fallback"), info)
			assert.Equal(t, "alias-model", task.Properties.OriginModelName)
			assert.Equal(t, "declared-model", task.Properties.UpstreamModelName)

			if testCase.wantTiered {
				require.NotNil(t, info.TieredBillingSnapshot)
				assert.Equal(t, "alias-model", info.TieredBillingSnapshot.ModelName)
				assert.Equal(t, testCase.wantExpr, info.TieredBillingSnapshot.ExprString)
				assert.Equal(t, billingexpr.ExprHashString(testCase.wantExpr), info.TieredBillingSnapshot.ExprHash)
				assert.NotEqual(t, "model_price_error", taskErr.Code)
			} else {
				assert.Nil(t, info.TieredBillingSnapshot)
				assert.Equal(t, "model_price_error", taskErr.Code)
			}
		})
	}
}

func seedRelayTaskTestFX(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ModelCostFXRow{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})

	var row model.ModelCostFXRow
	err = db.Where("source = ?", "cbr-xml-daily.ru").First(&row).Error
	require.True(t, err == nil || errors.Is(err, gorm.ErrRecordNotFound))
	effectiveAt, fetchedAt, currentVersion := row.EffectiveAt, row.FetchedAt, int64(0)
	if current, currentErr := model.CurrentModelCostFX("cbr-xml-daily.ru"); currentErr == nil {
		currentVersion = current.Version
		if current.EffectiveAt > effectiveAt {
			effectiveAt = current.EffectiveAt
		}
		if current.FetchedAt > fetchedAt {
			fetchedAt = current.FetchedAt
		}
	}
	for first := true; first || row.Version <= currentVersion; first = false {
		effectiveAt++
		fetchedAt++
		require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{
			Source: "cbr-xml-daily.ru", EffectiveAt: effectiveAt, FetchedAt: fetchedAt,
			Rates: map[string]float64{"USD": 100},
		}, row.Version))
		require.NoError(t, db.Where("source = ?", "cbr-xml-daily.ru").First(&row).Error)
	}
	require.NoError(t, model.LoadModelCostFX(context.Background(), "cbr-xml-daily.ru"))
}

func TestRelayTaskSubmitClampEmitsOneCorrelatedWarning(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousRate := settings.CustomCurrencyExchangeRate
	settings.CustomCurrencyExchangeRate = 100
	t.Cleanup(func() { settings.CustomCurrencyExchangeRate = previousRate })
	seedRelayTaskTestFX(t)
	saveBillingConfig(t)
	savedPrices := ratio_setting.ModelPrice2JSONString()
	savedRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
	})
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)

	for _, tc := range []struct{ name, price, ratio, mode, expression, op string }{
		{"strict price", "{\"declared-model\":1e20}", "{}", "{}", "{}", "QuotaFromFloat"},
		{"strict ratio", "{}", "{\"declared-model\":1e20}", "{}", "{}", "QuotaFromFloat"},
		{"checked expression", "{}", "{}", "{\"declared-model\":\"tiered_expr\"}", "{\"declared-model\":\"1e20\"}", "QuotaFromDecimal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(tc.price))
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(tc.ratio))
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_mode":    tc.mode,
				"billing_setting.billing_expr":    tc.expression,
				"group_ratio_setting.group_ratio": "{\"default\":1}",
			}))
			c, info := newTaskSubmitContext(t, "declared-model", "")
			c.Set(common.RequestIdKey, "task-clamp-audit-r3")
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			c.Set("group", "default")
			info.OriginModelName, info.UserGroup, info.UsingGroup = "declared-model", "default", "default"
			pinMappingOrderPlugin(t, c, billingFallbackPlugin)
			var audit bytes.Buffer
			common.LogWriterMu.Lock()
			previousWriter := gin.DefaultErrorWriter
			gin.DefaultErrorWriter = &audit
			common.LogWriterMu.Unlock()
			t.Cleanup(func() {
				common.LogWriterMu.Lock()
				gin.DefaultErrorWriter = previousWriter
				common.LogWriterMu.Unlock()
			})

			result, taskErr := RelayTaskSubmit(c, info)
			require.Nil(t, result)
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusForbidden, taskErr.StatusCode)
			assert.Equal(t, "insufficient_user_quota", taskErr.Code)
			assert.Equal(t, "insufficient quota", taskErr.Message)
			assert.True(t, taskErr.LocalError)
			var clamp *common.QuotaClamp
			require.ErrorAs(t, taskErr.Error, &clamp)
			assert.ErrorIs(t, taskErr.Error, clamp)
			assert.Same(t, clamp, info.QuotaClamp)
			assert.Nil(t, info.Billing)
			assert.Zero(t, info.FinalPreConsumedQuota)
			assert.Zero(t, upstreamCalls.Load())
			// Only the FX publication table exists: any funding/task/consume write
			// would fail before this local quota refusal.
			warnings := 0
			for _, line := range strings.Split(audit.String(), "\n") {
				if !strings.Contains(line, "[WARN]") {
					continue
				}
				warnings++
				assert.Contains(t, line, "task-clamp-audit-r3")
				assert.Contains(t, line, "op="+tc.op+" kind=overflow clamped=2147483647")
				assert.NotContains(t, line, "original=")
				assert.NotContains(t, line, "declared-model")
			}
			assert.Equal(t, 1, warnings, audit.String())
		})
	}
}

func TestMidjourneyClampRefusalEmitsOneCorrelatedWarning(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousRate := settings.CustomCurrencyExchangeRate
	settings.CustomCurrencyExchangeRate = 100
	t.Cleanup(func() { settings.CustomCurrencyExchangeRate = previousRate })
	seedRelayTaskTestFX(t)
	saveBillingConfig(t)
	previousPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":1e20,"swap_face":1e20}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	// Any DB operation after the RAM publication would cross the refusal boundary.
	var dbOperations atomic.Int32
	recordOperation := func(*gorm.DB) { dbOperations.Add(1) }
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("mj-audit:query", recordOperation))
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("mj-audit:create", recordOperation))
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("mj-audit:update", recordOperation))
	require.NoError(t, model.DB.Callback().Raw().Before("gorm:raw").Register("mj-audit:raw", recordOperation))
	previousLogDB := model.LOG_DB
	model.LOG_DB = model.DB
	t.Cleanup(func() { model.LOG_DB = previousLogDB })
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)

	for _, tc := range []struct {
		name, model, path, body string
		handler                 func(*gin.Context, *relaycommon.RelayInfo) *dto.MidjourneyResponse
	}{
		{"imagine", "mj_imagine", "/mj/submit/imagine", `{"prompt":"lighthouse"}`, RelayMidjourneySubmit},
		{"swap-face", "swap_face", "/mj/submit/swap-face", `{"sourceBase64":"private-source","targetBase64":"private-target"}`, RelaySwapFace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(common.RequestIdKey, "mj-clamp-audit-"+tc.name)
			c.Set("base_url", upstream.URL)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeMidjourney)
			info := &relaycommon.RelayInfo{
				UserId: 4452, UserGroup: "default", UsingGroup: "default", OriginModelName: tc.model,
				RelayMode: relayconstant.RelayModeMidjourneyImagine,
			}
			var audit bytes.Buffer
			common.LogWriterMu.Lock()
			previousWriter := gin.DefaultErrorWriter
			gin.DefaultErrorWriter = &audit
			common.LogWriterMu.Unlock()
			t.Cleanup(func() {
				common.LogWriterMu.Lock()
				gin.DefaultErrorWriter = previousWriter
				common.LogWriterMu.Unlock()
			})
			result := tc.handler(c, info)
			require.NotNil(t, result)
			assert.Equal(t, 4, result.Code)
			assert.Equal(t, "quota_not_enough", result.Description)
			encoded, err := common.Marshal(result)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "QuotaFromFloat")
			assert.NotContains(t, string(encoded), "private-")
			assert.Zero(t, dbOperations.Load(), "no funding query/write, task insertion or denied consume event")
			assert.Zero(t, upstreamCalls.Load())
			assert.Nil(t, info.Billing)
			warnings := 0
			for _, line := range strings.Split(audit.String(), "\n") {
				if !strings.Contains(line, "[WARN]") {
					continue
				}
				warnings++
				assert.Contains(t, line, "mj-clamp-audit-"+tc.name)
				assert.Contains(t, line, "op=QuotaFromFloat kind=overflow clamped=2147483647")
				assert.NotContains(t, line, "original=")
				assert.NotContains(t, line, "private-")
			}
			assert.Equal(t, 1, warnings, audit.String())
			require.NotNil(t, info.QuotaClamp)
			assert.Equal(t, "QuotaFromFloat", info.QuotaClamp.Op)
			assert.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
			assert.Equal(t, common.MaxQuota, info.QuotaClamp.Clamped)
		})
	}
}

func TestRelayTaskExpressionFXRateMatrix(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousRate := settings.CustomCurrencyExchangeRate
	settings.CustomCurrencyExchangeRate = 100
	previousRedis, previousBatch := common.RedisEnabled, common.BatchUpdateEnabled
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() {
		settings.CustomCurrencyExchangeRate = previousRate
		common.RedisEnabled, common.BatchUpdateEnabled = previousRedis, previousBatch
	})
	seedRelayTaskTestFX(t)
	saveBillingConfig(t)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":          `{"declared-model":"tiered_expr"}`,
		"billing_setting.billing_expr":          `{"declared-model":"tier(\"task\", 2)"}`,
		"group_ratio_setting.group_ratio":       `{"default":1.45}`,
		"group_ratio_setting.group_group_ratio": `{}`,
	}))
	require.NoError(t, model.DB.AutoMigrate(&model.User{}))
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)
	for i, tc := range []struct {
		rate, effective float64
		quota           int
	}{
		{90, 1.305, 1_305_000}, {100, 1.45, 1_450_000}, {110, 1.595, 1_595_000},
	} {
		t.Run(fmt.Sprintf("rate_%g", tc.rate), func(t *testing.T) {
			current, err := model.CurrentModelCostFX("cbr-xml-daily.ru")
			require.NoError(t, err)
			require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{
				Source: current.Source, EffectiveAt: current.EffectiveAt + 1, FetchedAt: current.FetchedAt + 1,
				Rates: map[string]float64{"USD": tc.rate},
			}, current.Version))
			require.NoError(t, model.LoadModelCostFX(context.Background(), current.Source))
			userID := 4460 + i
			require.NoError(t, model.DB.Create(&model.User{Id: userID, Username: fmt.Sprintf("task-matrix-%d", i), AffCode: fmt.Sprintf("mx%d", i), Quota: 5_000_000, Group: "default"}).Error)
			c, info := newTaskSubmitContext(t, "declared-model", "")
			c.Set("group", "default")
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			info.OriginModelName, info.UserGroup, info.UsingGroup = "declared-model", "default", "default"
			info.UserId, info.IsPlayground = userID, true
			info.UserSetting.BillingPreference = "wallet_only"
			pinMappingOrderPlugin(t, c, billingFallbackPlugin)
			observedFunding := false
			require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("matrix:before-funding", func(tx *gorm.DB) {
				if tx.Statement.Table != "users" {
					return
				}
				observedFunding = true
				assert.Equal(t, tc.rate, info.BillingFXRate)
				assert.Equal(t, tc.quota, info.PriceData.Quota)
				if assert.NotNil(t, info.TieredBillingSnapshot) {
					assert.Equal(t, tc.effective, info.TieredBillingSnapshot.GroupRatio)
				}
			}))
			t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove("matrix:before-funding")) })
			result, taskErr := RelayTaskSubmit(c, info)
			require.Nil(t, taskErr)
			require.NotNil(t, result)
			assert.True(t, observedFunding, "FX must be captured before actual wallet funding")
			assert.Equal(t, tc.quota, result.Quota)
			assert.Equal(t, tc.quota, info.FinalPreConsumedQuota)
			require.NotNil(t, info.Billing)
			assert.Equal(t, tc.quota, info.Billing.GetPreConsumedQuota())
			assert.Equal(t, 1.45, info.PriceData.GroupRatioInfo.GroupRatio)
			assert.Equal(t, tc.effective, info.PriceData.EffectiveGroupRatio())
			require.NotNil(t, info.TieredBillingSnapshot)
			assert.Equal(t, tc.quota, info.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
			assert.Equal(t, 1_000_000.0, info.TieredBillingSnapshot.EstimatedQuotaBeforeGroup)
			assert.True(t, info.TieredBillingSnapshot.TaskUsageBilling)
			assert.Equal(t, tc.rate, info.BillingFXRate)
			var user model.User
			require.NoError(t, model.DB.First(&user, userID).Error)
			assert.Equal(t, 5_000_000-tc.quota, user.Quota)
		})
	}
	assert.Equal(t, int32(3), upstreamCalls.Load())
}
