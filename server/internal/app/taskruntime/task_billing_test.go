package taskruntime

import (
	mappingfx "github.com/QuantumNous/new-api/gateway/mapping"
	fxstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/fx"
	pricingdomainfx "github.com/QuantumNous/new-api/internal/pricing/domain"
	billinggatewayfx "github.com/QuantumNous/new-api/relay/billing"

	"context"
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	usageadapter "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	taskdomain "github.com/QuantumNous/new-api/internal/tasks/domain"
	usagedomain "github.com/QuantumNous/new-api/internal/usage/domain"
	usagedto "github.com/QuantumNous/new-api/internal/usage/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/taskrecord"
	usagegateway "github.com/QuantumNous/new-api/relay/usage"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

func TestPriceDataOtherRatiosFilterAndSnapshot(t *testing.T) {
	priceData := types.PriceData{}

	priceData.AddOtherRatio("zero", 0)
	priceData.AddOtherRatio("negative", -0.5)
	priceData.AddOtherRatio("nan", math.NaN())
	priceData.AddOtherRatio("inf", math.Inf(1))
	priceData.AddOtherRatio("one", 1)
	priceData.AddOtherRatio("positive", 2.5)

	ratios := priceData.OtherRatios()
	require.Len(t, ratios, 2)
	assert.Equal(t, 1.0, ratios["one"])
	assert.Equal(t, 2.5, ratios["positive"])
	assert.True(t, priceData.HasOtherRatio("one"))
	assert.False(t, priceData.HasOtherRatio("zero"))

	ratios["positive"] = 99
	ratios["new"] = 3
	nextSnapshot := priceData.OtherRatios()
	assert.Equal(t, 2.5, nextSnapshot["positive"])
	assert.NotContains(t, nextSnapshot, "new")
}

func TestPriceDataReplaceAndApplyOtherRatios(t *testing.T) {
	priceData := types.PriceData{}

	replaced := priceData.ReplaceOtherRatios(map[string]float64{
		"zero":     0,
		"negative": -3,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
		"one":      1,
		"duration": 2,
		"size":     1.5,
	})

	require.True(t, replaced)
	assert.Equal(t, 3.0, priceData.OtherRatioMultiplier())
	assert.Equal(t, 30.0, priceData.ApplyOtherRatiosToFloat(10))
	assert.Equal(t, 10.0, priceData.RemoveOtherRatiosFromFloat(30))
	assert.True(t, decimal.NewFromInt(30).Equal((domain.Prices{OtherRatios: priceData.OtherRatios()}).ApplyOtherRatiosToDecimal(decimal.NewFromInt(10))))

	replaced = priceData.ReplaceOtherRatios(map[string]float64{
		"zero": 0,
		"nan":  math.NaN(),
	})

	require.False(t, replaced)
	assert.Nil(t, priceData.OtherRatios())
	assert.Equal(t, 1.0, priceData.OtherRatioMultiplier())
}

func callLogTaskConsumption(t *testing.T, info *relaycommon.RelayInfo, task *model.Task) *model.Log {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	ctx.Set("token_name", "test_token")
	LogTaskConsumption(ctx, info, task)
	log := getLastLog(t)
	require.NotNil(t, log)
	return log
}

func TestLogTaskConsumptionIncludesTieredSnapshotUsageFacts(t *testing.T) {
	taskfixture.Truncate(t)
	const userID, channelID = 40, 40
	taskfixture.SeedUser(t, userID, 10_000)
	taskfixture.SeedChannel(t, channelID)

	expression := `tier("720P", u("seconds") * 5)`
	task := taskfixture.MakeTask(userID, channelID, 100, 0, BillingSourceWallet, 0)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         0,
		OriginModelName: "wan2.5-i2v-preview",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: "GENERATE"},
		PriceData: types.PriceData{
			ModelPrice:     0.02,
			Quota:          100,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			ExprString:    expression,
			EstimatedTier: "720P",
			UsageFacts: map[string]any{
				"resolution": "720P",
				"seconds":    5,
			},
		},
	}

	log := callLogTaskConsumption(t, info, task)

	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, "tiered_expr", other["billing_mode"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(expression)), other["expr_b64"])
	assert.Equal(t, "720P", other["matched_tier"])
	facts, ok := other["usage_facts"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "720P", facts["resolution"])
	assert.Equal(t, float64(5), facts["seconds"])
	assert.NotContains(t, other, "resolution")
	assert.NotContains(t, other, "seconds")
	assert.Contains(t, log.Content, "parameters: ")
	assert.Contains(t, log.Content, "resolution: 720P")
	assert.Contains(t, log.Content, "seconds: 5")
}

func TestLogTaskConsumptionWithoutSnapshotKeepsRatioMode(t *testing.T) {
	taskfixture.Truncate(t)
	const userID, channelID = 41, 41
	taskfixture.SeedUser(t, userID, 10_000)
	taskfixture.SeedChannel(t, channelID)

	priceData := types.PriceData{
		ModelPrice:     0.02,
		Quota:          100,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	priceData.AddOtherRatio("size", 2)
	task := taskfixture.MakeTask(userID, channelID, 100, 0, BillingSourceWallet, 0)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         0,
		OriginModelName: "test-model",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: "GENERATE"},
		PriceData:       priceData,
	}

	log := callLogTaskConsumption(t, info, task)

	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, true, other["is_task"])
	assert.Equal(t, "/v1/videos", other["request_path"])
	assert.NotContains(t, other, "billing_mode")
	assert.NotContains(t, other, "expr_b64")
	assert.NotContains(t, other, "matched_tier")
	assert.NotContains(t, other, "usage_facts")
	assert.Contains(t, log.Content, "parameters: ")
	assert.Contains(t, log.Content, "size: 2.00")
}

func getUserQuota(t *testing.T, id int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&user).Error)
	return user.Quota
}

func getUserUsageAccounting(t *testing.T, id int) (int, int) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").Where("id = ?", id).First(&user).Error)
	return user.UsedQuota, user.RequestCount
}

func getChannelUsedQuota(t *testing.T, id int) int64 {
	t.Helper()
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&channel).Error)
	return channel.UsedQuota
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").Where("id = ?", id).First(&token).Error)
	return token.RemainQuota
}

func getTokenUsedQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&token).Error)
	return token.UsedQuota
}

func getSubscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", id).First(&sub).Error)
	return sub.AmountUsed
}

func getTaskQuota(t *testing.T, id int64) int {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&task).Error)
	return task.Quota
}

func getMidjourneyTask(t *testing.T, id int) model.Midjourney {
	t.Helper()
	var task model.Midjourney
	require.NoError(t, model.DB.First(&task, id).Error)
	return task
}

func getLastLog(t *testing.T) *model.Log {
	t.Helper()
	var log model.Log
	err := model.LOG_DB.Order("id desc").First(&log).Error
	if err != nil {
		return nil
	}
	return &log
}

func countLogs(t *testing.T) int64 {
	t.Helper()
	var count int64
	model.LOG_DB.Model(&model.Log{}).Count(&count)
	return count
}

// ===========================================================================
// Legacy Midjourney billing tests
// ===========================================================================

func TestPrepareMidjourneyTaskBillingKeepsUnbilledMarkerClear(t *testing.T) {
	task := &model.Midjourney{Quota: 900, TokenId: 7, BillingChannelId: 8}

	prepared, err := PrepareMidjourneyTaskBilling(&relaycommon.RelayInfo{}, task, 900, false)

	require.NoError(t, err)
	assert.False(t, prepared)
	assert.Zero(t, task.Quota)
	assert.Zero(t, task.TokenId)
	assert.Zero(t, task.BillingChannelId)
}

func TestSettleMidjourneyTaskBillingRequiresPersistedTask(t *testing.T) {
	taskfixture.Truncate(t)

	const userID, tokenID, channelID = 49, 49, 49
	const initialUserQuota, initialTokenQuota, chargedQuota = 10000, 5000, 3000
	taskfixture.SeedUser(t, userID, initialUserQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-midjourney-unpersisted", initialTokenQuota)
	taskfixture.SeedChannel(t, channelID)

	relayInfo := &relaycommon.RelayInfo{
		UserId:    userID,
		TokenId:   tokenID,
		TokenKey:  "sk-midjourney-unpersisted",
		UserQuota: initialUserQuota,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: channelID,
		},
	}
	task := &model.Midjourney{UserId: userID, ChannelId: channelID}
	prepared, err := PrepareMidjourneyTaskBilling(relayInfo, task, chargedQuota, true)
	require.NoError(t, err)
	require.True(t, prepared)

	billed, err := SettleMidjourneyTaskBilling(relayInfo, task, prepared)

	require.Error(t, err)
	assert.False(t, billed)
	assert.Equal(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
}

func TestMidjourneyRefundRestoresEveryAccountingElementOnBillingChannel(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, billingChannelID, executionChannelID = 50, 50, 50, 51
	const initialUserQuota, initialTokenQuota, chargedQuota = 10000, 5000, 3000
	taskfixture.SeedUser(t, userID, initialUserQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-midjourney", initialTokenQuota)
	taskfixture.SeedChannel(t, billingChannelID)
	taskfixture.SeedChannel(t, executionChannelID)

	relayInfo := &relaycommon.RelayInfo{
		UserId:     userID,
		TokenId:    tokenID,
		TokenKey:   "sk-midjourney",
		UserQuota:  initialUserQuota,
		UsingGroup: "default",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: billingChannelID,
		},
	}
	task := &model.Midjourney{
		UserId:    userID,
		Action:    "IMAGINE",
		MjId:      "mj-accounting-refund",
		ChannelId: executionChannelID,
		Progress:  "0%",
	}

	prepared, err := PrepareMidjourneyTaskBilling(relayInfo, task, chargedQuota, true)
	require.NoError(t, err)
	require.True(t, prepared)
	assert.Equal(t, chargedQuota, task.Quota)
	assert.Zero(t, task.TokenId)
	assert.Equal(t, billingChannelID, task.BillingChannelId)
	require.NoError(t, fixtureStore.InsertMidjourney(task))

	billed, err := SettleMidjourneyTaskBilling(relayInfo, task, prepared)
	require.NoError(t, err)
	require.True(t, billed)
	assert.Equal(t, initialUserQuota-chargedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-chargedQuota, getTokenRemainQuota(t, tokenID))
	persisted := getMidjourneyTask(t, task.Id)
	assert.Equal(t, chargedQuota, persisted.Quota)
	assert.Equal(t, tokenID, persisted.TokenId)
	assert.Equal(t, billingChannelID, persisted.BillingChannelId)

	taskfixture.SeedChargedAccounting(t, userID, billingChannelID, tokenID, chargedQuota, 1)

	assert.True(t, RefundMidjourneyQuota(ctx, task, "构图失败"))
	assert.Equal(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, billingChannelID))
	assert.Zero(t, getChannelUsedQuota(t, executionChannelID))

	persisted = getMidjourneyTask(t, task.Id)
	assert.Zero(t, persisted.Quota)
	assert.Equal(t, tokenID, persisted.TokenId)
	assert.Equal(t, billingChannelID, persisted.BillingChannelId)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, chargedQuota, log.Quota)
	assert.Equal(t, tokenID, log.TokenId)
	assert.Equal(t, billingChannelID, log.ChannelId)

	assert.True(t, RefundMidjourneyQuota(ctx, task, "duplicate poll"))
	assert.Equal(t, int64(1), countLogs(t))
}

