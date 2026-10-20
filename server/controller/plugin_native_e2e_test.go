package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type nativeRouteBilling struct {
	events      []string
	preConsumed int
	userID      int
	settled     bool
}

func (b *nativeRouteBilling) Settle(int) error {
	b.events = append(b.events, "settle")
	b.settled = true
	return nil
}

func (b *nativeRouteBilling) Refund(*gin.Context) {
	b.events = append(b.events, "refund")
	if !b.settled && b.preConsumed > 0 {
		_ = model.IncreaseUserQuota(b.userID, b.preConsumed, true)
		b.preConsumed = 0
	}
}

func (b *nativeRouteBilling) NeedsRefund() bool {
	return !b.settled && b.preConsumed > 0
}

func (b *nativeRouteBilling) GetPreConsumedQuota() int {
	return b.preConsumed
}

func (b *nativeRouteBilling) Reserve(quota int) error {
	b.events = append(b.events, "reserve")
	if b.userID == 0 {
		b.preConsumed = quota
		return nil
	}
	if err := model.DecreaseUserQuota(b.userID, quota, true); err != nil {
		return err
	}
	b.preConsumed = quota
	return nil
}

func TestKlingNativeRouteSubmitPollSettleAndQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousMemoryCache := common.MemoryCacheEnabled
	previousBatchUpdate := common.BatchUpdateEnabled
	previousLogConsume := common.LogConsumeEnabled
	previousRedisEnabled := common.RedisEnabled
	previousCustomRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Channel{}, &model.Task{}, &model.Log{}, &model.ModelCostFXRow{}))
	model.DB = database
	model.LOG_DB = database
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = false
	common.RedisEnabled = false
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	previousModelRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"kling-v1":1}`))
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.MemoryCacheEnabled = previousMemoryCache
		common.BatchUpdateEnabled = previousBatchUpdate
		common.LogConsumeEnabled = previousLogConsume
		common.RedisEnabled = previousRedisEnabled
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousCustomRate
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatios))
	})
	seedNativeRouteFX(t, database)
	require.NoError(t, database.Create(&model.User{
		Id:       7,
		Username: "native-route-user",
		Group:    "default",
		Quota:    1_000_000,
	}).Error)

	var submitCalls atomic.Int32
	var queryCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/kling/v1/videos/text2video":
			submitCalls.Add(1)
			body, readErr := io.ReadAll(r.Body)
			if !assert.NoError(t, readErr) {
				http.Error(w, "read request", http.StatusInternalServerError)
				return
			}
			assert.Contains(t, string(body), `"model_name":"kling-v1"`)
			_, _ = io.WriteString(w, `{"code":0,"message":"","data":{"task_id":"kling-private-1","task_status":"submitted"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/kling/v1/videos/text2video/kling-private-1":
			queryCalls.Add(1)
			_, _ = io.WriteString(w, `{"code":0,"message":"","data":{"task_id":"kling-private-1","task_status":"succeed","task_status_msg":"","task_result":{"videos":[{"id":"video-private","url":"https://cdn.example/video.mp4","duration":"5"}]},"final_unit_deduction":"1"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	channel := model.Channel{
		Type:    constant.ChannelTypeKling,
		Name:    "kling-native-e2e",
		Key:     "sk-test",
		BaseURL: &upstream.URL,
		Status:  common.ChannelStatusEnabled,
		Models:  "kling-v1",
		Group:   "default",
	}
	require.NoError(t, database.Create(&channel).Error)

	generation := pluginruntime.DefaultRegistry.Generation()
	require.NotNil(t, generation)
	submitBinding, found := generation.LookupDeclaredRoute(http.MethodPost, "/kling/v1/videos/text2video")
	require.True(t, found)
	require.Equal(t, "kling", submitBinding.Plugin.Meta.Key)

	submitRecorder := httptest.NewRecorder()
	submitContext, _ := gin.CreateTestContext(submitRecorder)
	submitContext.Request = httptest.NewRequest(
		http.MethodPost,
		"/kling/v1/videos/text2video",
		bytes.NewBufferString(`{"model_name":"kling-v1","prompt":"a lighthouse"}`),
	)
	submitContext.Request.Header.Set("Content-Type", "application/json")
	submitContext.Set(pluginruntime.ContextKeyPinnedRoute, pluginruntime.PinnedRoute{
		Generation: generation,
		Plugin:     submitBinding.Plugin,
		Route:      submitBinding.Route,
	})
	common.SetContextKey(submitContext, constant.ContextKeyUserId, 7)
	common.SetContextKey(submitContext, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(submitContext, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(submitContext, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(submitContext, constant.ContextKeyUserQuota, 1_000_000)

	middleware.PrepareTaskPluginRoute()(submitContext)
	require.False(t, submitContext.IsAborted(), submitRecorder.Body.String())
	require.Equal(t, "kling-v1", submitContext.GetString("resolved_task_model"))
	require.Equal(t, "text_to_video", submitContext.GetString("task_action"))
	require.Nil(t, middleware.SetupContextForSelectedChannel(submitContext, &channel, "kling-v1"))

	billing := &nativeRouteBilling{userID: 7}
	relayInfo := &relaycommon.RelayInfo{
		UserId:          7,
		UserGroup:       "default",
		UsingGroup:      "default",
		UserQuota:       1_000_000,
		TokenGroup:      "default",
		OriginModelName: "kling-v1",
		Billing:         billing,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			Action:        submitContext.GetString("task_action"),
			PublicTaskID:  "task_kling_public",
			LockedChannel: &channel,
		},
	}

	outcome, taskErr := executeTaskSubmissionWith(submitContext, relayInfo, relay.RelayTaskSubmit)
	require.Nil(t, taskErr)
	require.NotNil(t, outcome)
	require.Equal(t, []string{"reserve", "settle"}, billing.events)
	require.False(t, submitContext.Writer.Written())

	presentTaskSubmission(submitContext, outcome)
	require.Equal(t, http.StatusOK, submitRecorder.Code)
	assert.Contains(t, submitRecorder.Body.String(), `"task_id":"task_kling_public"`)
	assert.NotContains(t, submitRecorder.Body.String(), "kling-private-1")
	assert.Equal(t, int32(1), submitCalls.Load())

	var persisted model.Task
	require.NoError(t, database.Where("task_id = ?", "task_kling_public").First(&persisted).Error)
	assert.Equal(t, constant.TaskPlatform("kling"), persisted.Platform)
	assert.Equal(t, "kling-private-1", persisted.PrivateData.UpstreamTaskID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusNotStart), persisted.Status)

	previousAdaptorFactory := service.GetTaskAdaptorFunc
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor {
		return relay.GetTaskAdaptor(platform)
	}
	t.Cleanup(func() { service.GetTaskAdaptorFunc = previousAdaptorFactory })
	service.DispatchPlatformUpdate(
		context.Background(),
		persisted.Platform,
		map[int][]string{channel.Id: {"kling-private-1"}},
		map[string]*model.Task{"kling-private-1": &persisted},
	)

	require.NoError(t, database.Where("task_id = ?", "task_kling_public").First(&persisted).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), persisted.Status)
	assert.Equal(t, "100%", persisted.Progress)
	assert.Equal(t, 1, persisted.Quota)
	assert.Equal(t, int32(1), queryCalls.Load())
	var settledUser model.User
	require.NoError(t, database.First(&settledUser, 7).Error)
	assert.Equal(t, 999_999, settledUser.Quota)

	queryBinding, found := generation.LookupDeclaredRoute(http.MethodGet, "/kling/v1/videos/text2video/:task_id")
	require.True(t, found)
	queryRecorder := httptest.NewRecorder()
	queryContext, _ := gin.CreateTestContext(queryRecorder)
	queryContext.Request = httptest.NewRequest(http.MethodGet, "/kling/v1/videos/text2video/task_kling_public", nil)
	queryContext.Params = gin.Params{{Key: "task_id", Value: "task_kling_public"}}
	queryContext.Set(pluginruntime.ContextKeyPinnedRoute, pluginruntime.PinnedRoute{
		Generation: generation,
		Plugin:     queryBinding.Plugin,
		Route:      queryBinding.Route,
	})
	common.SetContextKey(queryContext, constant.ContextKeyUserId, 7)

	middleware.PrepareTaskPluginRoute()(queryContext)

	require.True(t, queryContext.IsAborted())
	require.Equal(t, http.StatusOK, queryRecorder.Code)
	assert.Contains(t, queryRecorder.Body.String(), `"task_id":"task_kling_public"`)
	assert.Contains(t, queryRecorder.Body.String(), `"task_status":"succeed"`)
	assert.NotContains(t, queryRecorder.Body.String(), "kling-private-1")
	assert.NotContains(t, queryRecorder.Body.String(), upstream.URL)
}

func seedNativeRouteFX(t *testing.T, db *gorm.DB) {
	t.Helper()
	var row model.ModelCostFXRow
	err := db.Where("source = ?", "cbr-xml-daily.ru").First(&row).Error
	require.True(t, err == nil || errors.Is(err, gorm.ErrRecordNotFound))
	effectiveAt, fetchedAt := row.EffectiveAt, row.FetchedAt
	currentVersion := int64(0)
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

func TestGetChannelRecomposesRetainedFXForSelectedTieredGroupTransitions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name             string
		initialGroup     float64
		selectedGroup    string
		selectedRatio    float64
		initialFreeModel bool
		wantEffective    float64
		wantReservation  []int
	}{
		{name: "paid to paid", initialGroup: 2, selectedGroup: "selected", selectedRatio: 3, wantEffective: 2.7, wantReservation: []int{270}},
		{name: "paid to free", initialGroup: 2, selectedGroup: "selected", selectedRatio: 0, wantEffective: 0},
		{name: "free to paid", initialGroup: 0, selectedGroup: "selected", selectedRatio: 3, initialFreeModel: true, wantEffective: 2.7, wantReservation: []int{270}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database := setupRoutingRetryTestDatabase(t, testCase.selectedGroup, testCase.selectedRatio)
			seedNativeRouteFX(t, database)
			priority := int64(0)
			weight := uint(100)
			require.NoError(t, database.Create(&model.Channel{Id: 4450, Type: constant.ChannelTypeOpenAI, Name: "selected", Key: "sk-test", Status: common.ChannelStatusEnabled, Weight: &weight, Models: "routing-fx-model", Group: testCase.selectedGroup, Priority: &priority}).Error)
			require.NoError(t, database.Create(&model.Ability{Group: testCase.selectedGroup, Model: "routing-fx-model", ChannelId: 4450, Enabled: true, Priority: &priority, Weight: weight}).Error)
			model.InitChannelCache()

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenAutoGroups, []string{testCase.selectedGroup})
			billing := &nativeRouteBilling{}
			info := &relaycommon.RelayInfo{
				UserGroup: "default", UsingGroup: "initial", TokenGroup: "auto", OriginModelName: "routing-fx-model",
				ChannelMeta: &relaycommon.ChannelMeta{}, BillingFXRate: 90, BillingFXFactor: .9, Billing: billing,
				PriceData:             types.PriceData{FreeModel: testCase.initialFreeModel, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: testCase.initialGroup}, BillingGroupRatio: testCase.initialGroup * .9},
				TieredBillingSnapshot: &billingexpr.BillingSnapshot{BillingMode: billing_setting.BillingModeTieredExpr, GroupRatio: testCase.initialGroup * .9, EstimatedQuotaBeforeGroup: 100, EstimatedQuotaAfterGroup: int(testCase.initialGroup * 90)},
			}
			retry := 0
			channel, channelErr := getChannel(c, info, &service.RetryParam{Ctx: c, TokenGroup: "auto", ModelName: "routing-fx-model", RequestPath: c.Request.URL.Path, Retry: &retry})
			if channelErr != nil {
				t.Fatal(channelErr.Error())
			}
			require.NotNil(t, channel)
			require.Equal(t, testCase.selectedGroup, info.UsingGroup)
			assert.Equal(t, testCase.selectedRatio, info.PriceData.GroupRatioInfo.GroupRatio)
			assert.Equal(t, testCase.wantEffective, info.PriceData.BillingGroupRatio)

			require.Nil(t, service.PrepareTieredBillingForSelectedGroup(c, info))
			assert.Equal(t, testCase.wantEffective, info.TieredBillingSnapshot.GroupRatio)
			assert.Equal(t, int(testCase.wantEffective*100), info.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
			assert.Equal(t, testCase.wantReservation, billing.eventsToReservationTargets())
		})
	}
}

func TestTaskRetryRejectsLocalClampBeforeBillingOrUpstreamAndKeepsProviderRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := setupRoutingRetryTestDatabase(t, "default", 1)
	seedNativeRouteFX(t, database)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"clamp-model":1e20,"retry-model":1}`))
	require.NoError(t, database.Create(&model.User{Id: 4451, Username: "routing-retry", Group: "default", Quota: 1000, Status: common.UserStatusEnabled}).Error)
	previousRetryTimes := common.RetryTimes
	common.RetryTimes = 1
	t.Cleanup(func() { common.RetryTimes = previousRetryTimes })

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		http.Error(w, "provider failed", http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	priority := int64(0)
	weight := uint(100)
	channel := &model.Channel{Id: 4451, Type: constant.ChannelTypeTaskPlugin, Name: "task-retry", Key: "sk-test", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, Weight: &weight, Models: "clamp-model,retry-model", Group: "default", Priority: &priority}
	require.NoError(t, database.Create(channel).Error)

	for _, testCase := range []struct {
		name           string
		model          string
		wantAttempts   int
		wantStatus     int
		wantMessage    string
		invalidFX      bool
		nativeRenderer bool
		providerRetry  bool
	}{
		{name: "native renderer presents clamp before billing or upstream", model: "clamp-model", wantAttempts: 0, wantStatus: http.StatusForbidden, wantMessage: "insufficient quota", nativeRenderer: true},
		{name: "host fallback presents invalid FX configuration before upstream", model: "retry-model", wantAttempts: 0, wantStatus: http.StatusServiceUnavailable, wantMessage: "model cost accounting configuration is invalid", invalidFX: true},
		{name: "provider 5xx remains retryable", model: "retry-model", wantAttempts: 2, providerRetry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			upstreamCalls.Store(0)
			if testCase.invalidFX {
				operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 1
				t.Cleanup(func() { operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100 })
			}
			plugin, err := pluginruntime.CompilePlugin(`
export const meta = {apiVersion:1,key:"retry-contract",name:"Retry Contract",version:"1.0.0",author:{name:"Test"},models:["clamp-model","retry-model"],fetchMode:"per_task"};
export const native = {error: function(_, error) { return {error: {message: error.message, code: error.code}}; }};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit",method:"POST",body:{model:ctx.model},action:"text_to_video"};}
export function parseSubmitResponse(){return {taskId:"upstream"};}
export function buildQueryRequest(){return {url:"https://provider.example/query"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`, pluginruntime.Options{})
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/plugin/submit", strings.NewReader(`{"model":"`+testCase.model+`"}`))
			c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: plugin})
			if testCase.nativeRenderer {
				c.Set(pluginruntime.ContextKeyPinnedRoute, pluginruntime.PinnedRoute{Plugin: plugin})
				c.Set(pluginruntime.ContextKeyRouteRequest, pluginruntime.RouteRequestContext{Path: c.Request.URL.Path, Method: c.Request.Method, Params: map[string]string{}, Query: map[string][]string{}})
			}
			c.Set("task_request", map[string]any{"model": testCase.model})
			c.Set("resolved_task_model", testCase.model)
			c.Set("platform", "retry-contract")
			common.SetContextKey(c, constant.ContextKeyUserId, 4451)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUserQuota, 1000)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, testCase.model)
			require.Nil(t, middleware.SetupContextForSelectedChannel(c, channel, testCase.model))
			if testCase.providerRetry {
				billing := &nativeRouteBilling{userID: 4451}
				info := &relaycommon.RelayInfo{UserGroup: "default", UsingGroup: "default", TokenGroup: "default", OriginModelName: testCase.model, Billing: billing, PriceData: types.PriceData{FreeModel: true}, TaskRelayInfo: &relaycommon.TaskRelayInfo{LockedChannel: channel}}
				_, taskErr := executeTaskSubmissionWith(c, info, relay.RelayTaskSubmit)
				require.NotNil(t, taskErr)
				assert.Equal(t, "fail_to_fetch_task", taskErr.Code)
				assert.False(t, taskErr.LocalError)
				assert.Zero(t, billing.preConsumed)
			} else {
				RelayTask(c)

				require.Equal(t, testCase.wantStatus, recorder.Code)
				assert.Contains(t, recorder.Body.String(), testCase.wantMessage)
				assert.NotContains(t, recorder.Body.String(), "QuotaRound")
				assert.NotContains(t, recorder.Body.String(), "1e20")
			}

			var user model.User
			require.NoError(t, database.First(&user, 4451).Error)
			assert.Equal(t, 1000, user.Quota)
			if testCase.wantAttempts == 0 {
				assert.Zero(t, upstreamCalls.Load(), "the local admission rejection must not reach upstream")
				var storedChannel model.Channel
				require.NoError(t, database.First(&storedChannel, channel.Id).Error)
				assert.Equal(t, common.ChannelStatusEnabled, storedChannel.Status)
			} else {
				assert.Equal(t, int32(2), upstreamCalls.Load())
			}
		})
	}
}

