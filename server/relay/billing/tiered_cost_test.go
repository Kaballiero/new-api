package billing_test

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/contracttest"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	billinggateway "github.com/QuantumNous/new-api/relay/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testQuotaPerUnit = 500_000.0

func newCostDatabase(t *testing.T) {
	t.Helper()
	memory, batch, redis, logs := common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled
	common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled = false, false, false, true
	t.Cleanup(func() {
		common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled = memory, batch, redis, logs
	})
	contracttest.New(t)
}
func seedUser(t *testing.T, id, quota int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{Id: id, Username: fmt.Sprintf("cost-user-%d", id), Quota: quota, Group: "default"}).Error)
}

func TestPostAudioConsumeQuotaUsesEffectiveBillingGroupRatio(t *testing.T) {
	newCostDatabase(t)
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() { common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch })

	require.NoError(t, model.LOG_DB.Exec("DELETE FROM logs").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM tokens").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM users").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 901, Username: "audio-effective", Password: "placeholder", Quota: 100, Group: "default"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 901, UserId: 901, Key: "audio-effective-token", Name: "audio-effective-token", RemainQuota: 100}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 901, Name: "audio-effective", Type: 1, Key: "test"}).Error)

	c, _ := gin.CreateTestContext(nil)
	c.Set("token_name", "audio-effective-token")
	info := &relaycommon.RelayInfo{
		UserId:          901,
		TokenId:         901,
		TokenKey:        "audio-effective-token",
		OriginModelName: "audio-effective-model",
		StartTime:       time.Now(),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 901},
		PriceData: types.PriceData{
			ModelRatio:        1,
			BillingGroupRatio: 2,
			GroupRatioInfo:    types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	usage := &dto.Usage{PromptTokens: 1, TotalTokens: 1, PromptTokensDetails: dto.InputTokenDetails{TextTokens: 1}}
	billinggateway.PostAudioConsumeQuota(c, info, usage, "")

	var user model.User
	var token model.Token
	var channel model.Channel
	require.NoError(t, model.DB.First(&user, 901).Error)
	require.NoError(t, model.DB.First(&token, 901).Error)
	require.NoError(t, model.DB.First(&channel, 901).Error)
	assert.Equal(t, 98, user.Quota)
	assert.Equal(t, 2, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	assert.Equal(t, 98, token.RemainQuota)
	assert.Equal(t, 2, token.UsedQuota)
	assert.EqualValues(t, 2, channel.UsedQuota)
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, 2, logs[0].Quota)
	assert.Contains(t, logs[0].Content, "分组倍率 1.00")
	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.Equal(t, float64(1), other["group_ratio"])
}

// ---------------------------------------------------------------------------
// Group ratio tests
// ---------------------------------------------------------------------------

type recordingBillingSettler struct {
	preConsumedQuota int
	reserveTargets   []int
}

func (*recordingBillingSettler) Settle(int) error { return nil }

func (*recordingBillingSettler) Refund(*gin.Context) {}

func (*recordingBillingSettler) NeedsRefund() bool { return false }

func (s *recordingBillingSettler) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}

func (s *recordingBillingSettler) Reserve(targetQuota int) error {
	s.reserveTargets = append(s.reserveTargets, targetQuota)
	if targetQuota > s.preConsumedQuota {
		s.preConsumedQuota = targetQuota
	}
	return nil
}

func TestPrepareTieredBillingForSelectedGroupUpdatesReservation(t *testing.T) {
	const expr = `tier("base", p)`
	billing := &recordingBillingSettler{preConsumedQuota: 50_000}
	relayInfo := &relaycommon.RelayInfo{
		Billing:               billing,
		FinalPreConsumedQuota: 50_000,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                expr,
			ExprHash:                  billingexpr.ExprHashString(expr),
			GroupRatio:                0.10,
			EstimatedQuotaBeforeGroup: 500_000,
			EstimatedQuotaAfterGroup:  50_000,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.20},
		},
	}

	require.Nil(t, billinggateway.PrepareTieredBillingForSelectedGroup(nil, relayInfo))
	require.Equal(t, []int{100_000}, billing.reserveTargets)
	assert.Equal(t, 100_000, billing.preConsumedQuota)
	assert.Equal(t, 100_000, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, 0.20, relayInfo.TieredBillingSnapshot.GroupRatio)
	assert.Equal(t, 100_000, relayInfo.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
}

func TestPrepareTieredBillingForSelectedGroupRetainsClampCause(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1,
			EstimatedQuotaBeforeGroup: float64(common.MaxQuota) * 2,
			EstimatedQuotaAfterGroup:  common.MaxQuota,
		},
		PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2}},
	}

	apiErr := billinggateway.PrepareTieredBillingForSelectedGroup(nil, relayInfo)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	var clamp *common.QuotaClamp
	require.ErrorAs(t, apiErr, &clamp)
	assert.True(t, errors.Is(apiErr.Err, clamp))
	assert.Same(t, clamp, relayInfo.QuotaClamp)
}