func TestSettleMidjourneyTaskBillingFundingFailureClearsMarkers(t *testing.T) {
	taskfixture.WithSQLite(t)
	taskfixture.Truncate(t)

	const userID, tokenID, channelID = 52, 52, 52
	const initialUserQuota, initialTokenQuota, chargedQuota = 10000, 5000, 3000
	taskfixture.SeedUser(t, userID, initialUserQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-midjourney-funding-failure", initialTokenQuota)
	taskfixture.SeedChannel(t, channelID)

	relayInfo := &relaycommon.RelayInfo{
		UserId:    userID,
		TokenId:   tokenID,
		TokenKey:  "sk-midjourney-funding-failure",
		UserQuota: initialUserQuota,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: channelID,
		},
	}
	task := &model.Midjourney{UserId: userID, MjId: "mj-funding-failure", ChannelId: channelID}
	prepared, err := PrepareMidjourneyTaskBilling(relayInfo, task, chargedQuota, true)
	require.NoError(t, err)
	require.True(t, prepared)
	require.NoError(t, fixtureStore.InsertMidjourney(task))

	require.NoError(t, model.DB.Exec(`
		CREATE TRIGGER fail_midjourney_user_update
		BEFORE UPDATE ON users
		WHEN OLD.id = 52
		BEGIN
			SELECT RAISE(ABORT, 'forced user quota failure');
		END;
	`).Error)
	t.Cleanup(func() {
		model.DB.Exec("DROP TRIGGER IF EXISTS fail_midjourney_user_update")
	})

	billed, err := SettleMidjourneyTaskBilling(relayInfo, task, prepared)

	require.Error(t, err)
	assert.False(t, billed)
	assert.Equal(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	persisted := getMidjourneyTask(t, task.Id)
	assert.Zero(t, persisted.Quota)
	assert.Zero(t, persisted.TokenId)
	assert.Zero(t, persisted.BillingChannelId)
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Zero(t, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	assert.Zero(t, countLogs(t))
}

func TestSettleMidjourneyTaskBillingTokenFailureKeepsFundingRefundable(t *testing.T) {
	taskfixture.WithSQLite(t)
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 53, 53, 53
	const initialUserQuota, initialTokenQuota, chargedQuota = 10000, 5000, 3000
	taskfixture.SeedUser(t, userID, initialUserQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-midjourney-token-failure", initialTokenQuota)
	taskfixture.SeedChannel(t, channelID)

	relayInfo := &relaycommon.RelayInfo{
		UserId:    userID,
		TokenId:   tokenID,
		TokenKey:  "sk-midjourney-token-failure",
		UserQuota: initialUserQuota,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: channelID,
		},
	}
	task := &model.Midjourney{UserId: userID, MjId: "mj-token-failure", ChannelId: channelID}
	prepared, err := PrepareMidjourneyTaskBilling(relayInfo, task, chargedQuota, true)
	require.NoError(t, err)
	require.True(t, prepared)
	require.NoError(t, fixtureStore.InsertMidjourney(task))

	require.NoError(t, model.DB.Exec(`
		CREATE TRIGGER fail_midjourney_token_update
		BEFORE UPDATE ON tokens
		WHEN OLD.id = 53
		BEGIN
			SELECT RAISE(ABORT, 'forced token quota failure');
		END;
	`).Error)
	t.Cleanup(func() {
		model.DB.Exec("DROP TRIGGER IF EXISTS fail_midjourney_token_update")
	})

	billed, err := SettleMidjourneyTaskBilling(relayInfo, task, prepared)

	require.Error(t, err)
	require.True(t, billed)
	assert.Equal(t, initialUserQuota-chargedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	persisted := getMidjourneyTask(t, task.Id)
	assert.Equal(t, chargedQuota, persisted.Quota)
	assert.Zero(t, persisted.TokenId)
	assert.Equal(t, channelID, persisted.BillingChannelId)

	taskfixture.SeedChargedAccounting(t, userID, channelID, 0, chargedQuota, 1)
	assert.True(t, RefundMidjourneyQuota(ctx, task, "token settlement failed"))
	assert.Equal(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Zero(t, log.TokenId)
}

func TestPrepareMidjourneyTaskBillingRejectsSubscriptionBeforeCharge(t *testing.T) {
	task := &model.Midjourney{Quota: 900, TokenId: 7, BillingChannelId: 8}
	relayInfo := &relaycommon.RelayInfo{BillingSource: BillingSourceSubscription, SubscriptionId: 1}

	prepared, err := PrepareMidjourneyTaskBilling(relayInfo, task, 900, true)

	require.Error(t, err)
	assert.False(t, prepared)
	assert.Zero(t, task.Quota)
	assert.Zero(t, task.TokenId)
	assert.Zero(t, task.BillingChannelId)
}

func TestRefundMidjourneyQuotaUsesLegacyChannelFallbackWithoutTokenAdjustment(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 54, 54, 54
	const walletAfterCharge, tokenQuota, chargedQuota = 7000, 5000, 3000
	taskfixture.SeedUser(t, userID, walletAfterCharge)
	taskfixture.SeedToken(t, tokenID, userID, "sk-midjourney-legacy", tokenQuota)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, 0, chargedQuota, 1)
	task := &model.Midjourney{
		UserId:    userID,
		MjId:      "mj-legacy-fallback",
		Action:    "IMAGINE",
		ChannelId: channelID,
		Quota:     chargedQuota,
		TokenId:   0,
		Progress:  "0%",
	}
	require.NoError(t, fixtureStore.InsertMidjourney(task))

	assert.True(t, RefundMidjourneyQuota(ctx, task, "legacy failure"))

	assert.Equal(t, walletAfterCharge+chargedQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, channelID, log.ChannelId)
	assert.Zero(t, log.TokenId)
}

// ===========================================================================
// RefundTaskQuota tests
// ===========================================================================

func TestRefundTaskQuota_Wallet(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1, 1, 1
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	taskfixture.SeedUser(t, userID, initQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-test-key", tokenRemain)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	assert.True(t, RefundTaskQuota(ctx, task, "task failed: upstream error"))

	// User quota should increase by preConsumed
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Token remain_quota should increase, used_quota should decrease
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	// A refund log should be created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, "test-model", log.ModelName)
	assert.Zero(t, task.Quota)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_ReconciledEvidenceAndStaleWorker(t *testing.T) {
	for _, tc := range []struct {
		name     string
		evidence model.TaskReconciliation
		refund   bool
	}{
		{"missing cost", model.TaskReconciliation{}, false},
		{"unconfirmed zero", model.TaskReconciliation{HasUpstreamCost: true}, false},
		{"paid failure", model.TaskReconciliation{HasUpstreamCost: true, UpstreamCostConfirmed: true, UpstreamCostUSD: 0.1}, false},
		{"reservation pending", model.TaskReconciliation{HasUpstreamCost: true, UpstreamCostConfirmed: true, ReservationPending: true}, false},
		{"submission pending", model.TaskReconciliation{HasUpstreamCost: true, UpstreamCostConfirmed: true, SubmissionPending: true}, false},
		{"refund pending", model.TaskReconciliation{HasUpstreamCost: true, UpstreamCostConfirmed: true, RefundPending: true}, false},
		{"confirmed zero", model.TaskReconciliation{HasUpstreamCost: true, UpstreamCostConfirmed: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskfixture.Truncate(t)
			const charged = 3000
			taskfixture.SeedUser(t, 1, 10000)
			taskfixture.SeedToken(t, 1, 1, "sk-test-key", 5000)
			taskfixture.SeedChannel(t, 1)
			taskfixture.SeedChargedAccounting(t, 1, 1, 1, charged, 1)
			task := taskfixture.MakeTask(1, 1, charged, 1, BillingSourceWallet, 0)
			task.Status = model.TaskStatusFailure
			tc.evidence.ReservedQuota = charged
			task.PrivateData.Reconciliation = &tc.evidence
			require.NoError(t, model.DB.Create(task).Error)
			// A second process loaded the charged task before the first refund.
			var stale model.Task
			require.NoError(t, model.DB.First(&stale, task.ID).Error)
			assert.Equal(t, tc.refund, RefundTaskQuota(t.Context(), task, "fixture failure"))
			assert.False(t, RefundTaskQuota(t.Context(), &stale, "stale worker"))
			wantQuota, wantToken, wantCharged := 10000, 5000, charged
			if tc.refund {
				wantQuota += charged
				wantToken += charged
				wantCharged = 0
			}
			assert.Equal(t, wantQuota, getUserQuota(t, 1))
			assert.Equal(t, wantToken, getTokenRemainQuota(t, 1))
			assert.Equal(t, wantCharged, getTaskQuota(t, task.ID))
			assert.Equal(t, int64(wantCharged), getChannelUsedQuota(t, 1))
			var loaded model.Task
			require.NoError(t, model.DB.First(&loaded, task.ID).Error)
			require.NotNil(t, loaded.PrivateData.Reconciliation)
			assert.Equal(t, tc.refund, loaded.PrivateData.Reconciliation.Finalized)
			assert.Equal(t, charged, loaded.PrivateData.Reconciliation.ReservedQuota)
		})
	}
}

func TestReconciledRefundInterruptionRetainsHoldAfterReload(t *testing.T) {
	for _, stage := range []string{"after claim", "wallet unavailable", "token unavailable"} {
		t.Run(stage, func(t *testing.T) {
			taskfixture.Truncate(t)
			const charged = 3000
			taskfixture.SeedUser(t, 1, 10000)
			taskfixture.SeedToken(t, 1, 1, "sk-refund-interruption-fixture", 5000)
			taskfixture.SeedChannel(t, 1)
			taskfixture.SeedChargedAccounting(t, 1, 1, 1, charged, 1)
			task := taskfixture.MakeTask(1, 1, charged, 1, BillingSourceWallet, 0)
			task.Status = model.TaskStatusFailure
			task.PrivateData.Reconciliation = &model.TaskReconciliation{
				ReservedQuota: charged, HasUpstreamCost: true, UpstreamCostConfirmed: true,
			}
			require.NoError(t, model.DB.Create(task).Error)
			var stale model.Task
			require.NoError(t, model.DB.First(&stale, task.ID).Error)
			if stage == "after claim" {
				won, err := fixtureStore.ClaimReconciledTaskRefund(task, charged)
				require.NoError(t, err)
				require.True(t, won)
			} else {
				failedTable := "users"
				if stage == "token unavailable" {
					failedTable = "tokens"
				}
				callback := "test:refund_interruption"
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Table == failedTable {
						tx.AddError(fmt.Errorf("injected %s update failure", failedTable))
					}
				}))
				t.Cleanup(func() { _ = model.DB.Callback().Update().Remove(callback) })
				assert.False(t, RefundTaskQuota(t.Context(), task, "interruption fixture"))
				require.NoError(t, model.DB.Callback().Update().Remove(callback))
			}

			// The failing dependency is healthy again. A new DB connection and a
			// stale worker must still leave the unresolved transfer alone.
			taskfixture.WithOtherNode(t, func() {
				var loaded model.Task
				require.NoError(t, model.DB.First(&loaded, task.ID).Error)
				require.NotNil(t, loaded.PrivateData.Reconciliation)
				assert.True(t, loaded.PrivateData.Reconciliation.RefundPending)
				assert.True(t, loaded.PrivateData.Reconciliation.Required)
				assert.False(t, loaded.PrivateData.Reconciliation.Finalized)
				assert.Equal(t, charged, loaded.PrivateData.Reconciliation.ReservedQuota)
				assert.Zero(t, loaded.Quota)
				assert.False(t, RefundTaskQuota(t.Context(), &loaded, "after reconnect"))
				assert.False(t, RefundTaskQuota(t.Context(), &stale, "stale worker"))
				wantWallet, wantUsed, wantLogs := 10000, charged, int64(0)
				if stage == "token unavailable" {
					wantWallet, wantUsed, wantLogs = 13000, 0, 1
				}
				assert.Equal(t, wantWallet, getUserQuota(t, 1))
				assert.Equal(t, 5000, getTokenRemainQuota(t, 1))
				assert.Equal(t, int64(wantUsed), getChannelUsedQuota(t, 1))
				var user model.User
				require.NoError(t, model.DB.First(&user, 1).Error)
				assert.Equal(t, wantUsed, user.UsedQuota)
				assert.Equal(t, 1, user.RequestCount)
				var token model.Token
				require.NoError(t, model.DB.First(&token, 1).Error)
				assert.Equal(t, charged, token.UsedQuota)
				var refundLogs int64
				require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeRefund).Count(&refundLogs).Error)
				assert.Equal(t, wantLogs, refundLogs)
			})
		})
	}
}

func TestReconciledSubmissionManualEvidenceThenRefundOnce(t *testing.T) {
	taskfixture.Truncate(t)
	const charged = 3000
	taskfixture.SeedUser(t, 1, 10000)
	taskfixture.SeedToken(t, 1, 1, "sk-manual-evidence-fixture", 5000)
	taskfixture.SeedChannel(t, 1)
	taskfixture.SeedChargedAccounting(t, 1, 1, 1, charged, 1)
	task := taskfixture.MakeTask(1, 1, charged, 1, BillingSourceWallet, 0)
	task.Status = model.TaskStatusFailure
	task.PrivateData.Reconciliation = &model.TaskReconciliation{
		ReservedQuota: charged, SubmissionPending: true, Required: true,
	}
	require.NoError(t, model.DB.Create(task).Error)
	assert.False(t, RefundTaskQuota(t.Context(), task, "unknown submission"))
	assert.Equal(t, 10000, getUserQuota(t, 1))

	taskfixture.WithOtherNode(t, func() {
		var loaded model.Task
		require.NoError(t, model.DB.First(&loaded, task.ID).Error)
		// Operator fixture: reservation is independently known complete and
		// the exact upstream job has a terminal non-BYOK failure costing zero.
		// This data edit rehearses the evidence handoff, not a production UI.
		loaded.PrivateData.UpstreamTaskID = "operator-confirmed-zero-cost-job"
		loaded.PrivateData.Reconciliation.SubmissionPending = false
		loaded.PrivateData.Reconciliation.HasUpstreamCost = true
		loaded.PrivateData.Reconciliation.UpstreamCostConfirmed = true
		won, err := fixtureStore.UpdateTaskWithStatus(&loaded, model.TaskStatusFailure)
		require.NoError(t, err)
		require.True(t, won)
		var stale model.Task
		require.NoError(t, model.DB.First(&stale, task.ID).Error)
		require.True(t, RefundTaskQuota(t.Context(), &loaded, "operator confirmed zero-cost failure"))
		assert.False(t, RefundTaskQuota(t.Context(), &stale, "duplicate operator"))
		var finished model.Task
		require.NoError(t, model.DB.First(&finished, task.ID).Error)
		assert.True(t, RefundTaskQuota(t.Context(), &finished, "repeated check"))
		assert.True(t, finished.PrivateData.Reconciliation.Finalized)
		assert.False(t, finished.PrivateData.Reconciliation.Required)
		assert.False(t, finished.PrivateData.Reconciliation.RefundPending)
		assert.Zero(t, finished.Quota)
		assert.Equal(t, 13000, getUserQuota(t, 1))
		assert.Equal(t, 8000, getTokenRemainQuota(t, 1))
		assert.Zero(t, getChannelUsedQuota(t, 1))
		var refundLogs int64
		require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeRefund).Count(&refundLogs).Error)
		assert.Equal(t, int64(1), refundLogs)
	})
}

func TestRefundTaskQuota_Subscription(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 2, 2, 2, 1
	const preConsumed = 2000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	taskfixture.SeedUser(t, userID, 0)
	taskfixture.SeedToken(t, tokenID, userID, "sk-sub-key", tokenRemain)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedSubscription(t, subID, userID, subTotal, subUsed)
	taskfixture.SeedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)
	require.NoError(t, model.DB.Create(task).Error)

	assert.True(t, RefundTaskQuota(ctx, task, "subscription task failed"))

	// Subscription used should decrease by preConsumed
	assert.Equal(t, subUsed-int64(preConsumed), getSubscriptionUsed(t, subID))

	// Token should also be refunded
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_ZeroQuota(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID = 3
	taskfixture.SeedUser(t, userID, 5000)

	task := taskfixture.MakeTask(userID, 0, 0, 0, BillingSourceWallet, 0)

	assert.True(t, RefundTaskQuota(ctx, task, "zero quota task"))

	// No change to user quota
	assert.Equal(t, 5000, getUserQuota(t, userID))

	// No log created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuota_NoToken(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, channelID = 4, 4
	const initQuota, preConsumed = 10000, 1500

	taskfixture.SeedUser(t, userID, initQuota)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0) // TokenId=0
	require.NoError(t, model.DB.Create(task).Error)

	assert.True(t, RefundTaskQuota(ctx, task, "no token task failed"))

	// User quota refunded
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	// Log created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_FundingFailureKeepsAccountingAndPendingMarker(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, channelID, preConsumed = 5, 5, 1200
	taskfixture.SeedUser(t, userID, 5000)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)
	task := taskfixture.MakeTask(userID, channelID, preConsumed, 0, BillingSourceSubscription, 9999)
	task.Status = model.TaskStatusFailure
	require.NoError(t, model.DB.Create(task).Error)

	assert.False(t, RefundTaskQuota(ctx, task, "subscription missing"))
	assert.Equal(t, 5000, getUserQuota(t, userID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, preConsumed, getTaskQuota(t, task.ID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(0), countLogs(t))
}

// ===========================================================================
// RecalculateTaskQuota tests
// ===========================================================================

func TestRecalculate_PositiveDelta(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 10, 10, 10
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000 // under-charged by 1000
	const tokenRemain = 5000

	taskfixture.SeedUser(t, userID, initQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-recalc-pos", tokenRemain)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should decrease by the delta (1000 additional charge)
	assert.Equal(t, initQuota-(actualQuota-preConsumed), getUserQuota(t, userID))

	// Token should also be charged the delta
	assert.Equal(t, tokenRemain-(actualQuota-preConsumed), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Consume (additional charge)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeConsume, log.Type)
	assert.Equal(t, actualQuota-preConsumed, log.Quota)
}

func TestRecalculate_NegativeDelta(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 11, 11, 11
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged by 2000
	const tokenRemain = 5000

	taskfixture.SeedUser(t, userID, initQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-recalc-neg", tokenRemain)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should increase by abs(delta) = 2000 (refund overpayment)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))

	// Token should be refunded the difference
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	// task.Quota updated
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Refund
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed-actualQuota, log.Quota)
}

func TestRecalculate_ZeroDelta(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID = 12
	const initQuota, preConsumed = 10000, 3000

	taskfixture.SeedUser(t, userID, initQuota)

	task := taskfixture.MakeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, preConsumed, "exact match")

	// No change to user quota
	assert.Equal(t, initQuota, getUserQuota(t, userID))

	// No log created (delta is zero)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_ActualQuotaZero(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, preConsumed = 13, 5000
	const initQuota = 10000

	taskfixture.SeedUser(t, userID, initQuota)

	task := taskfixture.MakeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	RecalculateTaskQuota(ctx, task, 0, "zero actual")

	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Zero(t, task.Quota)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
}

func TestRecalculate_RejectsNegativeActualQuota(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, preConsumed = 34, 5000
	const initQuota = 10000
	taskfixture.SeedUser(t, userID, initQuota)
	task := taskfixture.MakeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, -1, "invalid negative actual")

	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_Subscription_NegativeDelta(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 14, 14, 14, 2
	const preConsumed = 5000
	const actualQuota = 2000 // over-charged by 3000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	taskfixture.SeedUser(t, userID, 0)
	taskfixture.SeedToken(t, tokenID, userID, "sk-sub-recalc", tokenRemain)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedSubscription(t, subID, userID, subTotal, subUsed)
	taskfixture.SeedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)

	RecalculateTaskQuota(ctx, task, actualQuota, "subscription over-charge")

	// Subscription used should decrease by delta (refund 3000)
	assert.Equal(t, subUsed-int64(preConsumed-actualQuota), getSubscriptionUsed(t, subID))

	// Token refunded
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	assert.Equal(t, actualQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

// ===========================================================================
// CAS + Billing integration tests
// Simulates the flow in updateVideoSingleTask (service/task_polling.go)
// ===========================================================================

// simulatePollBilling reproduces the CAS + billing logic from updateVideoSingleTask.
// It takes a persisted task (already in DB), applies the new status, and performs
// the conditional update + billing exactly as the polling loop does.
func simulatePollBilling(ctx context.Context, task *model.Task, newStatus model.TaskStatus, actualQuota int) {
	snap := task.Snapshot()

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota

	task.Status = newStatus
	switch string(newStatus) {
	case model.TaskStatusSuccess:
		task.Progress = "100%"
		task.FinishTime = 9999
		shouldSettle = true
	case model.TaskStatusFailure:
		task.Progress = "100%"
		task.FinishTime = 9999
		task.FailReason = "upstream error"
		if quota != 0 {
			shouldRefund = true
		}
	default:
		task.Progress = "50%"
	}

	isDone := task.Status == model.TaskStatus(model.TaskStatusSuccess) || task.Status == model.TaskStatus(model.TaskStatusFailure)
	if isDone && snap.Status != task.Status {
		won, err := fixtureStore.UpdateTaskWithStatus(task, snap.Status)
		if err != nil {
			shouldRefund = false
			shouldSettle = false
		} else if !won {
			shouldRefund = false
			shouldSettle = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		_, _ = fixtureStore.UpdateTaskWithStatus(task, snap.Status)
	}

	if shouldSettle && actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "test settle")
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, task.FailReason)
	}
}

func TestCASGuardedRefund_Win(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 20, 20, 20
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	taskfixture.SeedUser(t, userID, initQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-cas-refund-win", tokenRemain)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS wins: task in DB should now be FAILURE
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Zero(t, reloaded.Quota)

	// Refund should have happened
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestCASGuardedRefund_Lose(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 21, 21, 21
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	taskfixture.SeedUser(t, userID, initQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-cas-refund-lose", tokenRemain)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	// Create task with IN_PROGRESS in DB
	task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate another process already transitioning to FAILURE
	model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusFailure)

	// Our process still has the old in-memory state (IN_PROGRESS) and tries to transition
	// task.Status is still IN_PROGRESS in the snapshot
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS lost: user quota should NOT change (no double refund)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))

	// No billing log should be created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestCASGuardedSettle_Win(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 22, 22, 22
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged, should get partial refund
	const tokenRemain = 8000

	taskfixture.SeedUser(t, userID, initQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-cas-settle-win", tokenRemain)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusSuccess), actualQuota)

	// CAS wins: task should be SUCCESS
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)

	// Settlement should refund the over-charge (5000 - 3000 = 2000 back to user)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)
}

