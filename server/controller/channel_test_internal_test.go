package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateChannelProxy(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr bool
	}{
		{name: "empty"},
		{name: "http", proxy: "http://proxy.example:8080"},
		{name: "https", proxy: "https://proxy.example:8443"},
		{name: "socks5", proxy: "socks5://proxy.example"},
		{name: "socks5h", proxy: "socks5h://proxy.example:1080/"},
		{name: "unsupported", proxy: "ftp://proxy.example", wantErr: true},
		{name: "path", proxy: "socks5://proxy.example:1080/path", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting, err := common.Marshal(dto.ChannelSettings{Proxy: test.proxy})
			require.NoError(t, err)
			channel := &model.Channel{
				Type:    constant.ChannelTypeOpenAI,
				Setting: common.GetPointer(string(setting)),
			}

			err = validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "invalid channel proxy")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateChannelRequiresNewAPIBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL *string
		wantErr bool
	}{
		{name: "missing", wantErr: true},
		{name: "blank", baseURL: common.GetPointer("  "), wantErr: true},
		{name: "configured", baseURL: common.GetPointer("https://new-api.example")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{
				Type:    constant.ChannelTypeNewAPI,
				BaseURL: test.baseURL,
			}

			err := validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "New API channel base URL cannot be empty")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNewAPIChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeNewAPI)

	require.True(t, ok)
	assert.Equal(t, constant.APITypeNewAPI, apiType)
	assert.Equal(t, "New API", constant.GetChannelTypeName(constant.ChannelTypeNewAPI))
	require.Greater(t, len(constant.ChannelBaseURLs), constant.ChannelTypeNewAPI)
	assert.Empty(t, constant.ChannelBaseURLs[constant.ChannelTypeNewAPI])
}

func TestResponsesCompactChannelSupport(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		apiType     int
		want        bool
	}{
		{name: "OpenAI", channelType: constant.ChannelTypeOpenAI, apiType: constant.APITypeOpenAI, want: true},
		{name: "Azure", channelType: constant.ChannelTypeAzure, apiType: constant.APITypeOpenAI, want: true},
		{name: "Codex", channelType: constant.ChannelTypeCodex, apiType: constant.APITypeCodex, want: true},
		{name: "Advanced Custom", channelType: constant.ChannelTypeAdvancedCustom, apiType: constant.APITypeAdvancedCustom, want: true},
		{name: "Sub2API", channelType: constant.ChannelTypeSub2API, apiType: constant.APITypeSub2API, want: true},
		{name: "New API", channelType: constant.ChannelTypeNewAPI, apiType: constant.APITypeNewAPI, want: true},
		{name: "Anthropic", channelType: constant.ChannelTypeAnthropic, apiType: constant.APITypeAnthropic, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, common.SupportsResponsesCompact(test.channelType, test.apiType))
		})
	}
}

func TestMultiprotocolGatewayEndpointTypes(t *testing.T) {
	want := []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAIResponseCompact,
		constant.EndpointTypeAnthropic,
		constant.EndpointTypeGemini,
		constant.EndpointTypeOpenAIAlphaSearch,
	}

	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeNewAPI, "gpt-5"))
	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeSub2API, "gpt-5"))
}

func TestCopyChannelRejectsInvalidLegacyProxySettings(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	settingBytes, err := common.Marshal(dto.ChannelSettings{
		Proxy: "socks5://proxy.example/legacy-path",
	})
	require.NoError(t, err)
	setting := string(settingBytes)
	origin := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Name:    "legacy proxy channel",
		Key:     "test-key",
		Models:  "gpt-test",
		Group:   "default",
		Setting: &setting,
	}
	require.NoError(t, db.Create(origin).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", origin.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy", nil)

	CopyChannel(ctx)

	assert.Contains(t, recorder.Body.String(), "invalid channel settings")
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
	assert.Equal(t, int64(1), channelCount)
}

func TestDeleteChannelResetsProxyCacheWhenPreReadFails(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	service.ResetProxyClientCache()
	t.Cleanup(service.ResetProxyClientCache)

	proxyURL := "http://proxy.example:8080"
	beforeDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "999999"}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/999999", nil)

	DeleteChannel(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":true`)
	afterDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)
	assert.NotSame(t, beforeDelete, afterDelete)
}

