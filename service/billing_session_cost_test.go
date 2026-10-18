package service

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

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
			session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: user.Id}, trusted: true}
			info.Billing = session
			hold, clamp, err := info.OpenRouterCost.Basis.Quota(2)
			require.NoError(t, err)
			require.Nil(t, clamp)
			require.NoError(t, session.Reserve(hold))
			info.FinalPreConsumedQuota = session.GetPreConsumedQuota()
			require.Equal(t, 1500000, info.FinalPreConsumedQuota)
			info.OpenRouterCost.Observe([]byte(tc.response), true)
			require.True(t, info.OpenRouterCost.Final)
			info.PriceData.BillingGroupRatio, info.PriceData.ModelPrice = 9, 999
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			PostTextConsumeQuota(ctx, info, &dto.Usage{Cost: info.OpenRouterCost.CostUSD}, nil)
			outcome := session.OpenRouterSettlementOutcome()
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

type costTestFunding struct {
	fail    bool
	applied int
	calls   int
}

func (f *costTestFunding) Source() string       { return BillingSourceSubscription }
func (f *costTestFunding) PreConsume(int) error { return nil }
func (f *costTestFunding) Refund() error        { return nil }
func (f *costTestFunding) Settle(delta int) error {
	f.calls++
	if f.fail {
		return errors.New("subscription quota insufficient")
	}
	f.applied += delta
	return nil
}

func TestOpenRouterSettlementAppliedMoney(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		target                 int
		fundingFail, tokenFail bool
		status                 string
		funding, token         int
	}{
		{"subscription overrun", 150, true, false, "funding_delta_failed", 100, 100},
		{"refund delta fails", 50, true, false, "funding_delta_failed", 100, 100},
		{"funding succeeds token fails", 150, false, true, "token_delta_failed", 150, 100},
		{"equal hold settles", 100, false, false, "settled", 100, 100},
		{"zero refunds hold", 0, false, false, "settled", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldBatch, oldRedis := common.BatchUpdateEnabled, common.RedisEnabled
			common.BatchUpdateEnabled = false
			common.RedisEnabled = false
			t.Cleanup(func() { common.BatchUpdateEnabled = oldBatch; common.RedisEnabled = oldRedis })
			token := model.Token{Key: "or-outcome-" + tc.name, RemainQuota: 1000}
			require.NoError(t, model.DB.Create(&token).Error)
			t.Cleanup(func() { require.NoError(t, model.DB.Unscoped().Delete(&token).Error) })
			if tc.tokenFail {
				name := "or_cost_token_failure"
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
					if tx.Statement.Table == "tokens" {
						tx.AddError(errors.New("injected token database error"))
					}
				}))
				t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove(name)) })
			}
			funding := &costTestFunding{fail: tc.fundingFail, applied: 100}
			info := &relaycommon.RelayInfo{TokenId: token.Id, OpenRouterCost: &relaycommon.OpenRouterCostState{}}
			session := &BillingSession{relayInfo: info, funding: funding, preConsumedQuota: 100, tokenConsumed: 100}
			err := session.Settle(tc.target)
			if tc.fundingFail || tc.tokenFail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			result := session.OpenRouterSettlementOutcome()
			assert.Equal(t, tc.status, result.SettlementStatus)
			assert.Equal(t, tc.funding, result.FundingAppliedQuota)
			assert.Equal(t, tc.token, result.TokenAppliedQuota)
			assert.False(t, session.NeedsRefund())
			require.NoError(t, session.Settle(tc.target))
			assert.LessOrEqual(t, funding.calls, 1)
		})
	}
}

func TestOpenRouterTrustedTransitionReservesAtomically(t *testing.T) {
	oldBatch, oldRedis := common.BatchUpdateEnabled, common.RedisEnabled
	common.BatchUpdateEnabled = false
	common.RedisEnabled = false
	t.Cleanup(func() { common.BatchUpdateEnabled = oldBatch; common.RedisEnabled = oldRedis })
	user := model.User{Username: "or-trusted-transition", Quota: 120}
	require.NoError(t, model.DB.Create(&user).Error)
	t.Cleanup(func() { require.NoError(t, model.DB.Unscoped().Delete(&user).Error) })
	token := model.Token{UserId: user.Id, Key: "or-trusted-transition", RemainQuota: 1000}
	require.NoError(t, model.DB.Create(&token).Error)
	t.Cleanup(func() { require.NoError(t, model.DB.Unscoped().Delete(&token).Error) })
	info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, OpenRouterCost: &relaycommon.OpenRouterCostState{}}
	session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: user.Id}, trusted: true}
	require.NoError(t, session.Reserve(100))
	assert.Equal(t, 100, session.GetPreConsumedQuota())
	assert.False(t, session.trusted)
	require.Error(t, session.Reserve(150))
	assert.Equal(t, 100, session.GetPreConsumedQuota())
	require.NoError(t, model.DB.First(&user, user.Id).Error)
	require.NoError(t, model.DB.First(&token, token.Id).Error)
	assert.Equal(t, 20, user.Quota)
	assert.Equal(t, 900, token.RemainQuota)
	require.NoError(t, session.Reserve(50))
	assert.Equal(t, 100, session.GetPreConsumedQuota())
}

func TestOpenRouterSubscriptionOverrunKeepsAppliedHold(t *testing.T) {
	const id = 940015
	seedSubscription(t, id, id, 100, 50)
	t.Cleanup(func() { require.NoError(t, model.DB.Delete(&model.UserSubscription{}, id).Error) })
	info := &relaycommon.RelayInfo{OpenRouterCost: &relaycommon.OpenRouterCostState{}}
	session := &BillingSession{relayInfo: info, funding: &SubscriptionFunding{subscriptionId: id, preConsumed: 50}, preConsumedQuota: 50, tokenConsumed: 50}
	require.Error(t, session.Settle(150))
	outcome := session.OpenRouterSettlementOutcome()
	assert.Equal(t, "funding_delta_failed", outcome.SettlementStatus)
	assert.Equal(t, 50, outcome.FundingAppliedQuota)
	assert.Equal(t, int64(50), getSubscriptionUsed(t, id))
	assert.False(t, session.NeedsRefund())
}