func TestNonTerminalUpdate_NoBilling(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, channelID = 23, 23
	const initQuota, preConsumed = 10000, 3000

	taskfixture.SeedUser(t, userID, initQuota)
	taskfixture.SeedChannel(t, channelID)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	task.Progress = "20%"
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate a non-terminal poll update (still IN_PROGRESS, progress changed)
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusInProgress), 0)

	// User quota should NOT change
	assert.Equal(t, initQuota, getUserQuota(t, userID))

	// No billing log
	assert.Equal(t, int64(0), countLogs(t))

	// Task progress should be updated in DB
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, "50%", reloaded.Progress)
}

// ===========================================================================
// Mock adaptor for settleTaskBillingOnComplete tests
// ===========================================================================

type mockAdaptor struct {
	adjustReturn int
}

func (m *mockAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (m *mockAdaptor) FetchTask(string, string, *model.Task, string) (*http.Response, error) {
	return nil, nil
}

func (m *mockAdaptor) ParseTaskResult(*model.Task, *http.Response, []byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (m *mockAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return m.adjustReturn
}

// ===========================================================================
// PerCallBilling tests — settleTaskBillingOnComplete
// ===========================================================================

func TestSettle_PerCallBilling_SkipsAdaptorAdjust(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 30, 30, 30
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 8000

	taskfixture.SeedUser(t, userID, initQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-percall-adaptor", tokenRemain)
	taskfixture.SeedChannel(t, channelID)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 2000}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settled, err := settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)
	require.NoError(t, err)

	// Per-call: no adjustment despite adaptor returning 2000
	assert.False(t, settled)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_PerCallBilling_SkipsTotalTokens(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 31, 31, 31
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 7000

	taskfixture.SeedUser(t, userID, initQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-percall-tokens", tokenRemain)
	taskfixture.SeedChannel(t, channelID)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 0}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 9999}

	settled, err := settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)
	require.NoError(t, err)

	// Per-call: no recalculation by tokens
	assert.False(t, settled)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_NonPerCallBilling_AppliesAdaptorAdjustment(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 32, 32, 32
	const initQuota, preConsumed = 10000, 5000
	const adaptorQuota = 3000
	const tokenRemain = 8000

	taskfixture.SeedUser(t, userID, initQuota)
	taskfixture.SeedToken(t, tokenID, userID, "sk-nonpercall-adj", tokenRemain)
	taskfixture.SeedChannel(t, channelID)

	task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	// PerCallBilling defaults to false

	adaptor := &mockAdaptor{adjustReturn: adaptorQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settled, err := settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)
	require.NoError(t, err)

	// Non-per-call: adaptor adjustment applies (refund 2000)
	assert.True(t, settled)
	assert.Equal(t, initQuota+(preConsumed-adaptorQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-adaptorQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, adaptorQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestSettle_TieredEvaluationFailureKeepsPreConsumedCharge(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID, preConsumed = 33, 5_000
	const initialQuota = 10_000
	taskfixture.SeedUser(t, userID, initialQuota)

	task := taskfixture.MakeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		ExprString:       `tier("broken",`,
		ExprHash:         billingexpr.ExprHashString(`tier("broken",`),
		GroupRatio:       1,
		QuotaPerUnit:     1_000,
		ExprVersion:      1,
		TaskUsageBilling: true,
	}

	settled, err := settleTaskBillingOnComplete(ctx, &mockAdaptor{}, task, &relaycommon.TaskInfo{Status: model.TaskStatusFailure})
	require.NoError(t, err)

	assert.True(t, settled)
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_TieredFailureReturnsFalseForCallerRefund(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID = 37
	const initialQuota, preConsumed = 10_000, 25
	taskfixture.SeedUser(t, userID, initialQuota)

	expression := `tier("base", u("seconds") + u("clips") * 10)`
	task := taskfixture.MakeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatusFailure
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		ExprString:       expression,
		ExprHash:         billingexpr.ExprHashString(expression),
		GroupRatio:       1,
		QuotaPerUnit:     1,
		ExprVersion:      1,
		TaskUsageBilling: true,
		UsageFacts:       map[string]any{"seconds": float64(5), "clips": float64(2)},
		EstimatedTier:    "base",
	}

	settled, _ := settleTaskBillingOnComplete(
		ctx,
		&mockAdaptor{adjustReturn: 1},
		task,
		&relaycommon.TaskInfo{Status: model.TaskStatusFailure, UsageFacts: map[string]any{"seconds": float64(8)}},
	)

	assert.False(t, settled)
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, map[string]any{"seconds": float64(5), "clips": float64(2)}, task.PrivateData.BillingContext.TieredSnapshot.UsageFacts)
	assert.Equal(t, "base", task.PrivateData.BillingContext.TieredSnapshot.EstimatedTier)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_TieredSuccessStillRecomputes(t *testing.T) {
	taskfixture.Truncate(t)
	ctx := context.Background()

	const userID = 38
	const initialQuota, preConsumed = 10_000, 50
	taskfixture.SeedUser(t, userID, initialQuota)

	expression := `tier("base", u("seconds") + u("clips") * 10)`
	task := taskfixture.MakeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		ExprString:       expression,
		ExprHash:         billingexpr.ExprHashString(expression),
		GroupRatio:       1,
		QuotaPerUnit:     1,
		ExprVersion:      1,
		TaskUsageBilling: true,
		UsageFacts:       map[string]any{"seconds": float64(5), "clips": float64(2)},
		EstimatedTier:    "base",
	}

	settled, _ := settleTaskBillingOnComplete(
		ctx,
		&mockAdaptor{adjustReturn: 1},
		task,
		&relaycommon.TaskInfo{Status: model.TaskStatusSuccess, UsageFacts: map[string]any{"seconds": float64(8)}},
	)

	assert.True(t, settled)
	assert.Equal(t, 28, task.Quota)
	assert.Equal(t, map[string]any{"seconds": float64(8), "clips": float64(2)}, task.PrivateData.BillingContext.TieredSnapshot.UsageFacts)
	assert.Equal(t, "base", task.PrivateData.BillingContext.TieredSnapshot.EstimatedTier)
	assert.Equal(t, initialQuota+(preConsumed-28), getUserQuota(t, userID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, "tiered_expr", other["billing_mode"])
	assert.Equal(t, "base", other["matched_tier"])
	facts, ok := other["usage_facts"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"seconds": float64(8), "clips": float64(2)}, facts)
}

func TestSettle_TieredUsageFactsMergeCompletionOverSubmission(t *testing.T) {
	tests := []struct {
		name            string
		completionFacts map[string]any
		expectedQuota   int
		expectedFacts   map[string]any
	}{
		{
			name:          "submission facts survive missing completion facts",
			expectedQuota: 25,
			expectedFacts: map[string]any{"seconds": float64(5), "clips": float64(2)},
		},
		{
			name:            "completion facts partially override submission facts",
			completionFacts: map[string]any{"seconds": float64(8)},
			expectedQuota:   28,
			expectedFacts:   map[string]any{"seconds": float64(8), "clips": float64(2)},
		},
		{
			name:            "completion facts fully override submission facts",
			completionFacts: map[string]any{"seconds": float64(8), "clips": float64(3)},
			expectedQuota:   38,
			expectedFacts:   map[string]any{"seconds": float64(8), "clips": float64(3)},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			taskfixture.Truncate(t)
			const userID = 34
			const initialQuota = 10_000
			const preConsumed = 50
			taskfixture.SeedUser(t, userID, initialQuota)

			expression := `tier("base", u("seconds") + u("clips") * 10)`
			submissionFacts := map[string]any{"seconds": float64(5), "clips": float64(2)}
			task := taskfixture.MakeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
			task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
				ExprString:       expression,
				ExprHash:         billingexpr.ExprHashString(expression),
				GroupRatio:       1,
				QuotaPerUnit:     1,
				ExprVersion:      1,
				TaskUsageBilling: true,
				UsageFacts:       submissionFacts,
				EstimatedTier:    "base",
			}

			settled, _ := settleTaskBillingOnComplete(
				context.Background(),
				&mockAdaptor{},
				task,
				&relaycommon.TaskInfo{Status: model.TaskStatusSuccess, UsageFacts: testCase.completionFacts},
			)

			assert.True(t, settled)
			assert.Equal(t, testCase.expectedQuota, task.Quota)
			assert.Equal(t, map[string]any{"seconds": float64(5), "clips": float64(2)}, submissionFacts)
			require.NotNil(t, task.PrivateData.BillingContext.TieredSnapshot)
			assert.Equal(t, testCase.expectedFacts, task.PrivateData.BillingContext.TieredSnapshot.UsageFacts)
			assert.Equal(t, "base", task.PrivateData.BillingContext.TieredSnapshot.EstimatedTier)

			log := getLastLog(t)
			require.NotNil(t, log)
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
			assert.Equal(t, "tiered_expr", other["billing_mode"])
			assert.Equal(t, "base", other["matched_tier"])
			facts, ok := other["usage_facts"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, testCase.expectedFacts, facts)
			assert.NotContains(t, other, "seconds")
			assert.NotContains(t, other, "clips")
		})
	}
}

func TestSettle_TieredSnapshotWriteBackUsesSettledFactsAndMatchedTier(t *testing.T) {
	taskfixture.Truncate(t)
	const userID = 36
	const initialQuota = 10_000
	const preConsumed = 25
	taskfixture.SeedUser(t, userID, initialQuota)

	expression := `u("resolution") == "1080P" ? tier("1080P", u("seconds") * 10) : tier("720P", u("seconds") * 5)`
	task := taskfixture.MakeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		ExprString:       expression,
		ExprHash:         billingexpr.ExprHashString(expression),
		GroupRatio:       1,
		QuotaPerUnit:     1,
		ExprVersion:      1,
		TaskUsageBilling: true,
		UsageFacts:       map[string]any{"resolution": "720P", "seconds": float64(5)},
		EstimatedTier:    "720P",
	}

	settled, _ := settleTaskBillingOnComplete(
		context.Background(),
		&mockAdaptor{},
		task,
		&relaycommon.TaskInfo{
			Status:     model.TaskStatusSuccess,
			UsageFacts: map[string]any{"resolution": "1080P"},
		},
	)

	require.True(t, settled)
	snap := task.PrivateData.BillingContext.TieredSnapshot
	require.NotNil(t, snap)
	assert.Equal(t, map[string]any{"resolution": "1080P", "seconds": float64(5)}, snap.UsageFacts)
	assert.Equal(t, "1080P", snap.EstimatedTier)
	assert.Equal(t, 50, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, "tiered_expr", other["billing_mode"])
	assert.Equal(t, "1080P", other["matched_tier"])
	facts, ok := other["usage_facts"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1080P", facts["resolution"])
	assert.Equal(t, float64(5), facts["seconds"])
	assert.NotContains(t, other, "resolution")
	assert.NotContains(t, other, "seconds")
}

func TestSettle_TokenRecalcFallsBackToCompletionTokens(t *testing.T) {
	previousRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"test-model":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
	})

	tests := []struct {
		name             string
		totalTokens      int
		completionTokens int
		wantSettled      bool
		wantQuota        int
	}{
		{
			name:             "total tokens still win when both are present",
			totalTokens:      80,
			completionTokens: 20,
			wantSettled:      true,
			wantQuota:        80,
		},
		{
			name:             "completion tokens trigger recalc when total is zero",
			totalTokens:      0,
			completionTokens: 80,
			wantSettled:      true,
			wantQuota:        80,
		},
		{
			name:        "neither token count skips recalc",
			wantSettled: false,
			wantQuota:   50,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			taskfixture.Truncate(t)
			const userID, tokenID, channelID = 35, 35, 35
			const initialQuota, preConsumed, tokenRemain = 10_000, 50, 8_000
			taskfixture.SeedUser(t, userID, initialQuota)
			taskfixture.SeedToken(t, tokenID, userID, "sk-completion-fallback", tokenRemain)
			taskfixture.SeedChannel(t, channelID)

			task := taskfixture.MakeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
			settled, _ := settleTaskBillingOnComplete(
				context.Background(),
				&mockAdaptor{},
				task,
				&relaycommon.TaskInfo{
					Status:           model.TaskStatusSuccess,
					TotalTokens:      testCase.totalTokens,
					CompletionTokens: testCase.completionTokens,
				},
			)

			assert.Equal(t, testCase.wantSettled, settled)
			assert.Equal(t, testCase.wantQuota, task.Quota)
		})
	}
}

func TestRecalculateTaskQuotaByTokensUsesPersistedFXAfterReload(t *testing.T) {
	taskfixture.Truncate(t)
	previousRatios := ratio_setting.ModelRatio2JSONString()
	previousGroups := ratio_setting.GroupRatio2JSONString()
	previousSpecialGroups := ratio_setting.GroupGroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"test-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":3}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(previousSpecialGroups))
	})

	const userID, channelID, preConsumed = 61, 61, 20
	taskfixture.SeedUser(t, userID, 10_000)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)

	specialRatio := 2.0
	task := taskfixture.MakeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.GroupRatio = 4
	task.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(200)
	task.PrivateData.BillingContext.SubmitGroup = &model.TaskBillingSubmitGroup{
		PureRatio:       2,
		HasSpecialRatio: true,
		SpecialRatio:    &specialRatio,
	}
	require.NoError(t, model.DB.Create(task).Error)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.NotNil(t, reloaded.PrivateData.BillingContext)
	require.NotNil(t, reloaded.PrivateData.BillingContext.BillingFX)
	require.NotNil(t, reloaded.PrivateData.BillingContext.SubmitGroup)

	require.True(t, RecalculateTaskQuotaByTokens(context.Background(), &reloaded, 10))
	assert.Equal(t, 60, reloaded.Quota)
	assert.Equal(t, 9_960, getUserQuota(t, userID))
	assert.Equal(t, int64(60), getChannelUsedQuota(t, channelID))

	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, float64(3), other["group_ratio"])
	assert.NotContains(t, other, "user_group_ratio")
	admin, ok := other["admin_info"].(map[string]any)
	require.True(t, ok)
	fx, ok := admin["billing_fx"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(1), fx["schema_version"])
	assert.Equal(t, "completion_tokens", admin["billing_stage"])
	assert.Equal(t, map[string]any{"pure_ratio": float64(3), "effective_ratio": float64(6)}, admin["billing_applied_group"])
	assert.Equal(t, map[string]any{"pure_ratio": float64(2), "has_special_ratio": true, "special_ratio": float64(2)}, admin["billing_submit_group"])
}

