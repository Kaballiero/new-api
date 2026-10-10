package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay"
	jspluginadaptor "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaykittypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type taskSubmissionTestBilling struct {
	events    *[]string
	settleErr error
	onSettle  func()
	refunds   int
}

func (b *taskSubmissionTestBilling) Settle(int) error {
	*b.events = append(*b.events, "settle")
	if b.onSettle != nil {
		b.onSettle()
	}
	return b.settleErr
}

func (b *taskSubmissionTestBilling) Refund(*gin.Context) {
	*b.events = append(*b.events, "refund")
	b.refunds++
}

func (b *taskSubmissionTestBilling) NeedsRefund() bool        { return b.refunds == 0 }
func (b *taskSubmissionTestBilling) GetPreConsumedQuota() int { return 0 }
func (b *taskSubmissionTestBilling) Reserve(int) error {
	*b.events = append(*b.events, "reserve")
	return nil
}

func TestPresentTaskSubmissionUsesNativePresenterAfterPersistence(t *testing.T) {
	plugin, err := pluginruntime.CompilePlugin(`
export const meta = {apiVersion:1,key:"presenter-test",name:"Presenter",version:"1.0.0",author:{name:"Test"},models:["model"],fetchMode:"per_task",routes:[{method:"POST",path:"/vendor/jobs",type:"submit",decode:"decode",render:"created"}]};
export const native = {decode:function(ctx){return {kind:"submit",model:"model",requestBody:ctx.body.value};},created:function(ctx,task){return {data:{task_id:task.task_id},upstream:task.data};}};
export function buildSubmitRequest(){return {}} export function parseSubmitResponse(){return {taskId:"upstream"}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {status:"SUCCESS"}}
`, pluginruntime.Options{})
	require.NoError(t, err)
	priceData := types.PriceData{}
	priceData.AddOtherRatio("seconds", 5)
	task := &model.Task{TaskID: "task_public", SubmitTime: 123}
	task.SetData(map[string]any{"task_id": "upstream_private"})
	outcome := &taskSubmissionOutcome{
		Result:    &relay.TaskSubmitResult{},
		Task:      task,
		RelayInfo: &relaycommon.RelayInfo{PriceData: priceData},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/vendor/jobs", strings.NewReader(`{"model":"model"}`))
	c.Set(pluginruntime.ContextKeyPinnedRoute, pluginruntime.PinnedRoute{Plugin: plugin, Route: plugin.Meta.Routes[0]})
	c.Set(pluginruntime.ContextKeyRouteRequest, pluginruntime.RouteRequestContext{Path: "/vendor/jobs", Method: http.MethodPost, Body: map[string]any{"kind": "json", "value": map[string]any{"model": "model"}}})

	presentTaskSubmission(c, outcome)

	assert.JSONEq(t, `{
		"data":{"task_id":"task_public"},
		"upstream":{"task_id":"upstream_private"}
	}`, recorder.Body.String())
	assert.JSONEq(t, `{"seconds":5}`, recorder.Header().Get("X-Api-Other-Ratios"))
}

func TestPresentTaskSubmissionFallbackUsesPersistedPublicID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	outcome := &taskSubmissionOutcome{
		Result:    &relay.TaskSubmitResult{},
		Task:      &model.Task{TaskID: "task_persisted", SubmitTime: 456},
		RelayInfo: &relaycommon.RelayInfo{OriginModelName: "video-model"},
	}

	presentTaskSubmission(c, outcome)

	assert.JSONEq(t, `{
		"id":"task_persisted",
		"task_id":"task_persisted",
		"status":"queued",
		"model":"video-model",
		"created_at":456
	}`, recorder.Body.String())
}