func TestPrepareTieredBillingForSelectedGroupStartsBillingAfterFreeGroup(t *testing.T) {
	newCostDatabase(t)
	gin.SetMode(gin.TestMode)

	const userID = 700
	seedUser(t, userID, 500_000)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		IsPlayground:    true,
		ForcePreConsume: true,
		OriginModelName: "gpt-test",
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                `tier("base", p)`,
			ExprHash:                  billingexpr.ExprHashString(`tier("base", p)`),
			GroupRatio:                0,
			EstimatedQuotaBeforeGroup: 500_000,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		PriceData: types.PriceData{
			FreeModel:      true,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.20},
		},
	}
	ctx, _ := gin.CreateTestContext(nil)

	require.Nil(t, billinggateway.PrepareTieredBillingForSelectedGroup(ctx, relayInfo))
	require.NotNil(t, relayInfo.Billing)
	assert.False(t, relayInfo.PriceData.FreeModel, "FreeModel must be cleared after switching to a paid group")
	assert.Equal(t, 100_000, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, 0.20, relayInfo.TieredBillingSnapshot.GroupRatio)
	assert.Equal(t, 100_000, relayInfo.TieredBillingSnapshot.EstimatedQuotaAfterGroup)

	userQuota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	assert.Equal(t, 400_000, userQuota)
}

func TestPrepareTieredBillingForSelectedGroupPaidToFreeKeepsFreeModelFalse(t *testing.T) {
	const expr = `tier("base", p)`
	billing := &recordingBillingSettler{preConsumedQuota: 50_000}
	relayInfo := &relaycommon.RelayInfo{
		Billing:               billing,
		FinalPreConsumedQuota: 50_000,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                expr,
			ExprHash:                  billingexpr.ExprHashString(expr),
			GroupRatio:                0.10,
			EstimatedQuotaBeforeGroup: 500_000,
			EstimatedQuotaAfterGroup:  50_000,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0},
		},
	}

	require.Nil(t, billinggateway.PrepareTieredBillingForSelectedGroup(nil, relayInfo))

	// Pre-consume did happen under the paid group, so FreeModel stays false;
	// settlement already yields 0 for GroupRatio == 0 and the session refunds.
	assert.False(t, relayInfo.PriceData.FreeModel)
	assert.Empty(t, billing.reserveTargets)
	assert.Equal(t, 50_000, relayInfo.FinalPreConsumedQuota)
}

func TestPrepareTieredBillingForSelectedGroupTopUpArrearsAllowsNegativeBalance(t *testing.T) {
	newCostDatabase(t)

	const userID = 701
	// Balance covers the initial 50k pre-consume; the remaining 20k cannot cover
	// the 50k top-up to the more expensive retry group.
	// The top-up must NOT abort the request: the full delta is deducted, the
	// uncovered 30k becomes arrears (negative balance), mirroring how
	// settlement charges a positive delta unconditionally.
	seedUser(t, userID, 70_000)

	relayInfo := &relaycommon.RelayInfo{
		UserId:                userID,
		IsPlayground:          true,
		FinalPreConsumedQuota: 50_000,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                `tier("base", p)`,
			ExprHash:                  billingexpr.ExprHashString(`tier("base", p)`),
			GroupRatio:                0.10,
			EstimatedQuotaBeforeGroup: 500_000,
			EstimatedQuotaAfterGroup:  50_000,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.20},
		},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	relayInfo.UserSetting.BillingPreference = "wallet_only"
	require.Nil(t, billinggateway.PreConsumeBilling(ctx, 50_000, relayInfo))
	session := relayInfo.Billing

	require.Nil(t, billinggateway.PrepareTieredBillingForSelectedGroup(nil, relayInfo))

	// Full reservation recorded; wallet charged the full delta into arrears.
	assert.Equal(t, 100_000, session.GetPreConsumedQuota())
	assert.Equal(t, 100_000, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, 100_000, relayInfo.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
	userQuota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	assert.Equal(t, -30_000, userQuota)

	// Settlement still reconciles against the full reservation: actual 80k
	// refunds the 20k over-reserve, landing at seed - (actual - initial) = -10k.
	require.NoError(t, session.Settle(80_000))
	userQuota, err = model.GetUserQuota(userID, false)
	require.NoError(t, err)
	assert.Equal(t, -10_000, userQuota)
}