func setupRoutingRetryTestDatabase(t *testing.T, selectedGroup string, selectedRatio float64) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	previousMemoryCache := common.MemoryCacheEnabled
	previousRedisEnabled := common.RedisEnabled
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	previousModelPrices := ratio_setting.ModelPrice2JSONString()
	previousAutoGroups := setting.AutoGroups2JsonString()
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousFXDenomination := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}, &model.ModelCostFXRow{}))
	model.DB = database
	common.MemoryCacheEnabled = true
	common.RedisEnabled = false
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"initial":1,"default":1,"selected":`+fmt.Sprintf("%g", selectedRatio)+`}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`[]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","selected":"Selected"}`))
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
		common.RedisEnabled = previousRedisEnabled
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousFXDenomination
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousModelPrices))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		if sqlDB, closeErr := database.DB(); closeErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return database
}

func (b *nativeRouteBilling) eventsToReservationTargets() []int {
	var reservationTargets []int
	for _, event := range b.events {
		if event == "reserve" {
			reservationTargets = append(reservationTargets, b.preConsumed)
		}
	}
	return reservationTargets
}

func TestMidjourneyAndSwapFaceCaptureFXBeforeUpstreamAndKeepSavedRefund(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	database := setupRoutingRetryTestDatabase(t, "default", 1)
	previousLogDB := model.LOG_DB
	model.LOG_DB = database
	t.Cleanup(func() { model.LOG_DB = previousLogDB })
	require.NoError(t, database.AutoMigrate(&model.Midjourney{}, &model.Log{}, &model.Token{}))
	seedNativeRouteFX(t, database)
	currentFX, err := model.CurrentModelCostFX("cbr-xml-daily.ru")
	require.NoError(t, err)
	require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{
		Source: "cbr-xml-daily.ru", EffectiveAt: currentFX.EffectiveAt + 1, FetchedAt: currentFX.FetchedAt + 1,
		Rates: map[string]float64{"USD": 90},
	}, currentFX.Version))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":1,"swap_face":1e20,"mj_overflow":1e20}`))
	require.NoError(t, database.Create(&model.User{Id: 4452, Username: "mj-fx", Group: "default", Quota: 4_000_000, Status: common.UserStatusEnabled}).Error)

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestNumber := upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, fmt.Sprintf(`{"code":1,"description":"accepted","result":"mj-fx-submit-%d"}`, requestNumber))
	}))
	t.Cleanup(upstream.Close)
	weight := uint(100)
	channel := &model.Channel{Id: 4452, Type: constant.ChannelTypeMidjourney, Name: "mj-fx", Key: "sk-test", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, Weight: &weight, Models: "mj_imagine,swap_face,mj_overflow", Group: "default"}
	require.NoError(t, database.Create(channel).Error)

	newContext := func(body string) (*gin.Context, *relaycommon.RelayInfo) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		require.Nil(t, middleware.SetupContextForSelectedChannel(c, channel, "mj_imagine"))
		return c, &relaycommon.RelayInfo{UserId: 4452, UserGroup: "default", UsingGroup: "default", OriginModelName: "mj_imagine", StartTime: time.Now(), RelayMode: relayconstant.RelayModeMidjourneyImagine}
	}

	c, info := newContext(`{"prompt":"lighthouse"}`)
	require.Nil(t, relay.RelayMidjourneySubmit(c, info))
	assert.Equal(t, int32(1), upstreamCalls.Load())
	assert.Equal(t, 90.0, info.BillingFXRate)
	assert.Equal(t, 0.9, info.BillingFXFactor)
	var task model.Midjourney
	require.NoError(t, database.Where("mj_id = ?", "mj-fx-submit-1").First(&task).Error)
	assert.Equal(t, 900_000, task.Quota, "USD price and group 2 must be FX-scaled exactly once")
	var user model.User
	require.NoError(t, database.First(&user, 4452).Error)
	assert.Equal(t, 3_100_000, user.Quota)
	var submitLog model.Log
	require.NoError(t, database.Where("type = ?", model.LogTypeConsume).First(&submitLog).Error)
	other, err := common.StrToMap(submitLog.Other)
	require.NoError(t, err)
	assert.Equal(t, float64(2), other["group_ratio"])
	assert.Contains(t, submitLog.Content, "分组倍率 2.00")
	admin := other["admin_info"].(map[string]any)
	fx := admin["billing_fx"].(map[string]any)
	assert.Equal(t, float64(1), fx["schema_version"])
	assert.Equal(t, "cbr-xml-daily.ru", fx["source"])
	assert.Equal(t, float64(90), fx["rate"])
	assert.Contains(t, fx, "publication_version")
	assert.Contains(t, fx, "effective_at")
	assert.Contains(t, fx, "fetched_at")
	assert.Equal(t, float64(2), admin["billing_applied_group"].(map[string]any)["pure_ratio"])

	current, err := model.CurrentModelCostFX("cbr-xml-daily.ru")
	require.NoError(t, err)
	require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{Source: current.Source, EffectiveAt: current.EffectiveAt + 1, FetchedAt: current.FetchedAt + 1, Rates: map[string]float64{"USD": 110}}, current.Version))
	assert.True(t, service.RefundMidjourneyQuota(context.Background(), &task, "test failure"))
	require.NoError(t, database.First(&user, 4452).Error)
	assert.Equal(t, 4_000_000, user.Quota, "refund must use the saved 900000 quota, not current FX")
	require.NoError(t, database.First(&task, task.Id).Error)
	assert.Zero(t, task.Quota)

	currentFX, err = model.CurrentModelCostFX("cbr-xml-daily.ru")
	require.NoError(t, err)
	require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{
		Source: "cbr-xml-daily.ru", EffectiveAt: currentFX.EffectiveAt + 1, FetchedAt: currentFX.FetchedAt + 1,
		Rates: map[string]float64{"USD": 90},
	}, currentFX.Version))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":1,"swap_face":1,"mj_overflow":1e20}`))

	c, swapInfo := newContext(`{"sourceBase64":"source","targetBase64":"target"}`)
	c.Request.URL.Path = "/mj/submit/swap-face"
	swapInfo.OriginModelName = "swap_face"
	require.Nil(t, relay.RelaySwapFace(c, swapInfo))
	assert.Equal(t, int32(2), upstreamCalls.Load())
	assert.Equal(t, 90.0, swapInfo.BillingFXRate)
	assert.Equal(t, 0.9, swapInfo.BillingFXFactor)
	var swapTask model.Midjourney
	require.NoError(t, database.Where("mj_id = ?", "mj-fx-submit-2").First(&swapTask).Error)
	assert.Equal(t, 900_000, swapTask.Quota)
	require.NoError(t, database.First(&user, 4452).Error)
	assert.Equal(t, 3_100_000, user.Quota)
	var swapLog model.Log
	require.NoError(t, database.Where("type = ?", model.LogTypeConsume).Order("id DESC").First(&swapLog).Error)
	swapOther, err := common.StrToMap(swapLog.Other)
	require.NoError(t, err)
	swapAdmin := swapOther["admin_info"].(map[string]any)
	assert.Equal(t, float64(90), swapAdmin["billing_fx"].(map[string]any)["rate"])
	assert.Equal(t, float64(2), swapAdmin["billing_applied_group"].(map[string]any)["pure_ratio"])
	require.True(t, service.RefundMidjourneyQuota(context.Background(), &swapTask, "test failure"))
	require.NoError(t, database.First(&user, 4452).Error)
	assert.Equal(t, 4_000_000, user.Quota)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":1,"swap_face":1e20,"mj_overflow":1e20}`))

	for _, testCase := range []struct {
		name  string
		run   func(*gin.Context, *relaycommon.RelayInfo) *dto.MidjourneyResponse
		body  string
		clamp bool
	}{
		{name: "midjourney invalid captured FX", body: `{"prompt":"lighthouse"}`, run: relay.RelayMidjourneySubmit},
		{name: "swap face invalid captured FX", body: `{"sourceBase64":"source","targetBase64":"target"}`, run: relay.RelaySwapFace},
		{name: "midjourney saturation", body: `{"prompt":"lighthouse"}`, run: relay.RelayMidjourneySubmit, clamp: true},
		{name: "swap face saturation", body: `{"sourceBase64":"source","targetBase64":"target"}`, run: relay.RelaySwapFace, clamp: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			upstreamCalls.Store(0)
			c, info := newContext(testCase.body)
			if testCase.name[:4] == "swap" {
				info.OriginModelName = "swap_face"
				c.Request.URL.Path = "/mj/submit/swap-face"
			}
			if testCase.clamp {
				if testCase.name[:4] != "swap" {
					info.OriginModelName = "mj_overflow"
				}
			} else {
				info.BillingFXFactor = 1
			}
			response := testCase.run(c, info)
			require.NotNil(t, response)
			assert.Equal(t, 4, response.Code)
			if testCase.clamp {
				assert.Equal(t, "quota_not_enough", response.Description)
			}
			assert.Zero(t, upstreamCalls.Load())
			var user model.User
			require.NoError(t, database.First(&user, 4452).Error)
			assert.Equal(t, 4_000_000, user.Quota, "rejected admission must not debit funding")
			var logs int64
			require.NoError(t, database.Model(&model.Log{}).Count(&logs).Error)
			assert.Equal(t, int64(4), logs, "rejected admission must not add a diagnostic consume event")
		})
	}
}