func TestDeleteChannelBatchReportsAndAuditsActualDeletedCount(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	channel := &model.Channel{Name: "existing", Key: "test-key"}
	require.NoError(t, db.Create(channel).Error)

	requestBody, err := common.Marshal(ChannelBatch{Ids: []int{channel.Id, 999999}})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/batch", bytes.NewReader(requestBody))
	ctx.Request.Header.Set("Content-Type", "application/json")

	DeleteChannelBatch(ctx)

	var response struct {
		Success bool  `json:"success"`
		Data    int64 `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, int64(1), response.Data)

	var auditLog model.AuditLog
	require.NoError(t, db.Order("id desc").First(&auditLog).Error)
	var auditData struct {
		Operation struct {
			Params map[string]any `json:"params"`
		} `json:"op"`
	}
	encodedAudit, err := common.Marshal(auditLog.Other)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(encodedAudit, &auditData))
	assert.Equal(t, float64(1), auditData.Operation.Params["count"])
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

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	requestRules := []billingexpr.RequestRuleTrace{{
		Cond:       `param("service_tier") == "fast"`,
		Multiplier: 2,
		Matched:    true,
	}}
	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier:  "base",
		RequestRules: requestRules,
	})

	fields := other.Snapshot()
	require.Equal(t, "tiered_expr", fields["billing_mode"])
	require.Equal(t, "base", fields["matched_tier"])
	require.Equal(t, requestRules, fields["request_rules"])
	require.NotEmpty(t, fields["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Id)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 2, selected[1].Id)
}

func TestSelectChannelsForAutomaticTestAutoBanOnlyUsesEligibleChannels(t *testing.T) {
	autoBanEnabled := 1
	autoBanDisabled := 0
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled, AutoBan: &autoBanEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled, AutoBan: &autoBanDisabled},
		{Id: 3, Status: common.ChannelStatusAutoDisabled, AutoBan: &autoBanEnabled},
		{Id: 4, Status: common.ChannelStatusManuallyDisabled, AutoBan: &autoBanEnabled},
		{Id: 5, Status: common.ChannelStatusEnabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeAutoBanOnly)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 3, selected[1].Id)
}

func TestRunChannelTestWorkersHonorsConfiguredConcurrency(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusEnabled},
	}
	started := make(chan struct{}, len(channels))
	release := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	progress := make([]int, 0, len(channels)+1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			context.Background(),
			channels,
			2,
			func(_ context.Context, _ *model.Channel) channelTestSummary {
				current := active.Add(1)
				defer active.Add(-1)
				for {
					observed := maxActive.Load()
					if current <= observed || maxActive.CompareAndSwap(observed, current) {
						break
					}
				}
				started <- struct{}{}
				<-release
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	select {
	case <-started:
		t.Fatal("started more channel tests than the configured concurrency")
	default:
	}
	close(release)

	summary := <-summaryResult

	assert.Equal(t, int32(2), maxActive.Load())
	assert.Equal(t, channelTestSummary{Tested: 4, Succeeded: 4}, summary)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, progress)
}

func TestRunChannelTestWorkersStopsAfterCancellation(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	ctx, cancel := context.WithCancel(context.Background())
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusEnabled},
	}
	started := make(chan struct{}, len(channels))
	progress := make([]int, 0, 1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			ctx,
			channels,
			2,
			func(ctx context.Context, _ *model.Channel) channelTestSummary {
				started <- struct{}{}
				<-ctx.Done()
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	cancel()

	summary := <-summaryResult

	select {
	case <-started:
		t.Fatal("started another channel test after cancellation")
	default:
	}
	assert.Equal(t, channelTestSummary{Tested: 2, Succeeded: 2}, summary)
	assert.Equal(t, []int{0}, progress)
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)
	ctx.Request.Header.Set("Accept-Language", "zh-CN")
	require.NoError(t, i18n.Init())

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}

func TestChannelProbePreservesUnsupportedResponses(t *testing.T) {
	user, _ := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}))
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory })
	channel := &model.Channel{Type: constant.ChannelTypeTaskPlugin, Name: "unsupported-probe", Key: "key", Models: "model"}
	require.NoError(t, model.DB.Create(channel).Error)
	t.Run("manual", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("id", user.Id)
		ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprint(channel.Id)}}
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/test/"+fmt.Sprint(channel.Id), nil)
		require.NotPanics(t, func() { TestChannel(ctx) })
		assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
		var response struct {
			Success bool
			Code    string
			Time    float64
			Data    ChannelTestResponse
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		assert.False(t, response.Success)
		assert.Equal(t, "channel_test_failed", response.Code)
		assert.Zero(t, response.Time)
		assert.Zero(t, response.Data.Time)
	})
	t.Run("validation", func(t *testing.T) {
		body, err := common.Marshal(ValidateModelsRequest{ChannelId: channel.Id, Models: []string{"model"}})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("id", user.Id)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/validate_models", strings.NewReader(string(body)))
		ctx.Request.Header.Set("Content-Type", "application/json")
		require.NotPanics(t, func() { ValidateModels(ctx) })
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool
			Data    ValidateModelsResponse
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		require.Len(t, response.Data.Results, 1)
		assert.Equal(t, "uncertain", response.Data.Results[0].Status)
		assert.Equal(t, 1, response.Data.Summary.Uncertain)
	})
}

func TestChannelProbeTimingPreservesRelayOwnedCacheMutations(t *testing.T) {
	setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Name: "polling-probe", Key: "first\nsecond", Models: "gpt-4o", Group: "default", Status: common.ChannelStatusEnabled, ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling}}
	require.NoError(t, channel.Insert())
	model.InitChannelCache()
	t.Cleanup(func() {
		require.NoError(t, model.DB.Delete(channel).Error)
		model.InitChannelCache()
		common.MemoryCacheEnabled = previousMemory
	})
	native, err := model.CacheGetChannel(channel.Id)
	require.NoError(t, err)
	native.ChannelInfo.MultiKeyPollingIndex = 0
	native.Setting = common.GetPointer("{")
	native.OtherSettings = "{"
	store := channelstore.NewStore()
	snapshot, err := store.Cached(channel.Id)
	require.NoError(t, err)
	key, index, apiErr := native.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "first", key)
	assert.Zero(t, index)
	native.GetSetting()
	native.GetOtherSettings()
	store.SaveResponseTime(snapshot, 123)
	cached, err := model.CacheGetChannel(channel.Id)
	require.NoError(t, err)
	assert.Same(t, native, cached)
	assert.Equal(t, 1, cached.ChannelInfo.MultiKeyPollingIndex)
	assert.Nil(t, cached.Setting)
	assert.Equal(t, "{}", cached.OtherSettings)
	key, index, apiErr = cached.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "second", key)
	assert.Equal(t, 1, index)
	var persisted model.Channel
	require.NoError(t, model.DB.First(&persisted, channel.Id).Error)
	assert.Equal(t, 123, persisted.ResponseTime)
}

func channelMatrixDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLog := model.DB, model.LOG_DB
	previousMain, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousSQLite := common.IsMasterNode, common.SQLitePath
	previousMemory, previousRedis := common.MemoryCacheEnabled, common.RedisEnabled
	dialect := os.Getenv("TEST_CHANNEL_DIALECT")
	if dialect == "" {
		dialect = "sqlite"
	}
	dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
	if os.Getenv("TEST_CHANNEL_UPGRADE_PHASE") == "" {
		initial, isolated := newAuditTestDatabase(t, dialect, dsn)
		dsn = isolated
		connection, err := initial.DB()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	} else if dialect == "sqlite" {
		dsn = os.Getenv("TEST_CHANNEL_UPGRADE_DSN")
		require.NotEmpty(t, dsn)
	}
	if dialect == "sqlite" {
		common.SQLitePath = dsn
		dsn = "local"
	}
	t.Setenv("SQL_DSN", dsn)
	t.Setenv("LOG_SQL_DSN", "")
	common.IsMasterNode = false
	common.MemoryCacheEnabled = false
	common.RedisEnabled = false
	require.NoError(t, model.InitDB())
	db := model.DB
	model.LOG_DB = db
	t.Cleanup(func() {
		connection, err := db.DB()
		if err == nil {
			require.NoError(t, connection.Close())
		}
		model.DB, model.LOG_DB = previousDB, previousLog
		common.SetDatabaseTypes(previousMain, previousLogType)
		common.IsMasterNode, common.SQLitePath = previousMaster, previousSQLite
		common.MemoryCacheEnabled, common.RedisEnabled = previousMemory, previousRedis
	})
	var version string
	query := "SELECT VERSION()"
	if dialect == "sqlite" {
		query = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("channels database: %s %s", dialect, version)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}, &model.Log{}, &model.AuditLog{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	}
	return db
}

func channelMatrixRequest(t *testing.T, userID int, method, path string, body any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := common.Marshal(body)
	require.NoError(t, err)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Set("id", userID)
	ctx.Set("role", common.RoleRootUser)
	ctx.Set("username", "channel-matrix-owner")
	ctx.Request = httptest.NewRequest(method, path, bytes.NewReader(encoded))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if id := ctx.Request.URL.Query().Get("channel_id"); id != "" {
		ctx.Params = gin.Params{{Key: "id", Value: id}}
	}
	handler(ctx)
	return response
}

func TestChannelsNativeCompatibility(t *testing.T) {
	db := channelMatrixDatabase(t)
	user := &model.User{Username: "channel-matrix-owner", AffCode: "channel-matrix-owner", Group: "default", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, db.Create(user).Error)
	tag := "native-tag"
	base := "https://unused.example"
	request := AddChannelRequest{Mode: "batch", BatchAddSetKeyPrefix2Name: true, Channel: &model.Channel{Type: constant.ChannelTypeOpenAI, Name: "native", Key: "one\ntwo", Models: "gpt-4o", Group: "default", BaseURL: &base, Tag: &tag}}
	response := channelMatrixRequest(t, user.Id, http.MethodPost, "/api/channel", request, AddChannel)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var channels []model.Channel
	require.NoError(t, db.Order("id asc").Find(&channels).Error)
	require.Len(t, channels, 2)
	assert.Equal(t, "native one", channels[0].Name)
	assert.Equal(t, "native one two", channels[1].Name)
	id := channels[0].Id
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Updates(map[string]any{"used_quota": 123, "balance": 37.5, "created_time": 42}).Error)
	response = channelMatrixRequest(t, user.Id, http.MethodPut, "/api/channel", map[string]any{"id": id, "name": "edited", "priority": 9, "models": "gpt-4o,gpt-4o-mini", "used_quota": 999, "balance": 0, "created_time": 99}, UpdateChannel)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result struct {
		Success bool
		Data    model.Channel
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success, response.Body.String())
	assert.Empty(t, result.Data.Key)
	current, err := model.GetChannelById(id, true)
	require.NoError(t, err)
	assert.Equal(t, "one", current.Key)
	assert.EqualValues(t, 123, current.UsedQuota)
	assert.Equal(t, 37.5, current.Balance)
	assert.EqualValues(t, 42, current.CreatedTime)
	response = channelMatrixRequest(t, user.Id, http.MethodGet, "/api/channel/?group=default&tag_mode=true", nil, GetAllChannels)
	require.Equal(t, http.StatusOK, response.Code)
	var page struct {
		Success bool
		Data    struct {
			Items []model.Channel
			Total int64
		}
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &page))
	require.True(t, page.Success, response.Body.String())
	assert.EqualValues(t, 1, page.Data.Total)
	require.Len(t, page.Data.Items, 2)
	for _, channel := range page.Data.Items {
		assert.Empty(t, channel.Key)
	}
	response = channelMatrixRequest(t, user.Id, http.MethodGet, "/api/channel/search?keyword=edited&group=default", nil, SearchChannels)
	require.Equal(t, http.StatusOK, response.Code)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &page))
	require.True(t, page.Success, response.Body.String())
	assert.EqualValues(t, 1, page.Data.Total)
	response = channelMatrixRequest(t, user.Id, http.MethodPost, fmt.Sprintf("/api/channel/status?channel_id=%d", id), ChannelStatusRequest{Status: common.ChannelStatusManuallyDisabled}, UpdateChannelStatus)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"data":true`)
	var ability model.Ability
	require.NoError(t, db.Where("channel_id = ?", id).First(&ability).Error)
	assert.False(t, ability.Enabled)
	response = channelMatrixRequest(t, user.Id, http.MethodPost, "/api/channel/status/batch", ChannelStatusBatchRequest{Ids: []int{id}, Status: common.ChannelStatusEnabled}, BatchUpdateChannelStatus)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"data":1`)
	require.NoError(t, db.Where("channel_id = ?", id).First(&ability).Error)
	assert.True(t, ability.Enabled)
	response = channelMatrixRequest(t, user.Id, http.MethodPost, "/api/channel/batch/tag", ChannelBatch{Ids: []int{id}, Tag: nil}, BatchSetChannelTag)
	require.Equal(t, http.StatusOK, response.Code)
	current, err = model.GetChannelById(id, true)
	require.NoError(t, err)
	assert.Nil(t, current.Tag)
	response = channelMatrixRequest(t, user.Id, http.MethodPost, "/api/channel", AddChannelRequest{Mode: "multi_to_single", MultiKeyMode: constant.MultiKeyModePolling, Channel: &model.Channel{Type: constant.ChannelTypeOpenAI, Name: "multi", Key: "first\nsecond", Models: "gpt-4o", Group: "default"}}, AddChannel)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var multi model.Channel
	require.NoError(t, db.Where("name = ?", "multi").First(&multi).Error)
	index := 0
	response = channelMatrixRequest(t, user.Id, http.MethodPost, "/api/channel/multi_key/manage", MultiKeyManageRequest{ChannelId: multi.Id, Action: "disable_key", KeyIndex: &index}, ManageMultiKeys)
	require.Equal(t, http.StatusOK, response.Code)
	current, err = model.GetChannelById(multi.Id, true)
	require.NoError(t, err)
	assert.True(t, current.ChannelInfo.IsMultiKey)
	assert.Equal(t, 2, current.ChannelInfo.MultiKeySize)
	assert.Equal(t, 2, current.ChannelInfo.MultiKeyStatusList[0])
	for _, handler := range []gin.HandlerFunc{DisableTagChannels, EnableTagChannels} {
		response = channelMatrixRequest(t, user.Id, http.MethodPost, "/api/channel/tag", map[string]any{"tag": tag, "priority": "invalid"}, handler)
		require.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Body.String(), `"success":false`)
	}
	beforeCount, err := model.CountAllChannels()
	require.NoError(t, err)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("channels_matrix_ability_failure", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "abilities" {
			tx.AddError(errors.New("forced channels ability failure"))
		}
	}))
	t.Cleanup(func() { db.Callback().Create().Remove("channels_matrix_ability_failure") })
	response = channelMatrixRequest(t, user.Id, http.MethodPost, "/api/channel", AddChannelRequest{Mode: "single", Channel: &model.Channel{Type: constant.ChannelTypeOpenAI, Name: "rollback", Key: "key", Models: "gpt-4o", Group: "default"}}, AddChannel)
	require.Equal(t, http.StatusInternalServerError, response.Code)
	afterCount, err := model.CountAllChannels()
	require.NoError(t, err)
	assert.Equal(t, beforeCount, afterCount)
	db.Callback().Create().Remove("channels_matrix_ability_failure")
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	assert.True(t, db.Migrator().HasIndex(&model.Channel{}, "Name"))
	assert.True(t, db.Migrator().HasIndex(&model.Channel{}, "Tag"))
	response = channelMatrixRequest(t, user.Id, http.MethodPost, "/api/channel/batch", ChannelBatch{Ids: []int{id, 999999}}, DeleteChannelBatch)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"data":1`)
	var remaining int64
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", id).Count(&remaining).Error)
	assert.Zero(t, remaining)
}