func TestTaskBillingRealDialectPersistenceAndLogPrivacy(t *testing.T) {
	taskfixture.Truncate(t)
	previousRatios := ratio_setting.ModelRatio2JSONString()
	previousGroups := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"test-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":3}`))

	const userID, channelID, preConsumed = 91, 91, 20
	taskfixture.SeedUser(t, userID, 10_000)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)

	now := time.Now().Unix()
	require.NoError(t, fxstorage.SaveModelCostFX(context.Background(), pricingdomainfx.ModelCostFXSnapshot{
		Source: pricingdomainfx.ModelCostFXSource, EffectiveAt: now, FetchedAt: now, Rates: map[string]float64{"USD": 100},
	}, 0))
	specialRatio := 2.0
	modern := taskfixture.MakeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	modern.PrivateData.BillingContext.GroupRatio = 4
	modern.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(200)
	modern.PrivateData.BillingContext.SubmitGroup = &model.TaskBillingSubmitGroup{
		PureRatio: 2, HasSpecialRatio: true, SpecialRatio: &specialRatio,
	}
	require.NoError(t, model.DB.Create(modern).Error)

	modern.Progress = "25%"
	require.NoError(t, fixtureStore.UpdateTask(modern))
	modern.Status = model.TaskStatusSuccess
	won, err := fixtureStore.UpdateTaskWithStatus(modern, model.TaskStatusInProgress)
	require.NoError(t, err)
	require.True(t, won)

	require.NoError(t, fxstorage.SaveModelCostFX(context.Background(), pricingdomainfx.ModelCostFXSnapshot{
		Source: pricingdomainfx.ModelCostFXSource, EffectiveAt: now + 1, FetchedAt: now + 1, Rates: map[string]float64{"USD": 300},
	}, 1))
	var changed model.ModelCostFXRow
	require.NoError(t, model.DB.Where("source = ?", pricingdomainfx.ModelCostFXSource).First(&changed).Error)
	assert.Contains(t, changed.RatesJSON, "300")

	taskfixture.WithOtherNode(t, func() {
		var reloaded model.Task
		require.NoError(t, model.DB.First(&reloaded, modern.ID).Error)
		require.Equal(t, "25%", reloaded.Progress)
		require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
		require.NotNil(t, reloaded.PrivateData.BillingContext)
		require.Equal(t, 2.0, reloaded.PrivateData.BillingContext.BillingFX.Rate/100)
		require.True(t, RecalculateTaskQuotaByTokens(context.Background(), &reloaded, 10))
		require.Equal(t, 60, reloaded.Quota)
	})
	assert.Equal(t, 9_960, getUserQuota(t, userID))
	assert.Equal(t, int64(60), getChannelUsedQuota(t, channelID))

	const legacyUserID, legacyChannelID = 92, 92
	taskfixture.SeedUser(t, legacyUserID, 10_000)
	taskfixture.SeedChannel(t, legacyChannelID)
	taskfixture.SeedChargedAccounting(t, legacyUserID, legacyChannelID, 0, preConsumed, 1)
	legacy := taskfixture.MakeTask(legacyUserID, legacyChannelID, preConsumed, 0, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(legacy).Error)
	require.True(t, RecalculateTaskQuotaByTokens(context.Background(), legacy, 10))
	assert.Equal(t, 30, legacy.Quota)

	const refundUserID, refundChannelID, refundQuota = 93, 93, 40
	taskfixture.SeedUser(t, refundUserID, 9_960)
	taskfixture.SeedChannel(t, refundChannelID)
	taskfixture.SeedChargedAccounting(t, refundUserID, refundChannelID, 0, refundQuota, 1)
	refund := taskfixture.MakeTask(refundUserID, refundChannelID, refundQuota, 0, BillingSourceWallet, 0)
	refund.PrivateData.BillingContext.GroupRatio = 4
	refund.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(200)
	refund.PrivateData.BillingContext.SubmitGroup = &model.TaskBillingSubmitGroup{PureRatio: 2}
	require.NoError(t, model.DB.Create(refund).Error)
	require.True(t, RefundTaskQuota(context.Background(), refund, "real dialect refund"))
	assert.Equal(t, 10_000, getUserQuota(t, refundUserID))
	assert.Zero(t, getTaskQuota(t, refund.ID))

	zeroDelta := taskfixture.MakeTask(userID, channelID, 60, 0, BillingSourceWallet, 0)
	zeroDelta.PrivateData.BillingContext.GroupRatio = 4
	zeroDelta.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(200)
	zeroDelta.PrivateData.BillingContext.SubmitGroup = &model.TaskBillingSubmitGroup{PureRatio: 2}
	require.NoError(t, model.DB.Create(zeroDelta).Error)
	logsBefore := countLogs(t)
	require.True(t, RecalculateTaskQuotaByTokens(context.Background(), zeroDelta, 10))
	assert.Equal(t, logsBefore, countLogs(t), "zero delta must not add a billing log")

	gin.SetMode(gin.TestMode)
	priceData := types.PriceData{
		ModelPrice: 0.02,
		GroupRatioInfo: types.GroupRatioInfo{
			GroupRatio: 2, GroupSpecialRatio: 2, HasSpecialRatio: true,
		},
		BillingGroupRatio: 4,
	}
	logInfo := &relaycommon.RelayInfo{BillingFXRate: 200, BillingFXFactor: 2, BillingFXSource: pricingdomainfx.ModelCostFXSource, BillingFXPublicationVersion: 1, BillingFXEffectiveAt: now, BillingFXFetchedAt: now, PriceData: priceData}
	taskfixture.SeedToken(t, 191, userID, "fx-real-3db-token", 10_000)
	for _, clampCase := range []struct {
		requestID string
		convert   func() (int, *common.QuotaClamp)
		original  any
		op        string
		kind      common.QuotaClampKind
		quota     int
	}{
		{"fx-real-3db-log", func() (int, *common.QuotaClamp) { return common.QuotaFromFloatChecked(math.Inf(1)) }, "+Inf", "QuotaFromFloat", common.QuotaClampOverflow, common.MaxQuota},
		{"fx-real-3db-nan", func() (int, *common.QuotaClamp) { return common.QuotaFromFloatChecked(math.NaN()) }, "NaN", "QuotaFromFloat", common.QuotaClampNaN, 0},
		{"fx-real-3db-neg-inf", func() (int, *common.QuotaClamp) { return common.QuotaFromFloatChecked(math.Inf(-1)) }, "-Inf", "QuotaFromFloat", common.QuotaClampUnderflow, common.MinQuota},
		{"fx-real-3db-finite", func() (int, *common.QuotaClamp) { return common.QuotaFromFloatChecked(float64(common.MaxQuota) + 1) }, float64(common.MaxQuota) + 1, "QuotaFromFloat", common.QuotaClampOverflow, common.MaxQuota},
		{"fx-real-3db-decimal", func() (int, *common.QuotaClamp) {
			return common.QuotaFromDecimalChecked(decimal.RequireFromString("1e400"))
		}, "+Inf", "QuotaFromDecimal", common.QuotaClampOverflow, common.MaxQuota},
	} {
		quota, clamp := clampCase.convert()
		require.NotNil(t, clamp, clampCase.requestID)
		assert.Equal(t, clampCase.quota, quota, clampCase.requestID)
		assert.Equal(t, clampCase.op, clamp.Op, clampCase.requestID)
		assert.Equal(t, clampCase.kind, clamp.Kind, clampCase.requestID)
		assert.Equal(t, quota, clamp.Clamped, clampCase.requestID)
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Set(common.RequestIdKey, clampCase.requestID)
		other := usagegateway.MidjourneyOther(logInfo, priceData)
		usagegateway.AppendTaskClamp(other, clamp)
		model.RecordConsumeLog(ctx, userID, model.RecordConsumeLogParams{
			ChannelId: channelID, TokenId: 191, TokenName: "fx-real-3db-token", ModelName: "test-model", Quota: 60, Group: "default", Other: other,
		})
		var persistedClamp model.Log
		require.NoError(t, model.LOG_DB.Where("request_id = ?", clampCase.requestID).First(&persistedClamp).Error)
		var persistedOther map[string]any
		require.NoError(t, common.UnmarshalJsonStr(persistedClamp.Other, &persistedOther))
		admin, ok := persistedOther["admin_info"].(map[string]any)
		require.True(t, ok, clampCase.requestID)
		assert.Equal(t, map[string]any{
			"original": clampCase.original, "op": clampCase.op,
			"kind": string(clampCase.kind), "clamped": float64(clampCase.quota),
		}, admin["quota_saturation"], clampCase.requestID)
		userLogs, total, err := (usageadapter.Store{}).ListOwnLogs(usagedomain.LogQuery{CountLimit: usagedomain.MaxLogSearchCount,
			UserID: userID, Kind: model.LogTypeConsume, StartTimestamp: 0, EndTimestamp: 0, ModelName: "",
			TokenName: "", Start: 0, Count: 10, Group: "", RequestID: clampCase.requestID, UpstreamRequestID: ""},
			usagedomain.LogVisibility(usagedomain.AudienceUser))
		require.NoError(t, err)
		require.Equal(t, int64(1), total)
		require.Len(t, userLogs, 1)
		tokenLogs, err := (usageadapter.Store{}).ListKeyLogs(191, common.MaxRecentItems, usagedomain.LogVisibility(usagedomain.AudienceUser))
		require.NoError(t, err)
		require.NotEmpty(t, tokenLogs)
		require.Equal(t, clampCase.requestID, tokenLogs[0].RequestId)
		for _, projected := range []*usagedto.Log{userLogs[0], tokenLogs[0]} {
			var public map[string]any
			require.NoError(t, common.UnmarshalJsonStr(projected.Other, &public))
			assert.NotContains(t, public, "admin_info", clampCase.requestID)
			assert.NotContains(t, public, "quota_saturation", clampCase.requestID)
			assert.Equal(t, float64(2), public["group_ratio"], clampCase.requestID)
		}
	}

	var persisted model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ?", "fx-real-3db-log").First(&persisted).Error)
	var storedOther map[string]any
	require.NoError(t, common.UnmarshalJsonStr(persisted.Other, &storedOther))
	assert.Equal(t, float64(2), storedOther["group_ratio"])
	admin, ok := storedOther["admin_info"].(map[string]any)
	require.True(t, ok)
	fx, ok := admin["billing_fx"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(200), fx["rate"])
	assert.Equal(t, float64(1), fx["schema_version"])
	assert.Equal(t, pricingdomainfx.ModelCostFXSource, fx["source"])
	assert.Equal(t, float64(1), fx["publication_version"])
	assert.Equal(t, float64(now), fx["effective_at"])
	assert.Equal(t, float64(now), fx["fetched_at"])
	assert.Equal(t, "submit", admin["billing_stage"])
	saturation, ok := admin["quota_saturation"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "+Inf", saturation["original"])

	adminProjection := []*model.Log{{Other: persisted.Other}}
	adminProjection[0].Other = usagerecord.FormatLogOtherJSON(
		adminProjection[0].Other, usagedomain.LogVisibility(usagedomain.AudienceAdmin))
	assert.Contains(t, adminProjection[0].Other, "billing_fx")

}

func TestRecalculateTaskQuotaByTokensKeepsLegacyArithmetic(t *testing.T) {
	taskfixture.Truncate(t)
	previousRatios := ratio_setting.ModelRatio2JSONString()
	previousGroups := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"test-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":3}`))

	const userID, channelID, preConsumed = 62, 62, 20
	taskfixture.SeedUser(t, userID, 10_000)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)
	task := taskfixture.MakeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)

	require.True(t, RecalculateTaskQuotaByTokens(context.Background(), task, 10))
	assert.Equal(t, 30, task.Quota)
	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.NotContains(t, other, "admin_info")
}

func TestRecalculateTaskQuotaByTokensRejectsIncompleteModernFXHistory(t *testing.T) {
	taskfixture.Truncate(t)
	previousRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"test-model":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
	})

	const userID, preConsumed = 63, 20
	taskfixture.SeedUser(t, userID, 10_000)
	task := taskfixture.MakeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(200)

	assert.False(t, RecalculateTaskQuotaByTokens(context.Background(), task, 10))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, 10_000, getUserQuota(t, userID))
	assert.Zero(t, countLogs(t))
}

func TestTaskBillingFXHistoryRejectsPresentNullAndMissingMembers(t *testing.T) {
	for _, raw := range []string{
		`{"billing_context":{"billing_fx":null,"submit_group":null,"group_ratio":1}}`,
		`{"billing_context":{"billing_fx":{"schema_version":1,"source":"cbr-xml-daily.ru","rate":100,"publication_version":1,"effective_at":1,"fetched_at":1},"submit_group":{},"group_ratio":0}}`,
		`{"billing_context":{"billing_fx":"wrong","submit_group":{"pure_ratio":0,"has_special_ratio":true,"special_ratio":0},"group_ratio":0}}`,
	} {
		var privateData model.TaskPrivateData
		require.NoError(t, privateData.Scan(raw))
		_, modern, err := taskdomain.TaskBillingFXFactor(record.ToDomainTaskBillingContext(privateData.BillingContext), pricingdomainfx.ModelCostFXSource)
		assert.True(t, modern)
		assert.Error(t, err)
	}
}

func TestTaskCompletionCallersKeepInvalidHistorySeparateFromRefundEligibility(t *testing.T) {
	t.Run("batch skips malformed token completion and refunds independently failed tiered task", func(t *testing.T) {
		taskfixture.Truncate(t)
		const channelID = 701
		taskfixture.SeedChannel(t, channelID)

		taskfixture.SeedUser(t, 701, 10_000)
		taskfixture.SeedChargedAccounting(t, 701, channelID, 0, 100, 1)
		invalid := taskfixture.MakeTask(701, channelID, 100, 0, BillingSourceWallet, 0)
		invalid.TaskID = "invalid-batch"
		invalid.PrivateData.UpstreamTaskID = "invalid-batch-upstream"
		invalid.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(100)
		require.NoError(t, model.DB.Create(invalid).Error)
		var invalidReloaded model.Task
		require.NoError(t, model.DB.First(&invalidReloaded, invalid.ID).Error)

		taskfixture.SeedUser(t, 702, 10_000)
		taskfixture.SeedChargedAccounting(t, 702, channelID, 0, 40, 1)
		tiered := taskfixture.MakeTask(702, channelID, 40, 0, BillingSourceWallet, 0)
		tiered.TaskID = "invalid-tiered-batch"
		tiered.PrivateData.UpstreamTaskID = "invalid-tiered-batch-upstream"
		tiered.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(100)
		tiered.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{ExprString: `1`, ExprHash: billingexpr.ExprHashString(`1`), GroupRatio: 1, QuotaPerUnit: 1, ExprVersion: 1, TaskUsageBilling: true}
		require.NoError(t, model.DB.Create(tiered).Error)
		var tieredReloaded model.Task
		require.NoError(t, model.DB.First(&tieredReloaded, tiered.ID).Error)

		adaptor := &scriptedBatchPollingAdaptor{results: map[string]*BatchTaskResult{
			invalidReloaded.GetUpstreamTaskID(): {TaskInfo: relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 10}},
			tieredReloaded.GetUpstreamTaskID():  {TaskInfo: relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: "known failed tiered task"}},
		}}
		require.NoError(t, updateBatchTasks(context.Background(), adaptor, channelID,
			[]string{invalidReloaded.GetUpstreamTaskID(), tieredReloaded.GetUpstreamTaskID()},
			map[string]*model.Task{invalidReloaded.GetUpstreamTaskID(): &invalidReloaded, tieredReloaded.GetUpstreamTaskID(): &tieredReloaded}))

		assert.Equal(t, 100, getTaskQuota(t, invalid.ID))
		assert.Equal(t, 10_000, getUserQuota(t, 701))
		assert.Zero(t, getTaskQuota(t, tiered.ID))
		assert.Equal(t, 10_040, getUserQuota(t, 702))
		assert.Equal(t, int64(1), countLogs(t))
	})

	t.Run("single video refunds independently failed per-call task despite malformed history", func(t *testing.T) {
		taskfixture.Truncate(t)
		const userID, channelID, savedQuota = 703, 703, 70
		taskfixture.SeedUser(t, userID, 10_000)
		taskfixture.SeedChargedAccounting(t, userID, channelID, 0, savedQuota, 1)
		task := taskfixture.MakeTask(userID, channelID, savedQuota, 0, BillingSourceWallet, 0)
		task.TaskID = "invalid-video"
		task.PrivateData.UpstreamTaskID = "invalid-video-upstream"
		task.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(100)
		task.PrivateData.BillingContext.PerCallBilling = true
		require.NoError(t, model.DB.Create(task).Error)
		var reloaded model.Task
		require.NoError(t, model.DB.First(&reloaded, task.ID).Error)

		adaptor := &scriptedPollingAdaptor{parse: &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: "known failure"}}
		require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, &model.Channel{Id: channelID, Key: "test"}, reloaded.GetUpstreamTaskID(), map[string]*model.Task{reloaded.GetUpstreamTaskID(): &reloaded}))

		assert.Zero(t, getTaskQuota(t, task.ID))
		assert.Equal(t, 10_070, getUserQuota(t, userID))
		assert.Equal(t, int64(1), countLogs(t))
	})

	t.Run("fail poll retains history error without inventing adjustment, then refunds known tiered failure", func(t *testing.T) {
		taskfixture.Truncate(t)
		const channelID = 704
		taskfixture.SeedUser(t, 704, 10_000)
		taskfixture.SeedChargedAccounting(t, 704, channelID, 0, 70, 1)
		invalid := taskfixture.MakeTask(704, channelID, 70, 0, BillingSourceWallet, 0)
		invalid.TaskID = "invalid-fail"
		invalid.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(100)
		require.NoError(t, model.DB.Create(invalid).Error)
		var invalidReloaded model.Task
		require.NoError(t, model.DB.First(&invalidReloaded, invalid.ID).Error)

		err := failTaskFromPoll(context.Background(), &mockAdaptor{adjustReturn: 5}, &invalidReloaded, model.TaskStatusInProgress, "poll failure")
		require.Error(t, err)
		assert.Equal(t, 70, getTaskQuota(t, invalid.ID))
		assert.Equal(t, 10_000, getUserQuota(t, 704))
		assert.Zero(t, countLogs(t))

		taskfixture.SeedUser(t, 705, 10_000)
		taskfixture.SeedChargedAccounting(t, 705, channelID, 0, 30, 1)
		tiered := taskfixture.MakeTask(705, channelID, 30, 0, BillingSourceWallet, 0)
		tiered.TaskID = "invalid-tiered-fail"
		tiered.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(100)
		tiered.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{ExprString: `1`, ExprHash: billingexpr.ExprHashString(`1`), GroupRatio: 1, QuotaPerUnit: 1, ExprVersion: 1, TaskUsageBilling: true}
		require.NoError(t, model.DB.Create(tiered).Error)
		var tieredReloaded model.Task
		require.NoError(t, model.DB.First(&tieredReloaded, tiered.ID).Error)
		require.Error(t, failTaskFromPoll(context.Background(), &mockAdaptor{}, &tieredReloaded, model.TaskStatusInProgress, "known failed tiered task"))
		assert.Zero(t, getTaskQuota(t, tiered.ID))
		assert.Equal(t, 10_030, getUserQuota(t, 705))
		assert.Equal(t, int64(1), countLogs(t))
	})
}

