package billing_test

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/model"
	service "github.com/QuantumNous/new-api/relay/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The service test binary does not run the host's i18n initialization.
func init() {
	if err := i18n.Init(); err != nil {
		panic(err)
	}
}

func TestOpenRouterLocalReserveSettlesActualCost(t *testing.T) {
	oldBatch, oldRedis, oldLogs := common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled
	common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled = false, false, false
	t.Cleanup(func() {
		common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled = oldBatch, oldRedis, oldLogs
	})
	for _, tc := range []struct {
		name, response string
		actualQuota    int
	}{
		{"actual cost exceeds local hold", `{"usage":{"cost":3}}`, 2250000},
		{"explicit zero refunds local hold", `{"usage":{"cost":0}}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const initial = 5000000
			user := model.User{Username: "local-reserve-" + tc.name, Quota: initial}
			require.NoError(t, model.DB.Create(&user).Error)
			t.Cleanup(func() { require.NoError(t, model.DB.Unscoped().Delete(&user).Error) })
			token := model.Token{UserId: user.Id, Key: user.Username, RemainQuota: initial}
			require.NoError(t, model.DB.Create(&token).Error)
			t.Cleanup(func() { require.NoError(t, model.DB.Unscoped().Delete(&token).Error) })
			info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, ChannelMeta: &relaycommon.ChannelMeta{}, OriginModelName: "local-reserve-model"}
			info.PriceData.BillingGroupRatio = 1.5
			info.OpenRouterCost = &relaycommon.OpenRouterCostState{Basis: relaycommon.NewOpenRouterCostBasis(info, 2), ConfiguredReserveUSD: common.GetPointer(2.0), Sent: true}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info.UserSetting.BillingPreference = "wallet_only"
			require.Nil(t, service.PreConsumeBilling(ctx, 0, info))
			session := info.Billing
			hold, clamp, err := info.OpenRouterCost.Basis.Quota(2)
			require.NoError(t, err)
			require.Nil(t, clamp)
			require.NoError(t, session.Reserve(hold))
			info.FinalPreConsumedQuota = session.GetPreConsumedQuota()
			require.Equal(t, 1500000, info.FinalPreConsumedQuota)
			info.OpenRouterCost.Observe([]byte(tc.response), true)
			require.True(t, info.OpenRouterCost.Final)
			info.PriceData.BillingGroupRatio, info.PriceData.ModelPrice = 9, 999
			service.PostTextConsumeQuota(ctx, info, &dto.Usage{Cost: info.OpenRouterCost.CostUSD}, nil)
			outcome := session.(interface {
				OpenRouterSettlementOutcome() billingdomain.Outcome
			}).OpenRouterSettlementOutcome()
			assert.Equal(t, "settled", outcome.SettlementStatus)
			assert.Equal(t, tc.actualQuota, outcome.TargetQuota)
			assert.Equal(t, tc.actualQuota, outcome.FundingAppliedQuota)
			assert.Equal(t, tc.actualQuota, outcome.TokenAppliedQuota)
			require.NoError(t, session.Settle(tc.actualQuota))
			require.NoError(t, model.DB.First(&user, user.Id).Error)
			require.NoError(t, model.DB.First(&token, token.Id).Error)
			assert.Equal(t, initial-tc.actualQuota, user.Quota)
			assert.Equal(t, initial-tc.actualQuota, token.RemainQuota)
			assert.False(t, session.NeedsRefund())
		})
	}
}