func TestPresentTaskSubmissionUsesHostOpenAIVideoCreateReceipt(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{
		Protocol:  "openai_video",
		Operation: pluginruntime.HostProtocolOperation{Name: "create"},
	})
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		CreatedAt:  456,
		Properties: model.Properties{OriginModelName: "video-model"},
	}
	outcome := &taskSubmissionOutcome{Result: &relay.TaskSubmitResult{}, Task: task, RelayInfo: &relaycommon.RelayInfo{}}

	presentTaskSubmission(c, outcome)

	assert.JSONEq(t, `{"id":"task_public","object":"video","model":"video-model","status":"queued","progress":0,"created_at":456}`, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "task_id")
}

func TestExecuteTaskSubmissionRefundsWhenInsertFails(t *testing.T) {
	events := make([]string, 0, 3)
	database := setupTaskSubmissionDatabase(t, false, &events)
	_ = database
	billing := &taskSubmissionTestBilling{events: &events}
	c := taskSubmissionTestContext()
	info := taskSubmissionRelayInfo(billing)

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream_private",
			Platform:       constant.TaskPlatform("plugin"),
		}, nil
	})

	assert.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, "task_insert_failed", taskErr.Code)
	assert.Equal(t, []string{"reserve", "insert", "refund"}, events)
	assert.Equal(t, 1, billing.refunds)
	assert.False(t, c.Writer.Written())
}

func TestExecuteTaskSubmissionSettlementFailureStaysDurableAndWritesNothing(t *testing.T) {
	events := make([]string, 0, 3)
	database := setupTaskSubmissionDatabase(t, true, &events)
	billing := &taskSubmissionTestBilling{events: &events, settleErr: errors.New("settlement failed")}
	c := taskSubmissionTestContext()
	info := taskSubmissionRelayInfo(billing)

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream_private",
			Platform:       constant.TaskPlatform("plugin"),
		}, nil
	})

	assert.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, "task_billing_settlement_failed", taskErr.Code)
	assert.Equal(t, []string{"reserve", "insert", "settle"}, events)
	assert.Zero(t, billing.refunds)
	var count int64
	require.NoError(t, database.Model(&model.Task{}).Where("task_id = ?", "task_public").Count(&count).Error)
	assert.Equal(t, int64(1), count)
	assert.False(t, c.Writer.Written())
}

func TestExecuteTaskSubmissionPersistsPinnedPluginProvenance(t *testing.T) {
	events := make([]string, 0, 3)
	database := setupTaskSubmissionDatabase(t, true, &events)
	previousLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = previousLogConsumeEnabled })

	c := taskSubmissionTestContext()
	c.Set(common.RequestIdKey, "request-public")
	c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{
		Generation: &pluginruntime.RoutingGeneration{Number: 42},
		Plugin: &pluginruntime.LoadedPlugin{Meta: pluginruntime.Meta{
			Key:        "document-parser",
			Name:       "Document Parser",
			Version:    "1.2.3",
			APIVersion: 1,
			Author: pluginruntime.AuthorMeta{
				Name: "Community Author",
				URL:  "https://plugins.example/author",
			},
		}},
	})
	billing := &taskSubmissionTestBilling{events: &events}
	info := taskSubmissionRelayInfo(billing)

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-private",
			Platform:       constant.TaskPlatform("document-parser"),
		}, nil
	})

	require.Nil(t, taskErr)
	require.NotNil(t, outcome)
	require.NotNil(t, outcome.Task.PrivateData.Execution)
	require.NotNil(t, outcome.Task.PrivateData.Execution.TaskPlugin)
	assert.Equal(t, "request-public", outcome.Task.PrivateData.Execution.RequestID)
	assert.Equal(t, "/plugin/submit", outcome.Task.PrivateData.Execution.RequestPath)
	assert.Equal(t, "1.2.3", outcome.Task.PrivateData.Execution.TaskPlugin.Version)
	assert.Equal(t, uint64(42), outcome.Task.PrivateData.Execution.TaskPlugin.Generation)
	require.NotNil(t, outcome.Task.PrivateData.Execution.TaskPlugin.Author)
	assert.Equal(t, "Community Author", outcome.Task.PrivateData.Execution.TaskPlugin.Author.Name)
	assert.Equal(t, "https://plugins.example/author", outcome.Task.PrivateData.Execution.TaskPlugin.Author.URL)

	var stored model.Task
	require.NoError(t, database.Where("task_id = ?", "task_public").First(&stored).Error)
	require.NotNil(t, stored.PrivateData.Execution)
	require.NotNil(t, stored.PrivateData.Execution.TaskPlugin)
	assert.Equal(t, "document-parser", stored.PrivateData.Execution.TaskPlugin.Key)
	require.NotNil(t, stored.PrivateData.Execution.TaskPlugin.Author)
	assert.Equal(t, "Community Author", stored.PrivateData.Execution.TaskPlugin.Author.Name)
	assert.Equal(t, "upstream-private", stored.PrivateData.UpstreamTaskID)
}