func TestSweepTimedOutTasksRefundsEligibleMalformedPerCallHistoryWithoutFX(t *testing.T) {
	taskfixture.Truncate(t)
	previousTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = previousTimeout })

	const userID, channelID, savedQuota = 707, 707, 55
	taskfixture.SeedUser(t, userID, 10_000)
	taskfixture.SeedChannel(t, channelID)
	taskfixture.SeedChargedAccounting(t, userID, channelID, 0, savedQuota, 1)
	task := taskfixture.MakeTask(userID, channelID, savedQuota, 0, BillingSourceWallet, 0)
	task.TaskID = "invalid-timeout-per-call"
	task.SubmitTime = time.Now().Add(-2 * time.Minute).Unix()
	task.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(100)
	task.PrivateData.BillingContext.PerCallBilling = true
	require.NoError(t, model.DB.Create(task).Error)

	sweepTimedOutTasks(context.Background())

	assert.Zero(t, getTaskQuota(t, task.ID))
	assert.Equal(t, 10_055, getUserQuota(t, userID))
	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.NotContains(t, other, "admin_info")
}

func TestRecalculateTaskQuotaByTokensModernF1TruncatesFractionalDebit(t *testing.T) {
	oldM, oldG, oldS := ratio_setting.ModelRatio2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldM))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldG))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldS))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	for _, tc := range []struct {
		name                string
		ratio               float64
		tokens, saved, want int
		other               map[string]float64
	}{
		{"fraction", .6, 1, 1, 0, nil},
		{"binary64_boundary", .29, 100, 30, 28, nil},
		{"other_ratio_debit", .29, 100, 30, 57, map[string]float64{"duration": 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskfixture.Truncate(t)
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(fmt.Sprintf(`{"test-model":%g}`, tc.ratio)))
			for i, modern := range []bool{false, true} {
				id := 706 + i
				taskfixture.SeedUser(t, id, 10000)
				taskfixture.SeedChannel(t, id)
				taskfixture.SeedToken(t, id, id, fmt.Sprintf("f1-token-%d", id), 8000)
				taskfixture.SeedChargedAccounting(t, id, id, id, tc.saved, 1)
				task := taskfixture.MakeTask(id, id, tc.saved, id, BillingSourceWallet, 0)
				task.TaskID = fmt.Sprintf("f1-task-%d", id)
				task.PrivateData.BillingContext.OtherRatios = tc.other
				if modern {
					task.PrivateData.BillingContext.BillingFX = taskfixture.BillingFX(100)
					task.PrivateData.BillingContext.SubmitGroup = &model.TaskBillingSubmitGroup{PureRatio: 1}
				}
				require.NoError(t, model.DB.Create(task).Error)
				var loaded model.Task
				require.NoError(t, model.DB.First(&loaded, task.ID).Error)
				require.True(t, RecalculateTaskQuotaByTokens(context.Background(), &loaded, tc.tokens))
				assert.Equal(t, tc.want, getTaskQuota(t, task.ID))
				assert.Equal(t, 10000+tc.saved-tc.want, getUserQuota(t, id))
				assert.Equal(t, 8000+tc.saved-tc.want, getTokenRemainQuota(t, id))
				assert.Equal(t, tc.want, getTokenUsedQuota(t, id))
				log := getLastLog(t)
				require.NotNil(t, log)
				if tc.want > tc.saved {
					assert.Equal(t, model.LogTypeConsume, log.Type)
					assert.Equal(t, tc.want-tc.saved, log.Quota)
				} else {
					assert.Equal(t, model.LogTypeRefund, log.Type)
					assert.Equal(t, tc.saved-tc.want, log.Quota)
				}
			}
		})
	}
}

func TestCaptureBillingFXRejectsCustomDenominationDrift(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previous := settings.CustomCurrencyExchangeRate
	settings.CustomCurrencyExchangeRate = 1
	t.Cleanup(func() { settings.CustomCurrencyExchangeRate = previous })

	_, err := billinggatewayfx.CaptureBillingFX(&relaycommon.RelayInfo{BillingFXRate: 100, BillingFXFactor: 1})
	require.Error(t, err)
	assert.True(t, mappingfx.IsBillingFXError(err))
}

func TestApplyBillingFXUsesPureGroupRatioAndPublishedFactor(t *testing.T) {
	tests := []struct {
		name   string
		pure   float64
		factor float64
		want   float64
	}{
		{name: "standard group", pure: 1.5, factor: 0.9, want: 1.35},
		{name: "free group", pure: 0, factor: 1.2, want: 0},
		{name: "unchanged factor", pure: 1.5, factor: 1, want: 1.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := billinggatewayfx.ApplyBillingFX(tt.pure, tt.factor)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTaskBillingCompletionAppliedGroup(t *testing.T) {
	oldM, oldG, oldS := ratio_setting.ModelRatio2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldM))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldG))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldS))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"test-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":3}`))
	for _, tc := range []struct {
		name, stage string
		pure        float64
		special     bool
		want        int
	}{
		{"tiered_ordinary", "completion_tiered", 1, false, 2},
		{"tiered_special", "completion_tiered", 2, true, 4},
		{"token_special_zero", "completion_tokens", 0, true, 0},
		{"token_special", "completion_tokens", 2, true, 40},
		{"adaptor_absolute", "adaptor_quota", 2, true, 7},
		{"saved_refund", "saved_quota", 2, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskfixture.Truncate(t)
			const id = 718
			taskfixture.SeedUser(t, id, 10000)
			taskfixture.SeedChannel(t, id)
			task := taskfixture.MakeTask(id, id, 50, 0, BillingSourceWallet, 0)
			task.TaskID = "applied-group-task"
			task.Status = model.TaskStatusSuccess
			bc := task.PrivateData.BillingContext
			bc.BillingFX = taskfixture.BillingFX(200)
			bc.GroupRatio = tc.pure * 2
			bc.SubmitGroup = &model.TaskBillingSubmitGroup{PureRatio: tc.pure, HasSpecialRatio: tc.special}
			if tc.special {
				v := tc.pure
				bc.SubmitGroup.SpecialRatio = &v
			}
			require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(fmt.Sprintf(`{"default":{"default":%g}}`, tc.pure)))
			if tc.stage == "completion_tiered" {
				expr := `0.000002`
				bc.TieredSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expr, ExprHash: billingexpr.ExprHashString(expr), GroupRatio: tc.pure * 2, QuotaPerUnit: 500000, ExprVersion: 1, TaskUsageBilling: true}
			}
			require.NoError(t, model.DB.Create(task).Error)
			var loaded model.Task
			require.NoError(t, model.DB.First(&loaded, task.ID).Error)
			if tc.stage == "saved_quota" {
				require.True(t, RefundTaskQuota(context.Background(), &loaded, "saved refund"))
			} else {
				adaptor := &mockAdaptor{}
				if tc.stage == "adaptor_quota" {
					adaptor.adjustReturn = 7
				}
				handled, err := settleTaskBillingOnComplete(context.Background(), adaptor, &loaded, &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 10})
				require.NoError(t, err)
				require.True(t, handled)
			}
			assert.Equal(t, tc.want, getTaskQuota(t, task.ID))
			assert.Equal(t, 10050-tc.want, getUserQuota(t, id))
			log := getLastLog(t)
			require.NotNil(t, log)
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
			admin, ok := other["admin_info"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tc.stage, admin["billing_stage"])
			if tc.stage == "completion_tokens" || tc.stage == "completion_tiered" {
				applied := map[string]any{"pure_ratio": tc.pure, "effective_ratio": tc.pure * 2}
				if tc.special {
					applied["special_ratio"] = tc.pure
					assert.Equal(t, tc.pure, other["user_group_ratio"])
				} else {
					assert.NotContains(t, other, "user_group_ratio")
				}
				assert.Equal(t, applied, admin["billing_applied_group"])
			} else {
				assert.NotContains(t, admin, "billing_applied_group")
			}
			logsBefore := countLogs(t)
			RecalculateTaskQuota(context.Background(), &loaded, loaded.Quota, "unchanged")
			assert.Equal(t, logsBefore, countLogs(t))
		})
	}
}

func TestOpenRouterCostSettlement(t *testing.T) {
	for _, tc := range []struct {
		name                                                                string
		cost                                                                float64
		confirmed, present, failed, legacy, missingToken, logFailure, batch bool
		unknown, invalidBasis, v2                                           bool
		target                                                              int
	}{
		{name: "v2_lower", v2: true, cost: 0.00002, confirmed: true, present: true, target: 40},
		{name: "v2_zero", v2: true, confirmed: true, present: true},
		{name: "v2_missing", v2: true, target: 50},
		{name: "v2_invalid_basis", v2: true, invalidBasis: true, cost: 0.00002, confirmed: true, present: true, target: 50},
		{name: "v2_missing_token_rollback", v2: true, cost: 0.00002, confirmed: true, present: true, missingToken: true, target: 50},
		{name: "lower", cost: 0.00002, confirmed: true, present: true, target: 40},
		{name: "higher", cost: 0.00005, confirmed: true, present: true, target: 100},
		{name: "equal", cost: 0.000025, confirmed: true, present: true, target: 50},
		{name: "zero_success", confirmed: true, present: true},
		{name: "zero_failure", confirmed: true, present: true, failed: true},
		{name: "paid_failure", cost: 0.00002, confirmed: true, present: true, failed: true, target: 40},
		{name: "missing", target: 50},
		{name: "unknown_mode", unknown: true, cost: 0.00002, confirmed: true, present: true, target: 50},
		{name: "invalid_basis", invalidBasis: true, cost: 0.00002, confirmed: true, present: true, target: 50},

		{name: "byok_or_unknown", cost: 0.00002, present: true, target: 50},
		{name: "invalid_negative", cost: -1, confirmed: true, present: true, target: 50},
		{name: "legacy", cost: 0.00002, confirmed: true, present: true, legacy: true, target: 50},
		{name: "missing_token_rollback", cost: 0.00002, confirmed: true, present: true, missingToken: true, target: 50},
		{name: "log_failure_no_replay", cost: 0.00002, confirmed: true, present: true, logFailure: true, target: 40},
		{name: "batch_enabled", cost: 0.00002, confirmed: true, present: true, batch: true, target: 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskfixture.Truncate(t)
			const id = 819
			taskfixture.SeedUser(t, id, 9950)
			taskfixture.SeedChannel(t, id)
			if !tc.missingToken {
				taskfixture.SeedToken(t, id, id, "cost-token", 9950)
			}
			taskfixture.SeedChargedAccounting(t, id, id, id, 50, 1)
			task := taskfixture.MakeTask(id, id, 50, id, BillingSourceWallet, 0)
			task.PrivateData.BillingContext.SettlementMode = model.TaskSettlementOpenRouterCostV1
			if tc.legacy {
				task.PrivateData.BillingContext.SettlementMode = ""
			}
			if tc.unknown {
				task.PrivateData.BillingContext.SettlementMode = "future_mode"
			}
			bc := task.PrivateData.BillingContext
			bc.GroupRatio = 4
			bc.BillingFX = taskfixture.BillingFX(200)
			bc.SubmitGroup = &model.TaskBillingSubmitGroup{PureRatio: 2}
			if tc.invalidBasis {
				bc.BillingFX.Rate = 300
			}
			bc.TieredSnapshot = &billingexpr.BillingSnapshot{GroupRatio: 4, QuotaPerUnit: 500000, ExprString: "tier(\"reserve\", 0.000025)"}
			if tc.v2 {
				bc.SettlementMode = model.TaskSettlementOpenRouterCostV2
				bc.OpenRouterCost = &common.OpenRouterCostBasis{Version: model.TaskSettlementOpenRouterCostV2, EffectiveGroupRatio: 4, QuotaPerUnit: 500000, EstimatedUSD: 0.000025}
				bc.TieredSnapshot = nil
				bc.OpenRouterOrderFacts = map[string]any{"requested_seconds": float64(5), "images": float64(1)}
				if tc.invalidBasis {
					bc.OpenRouterCost.Version = "unknown_version"
				}
			}
			task.PrivateData.Reconciliation = &model.TaskReconciliation{ReservedQuota: 50, HasUpstreamCost: tc.present, UpstreamCostConfirmed: tc.confirmed, UpstreamCostUSD: tc.cost}
			require.NoError(t, model.DB.Create(task).Error)
			var stale model.Task
			require.NoError(t, model.DB.First(&stale, task.ID).Error)
			// Reload proves the manual snapshot wire format carries settlement mode.
			require.Equal(t, bc.SettlementMode, stale.PrivateData.BillingContext.SettlementMode)
			if tc.v2 {
				assert.Equal(t, bc.OpenRouterCost, stale.PrivateData.BillingContext.OpenRouterCost)
				assert.Equal(t, bc.OpenRouterOrderFacts, stale.PrivateData.BillingContext.OpenRouterOrderFacts)
			}
			task.Status = model.TaskStatusSuccess
			if tc.failed {
				task.Status = model.TaskStatusFailure
			}
			prepareOpenRouterCostSettlement(task)
			won, err := fixtureStore.UpdateTaskWithStatus(task, model.TaskStatusInProgress)
			require.NoError(t, err)
			require.True(t, won)
			oldBatch, oldLogging := common.BatchUpdateEnabled, common.LogConsumeEnabled
			common.BatchUpdateEnabled, common.LogConsumeEnabled = tc.batch, false
			t.Cleanup(func() { common.BatchUpdateEnabled, common.LogConsumeEnabled = oldBatch, oldLogging })
			if tc.logFailure {
				require.NoError(t, model.LOG_DB.Callback().Create().Before("gorm:create").Register("cost_log_failure", func(tx *gorm.DB) { tx.AddError(fmt.Errorf("injected log failure")) }))
			}
			settled, err := settleTaskBillingOnComplete(context.Background(), &mockAdaptor{}, task, &relaycommon.TaskInfo{})
			require.True(t, settled)
			if tc.logFailure {
				require.NoError(t, model.LOG_DB.Callback().Create().Remove("cost_log_failure"))
			}
			if tc.missingToken || tc.logFailure {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			stale.Status = task.Status
			stale.PrivateData = task.PrivateData
			won, err = fixtureStore.UpdateTaskWithStatus(&stale, model.TaskStatusInProgress)
			require.NoError(t, err)
			assert.False(t, won)
			var saved model.Task
			require.NoError(t, model.DB.First(&saved, task.ID).Error)
			if tc.missingToken {
				require.Error(t, settleOpenRouterCost(&saved))
			} else {
				require.NoError(t, settleOpenRouterCost(&saved))
			}
			assert.Equal(t, tc.target, saved.Quota)
			var user model.User
			require.NoError(t, model.DB.First(&user, id).Error)
			assert.Equal(t, 10000-tc.target, user.Quota)
			assert.Equal(t, tc.target, user.UsedQuota)
			assert.Equal(t, 1, user.RequestCount)
			var channel model.Channel
			require.NoError(t, model.DB.First(&channel, id).Error)
			assert.Equal(t, int64(tc.target), channel.UsedQuota)
			if !tc.missingToken {
				var token model.Token
				require.NoError(t, model.DB.First(&token, id).Error)
				assert.Equal(t, 10000-tc.target, token.RemainQuota)
				assert.Equal(t, tc.target, token.UsedQuota)
			}
			var logs []model.Log
			require.NoError(t, model.LOG_DB.Find(&logs).Error)
			wantLogs := 0
			if tc.target != 50 && !tc.logFailure {
				wantLogs = 1
			}
			require.Len(t, logs, wantLogs)
			if wantLogs == 1 {
				expectedDelta := tc.target - 50
				if expectedDelta < 0 {
					expectedDelta = -expectedDelta
				}
				assert.Equal(t, expectedDelta, logs[0].Quota)
				if tc.target < 50 {
					assert.Equal(t, model.LogTypeRefund, logs[0].Type)
				} else {
					assert.Equal(t, model.LogTypeConsume, logs[0].Type)
				}
				assert.Contains(t, logs[0].Other, bc.SettlementMode)
				assert.Contains(t, logs[0].Other, "admin_info")
				assert.NotContains(t, logs[0].Other, "expr_b64")
			}
			r := saved.PrivateData.Reconciliation
			if tc.logFailure {
				assert.True(t, r.MainApplied)
				assert.True(t, r.SettlementPending)
				assert.False(t, r.Finalized)
			}
			if tc.missingToken {
				assert.False(t, r.MainApplied)
				assert.True(t, r.SettlementPending)
			}
		})
	}
}

func TestOpenRouterCostSettlementCacheAndSubscription(t *testing.T) {
	for _, tc := range []struct {
		name                                         string
		subscription, overage, hot, cacheFailure, v2 bool
	}{
		{name: "subscription", subscription: true}, {name: "subscription_overage", subscription: true, overage: true},
		{name: "v2_subscription", subscription: true, v2: true}, {name: "v2_subscription_overage", subscription: true, overage: true, v2: true},
		{name: "v2_hot_cache", hot: true, v2: true}, {name: "v2_cache_failure", hot: true, cacheFailure: true, v2: true},
		{name: "hot_cache_concurrent_reserve", hot: true}, {name: "cold_cache"}, {name: "cache_failure_no_replay", hot: true, cacheFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskfixture.Truncate(t)
			const id = 820
			taskfixture.SeedUser(t, id, 9950)
			taskfixture.SeedChannel(t, id)
			taskfixture.SeedToken(t, id, id, "cost-cache-token", 9950)
			taskfixture.SeedChargedAccounting(t, id, id, id, 50, 1)
			source, subID := BillingSourceWallet, 0
			if tc.subscription {
				source, subID = BillingSourceSubscription, id
				total := int64(1000)
				if tc.overage {
					total = 60
				}
				taskfixture.SeedSubscription(t, id, id, total, 50)
			}
			task := taskfixture.MakeTask(id, id, 50, id, source, subID)
			bc := task.PrivateData.BillingContext
			bc.SettlementMode = model.TaskSettlementOpenRouterCostV1
			bc.GroupRatio = 4
			bc.BillingFX = taskfixture.BillingFX(200)
			bc.SubmitGroup = &model.TaskBillingSubmitGroup{PureRatio: 2}
			bc.TieredSnapshot = &billingexpr.BillingSnapshot{GroupRatio: 4, QuotaPerUnit: 500000}
			if tc.v2 {
				bc.SettlementMode = model.TaskSettlementOpenRouterCostV2
				bc.OpenRouterCost = &common.OpenRouterCostBasis{Version: model.TaskSettlementOpenRouterCostV2, EffectiveGroupRatio: 4, QuotaPerUnit: 500000, EstimatedUSD: 0.000025}
				bc.TieredSnapshot = nil
			}
			task.PrivateData.Reconciliation = &model.TaskReconciliation{ReservedQuota: 50, HasUpstreamCost: true, UpstreamCostConfirmed: true, UpstreamCostUSD: 0.00005}
			require.NoError(t, model.DB.Create(task).Error)
			oldGroups := ratio_setting.GroupRatio2JSONString()
			t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups)) })
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":17}`))
			task.Status = model.TaskStatusSuccess
			prepareOpenRouterCostSettlement(task)
			require.Equal(t, 100, task.PrivateData.Reconciliation.TargetQuota)
			won, err := fixtureStore.UpdateTaskWithStatus(task, model.TaskStatusInProgress)
			require.NoError(t, err)
			require.True(t, won)
			server := miniredis.RunT(t)
			previousRedis, previousClient, previousBatch := common.RedisEnabled, common.RDB, common.BatchUpdateEnabled
			client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			common.RedisEnabled, common.RDB, common.BatchUpdateEnabled = true, client, true
			t.Cleanup(func() {
				common.RedisEnabled, common.RDB, common.BatchUpdateEnabled = previousRedis, previousClient, previousBatch
				client.Close()
			})
			userKey, tokenKey := fmt.Sprintf("user:%d", id), "token:"+common.GenerateHMAC("cost-cache-token")
			if tc.hot {
				_, err := model.GetUserCache(id)
				require.NoError(t, err)
				require.NoError(t, client.HSet(context.Background(), tokenKey, "Id", id, "RemainQuota", 9950, "UsedQuota", 50).Err())
				// Another initial reservation has updated Redis while its DB delta is queued.
				require.NoError(t, client.HIncrBy(context.Background(), userKey, "Quota", -70).Err())
				require.NoError(t, client.HIncrBy(context.Background(), tokenKey, "RemainQuota", -70).Err())
				require.NoError(t, client.HIncrBy(context.Background(), tokenKey, "UsedQuota", 70).Err())
			}
			if tc.cacheFailure {
				server.Close()
			}
			err = settleOpenRouterCost(task)
			if tc.overage || tc.cacheFailure {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			var saved model.Task
			require.NoError(t, model.DB.First(&saved, task.ID).Error)
			expected := 100
			if tc.overage {
				expected = 50
			}
			assert.Equal(t, expected, saved.Quota)
			if tc.cacheFailure {
				require.NoError(t, settleOpenRouterCost(&saved))
				assert.True(t, saved.PrivateData.Reconciliation.MainApplied)
				assert.True(t, saved.PrivateData.Reconciliation.SettlementPending)
			}
			if tc.hot && !tc.cacheFailure {
				assert.Equal(t, "9830", server.HGet(userKey, "Quota"))
				assert.Equal(t, "9830", server.HGet(tokenKey, "RemainQuota"))
				assert.Equal(t, "170", server.HGet(tokenKey, "UsedQuota"))
			}
			if !tc.hot {
				assert.False(t, server.Exists(userKey))
				assert.False(t, server.Exists(tokenKey))
			}
			if tc.subscription {
				var sub model.UserSubscription
				require.NoError(t, model.DB.First(&sub, id).Error)
				assert.Equal(t, int64(expected), sub.AmountUsed)
				var user model.User
				require.NoError(t, model.DB.First(&user, id).Error)
				assert.Equal(t, 9950, user.Quota)
			}
		})
	}
}