func TestChannelsStorageUpgradeCompatibility(t *testing.T) {
	phase := os.Getenv("TEST_CHANNEL_UPGRADE_PHASE")
	if phase == "" {
		t.Skip("TEST_CHANNEL_UPGRADE_PHASE is not configured")
	}
	require.Contains(t, []string{"baseline", "candidate"}, phase)
	db := channelMatrixDatabase(t)
	if phase == "baseline" {
		tag := "upgrade-tag"
		zero := int64(0)
		weight := uint(0)
		channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Name: "persisted-channel", Key: "persisted-first\npersisted-second", Models: "gpt-4o,gpt-4o-mini", Group: "default,team_1", Tag: &tag, Priority: &zero, Weight: &weight, Balance: 12.5, UsedQuota: 17, CreatedTime: 1700000000, OtherInfo: `{"status_reason":"legacy","status_time":123}`, ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling, MultiKeyStatusList: map[int]int{0: 2}, MultiKeyDisabledReason: map[int]string{0: "legacy reason"}, MultiKeyDisabledTime: map[int]int64{0: 1699999999}}}
		require.NoError(t, channel.Insert())
	}
	var channel model.Channel
	require.NoError(t, db.Where("name = ?", "persisted-channel").First(&channel).Error)
	assert.Equal(t, "persisted-first\npersisted-second", channel.Key)
	assert.Equal(t, 12.5, channel.Balance)
	assert.EqualValues(t, 17, channel.UsedQuota)
	require.NotNil(t, channel.Weight)
	assert.Zero(t, *channel.Weight)
	require.NotNil(t, channel.Priority)
	assert.Zero(t, *channel.Priority)
	assert.Nil(t, channel.HeaderOverride)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
	assert.Equal(t, 2, channel.ChannelInfo.MultiKeySize)
	assert.Equal(t, 2, channel.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "legacy reason", channel.ChannelInfo.MultiKeyDisabledReason[0])
	assert.EqualValues(t, 1699999999, channel.ChannelInfo.MultiKeyDisabledTime[0])
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	}
	assert.True(t, db.Migrator().HasIndex(&model.Channel{}, "Name"))
	assert.True(t, db.Migrator().HasIndex(&model.Channel{}, "Tag"))
	var count int64
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channel.Id).Count(&count).Error)
	assert.EqualValues(t, 4, count)
	var ability model.Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.Error(t, db.Create(&ability).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		locked, err := model.GetChannelByIdForUpdate(tx, channel.Id)
		if err == nil {
			assert.Equal(t, channel.Key, locked.Key)
		}
		return err
	}))
}