func TestExecuteTaskSubmissionPersistsAcceptedBillingFXContext(t *testing.T) {
	events := make([]string, 0, 3)
	database := setupTaskSubmissionDatabase(t, true, &events)
	previousLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = previousLogConsumeEnabled })

	billing := &taskSubmissionTestBilling{events: &events}
	info := taskSubmissionRelayInfo(billing)
	info.BillingFXRate = 90
	info.BillingFXFactor = 0.9
	info.PriceData = types.PriceData{
		ModelPrice:        2,
		GroupRatioInfo:    types.GroupRatioInfo{GroupRatio: 1.45, GroupSpecialRatio: 1.45, HasSpecialRatio: true},
		BillingGroupRatio: 1.305,
	}

	outcome, taskErr := executeTaskSubmissionWith(taskSubmissionTestContext(), info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		return &relay.TaskSubmitResult{UpstreamTaskID: "upstream-private", Platform: constant.TaskPlatform("plugin")}, nil
	})
	require.Nil(t, taskErr)
	require.NotNil(t, outcome)

	var stored model.Task
	require.NoError(t, database.Where("task_id = ?", "task_public").First(&stored).Error)
	context := stored.PrivateData.BillingContext
	require.NotNil(t, context)
	require.NotNil(t, context.BillingFX)
	assert.Equal(t, 90.0, context.BillingFX.Rate)
	assert.Equal(t, 0.9, context.BillingFX.Rate/100)
	assert.Equal(t, 1.305, context.GroupRatio)
	require.NotNil(t, context.SubmitGroup)
	assert.Equal(t, 1.45, context.SubmitGroup.PureRatio)
	assert.True(t, context.SubmitGroup.HasSpecialRatio)
	require.NotNil(t, context.SubmitGroup.SpecialRatio)
	assert.Equal(t, 1.45, *context.SubmitGroup.SpecialRatio)
}

func TestResponsesPricingAdmissionUsesExistingFallbackPresenter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := setupRoutingRetryTestDatabase(t, "default", 1)
	seedNativeRouteFX(t, database)
	previousModelRatios := ratio_setting.ModelRatio2JSONString()
	previousModelPrices := ratio_setting.ModelPrice2JSONString()
	previousCustomRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"kling-v1":1e20}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"responses-pricing":1e20}`))
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	t.Cleanup(func() {
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousCustomRate
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousModelPrices))
	})
	_, err := pluginruntime.DefaultRegistry.Register(`