func TestOpenRouterCostReservationLogRequired(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			taskfixture.Truncate(t)
			const id = 821
			taskfixture.SeedUser(t, id, 9950)
			taskfixture.SeedChannel(t, id)
			task := taskfixture.MakeTask(id, id, 50, 0, BillingSourceWallet, 0)
			task.PrivateData.BillingContext.SettlementMode = model.TaskSettlementOpenRouterCostV1
			info := &relaycommon.RelayInfo{UserId: id, OriginModelName: "test-model", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: id}, TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "GENERATE"}, PriceData: types.PriceData{Quota: 50, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			old := common.LogConsumeEnabled
			common.LogConsumeEnabled = false
			t.Cleanup(func() { common.LogConsumeEnabled = old })
			if fail {
				require.NoError(t, model.LOG_DB.Callback().Create().Before("gorm:create").Register("cost_reserve_log_failure", func(tx *gorm.DB) { tx.AddError(fmt.Errorf("injected reservation log failure")) }))
			}
			err := LogTaskConsumption(ctx, info, task)
			if fail {
				require.NoError(t, model.LOG_DB.Callback().Create().Remove("cost_reserve_log_failure"))
				require.Error(t, err)
				assert.Zero(t, countLogs(t))
			} else {
				require.NoError(t, err)
				log := getLastLog(t)
				require.NotNil(t, log)
				assert.Equal(t, 50, log.Quota)
				assert.Equal(t, model.LogTypeConsume, log.Type)
				assert.Contains(t, log.Other, "reserved_quota")
				assert.Contains(t, log.Other, "openrouter_cost_v1")
			}
		})
	}
}

func TestOpenRouterCostPerTaskPolling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status model.TaskStatus
		cost   float64
		target int
	}{
		{"lower", model.TaskStatusSuccess, 0.00002, 40}, {"higher", model.TaskStatusSuccess, 0.00005, 100},
		{"zero", model.TaskStatusSuccess, 0, 0}, {"paid_failure", model.TaskStatusFailure, 0.00002, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskfixture.Truncate(t)
			const id = 822
			taskfixture.SeedUser(t, id, 9950)
			taskfixture.SeedChannel(t, id)
			taskfixture.SeedToken(t, id, id, "poll-cost-token", 9950)
			taskfixture.SeedChargedAccounting(t, id, id, id, 50, 1)
			task := taskfixture.MakeTask(id, id, 50, id, BillingSourceWallet, 0)
			task.PrivateData.UpstreamTaskID = "upstream-cost-task"
			bc := task.PrivateData.BillingContext
			bc.SettlementMode = model.TaskSettlementOpenRouterCostV1
			bc.GroupRatio = 4
			bc.BillingFX = taskfixture.BillingFX(200)
			bc.SubmitGroup = &model.TaskBillingSubmitGroup{PureRatio: 2}
			bc.TieredSnapshot = &billingexpr.BillingSnapshot{GroupRatio: 4, QuotaPerUnit: 500000}
			task.PrivateData.Reconciliation = &model.TaskReconciliation{ReservedQuota: 50}
			require.NoError(t, model.DB.Create(task).Error)
			var stale model.Task
			require.NoError(t, model.DB.First(&stale, task.ID).Error)
			adaptor := &scriptedPollingAdaptor{body: []byte(`{}`), parse: &relaycommon.TaskInfo{Status: string(tc.status), UpstreamCostUSD: &tc.cost, UpstreamCostConfirmed: true}}
			channel := &model.Channel{Id: id, Type: constant.ChannelTypeOpenRouter, Key: "fixture"}
			require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, channel, task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): task}))
			// A stale nonterminal poller loses the terminal claim; a reloaded terminal
			// poller cannot overwrite finalized private state or replay settlement.
			require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, channel, stale.GetUpstreamTaskID(), map[string]*model.Task{stale.GetUpstreamTaskID(): &stale}))
			var loaded model.Task
			require.NoError(t, model.DB.First(&loaded, task.ID).Error)
			require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, channel, loaded.GetUpstreamTaskID(), map[string]*model.Task{loaded.GetUpstreamTaskID(): &loaded}))
			require.NoError(t, model.DB.First(&loaded, task.ID).Error)
			assert.Equal(t, tc.status, loaded.Status)
			assert.Equal(t, tc.target, loaded.Quota)
			assert.True(t, loaded.PrivateData.Reconciliation.Finalized)
			assert.True(t, loaded.PrivateData.Reconciliation.MainApplied)
			assert.False(t, loaded.PrivateData.Reconciliation.Required)
			assert.False(t, loaded.PrivateData.Reconciliation.SettlementPending)
			assert.Equal(t, 10000-tc.target, getUserQuota(t, id))
			assert.Equal(t, 10000-tc.target, getTokenRemainQuota(t, id))
			used, count := getUserUsageAccounting(t, id)
			assert.Equal(t, tc.target, used)
			assert.Equal(t, 1, count)
			assert.Equal(t, int64(tc.target), getChannelUsedQuota(t, id))
			assert.Equal(t, int64(1), countLogs(t))
		})
	}
}

// newOpenRouterAudioReservation exercises the real wallet/token reserve and the
// durable ownership transfer. Provider calls in these tests are local fixtures.
func newOpenRouterAudioReservation(t *testing.T, priorHold ...int) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	taskfixture.Truncate(t)
	previousLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 100
	t.Cleanup(func() { constant.TaskQueryLimit = previousLimit })
	disableTaskFixtureArtifacts(t)
	const id = 987
	taskfixture.SeedUser(t, id, 10000)
	taskfixture.SeedToken(t, id, id, "audio-task-fixture", 10000)
	taskfixture.SeedChannel(t, id)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelId: id, ApiKey: "account-fixture", UpstreamModelName: "vendor/audio"},
		RelayMode:   relayconstant.RelayModeAudioSpeech,
		UserId:      id, TokenId: id, TokenKey: "audio-task-fixture", OriginModelName: "audio-alias", RequestId: "audio-request-fixture", ForcePreConsume: true,
		OpenRouterCost: &relaycommon.OpenRouterCostState{Basis: common.OpenRouterCostBasis{Version: "openrouter_cost_v1", EffectiveGroupRatio: 1, QuotaPerUnit: 1000, EstimatedUSD: 0.05}},
	}
	info.UserSetting.BillingPreference = "wallet_only"
	info.PriceData.Quota = 50
	previousHold := 0
	if len(priorHold) > 0 {
		previousHold = priorHold[0]
		require.Nil(t, service.PreConsumeBilling(c, previousHold, info))
	}
	require.NoError(t, taskrecord.BeginOpenRouterSyncTask(c, info, 50))
	intent, err := openRouterSyncTask(info)
	require.NoError(t, err)
	assert.True(t, intent.PrivateData.Reconciliation.ReservationPending)
	assert.Zero(t, intent.PrivateData.Reconciliation.ReservedQuota)
	assert.Equal(t, 50, intent.PrivateData.OpenRouterAudio.PlannedQuota)
	assert.Equal(t, 10000-previousHold, getUserQuota(t, id), "intent precedes the audio reservation mutation")
	if info.Billing == nil {
		require.Nil(t, service.PreConsumeBilling(c, 50, info))
	} else {
		require.NoError(t, info.Billing.Reserve(50))
	}
	require.NoError(t, taskrecord.CommitOpenRouterSyncReservation(c, info))
	assert.Equal(t, 10000-max(50, previousHold), getUserQuota(t, id))
	assert.Equal(t, 10000-max(50, previousHold), getTokenRemainQuota(t, id))
	assert.False(t, info.Billing.NeedsRefund())
	return c, info
}

func TestOpenRouterAudioDurableCostReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name, raw                  string
		status                     int
		pending, manual            bool
		target                     int
		responseID, responseModel  string
		canonicalID, canonicalSlug string
		apiType, generationTime    string
		resultMimeType             string
		cancelled                  string
		modelStatus                int
		modelError                 bool
	}{
		{name: "positive", raw: "0.04", status: 200, target: 40},
		{name: "explicit zero", raw: "0", status: 200, target: 0},
		{name: "absent", raw: "", status: 200, pending: true, target: 50},
		{name: "null", raw: "null", status: 200, pending: true, target: 50},
		{name: "invalid string", raw: `"0"`, status: 200, manual: true, target: 50},
		{name: "negative", raw: "-1", status: 200, manual: true, target: 50},
		{name: "speech without generation time", raw: "0.04", status: 200, apiType: "tts", generationTime: "null", target: 40},
		{name: "speech explicit zero without generation time", raw: "0", status: 200, apiType: "tts", generationTime: "null", target: 0},
		{name: "speech missing cost stays pending", status: 200, apiType: "tts", generationTime: "null", pending: true, target: 50},
		{name: "speech null cost stays pending", raw: "null", status: 200, apiType: "tts", generationTime: "null", pending: true, target: 50},
		{name: "speech invalid duration", raw: "0.04", status: 200, apiType: "tts", generationTime: "-1", manual: true, target: 50},
		{name: "cancelled speech stays pending", raw: "0.04", status: 200, apiType: "tts", generationTime: "null", cancelled: "true", pending: true, target: 50},
		{name: "unknown cancellation stays pending", raw: "0.04", status: 200, apiType: "tts", generationTime: "null", cancelled: "null", pending: true, target: 50},
		{name: "other audio without generation time", raw: "0.04", status: 200, generationTime: "null", pending: true, target: 50},
		{name: "non speech result cannot bypass missing time", raw: "0.04", status: 200, apiType: "tts", generationTime: "null", resultMimeType: "application/json", pending: true, target: 50},
		{name: "speech canonical model", raw: "0.04", status: 200, apiType: "tts", generationTime: "null", responseModel: "vendor/audio-20261001", canonicalID: "vendor/audio", canonicalSlug: "vendor/audio-20261001", target: 40},
		{name: "different canonical model rejected", raw: "0.04", status: 200, responseModel: "vendor/other", canonicalID: "vendor/audio", canonicalSlug: "vendor/audio-20261001", manual: true, target: 50},
		{name: "canonical date is not guessed", raw: "0.04", status: 200, responseModel: "vendor/audio-20261001", canonicalID: "vendor/audio", manual: true, target: 50},
		{name: "catalog identity mismatch", raw: "0.04", status: 200, responseModel: "vendor/audio-20261001", canonicalID: "vendor/other", canonicalSlug: "vendor/audio-20261001", manual: true, target: 50},
		{name: "catalog error rejects matching data", raw: "0.04", status: 200, responseModel: "vendor/audio-20261001", canonicalID: "vendor/audio", canonicalSlug: "vendor/audio-20261001", modelError: true, manual: true, target: 50},
		{name: "canonical lookup temporary failure", raw: "0.04", status: 200, responseModel: "vendor/audio-20261001", modelStatus: 503, pending: true, target: 50},
		{name: "canonical lookup unauthorized", raw: "0.04", status: 200, responseModel: "vendor/audio-20261001", modelStatus: 401, manual: true, target: 50},
		{name: "canonical lookup redirect not followed", raw: "0.04", status: 200, responseModel: "vendor/audio-20261001", modelStatus: 307, manual: true, target: 50},
		{name: "wrong generation rejected before model lookup", raw: "0.04", status: 200, responseID: "gen-other", responseModel: "vendor/audio-20261001", manual: true, target: 50},
		{name: "unauthorized", status: 401, manual: true, target: 50},
		{name: "forbidden", status: 403, manual: true, target: 50},
		{name: "not found", status: 404, pending: true, target: 50},
		{name: "rate limit", status: 429, pending: true, target: 50},
		{name: "server error", status: 503, pending: true, target: 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, info := newOpenRouterAudioReservation(t)
			calls := 0
			modelCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "Bearer account-fixture", r.Header.Get("Authorization"))
				w.Header().Set("Retry-After", "3600")
				if r.URL.Path == "/model/vendor/audio" {
					modelCalls++
					if tc.modelStatus != 0 {
						w.Header().Set("Location", "/must-not-follow")
						w.WriteHeader(tc.modelStatus)
						return
					}
					errorJSON := "null"
					if tc.modelError {
						errorJSON = `{"message":"model metadata unavailable"}`
					}
					fmt.Fprintf(w, `{"data":{"id":%q,"canonical_slug":%q},"error":%s}`, tc.canonicalID, tc.canonicalSlug, errorJSON)
					return
				}
				calls++
				assert.Equal(t, "/generation", r.URL.Path)
				assert.Equal(t, "gen-audio-fixture", r.URL.Query().Get("id"))
				w.WriteHeader(tc.status)
				if tc.status == 200 {
					cost := ""
					if tc.raw != "" {
						cost = `,"total_cost":` + tc.raw
					}
					generationTime, responseModel, responseID, cancelled := tc.generationTime, tc.responseModel, tc.responseID, tc.cancelled
					if generationTime == "" {
						generationTime = "20"
					}
					if responseModel == "" {
						responseModel = "vendor/audio"
					}
					if responseID == "" {
						responseID = "gen-audio-fixture"
					}
					if cancelled == "" {
						cancelled = "false"
					}
					fmt.Fprintf(w, `{"data":{"id":%q,"model":%q,"api_type":%q,"cancelled":%s,"generation_time":%s%s}}`, responseID, responseModel, tc.apiType, cancelled, generationTime, cost)
				}
			}))
			defer server.Close()
			require.NoError(t, taskrecord.ConfigureOpenRouterSyncSubmission(info, server.URL+"/generation", http.Header{"Authorization": []string{"Bearer account-fixture"}}))
			require.NoError(t, taskrecord.RecordOpenRouterSyncSubmission(info, &http.Response{StatusCode: 200, Header: http.Header{"X-Generation-Id": []string{"gen-audio-fixture"}}}, nil))
			// Disconnect and discard request-owned settlement; a separate polling pass
			// must still reconcile the durable result and the frozen monetary basis.
			requestCtx, cancel := context.WithCancel(c.Request.Context())
			c.Request = c.Request.WithContext(requestCtx)
			cancel()
			resultMimeType := tc.resultMimeType
			if resultMimeType == "" {
				resultMimeType = "audio/pcm"
			}
			require.NoError(t, taskrecord.CompleteOpenRouterSyncTask(c, info, []byte("durable audio"), resultMimeType))
			assert.Zero(t, calls, "delivery must not depend on synchronous metadata lookup")
			service.PostTextConsumeQuota(c, info, nil, nil)
			assert.Equal(t, 9950, getUserQuota(t, info.UserId), "request completion cannot charge or refund the Task hold")
			info.PriceData.BillingGroupRatio = 99
			RunTaskPollingOnce(t.Context(), nil)
			task, err := openRouterSyncTask(info)
			require.NoError(t, err)
			assert.Equal(t, tc.target, task.Quota)
			assert.Equal(t, 10000-tc.target, getUserQuota(t, info.UserId))
			assert.Equal(t, 10000-tc.target, getTokenRemainQuota(t, info.TokenId))
			assert.Equal(t, 1, calls)
			if tc.responseModel != "" && tc.responseID == "" {
				assert.Equal(t, 1, modelCalls)
			} else {
				assert.Zero(t, modelCalls)
			}
			if tc.pending {
				assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)
				assert.False(t, task.PrivateData.Reconciliation.Finalized)
				assert.GreaterOrEqual(t, task.PrivateData.OpenRouterAudio.NextPoll, time.Now().Add(59*time.Minute).Unix())
			} else if tc.manual {
				assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
				assert.Contains(t, task.FailReason, "manual reconciliation")
			} else {
				assert.True(t, task.PrivateData.Reconciliation.Finalized)
				var publicState map[string]any
				require.NoError(t, common.Unmarshal(task.Data, &publicState))
				assert.Equal(t, "settled", publicState["billing_status"])
			}
			assert.True(t, task.PrivateData.OpenRouterAudio.ResultReady)
			assert.Empty(t, task.PrivateData.OpenRouterAudio.ResultSHA256, "cost reconciliation does not require a stored response")
			RunTaskPollingOnce(t.Context(), nil)
			assert.Equal(t, 10000-tc.target, getUserQuota(t, info.UserId), "restart/repeated poll cannot replay money")
			assert.Equal(t, 1, calls)
		})
	}
}

func TestOpenRouterAudioCrashOwnershipAndStaleWriters(t *testing.T) {
	for _, hasGeneration := range []bool{false, true} {
		t.Run(fmt.Sprint("generation_", hasGeneration), func(t *testing.T) {
			_, info := newOpenRouterAudioReservation(t)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				fmt.Fprint(w, `{"data":{"id":"gen-crash","model":"vendor/audio","cancelled":false,"generation_time":12,"total_cost":0.04}}`)
			}))
			defer server.Close()
			require.NoError(t, taskrecord.ConfigureOpenRouterSyncSubmission(info, server.URL+"/generation", nil))
			if hasGeneration {
				require.NoError(t, taskrecord.RecordOpenRouterSyncSubmission(info, &http.Response{StatusCode: 200, Header: http.Header{"X-Generation-Id": []string{"gen-crash"}}}, nil))
			}
			crashed, err := openRouterSyncTask(info)
			require.NoError(t, err)
			crashed.PrivateData.OpenRouterAudio.LeaseUntil = time.Now().Add(-time.Minute).Unix()
			require.NoError(t, fixtureStore.UpdateOpenRouterSync(crashed, crashed.PrivateData.OpenRouterAudio.Revision))
			// Separate copies model a request handler and a stale poller surviving the
			// ownership transfer. Neither may overwrite a subsequent terminal claim.
			stale, err := openRouterSyncTask(info)
			require.NoError(t, err)
			RunTaskPollingOnce(t.Context(), nil)
			settled, err := openRouterSyncTask(info)
			require.NoError(t, err)
			assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), settled.Status)
			assert.False(t, settled.PrivateData.OpenRouterAudio.ResultReady)
			if hasGeneration {
				assert.Equal(t, 1, calls)
				assert.Equal(t, 9960, getUserQuota(t, info.UserId), "lost delivery does not discard confirmed upstream spend")
				assert.True(t, settled.PrivateData.Reconciliation.Finalized)
			} else {
				assert.Zero(t, calls)
				assert.Equal(t, 9950, getUserQuota(t, info.UserId))
				assert.Contains(t, settled.FailReason, "manual reconciliation")
			}
			require.Error(t, fixtureStore.UpdateOpenRouterSync(stale, stale.PrivateData.OpenRouterAudio.Revision))
			require.Error(t, taskrecord.CompleteOpenRouterSyncTask(&gin.Context{}, info, []byte("late"), "audio/pcm"))
			assert.False(t, RefundTaskQuota(t.Context(), settled, "unknown must not refund"))
		})
	}
}

func TestOpenRouterAudioPendingReservationAndSettlementNeverReplay(t *testing.T) {
	t.Run("interrupted reservation", func(t *testing.T) {
		_, info := newOpenRouterAudioReservation(t)
		task, err := openRouterSyncTask(info)
		require.NoError(t, err)
		task.PrivateData.Reconciliation.ReservationPending = true
		task.PrivateData.OpenRouterAudio.LeaseUntil = time.Now().Add(-time.Minute).Unix()
		require.NoError(t, fixtureStore.UpdateOpenRouterSync(task, task.PrivateData.OpenRouterAudio.Revision))
		RunTaskPollingOnce(t.Context(), nil)
		saved, err := openRouterSyncTask(info)
		require.NoError(t, err)
		assert.Contains(t, saved.FailReason, "manual reconciliation")
		assert.Equal(t, 9950, getUserQuota(t, info.UserId))
	})
	t.Run("after main settlement before log", func(t *testing.T) {
		c, info := newOpenRouterAudioReservation(t)
		require.NoError(t, taskrecord.ConfigureOpenRouterSyncSubmission(info, "https://metadata.invalid/generation", nil))
		info.OpenRouterCost.CostUSD, info.OpenRouterCost.Valid, info.OpenRouterCost.Final = 0.04, true, true
		require.NoError(t, model.LOG_DB.Callback().Create().Before("gorm:create").Register("audio_cost_log_failure", func(tx *gorm.DB) { tx.AddError(fmt.Errorf("injected log failure")) }))
		require.NoError(t, taskrecord.CompleteOpenRouterSyncTask(c, info, []byte("result"), "audio/pcm"))
		require.NoError(t, model.LOG_DB.Callback().Create().Remove("audio_cost_log_failure"))
		saved, err := openRouterSyncTask(info)
		require.NoError(t, err)
		assert.True(t, saved.PrivateData.Reconciliation.MainApplied)
		assert.False(t, saved.PrivateData.Reconciliation.Finalized)
		assert.Equal(t, 9960, getUserQuota(t, info.UserId))
		RunTaskPollingOnce(t.Context(), nil)
		assert.Equal(t, 9960, getUserQuota(t, info.UserId))
		var logs []model.Log
		require.NoError(t, model.LOG_DB.Find(&logs).Error)
		assert.Len(t, logs, 1, "only reservation log exists; no blind financial/log replay")
	})
}

