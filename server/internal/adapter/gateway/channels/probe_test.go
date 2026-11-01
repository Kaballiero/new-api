package channels

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/gateway/tokenusage"
	"github.com/QuantumNous/new-api/i18n"
	channeladapter "github.com/QuantumNous/new-api/internal/adapter/channels"
	"github.com/QuantumNous/new-api/internal/contracttest"
	"github.com/QuantumNous/new-api/internal/contracttest/fxseed"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	usagegateway "github.com/QuantumNous/new-api/relay/usage"
	"github.com/QuantumNous/new-api/relaykit/dto"
	kittypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type probeTransport func(*http.Request) (*http.Response, error)

func (transport probeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestOpenRouterProbeHasNoClientFinancialEffects(t *testing.T) {
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		previousDB, previousLogDB := model.DB, model.LOG_DB
		previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
		previousMaster, previousRedis := common.IsMasterNode, common.RedisEnabled
		t.Setenv("SQL_DSN", pgfixture.ScopedDSN(t, dsn))
		t.Setenv("LOG_SQL_DSN", "")
		common.IsMasterNode, common.RedisEnabled = true, false
		require.NoError(t, i18n.Init())
		gin.SetMode(gin.TestMode)
		require.NoError(t, model.InitDB())
		require.NoError(t, model.InitLogDB())
		db := model.DB
		t.Cleanup(func() {
			pool, err := db.DB()
			require.NoError(t, err)
			require.NoError(t, pool.Close())
			model.DB, model.LOG_DB = previousDB, previousLogDB
			common.SetDatabaseTypes(previousMain, previousLog)
			common.IsMasterNode, common.RedisEnabled = previousMaster, previousRedis
		})
		var version string
		require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
		t.Logf("database version: %s", version)
	} else {
		contracttest.New(t)
	}
	fxseed.SeedNativeRouteFX(t, model.DB)
	tokenusage.InitTokenEncoders()
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	previousRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	previousEnable, previousDisable := common.AutomaticEnableChannelEnabled, common.AutomaticDisableChannelEnabled
	common.AutomaticEnableChannelEnabled, common.AutomaticDisableChannelEnabled = true, true
	previousCodes := operation_setting.AutomaticDisableStatusCodeRanges
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 402, End: 402}}
	previousExport := common.DataExportEnabled
	common.DataExportEnabled = false
	t.Cleanup(func() {
		constant.StreamingTimeout = previousTimeout
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousRate
		common.AutomaticEnableChannelEnabled, common.AutomaticDisableChannelEnabled = previousEnable, previousDisable
		operation_setting.AutomaticDisableStatusCodeRanges = previousCodes
		common.DataExportEnabled = previousExport
	})
	user := model.User{Username: "operator-probe", Group: "default", Quota: 0, UsedQuota: 7, RequestCount: 3}
	require.NoError(t, model.DB.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "unused-client-token", RemainQuota: 23, UsedQuota: 11}
	require.NoError(t, model.DB.Create(&token).Error)
	channel := model.Channel{
		Type: constant.ChannelTypeOpenRouter, Name: "operator-probe", Key: "fake-upstream-key",
		BaseURL: common.GetPointer("https://probe.invalid"), Models: "public-model", Group: "default",
		ModelMapping:  common.GetPointer(`{"public-model":"test/model"}`),
		ParamOverride: common.GetPointer(`{"provider":{"only":["chosen"],"allow_fallbacks":false}}`),
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	var initialUser model.User
	var initialToken model.Token
	require.NoError(t, model.DB.First(&initialUser, user.Id).Error)
	require.NoError(t, model.DB.First(&initialToken, token.Id).Error)
	client, err := httpclient.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
	require.NoError(t, err)
	previousTransport := client.Transport
	t.Cleanup(func() { client.Transport = previousTransport })
	for _, tc := range []struct {
		name, cost string
		endpoint   string
		stream     bool
		status     int
		userQuota  int
		wantQuota  int
		wantError  bool
	}{
		{name: "normal", cost: `,"cost":0.0123`, status: 200, wantQuota: 6150},
		{name: "funded operator without client token", cost: `,"cost":0.0123`, status: 200, userQuota: 1000000, wantQuota: 6150},
		{name: "stream", cost: `,"cost":0.0123`, stream: true, status: 200, wantQuota: 6150},
		{name: "explicit zero", cost: `,"cost":0`, status: 200},
		{name: "stream explicit zero", cost: `,"cost":0`, stream: true, status: 200},
		{name: "missing cost", status: 200, wantError: true},
		{name: "invalid cost", cost: `,"cost":-1`, status: 200, wantError: true},
		{name: "stream missing cost", stream: true, status: 200, wantError: true},
		{name: "stream invalid cost", cost: `,"cost":null`, stream: true, status: 200, wantError: true},
		{name: "upstream credit exhausted", status: 402, wantError: true},
		{name: "rerank", endpoint: string(constant.EndpointTypeJinaRerank), cost: `,"cost":0.0123`, status: 200, wantQuota: 6150},
		{name: "rerank missing cost", endpoint: string(constant.EndpointTypeJinaRerank), status: 200, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, model.DB.Model(&user).Update("quota", tc.userQuota).Error)
			expectedUser := initialUser
			expectedUser.Quota = tc.userQuota
			probeChannel := channel
			if tc.endpoint != "" {
				probeChannel.Setting = common.GetPointer(`{"openrouter_model_reserve_usd":{"test/model":0.25}}`)
				probeChannel.ParamOverride = nil
			}
			gets, posts := 0, 0
			client.Transport = probeTransport(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, "probe.invalid", request.URL.Host)
				header := http.Header{"Content-Type": []string{"application/json"}}
				var body string
				if request.Method == http.MethodGet {
					gets++
					require.Equal(t, "/v1/models/test/model/endpoints", request.URL.Path)
					body = `{"data":{"id":"test/model","architecture":{"output_modalities":["text","rerank"]},"endpoints":[{"tag":"chosen","context_length":10000,"max_completion_tokens":100,"pricing":{"prompt":"0.001","completion":"0.002"}}]}}`
					return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
				}
				posts++
				require.Equal(t, http.MethodPost, request.Method)
				if tc.endpoint == string(constant.EndpointTypeJinaRerank) {
					require.Equal(t, "/v1/rerank", request.URL.Path)
				} else {
					require.Equal(t, "/v1/chat/completions", request.URL.Path)
				}
				assert.Nil(t, request.GetBody, "paid probes must not be replayable")
				payload, readErr := io.ReadAll(request.Body)
				require.NoError(t, readErr)
				assert.Contains(t, string(payload), `"model":"test/model"`)
				if tc.endpoint == "" {
					assert.Contains(t, string(payload), `"only":["chosen"]`)
					assert.Contains(t, string(payload), `"allow_fallbacks":false`)
				}
				if tc.status != 200 {
					body = `{"error":{"message":"Insufficient upstream credits","type":"insufficient_quota","code":"insufficient_quota"}}`
				} else if tc.endpoint == string(constant.EndpointTypeJinaRerank) {
					body = fmt.Sprintf(`{"id":"gen-probe","results":[{"index":0,"relevance_score":0.9}],"usage":{"prompt_tokens":5%s}}`, tc.cost)
				} else if tc.stream {
					header.Set("Content-Type", "text/event-stream")
					body = "data: {\"id\":\"gen-probe\",\"object\":\"chat.completion.chunk\",\"model\":\"test/model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
						fmt.Sprintf("data: {\"id\":\"gen-probe\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1,\"total_tokens\":6%s}}\n\ndata: [DONE]\n\n", tc.cost)
				} else {
					body = fmt.Sprintf(`{"id":"gen-probe","object":"chat.completion","model":"test/model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6%s}}`, tc.cost)
				}
				return &http.Response{StatusCode: tc.status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})
			var before int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&before).Error)
			result := testChannel(context.Background(), &probeChannel, user.Id, "public-model", tc.endpoint, tc.stream)
			require.Equal(t, 1, gets)
			require.Equal(t, 1, posts, "probe error: %v", result.localErr)
			require.NotNil(t, result.context)
			assert.Zero(t, result.context.GetInt("token_id"))
			if tc.wantError {
				require.Error(t, result.localErr)
				require.NotNil(t, result.newAPIError)
				if tc.status == 402 {
					assert.Equal(t, 402, result.newAPIError.StatusCode)
					assert.True(t, channeladapter.ShouldDisableChannel(result.newAPIError))
				}
			} else {
				require.NoError(t, result.localErr)
				require.Nil(t, result.newAPIError)
				assert.True(t, channeladapter.ShouldEnableChannel(result.newAPIError, constant.ChannelStatusAutoDisabled))
				var log model.Log
				require.NoError(t, model.LOG_DB.Last(&log).Error)
				assert.Equal(t, tc.wantQuota, log.Quota)
				assert.Zero(t, log.TokenId)
				assert.Equal(t, tc.stream, log.IsStream)
				var other map[string]any
				require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
				admin := other["admin_info"].(map[string]any)
				assert.Zero(t, admin["reserved_quota"])
				assert.Equal(t, float64(tc.wantQuota)/common.QuotaPerUnit, admin["upstream_cost_usd"])
			}
			var after int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&after).Error)
			if tc.wantError {
				assert.Equal(t, before, after, "unknown cost must not produce a zero-cost consume log")
			} else {
				assert.Equal(t, before+1, after)
			}
			var actualUser model.User
			var actualToken model.Token
			require.NoError(t, model.DB.First(&actualUser, user.Id).Error)
			require.NoError(t, model.DB.First(&actualToken, token.Id).Error)
			assert.Equal(t, expectedUser, actualUser)
			assert.Equal(t, initialToken, actualToken)
			var tasks int64
			require.NoError(t, model.DB.Model(&model.Task{}).Count(&tasks).Error)
			assert.Zero(t, tasks)
		})
	}
	t.Run("ordinary client still requires quota", func(t *testing.T) {
		posts := 0
		client.Transport = probeTransport(func(request *http.Request) (*http.Response, error) {
			if request.Method == http.MethodPost {
				posts++
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":{"id":"test/model","endpoints":[{"tag":"chosen","context_length":10000,"max_completion_tokens":100,"pricing":{"prompt":"0.001","completion":"0.002"}}]}}`)), Request: request}, nil
		})
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, OriginModelName: "test/model", RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: kittypes.RelayFormatOpenAI,
			ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: "https://probe.invalid", UpstreamModelName: "test/model"}}
		info.PriceData.BillingGroupRatio = 1
		adapter := &openai.Adaptor{}
		adapter.Init(info)
		_, err := adapter.DoRequest(ctx, info, strings.NewReader(`{"model":"test/model","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`))
		require.Error(t, err)
		assert.ErrorContains(t, err, "quota")
		assert.Zero(t, posts)
	})
}

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result, err := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.NoError(t, err)
	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestChannelTestRecordingPreservesTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	requestRules := []billingexpr.RequestRuleTrace{{
		Cond:       `param("service_tier") == "fast"`,
		Multiplier: 2,
		Matched:    true,
	}}
	facts := usagegateway.Facts(ctx, info)
	facts.Tiered = usagegateway.Tiered(info, &billingexpr.TieredResult{
		MatchedTier:  "base",
		RequestRules: requestRules,
	})
	require.NotNil(t, facts.Tiered)
	require.NotNil(t, facts.Tiered.Actual)
	assert.Equal(t, info.TieredBillingSnapshot.ExprString, facts.Tiered.ExprString)
	assert.Equal(t, "base", facts.Tiered.Actual.MatchedTier)
	assert.Equal(t, requestRules[0].Cond, facts.Tiered.Actual.RequestRules[0].Cond)
	assert.Equal(t, requestRules[0].Multiplier, facts.Tiered.Actual.RequestRules[0].Multiplier)
	assert.Equal(t, requestRules[0].Matched, facts.Tiered.Actual.RequestRules[0].Matched)
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := ResolveProbeUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}