export const meta = {apiVersion:1,key:"responses-pricing",name:"Responses Pricing",version:"1.0.0",author:{name:"Test"},models:["responses-pricing"],fetchMode:"per_task",protocols:[{name:"openai_responses",supports:["sync","stream"]}]};
export function buildSubmitRequest(ctx){ return {url:ctx.baseUrl+"/submit",method:"POST",body:{model:ctx.model},action:"text_to_video"}; }
export function parseSubmitResponse(){ return {taskId:"private"}; }
export function buildQueryRequest(){ return {url:"https://provider.invalid"}; }
export function parseTaskResult(){ return {status:"SUCCESS"}; }
export const protocols = {openai_responses:{decodeRequest:function(ctx){ctx.requestBody=ctx.body.value; return {kind:"submit",model:ctx.body.value.model,action:"text_to_video"};},renderEvents:function(){return {events:[],done:false};},renderFinal:function(){return {};}}};
`, pluginruntime.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pluginruntime.DefaultRegistry.Unregister("responses-pricing")) })
	channelSetting := `{"task_plugin_key":"responses-pricing"}`
	baseURL := "http://127.0.0.1"
	channel := &model.Channel{Id: 4453, Type: constant.ChannelTypeTaskPlugin, Name: "responses-pricing", Key: "sk-test", BaseURL: &baseURL, Status: common.ChannelStatusEnabled, Models: "responses-pricing", Group: "default", Setting: &channelSetting}

	for _, testCase := range []struct {
		name, model, body, message string
		claimed                    bool
		customRate                 float64
		wantStatus                 int
	}{
		{name: "unclaimed clamp", model: "kling-v1", body: `{"model":"kling-v1","input":"x"}`, message: "insufficient quota", wantStatus: http.StatusForbidden, customRate: 100},
		{name: "unclaimed FX configuration", model: "kling-v1", body: `{"model":"kling-v1","input":"x"}`, message: "model cost accounting configuration is invalid", wantStatus: http.StatusServiceUnavailable, customRate: 1},
		{name: "claimed JSON clamp", model: "responses-pricing", body: `{"model":"responses-pricing","prompt":"x"}`, message: "Task protocol request was denied", claimed: true, wantStatus: http.StatusForbidden, customRate: 100},
		{name: "claimed stream FX configuration", model: "responses-pricing", body: `{"model":"responses-pricing","prompt":"x","stream":true}`, message: "Task protocol request failed", claimed: true, wantStatus: http.StatusServiceUnavailable, customRate: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = testCase.customRate
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(testCase.body))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyUserId, 4451)
			common.SetContextKey(c, constant.ContextKeyTokenId, 4451)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUserQuota, 1000)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, testCase.model)

			if !testCase.claimed {
				RelayTaskPluginEndpoint(c, func(c *gin.Context) { Relay(c, relaykittypes.RelayFormatOpenAIResponses) })
			} else {
				router := gin.New()
				router.POST("/v1/responses", func(c *gin.Context) {
					common.SetContextKey(c, constant.ContextKeyUserId, 4451)
					common.SetContextKey(c, constant.ContextKeyTokenId, 4451)
					common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
					common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
					common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
					common.SetContextKey(c, constant.ContextKeyUserQuota, 1000)
					common.SetContextKey(c, constant.ContextKeyOriginalModel, testCase.model)
				}, middleware.PinTaskPluginEndpoint(), middleware.PrepareTaskPluginEndpoint(), func(c *gin.Context) {
					require.Nil(t, middleware.SetupContextForSelectedChannel(c, channel, testCase.model))
					RelayTaskPluginEndpoint(c, func(c *gin.Context) { Relay(c, relaykittypes.RelayFormatOpenAIResponses) })
				})
				router.ServeHTTP(recorder, c.Request)
			}

			require.Equal(t, testCase.wantStatus, recorder.Code)
			assert.Contains(t, recorder.Body.String(), testCase.message)
			assert.NotContains(t, recorder.Body.String(), "QuotaRound")
			assert.NotContains(t, recorder.Body.String(), "1e20")
			assert.Empty(t, c.GetStringSlice("use_channel"))
		})
	}
}

func TestExecuteTaskSubmissionRefundsCancellationBeforeDurableBarrier(t *testing.T) {
	events := make([]string, 0, 2)
	setupTaskSubmissionDatabase(t, true, &events)
	billing := &taskSubmissionTestBilling{events: &events}
	c := taskSubmissionTestContext()
	requestContext, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(requestContext)
	info := taskSubmissionRelayInfo(billing)

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		cancel()
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream_private",
			Platform:       constant.TaskPlatform("plugin"),
		}, nil
	})

	assert.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, "request_cancelled", taskErr.Code)
	assert.Equal(t, []string{"refund"}, events)
	assert.Equal(t, 1, billing.refunds)
	assert.False(t, c.Writer.Written())
}

func TestExecuteTaskSubmissionAmbiguousOpenRouterAttemptUsesStandardRetryAndRefund(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(fmt.Sprintf("disconnect=%t", disconnect), func(t *testing.T) {
			events := make([]string, 0)
			setupTaskSubmissionDatabase(t, true, &events)
			billing := &taskSubmissionTestBilling{events: &events}
			c := taskSubmissionTestContext()
			c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: &pluginruntime.LoadedPlugin{Meta: pluginruntime.Meta{Key: "openrouter-video"}}})
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			previousRetries := common.RetryTimes
			common.RetryTimes = 3
			t.Cleanup(func() { common.RetryTimes = previousRetries })
			attempts := 0
			outcome, taskErr := executeTaskSubmissionWith(c, taskSubmissionRelayInfo(billing), func(c *gin.Context, _ *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
				attempts++
				if disconnect {
					cancel()
				}
				return nil, service.TaskErrorWrapper(errors.New("unknown paid submission outcome"), "do_request_failed", http.StatusBadGateway)
			})
			assert.Nil(t, outcome)
			require.NotNil(t, taskErr)
			expectedAttempts := 4
			if disconnect {
				expectedAttempts = 1
			}
			assert.Equal(t, expectedAttempts, attempts)
			assert.Equal(t, 1, billing.refunds)
			assert.Equal(t, []string{"refund"}, events)
			var taskCount int64
			require.NoError(t, model.DB.Model(&model.Task{}).Count(&taskCount).Error)
			assert.Zero(t, taskCount)
		})
	}
}

func TestExecuteTaskSubmissionDisconnectBeforeUpstreamAcceptanceSkipsSubmitAndRefunds(t *testing.T) {
	events := make([]string, 0, 1)
	setupTaskSubmissionDatabase(t, true, &events)
	billing := &taskSubmissionTestBilling{events: &events}
	c := taskSubmissionTestContext()
	requestContext, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(requestContext)
	info := taskSubmissionRelayInfo(billing)
	submitted := false

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		submitted = true
		return nil, nil
	})

	assert.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, "request_cancelled", taskErr.Code)
	assert.False(t, submitted)
	assert.Equal(t, []string{"refund"}, events)
	assert.Equal(t, 1, billing.refunds)
	assert.False(t, c.Writer.Written())
}

func TestExecuteTaskSubmissionCallerCancellationDuringSubmitRefundsBeforeDurableBarrier(t *testing.T) {
	events := make([]string, 0, 1)
	setupTaskSubmissionDatabase(t, true, &events)
	billing := &taskSubmissionTestBilling{events: &events}
	c := taskSubmissionTestContext()
	requestContext, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(requestContext)
	info := taskSubmissionRelayInfo(billing)
	submitStarted := make(chan struct{})
	done := make(chan struct{})
	var outcome *taskSubmissionOutcome
	var taskErr *dto.TaskError

	go func() {
		defer close(done)
		outcome, taskErr = executeTaskSubmissionWith(c, info, func(c *gin.Context, _ *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
			close(submitStarted)
			<-c.Request.Context().Done()
			return nil, service.TaskErrorWrapperLocal(c.Request.Context().Err(), "do_request_failed", http.StatusInternalServerError)
		})
	}()
	select {
	case <-submitStarted:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "submission did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "submission did not stop after disconnect")
	}

	assert.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, "request_cancelled", taskErr.Code)
	assert.Equal(t, []string{"refund"}, events)
	assert.Equal(t, 1, billing.refunds)
	assert.False(t, c.Writer.Written())
}

func TestExecuteTaskSubmissionDisconnectAfterDurableInsertDoesNotRefund(t *testing.T) {
	events := make([]string, 0, 3)
	database := setupTaskSubmissionDatabase(t, true, &events)
	previousLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = previousLogConsumeEnabled })
	c := taskSubmissionTestContext()
	requestContext, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(requestContext)
	billing := &taskSubmissionTestBilling{
		events:   &events,
		onSettle: cancel,
	}
	info := taskSubmissionRelayInfo(billing)

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream_private",
			Platform:       constant.TaskPlatform("plugin"),
		}, nil
	})

	require.Nil(t, taskErr)
	require.NotNil(t, outcome)
	assert.Equal(t, "task_public", outcome.Task.TaskID)
	assert.Equal(t, []string{"reserve", "insert", "settle"}, events)
	assert.Zero(t, billing.refunds)
	var count int64
	require.NoError(t, database.Model(&model.Task{}).Where("task_id = ?", "task_public").Count(&count).Error)
	assert.Equal(t, int64(1), count)
	assert.False(t, c.Writer.Written())
}

func setupTaskSubmissionDatabase(t *testing.T, migrate bool, events *[]string) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Callback().Create().Before("gorm:create").Register("test:task-submit-order", func(*gorm.DB) {
		*events = append(*events, "insert")
	}))
	if migrate {
		require.NoError(t, database.AutoMigrate(&model.Task{}))
	}
	model.DB = database
	t.Cleanup(func() { model.DB = previousDB })
	return database
}

func taskSubmissionTestContext() *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/plugin/submit", strings.NewReader(`{}`))
	return c
}

func taskSubmissionRelayInfo(billing relaycommon.BillingSettler) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          1,
		UsingGroup:      "default",
		OriginModelName: "plugin-model",
		Billing:         billing,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			PublicTaskID:  "task_public",
			LockedChannel: &model.Channel{Id: 1, Type: constant.ChannelTypeTaskPlugin, Name: "plugin"},
		},
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1, ChannelType: constant.ChannelTypeTaskPlugin},
	}
}

func TestExecuteOpenRouterSubmissionAccepts202BeforePersistingTask(t *testing.T) {
	events := []string{}
	database := setupTaskSubmissionDatabase(t, true, &events)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Channel{}, &model.ModelCostFXRow{}))
	require.NoError(t, database.Create(&model.User{Id: 1, Username: "openrouter-submitter", AffCode: "openrouter-submitter", Quota: 10000}).Error)
	require.NoError(t, database.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenRouter, Key: "accepted-provider-key"}).Error)
	logDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, logDB.AutoMigrate(&model.Log{}))
	previousLogDB, previousRedis, previousBatch, previousLogEnabled := model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = logDB, false, false, false
	previousRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	t.Cleanup(func() {
		model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousLogDB, previousRedis, previousBatch, previousLogEnabled
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousRate
	})
	current, _ := model.CurrentModelCostFX("cbr-xml-daily.ru")
	version := max(current.Version+1, 1)
	require.NoError(t, database.Create(&model.ModelCostFXRow{Source: "cbr-xml-daily.ru", Version: version, EffectiveAt: time.Now().Unix(), FetchedAt: time.Now().Unix(), RatesJSON: `{"USD":100}`}).Error)
	require.NoError(t, model.LoadModelCostFX(t.Context(), "cbr-xml-daily.ru"))
	savedConfig := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { savedConfig[key] = value; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig)) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":    `{"google/veo-3.1-lite":"tiered_expr"}`,
		"billing_setting.billing_expr":    `{"google/veo-3.1-lite":"tier(\"reserve\", u(\"requested_seconds\") * 0.000025)"}`,
		"group_ratio_setting.group_ratio": `{"default":1}`, "group_ratio_setting.group_group_ratio": `{}`,
	}))
	source, err := plugins.Source("openrouter-video")
	require.NoError(t, err)
	stamp := regexp.MustCompile(`"fetched_at": "([^"]+)"`).FindStringSubmatch(source)
	require.Len(t, stamp, 2)
	fetchedAt, err := time.Parse(time.RFC3339Nano, stamp[1])
	require.NoError(t, err)
	plugin, err := pluginruntime.NewRegistry().RegisterFactory(source, pluginruntime.Options{Key: "openrouter-video", Now: func() time.Time { return fetchedAt }})
	require.NoError(t, err)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "/v1/videos", r.URL.Path)
		assert.Equal(t, "Bearer accepted-provider-key", r.Header.Get("Authorization"))
		var count int64
		require.NoError(t, database.Model(&model.Task{}).Count(&count).Error)
		assert.Zero(t, count, "no task is persisted before a successful submission")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"accepted-job","status":"pending"}`))
	}))
	t.Cleanup(server.Close)
	service.InitHttpClient()
	c := taskSubmissionTestContext()
	c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: plugin})
	c.Set("group", "default")
	c.Set("task_request", map[string]any{"prompt": "ocean waves", "seconds": 4, "metadata": map[string]any{"resolution": "720p"}})
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenRouter)
	common.SetContextKey(c, constant.ContextKeyChannelId, 1)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "accepted-provider-key")
	billing := &taskSubmissionTestBilling{events: &events}
	info := taskSubmissionRelayInfo(billing)
	info.OriginModelName = "google/veo-3.1-lite"
	info.UserGroup = "default"
	info.BillingSource = service.BillingSourceWallet
	billing.onSettle = func() {
		var saved model.Task
		require.NoError(t, database.Where("task_id = ?", info.PublicTaskID).First(&saved).Error)
		require.NotNil(t, saved.PrivateData.Reconciliation)
		assert.True(t, saved.PrivateData.Reconciliation.ReservationPending)
	}
	events = nil
	outcome, taskErr := executeTaskSubmissionWith(c, info, relay.RelayTaskSubmit)
	require.Nil(t, taskErr)
	require.NotNil(t, outcome)
	assert.Equal(t, 1, calls)
	assert.Equal(t, []string{"reserve", "insert", "settle"}, events)
	assert.Zero(t, billing.refunds)
	var saved model.Task
	require.NoError(t, database.First(&saved, outcome.Task.ID).Error)
	assert.Equal(t, constant.TaskPlatform("openrouter-video"), saved.Platform)
	assert.Equal(t, "accepted-job", saved.GetUpstreamTaskID())
	assert.Equal(t, "accepted-provider-key", saved.PrivateData.Key)
	assert.Equal(t, model.TaskSettlementOpenRouterCostV1, saved.PrivateData.BillingContext.SettlementMode)
	assert.Equal(t, 50, saved.PrivateData.Reconciliation.ReservedQuota)
	assert.False(t, saved.PrivateData.Reconciliation.ReservationPending)
	assert.False(t, saved.PrivateData.Reconciliation.SubmissionPending)
	assert.False(t, saved.PrivateData.Reconciliation.Required)
	var logs []model.Log
	require.NoError(t, logDB.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, 50, logs[0].Quota)
	adaptor := jspluginadaptor.New(plugin)
	for _, tc := range []struct {
		usage     string
		confirmed bool
	}{
		{`{"cost":0.01,"is_byok":false}`, true}, {`{"cost":0.01,"is_byok":true}`, false}, {`{"cost":0.01}`, false},
	} {
		result, err := adaptor.ParseTaskResult(&saved, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, []byte(`{"id":"accepted-job","status":"completed","usage":`+tc.usage+`}`))
		require.NoError(t, err)
		require.NotNil(t, result.UpstreamCostUSD)
		assert.Equal(t, 0.01, *result.UpstreamCostUSD)
		assert.Equal(t, tc.confirmed, result.UpstreamCostConfirmed)
	}
}