func TestBillingSessionReserveWalletTopUpDecrementsBalance(t *testing.T) {
	newCostDatabase(t)

	const userID = 702
	seedUser(t, userID, 550_000)

	relayInfo := &relaycommon.RelayInfo{
		UserId:       userID,
		IsPlayground: true,
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	relayInfo.UserSetting.BillingPreference = "wallet_only"
	require.Nil(t, billinggateway.PreConsumeBilling(ctx, 50_000, relayInfo))
	session := relayInfo.Billing

	require.NoError(t, session.Reserve(100_000))

	assert.Equal(t, 100_000, session.GetPreConsumedQuota())
	assert.Equal(t, 100_000, relayInfo.FinalPreConsumedQuota)
	userQuota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	assert.Equal(t, 450_000, userQuota)
}

func TestTryTieredSettleUsesFinalGroupAfterRetry(t *testing.T) {
	const expr = `tier("base", p)`
	tests := []struct {
		name            string
		finalGroupRatio float64
		wantQuota       int
	}{
		{name: "more expensive final group", finalGroupRatio: 0.20, wantQuota: 100_000},
		{name: "free final group", finalGroupRatio: 0, wantQuota: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relayInfo := &relaycommon.RelayInfo{
				Billing:               &recordingBillingSettler{preConsumedQuota: 50_000},
				FinalPreConsumedQuota: 50_000,
				TieredBillingSnapshot: &billingexpr.BillingSnapshot{
					BillingMode:               "tiered_expr",
					ExprString:                expr,
					ExprHash:                  billingexpr.ExprHashString(expr),
					GroupRatio:                0.10,
					EstimatedQuotaBeforeGroup: 500_000,
					EstimatedQuotaAfterGroup:  50_000,
					QuotaPerUnit:              testQuotaPerUnit,
				},
				PriceData: types.PriceData{
					GroupRatioInfo: types.GroupRatioInfo{GroupRatio: tt.finalGroupRatio},
				},
			}

			require.Nil(t, billinggateway.PrepareTieredBillingForSelectedGroup(nil, relayInfo))
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			summary := billinggateway.New().TextCost(ctx, relayInfo, &dto.Usage{PromptTokens: 1_000_000, TotalTokens: 1_000_000})
			ok, quota, result := summary.TieredApplied, summary.Quota, summary.TieredResult

			require.True(t, ok)
			require.NotNil(t, result)
			assert.Equal(t, tt.wantQuota, quota)
			assert.Equal(t, tt.finalGroupRatio, relayInfo.TieredBillingSnapshot.GroupRatio)
			assert.Equal(t, tt.wantQuota, relayInfo.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
		})
	}
}

func TestBuildTieredTokenParams_Len_TierCondition(t *testing.T) {
	// Test that len-based tier conditions work correctly when p is reduced by cache
	usage := &dto.Usage{
		PromptTokens:     300000,
		CompletionTokens: 5000,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 250000,
			TextTokens:   50000,
		},
	}
	expr := `len <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3) : tier("long_context", p * 6 + c * 22.5 + cr * 0.6)`
	usedVars := billingexpr.UsedVars(expr)
	params := billinggateway.BuildTieredTokenParams(usage, false, usedVars)

	// Len = 300000 (raw prompt), P = 50000 (300000 - 250000 cache)
	if params.Len != 300000 {
		t.Fatalf("Len = %f, want 300000", params.Len)
	}
	if params.P != 50000 {
		t.Fatalf("P = %f, want 50000", params.P)
	}

	// Run expression: len=300000 > 200000, so long_context tier
	cost, trace, err := billingexpr.RunExpr(expr, params)
	if err != nil {
		t.Fatal(err)
	}
	if trace.MatchedTier != "long_context" {
		t.Fatalf("tier = %s, want long_context (len=300000 but p=50000)", trace.MatchedTier)
	}
	// long_context: 50000*6 + 5000*22.5 + 250000*0.6
	wantCost := 50000.0*6 + 5000*22.5 + 250000*0.6
	if math.Abs(cost-wantCost) > 1e-6 {
		t.Fatalf("cost = %f, want %f", cost, wantCost)
	}
}

func TestGatewayInvalidRealtimeUsage(t *testing.T) {
	for _, usage := range []*dto.RealtimeUsage{nil, {TotalTokens: -1}, {TotalTokens: 1, InputTokens: -1}, {TotalTokens: 1, InputTokens: 1, InputTokenDetails: dto.InputTokenDetails{TextTokens: 1, AudioTokens: 1}}} {
		require.Error(t, billinggateway.ValidateRealtimeUsage(usage))
		require.Error(t, billinggateway.PreWssConsumeQuota(nil, &relaycommon.RelayInfo{UsePrice: true}, usage))
		require.Error(t, billinggateway.PostWssConsumeQuota(nil, &relaycommon.RelayInfo{}, "fixture", usage, ""))
	}
}