func TestOpenRouterAudioConcurrentPollersSettleOnce(t *testing.T) {
	c, info := newOpenRouterAudioReservation(t)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"data":{"id":"gen-race","model":"vendor/audio","cancelled":false,"generation_time":1,"total_cost":0.04}}`)
	}))
	defer func() { releaseOnce.Do(func() { close(release) }); server.Close() }()
	require.NoError(t, taskrecord.ConfigureOpenRouterSyncSubmission(info, server.URL+"/generation", nil))
	require.NoError(t, taskrecord.RecordOpenRouterSyncSubmission(info, &http.Response{StatusCode: 200, Header: http.Header{"X-Generation-Id": []string{"gen-race"}}}, nil))
	require.NoError(t, taskrecord.CompleteOpenRouterSyncTask(c, info, []byte("audio"), "audio/pcm"))
	first, err := openRouterSyncTask(info)
	require.NoError(t, err)
	second, err := openRouterSyncTask(info)
	require.NoError(t, err)
	outcomes := make(chan error, 2)
	go func() { outcomes <- pollOpenRouterSync(t.Context(), first) }()
	go func() { outcomes <- pollOpenRouterSync(t.Context(), second) }()
	for range 2 {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("both independent pollers must reach the GET barrier")
		}
	}
	releaseOnce.Do(func() { close(release) })
	winners := 0
	for range 2 {
		if err := <-outcomes; err == nil {
			winners++
		}
	}
	assert.Equal(t, 1, winners)
	saved, err := openRouterSyncTask(info)
	require.NoError(t, err)
	assert.True(t, saved.PrivateData.Reconciliation.Finalized)
	assert.Equal(t, 9960, getUserQuota(t, info.UserId))
	assert.Equal(t, 9960, getTokenRemainQuota(t, info.TokenId))
	var user model.User
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Equal(t, 1, user.RequestCount)
	assert.Equal(t, 40, user.UsedQuota)
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Find(&logs).Error)
	assert.Len(t, logs, 2, "one reserve and one delta, despite concurrent GETs")
}

func TestOpenRouterAudioDelayedMetadataAndBoundedUnknown(t *testing.T) {
	for _, kind := range []string{"delayed", "deadline", "attempt_limit", "identity", "timeout", "retry_after_date"} {
		t.Run(kind, func(t *testing.T) {
			c, info := newOpenRouterAudioReservation(t)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if kind == "delayed" && calls == 1 || kind == "attempt_limit" {
					w.WriteHeader(404)
					return
				}
				if kind == "retry_after_date" {
					w.Header().Set("Retry-After", time.Now().Add(2*time.Hour).UTC().Format(http.TimeFormat))
					w.WriteHeader(429)
					return
				}
				id := "gen-delayed"
				if kind == "identity" {
					id = "some-other-account-request"
				}
				fmt.Fprintf(w, `{"data":{"id":%q,"model":"vendor/audio","cancelled":false,"generation_time":1,"total_cost":0.04}}`, id)
			}))
			defer server.Close()
			require.NoError(t, taskrecord.ConfigureOpenRouterSyncSubmission(info, server.URL+"/generation", nil))
			require.NoError(t, taskrecord.RecordOpenRouterSyncSubmission(info, &http.Response{StatusCode: 200, Header: http.Header{"X-Generation-Id": []string{"gen-delayed"}}}, nil))
			require.NoError(t, taskrecord.CompleteOpenRouterSyncTask(c, info, []byte("audio"), "audio/pcm"))
			task, err := openRouterSyncTask(info)
			require.NoError(t, err)
			switch kind {
			case "deadline":
				task.PrivateData.OpenRouterAudio.Deadline = time.Now().Add(-time.Second).Unix()
			case "attempt_limit":
				task.PrivateData.OpenRouterAudio.Attempts = 15
			}
			require.NoError(t, fixtureStore.UpdateOpenRouterSync(task, task.PrivateData.OpenRouterAudio.Revision))
			pollCtx := t.Context()
			if kind == "timeout" {
				var cancel context.CancelFunc
				pollCtx, cancel = context.WithDeadline(pollCtx, time.Now().Add(-time.Second))
				defer cancel()
			}
			require.NoError(t, pollOpenRouterSync(pollCtx, task))
			saved, err := openRouterSyncTask(info)
			require.NoError(t, err)
			switch kind {
			case "delayed":
				assert.Equal(t, 9950, getUserQuota(t, info.UserId))
				assert.False(t, saved.PrivateData.Reconciliation.Finalized)
				saved.PrivateData.OpenRouterAudio.NextPoll = 0
				require.NoError(t, fixtureStore.UpdateOpenRouterSync(saved, saved.PrivateData.OpenRouterAudio.Revision))
				require.NoError(t, pollOpenRouterSync(t.Context(), saved))
				assert.Equal(t, 9960, getUserQuota(t, info.UserId))
				assert.Equal(t, 2, calls)
			case "timeout":
				assert.Equal(t, 9950, getUserQuota(t, info.UserId))
				assert.Equal(t, "transport_error", saved.PrivateData.OpenRouterAudio.LastError)
				assert.Greater(t, saved.PrivateData.OpenRouterAudio.NextPoll, time.Now().Unix())
				assert.Zero(t, calls)
			case "retry_after_date":
				assert.GreaterOrEqual(t, saved.PrivateData.OpenRouterAudio.NextPoll, time.Now().Add(119*time.Minute).Unix())
				assert.Equal(t, 9950, getUserQuota(t, info.UserId))
			default:
				assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), saved.Status)
				assert.Contains(t, saved.FailReason, "manual reconciliation")
				assert.Equal(t, 9950, getUserQuota(t, info.UserId))
			}
		})
	}
}

func TestOpenRouterAudioRetainsLargerExistingHoldAccounting(t *testing.T) {
	c, info := newOpenRouterAudioReservation(t, 100)
	assert.Equal(t, 100, info.PriceData.Quota, "reserve statistics must reflect applied money, not the smaller estimate")
	assert.Equal(t, 0.05, info.OpenRouterCost.Basis.EstimatedUSD, "the frozen price estimate remains unchanged")
	require.NoError(t, taskrecord.ConfigureOpenRouterSyncSubmission(info, "https://metadata.invalid/generation", nil))
	info.OpenRouterCost.CostUSD, info.OpenRouterCost.Valid, info.OpenRouterCost.Final = 0.04, true, true
	require.NoError(t, taskrecord.CompleteOpenRouterSyncTask(c, info, []byte("result"), "audio/pcm"))
	saved, err := openRouterSyncTask(info)
	require.NoError(t, err)
	assert.Equal(t, 40, saved.Quota)
	assert.Equal(t, 100, saved.PrivateData.Reconciliation.ReservedQuota)
	assert.Equal(t, 9960, getUserQuota(t, info.UserId))
	assert.Equal(t, 9960, getTokenRemainQuota(t, info.TokenId))
	var user model.User
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Equal(t, 40, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, info.ChannelId).Error)
	assert.Equal(t, int64(40), channel.UsedQuota)
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Order("id").Find(&logs).Error)
	require.Len(t, logs, 2)
	assert.Equal(t, 100, logs[0].Quota)
	assert.Equal(t, 60, logs[1].Quota)
	assert.Equal(t, model.LogTypeRefund, logs[1].Type)
}

func TestOpenRouterAudioSlowMetadataDoesNotBlockVideoPolling(t *testing.T) {
	c, info := newOpenRouterAudioReservation(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer func() { releaseOnce.Do(func() { close(release) }); server.Close() }()
	require.NoError(t, taskrecord.ConfigureOpenRouterSyncSubmission(info, server.URL+"/generation", nil))
	require.NoError(t, taskrecord.RecordOpenRouterSyncSubmission(info, &http.Response{StatusCode: 200, Header: http.Header{"X-Generation-Id": []string{"gen-slow"}}}, nil))
	require.NoError(t, taskrecord.CompleteOpenRouterSyncTask(c, info, []byte("result"), "audio/pcm"))
	video := taskfixture.MakeTask(info.UserId, info.ChannelId, 0, info.TokenId, BillingSourceWallet, 0)
	video.TaskID = "task-video-after-audio"
	video.Platform = "video-fixture"
	video.PrivateData.UpstreamTaskID = "upstream-video"
	require.NoError(t, model.DB.Create(video).Error)
	adaptor := &taskPollingFetchAdaptor{fetched: make(chan string, 1)}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })
	finished := make(chan struct{})
	go func() { defer close(finished); RunTaskPollingOnce(t.Context(), nil) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("audio GET did not reach barrier")
	}
	select {
	case fetched := <-adaptor.fetched:
		assert.Equal(t, "upstream-video", fetched)
	default:
		t.Fatal("video must be fetched before waiting on audio metadata")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("polling pass did not finish")
	}
	assert.Equal(t, 9950, getUserQuota(t, info.UserId), "temporary metadata failure retains the hold")
}

// New chat attempts use the same wallet/token transfer as audio, but keep only
// credential/config digests. No response artifact store is available here.
func newOpenRouterChatImageReservation(t *testing.T, baseURL string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	taskfixture.Truncate(t)
	disableTaskFixtureArtifacts(t)
	const id = 987
	taskfixture.SeedUser(t, id, 10000)
	taskfixture.SeedToken(t, id, id, "chat-image-token", 10000)
	require.NoError(t, model.DB.Create(&model.Channel{Id: id, Name: "chat-image-fixture", Type: constant.ChannelTypeOpenRouter, Key: "chat-image-account", BaseURL: &baseURL}).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: id, ChannelType: constant.ChannelTypeOpenRouter, ApiKey: "chat-image-account", ChannelBaseUrl: baseURL, UpstreamModelName: "vendor/image"},
		RelayMode:   relayconstant.RelayModeChatCompletions, UserId: id, TokenId: id, TokenKey: "chat-image-token", OriginModelName: "image-alias", RequestId: "chat-image-request", ForcePreConsume: true,
		OpenRouterCost: &relaycommon.OpenRouterCostState{ChatImage: true, Basis: common.OpenRouterCostBasis{Version: "openrouter_cost_v1", EffectiveGroupRatio: 1, QuotaPerUnit: 1000, EstimatedUSD: .05}},
	}
	info.UserSetting.BillingPreference = "wallet_only"
	info.PriceData.Quota = 50
	require.NoError(t, taskrecord.BeginOpenRouterSyncTask(c, info, 50))
	assert.Equal(t, 10000, getUserQuota(t, id), "intent precedes hold")
	require.Nil(t, service.PreConsumeBilling(c, 50, info))
	require.NoError(t, taskrecord.CommitOpenRouterSyncReservation(c, info))
	require.NoError(t, taskrecord.ConfigureOpenRouterSyncSubmission(info, baseURL+"/v1/generation", http.Header{"Authorization": []string{"Bearer chat-image-account"}}))
	info.OpenRouterCost.CaptureGeneration = func(id string) error { return taskrecord.RecordOpenRouterSyncGeneration(info, id) }
	task, err := openRouterSyncTask(info)
	require.NoError(t, err)
	assert.Empty(t, task.PrivateData.Key)
	assert.Empty(t, task.PrivateData.OpenRouterSync.Headers)
	assert.False(t, info.Billing.NeedsRefund())
	return c, info
}

func TestOpenRouterChatImageDurableReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name, cost, metadataCost                           string
		noID, conflict, rotate, wrongModel, wrongOperation bool
		target                                             int
		confirmed                                          bool
	}{
		{name: "terminal cost", cost: "0.04", target: 40, confirmed: true},
		{name: "explicit zero", cost: "0", target: 0, confirmed: true},
		{name: "missing cost metadata", metadataCost: "0.04", target: 40, confirmed: true},
		{name: "null remains unknown", cost: "null", metadataCost: "null", target: 50},
		{name: "negative remains unknown", cost: "-1", metadataCost: "-1", target: 50},
		{name: "nonfinite remains unknown", cost: "1e309", metadataCost: "1e309", target: 50},
		{name: "string remains unknown", cost: `"0.04"`, metadataCost: `"0.04"`, target: 50},
		{name: "missing identity", noID: true, target: 50},
		{name: "header body disagreement", conflict: true, target: 50},
		{name: "credential rotated", rotate: true, metadataCost: "0.04", target: 50},
		{name: "wrong model", wrongModel: true, metadataCost: "0.04", target: 50},
		{name: "wrong operation", wrongOperation: true, metadataCost: "0.04", target: 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "Bearer chat-image-account", r.Header.Get("Authorization"))
				modelName, apiType := "vendor/image", "completions"
				if tc.wrongModel {
					modelName = "vendor/foreign"
				}
				if tc.wrongOperation {
					apiType = "rerank"
				}
				if r.URL.Path != "/v1/generation" {
					w.WriteHeader(404)
					return
				}
				fmt.Fprintf(w, `{"data":{"id":"gen-chat-fixture","model":%q,"api_type":%q,"cancelled":false,"generation_time":1,"total_cost":%s}}`, modelName, apiType, tc.metadataCost)
			}))
			defer server.Close()
			c, info := newOpenRouterChatImageReservation(t, server.URL)
			header := http.Header{}
			if tc.conflict {
				header.Set("X-Generation-Id", "gen-header")
			}
			require.NoError(t, taskrecord.RecordOpenRouterSyncSubmission(info, &http.Response{StatusCode: 200, Header: header}, nil))
			id := `"id":"gen-chat-fixture",`
			if tc.noID {
				id = ""
			}
			usage := `"usage":{}`
			if tc.cost != "" {
				usage = `"usage":{"cost":` + tc.cost + `}`
			}
			info.OpenRouterCost.Observe([]byte(`{`+id+`"choices":[{"message":{"content":"valid image/text"}}],`+usage+`}`), true)
			if tc.conflict {
				require.Error(t, taskrecord.CompleteOpenRouterSyncTask(c, info, nil, "application/json"))
				require.NoError(t, taskrecord.FailOpenRouterSyncTask(info, "invalid_chat_image_result"))
			} else {
				require.NoError(t, taskrecord.CompleteOpenRouterSyncTask(c, info, []byte("delivered response"), "application/json"))
			}
			if tc.rotate {
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", info.ChannelId).Update("key", "rotated-account").Error)
			}
			saved, err := openRouterSyncTask(info)
			require.NoError(t, err)
			if saved.Status != model.TaskStatusFailure && !saved.PrivateData.Reconciliation.Finalized {
				require.NoError(t, pollOpenRouterSync(t.Context(), saved))
			}
			saved, err = openRouterSyncTask(info)
			require.NoError(t, err)
			assert.Equal(t, tc.target, saved.Quota)
			assert.Equal(t, 10000-tc.target, getUserQuota(t, info.UserId))
			assert.Equal(t, 10000-tc.target, getTokenRemainQuota(t, info.TokenId))
			assert.Equal(t, tc.confirmed, saved.PrivateData.Reconciliation.UpstreamCostConfirmed)
			assert.False(t, info.Billing.NeedsRefund())
			view := taskdomain.OpenRouterSyncReconciliation(record.ToDomain(saved))
			assert.Equal(t, false, view["replay_allowed"])
			encoded, err := common.Marshal(view)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "chat-image-account")
			assert.NotContains(t, string(encoded), "credential_hash")
			if tc.rotate || tc.noID || tc.conflict {
				assert.Zero(t, calls)
			}
		})
	}
}

func TestOpenRouterChatImageInterruptedSubmissionAndLateCost(t *testing.T) {
	c, info := newOpenRouterChatImageReservation(t, "https://openrouter.invalid/api")
	saved, err := openRouterSyncTask(info)
	require.NoError(t, err)
	saved.PrivateData.OpenRouterSync.LeaseUntil = time.Now().Add(-time.Minute).Unix()
	require.NoError(t, fixtureStore.UpdateOpenRouterSync(saved, saved.PrivateData.OpenRouterSync.Revision))
	require.NoError(t, pollOpenRouterSync(t.Context(), saved))
	saved, err = openRouterSyncTask(info)
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), saved.Status)
	assert.Equal(t, 9950, getUserQuota(t, info.UserId))
	assert.False(t, info.Billing.NeedsRefund())
	// An original in-flight response may later carry a receipt and confirmed cost;
	// this does not create a second generation or a fresh financial owner.
	info.OpenRouterCost.Observe([]byte(`{"id":"gen-late","usage":{"cost":0.04}}`), true)
	require.NoError(t, taskrecord.CompleteOpenRouterSyncTask(c, info, []byte("original late result"), "application/json"))
	saved, err = openRouterSyncTask(info)
	require.NoError(t, err)
	assert.True(t, saved.PrivateData.Reconciliation.Finalized)
	assert.Equal(t, 9960, getUserQuota(t, info.UserId))
	require.Error(t, taskrecord.CompleteOpenRouterSyncTask(c, info, nil, "application/json"))
	assert.Equal(t, 9960, getUserQuota(t, info.UserId))
}

func TestOpenRouterChatImageConcurrentPollersSettleOnce(t *testing.T) {
	entered, release := make(chan struct{}, 2), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"data":{"id":"gen-chat-race","model":"vendor/image","api_type":"completions","cancelled":false,"generation_time":1,"total_cost":0.04}}`)
	}))
	defer func() { once.Do(func() { close(release) }); server.Close() }()
	c, info := newOpenRouterChatImageReservation(t, server.URL)
	info.OpenRouterCost.Observe([]byte(`{"id":"gen-chat-race"}`), true)
	require.NoError(t, taskrecord.CompleteOpenRouterSyncTask(c, info, nil, "application/json"))
	first, err := openRouterSyncTask(info)
	require.NoError(t, err)
	second, err := openRouterSyncTask(info)
	require.NoError(t, err)
	outcomes := make(chan error, 2)
	go func() { outcomes <- pollOpenRouterSync(t.Context(), first) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("metadata GET not reached")
	}
	go func() { outcomes <- pollOpenRouterSync(t.Context(), second) }()
	require.Error(t, <-outcomes, "stale poller must lose its budget claim before another GET")
	once.Do(func() { close(release) })
	require.NoError(t, <-outcomes)
	assert.Empty(t, entered, "one claimed metadata GET")
	saved, err := openRouterSyncTask(info)
	require.NoError(t, err)
	assert.True(t, saved.PrivateData.Reconciliation.Finalized)
	assert.Equal(t, 1, saved.PrivateData.OpenRouterSync.Attempts)
	assert.Equal(t, 9960, getUserQuota(t, info.UserId))
	assert.Equal(t, 9960, getTokenRemainQuota(t, info.TokenId))
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Find(&logs).Error)
	assert.Len(t, logs, 2, "one reservation and one settlement delta")
}

func TestOpenRouterChatImageEventCorrelationBeforeTerminalUsage(t *testing.T) {
	for _, tc := range []struct {
		name, next string
		conflict   bool
	}{
		{name: "same generation across events", next: "gen-event"},
		{name: "different generation across events", next: "gen-foreign", conflict: true},
		{name: "secret shaped correlation rejected", next: "gen-sk-private", conflict: true},
		{name: "invalid generation identity rejected", next: "foreign-request-id", conflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, info := newOpenRouterChatImageReservation(t, "https://openrouter.invalid/api")
			info.OpenRouterCost.Observe([]byte(`{"id":"gen-event","choices":[{"delta":{"content":"partial"}}]}`), false)
			saved, err := openRouterSyncTask(info)
			require.NoError(t, err)
			assert.Equal(t, "gen-event", saved.PrivateData.UpstreamTaskID, "receipt is durable before terminal usage arrives")
			assert.False(t, saved.PrivateData.Reconciliation.UpstreamCostConfirmed)
			info.OpenRouterCost.Observe([]byte(fmt.Sprintf(`{"id":%q,"usage":{"cost":0.04}}`, tc.next)), true)
			if tc.conflict {
				assert.False(t, info.OpenRouterCost.Final)
				saved, err = openRouterSyncTask(info)
				require.NoError(t, err)
				assert.True(t, saved.PrivateData.OpenRouterSync.IdentityConflict, "conflict survives a crash before the response error handler")
				require.NoError(t, taskrecord.FailOpenRouterSyncTask(info, "invalid_chat_image_result"))
				assert.Equal(t, 9950, getUserQuota(t, info.UserId))
				saved, err = openRouterSyncTask(info)
				require.NoError(t, err)
				assert.Equal(t, "gen-event", saved.PrivateData.UpstreamTaskID)
				assert.False(t, saved.PrivateData.Reconciliation.UpstreamCostConfirmed)
			} else {
				require.NoError(t, taskrecord.CompleteOpenRouterSyncTask(c, info, nil, "application/json"))
				assert.Equal(t, 9960, getUserQuota(t, info.UserId))
			}
		})
	}
}

func TestOpenRouterChatImageLookupBoundsAndConfigBinding(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		deadline, attempts, config bool
	}{
		{name: "expired deadline", deadline: true},
		{name: "exhausted persistent budget", attempts: true},
		{name: "changed account config", config: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				t.Error("blocked lookup must not reach upstream")
			}))
			defer server.Close()
			c, info := newOpenRouterChatImageReservation(t, server.URL)
			info.OpenRouterCost.Observe([]byte(`{"id":"gen-bounded"}`), true)
			require.NoError(t, taskrecord.CompleteOpenRouterSyncTask(c, info, nil, "application/json"))
			saved, err := openRouterSyncTask(info)
			require.NoError(t, err)
			if tc.deadline {
				saved.PrivateData.OpenRouterSync.Deadline = time.Now().Add(-time.Second).Unix()
			}
			if tc.attempts {
				saved.PrivateData.OpenRouterSync.Attempts = 16
			}
			require.NoError(t, fixtureStore.UpdateOpenRouterSync(saved, saved.PrivateData.OpenRouterSync.Revision))
			if tc.config {
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", info.ChannelId).Update("header_override", `{"Authorization":"Bearer another-account"}`).Error)
			}
			require.NoError(t, pollOpenRouterSync(t.Context(), saved))
			assert.Zero(t, calls)
			saved, err = openRouterSyncTask(info)
			require.NoError(t, err)
			assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), saved.Status)
			assert.Equal(t, "manual_reconciliation", taskdomain.OpenRouterSyncReconciliation(record.ToDomain(saved))["billing_state"])
			assert.Equal(t, 9950, getUserQuota(t, info.UserId))
		})
	}
}

func TestOpenRouterChatImageFirstSecretCorrelationNeverPersists(t *testing.T) {
	_, info := newOpenRouterChatImageReservation(t, "https://openrouter.invalid/api")
	info.OpenRouterCost.Observe([]byte(`{"id":"gen-sk-private","usage":{"cost":0.04}}`), true)
	assert.Empty(t, info.OpenRouterCost.GenerationID, "rejected candidate never becomes the in-memory generation binding")
	assert.False(t, info.OpenRouterCost.Final)
	saved, err := openRouterSyncTask(info)
	require.NoError(t, err)
	assert.True(t, saved.PrivateData.OpenRouterSync.IdentityConflict)
	assert.Empty(t, saved.PrivateData.UpstreamTaskID)
	require.NoError(t, taskrecord.FailOpenRouterSyncTask(info, "invalid_chat_image_result"))
	saved, err = openRouterSyncTask(info)
	require.NoError(t, err)
	assert.Empty(t, saved.PrivateData.UpstreamTaskID)
	encoded, err := common.Marshal(saved.PrivateData)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "gen-sk-private")
	assert.Equal(t, 9950, getUserQuota(t, info.UserId))
}
