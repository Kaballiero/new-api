package billing

import (
	mappingfx "github.com/QuantumNous/new-api/gateway/mapping"

	"errors"
	"math"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/billing/create_reservation"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/billing/increase_reservation"
	"github.com/QuantumNous/new-api/internal/billing/settle_reservation"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ledgerFixture struct {
	wallet, token, tokenUsed int
	subUsed, subTotal        int64
	fundingErr, tokenErr     error
	events                   []string
}

func (f *ledgerFixture) DebitWallet(_ int, n int) error {
	f.events = append(f.events, "debit wallet")
	if f.fundingErr != nil {
		return f.fundingErr
	}
	f.wallet -= n
	return nil
}

func (f *ledgerFixture) CreditWallet(_ int, n int) error {
	f.events = append(f.events, "credit wallet")
	if f.fundingErr != nil {
		return f.fundingErr
	}
	f.wallet += n
	return nil
}

func (f *ledgerFixture) AdjustSubscription(_ int, delta int64) error {
	f.events = append(f.events, "adjust subscription")
	if f.fundingErr != nil {
		return f.fundingErr
	}
	if f.subUsed+delta > f.subTotal {
		return errors.New("subscription used exceeds total")
	}
	f.subUsed = max(0, f.subUsed+delta)
	return nil
}

func (f *ledgerFixture) DebitToken(_ domain.Account, n int) error {
	f.events = append(f.events, "debit token")
	if f.tokenErr != nil {
		return f.tokenErr
	}
	f.token -= n
	f.tokenUsed += n
	return nil
}

func (f *ledgerFixture) CreditToken(_ domain.Account, n int) error {
	f.events = append(f.events, "credit token")
	if f.tokenErr != nil {
		return f.tokenErr
	}
	f.token += n
	f.tokenUsed -= n
	return nil
}

func quiet(string) {}

func TestBillingErrorLocales(t *testing.T) {
	require.NoError(t, i18n.Init())
	previous := operation_setting.GetGeneralSetting().QuotaDisplayType
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	t.Cleanup(func() { operation_setting.GetGeneralSetting().QuotaDisplayType = previous })
	for _, language := range []string{"en", "ru"} {
		for _, tc := range []struct {
			name, key, en, ru string
			failure           *domain.Failure
		}{
			{"wallet", i18n.MsgBillingWalletInsufficient, "Insufficient user quota, remaining quota: 5", "Недостаточно квоты пользователя, осталось: 5", &domain.Failure{Kind: domain.WalletEmpty, Remaining: 5}},
			{"reserve", i18n.MsgBillingWalletBelowReserve, "Failed to reserve quota, remaining user quota: 5, required quota: 10", "Не удалось зарезервировать квоту, осталось у пользователя: 5, требуется: 10", &domain.Failure{Kind: domain.WalletBelowEstimate, Remaining: 5, Required: 10}},
			{"subscription", i18n.MsgBillingSubscriptionInsufficient, "Insufficient subscription quota or no subscription configured: no active subscription", "Недостаточно квоты подписки или подписка не настроена: no active subscription", &domain.Failure{Kind: domain.SubscriptionUnavailable, Cause: errors.New("no active subscription")}},
		} {
			t.Run(language+"/"+tc.name, func(t *testing.T) {
				err := mapError(language, tc.failure)
				assert.Equal(t, 403, err.StatusCode)
				assert.Equal(t, types.ErrorCodeInsufficientUserQuota, err.GetErrorCode())
				assert.True(t, types.IsSkipRetryError(err))
				assert.False(t, types.IsRecordErrorLog(err))
				want := tc.en
				if language == "ru" {
					want = tc.ru
				}
				assert.Equal(t, want, err.Error())
				translated := i18n.Translate(language, tc.key, map[string]any{"Remaining": "5", "Required": "10", "Reason": "no active subscription"})
				assert.NotEqual(t, tc.key, translated)
				assert.Equal(t, translated, err.Error())
			})
		}
	}
}

// TEST_POSTGRES_DSN must point at an isolated PostgreSQL test database. This
// verifies the new adapter against real storage; it never runs a fake dialect.

func TestSessionSerializesConcurrentTerminalCalls(t *testing.T) {
	f := &ledgerFixture{wallet: 900, token: 900, tokenUsed: 100}
	g := &Gateway{}
	g.usecases.Settle = settle_reservation.NewUsecase(f, quiet)
	session := &Session{gateway: g, info: &relaycommon.RelayInfo{}, reservation: &domain.Reservation{Source: domain.Wallet, PreConsumed: 100, TokenConsumed: 100}}
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() { results <- session.Settle(150) })
	}
	workers.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	assert.Equal(t, 850, f.wallet)
	assert.Equal(t, 850, f.token)
	assert.Equal(t, []string{"debit wallet", "debit token"}, f.events)
}

// This fake covers only the admission/increase ports reached by gateway mapping.
// Money sequencing itself is tested next to each scenario.
type admissionStore struct {
	balance     int
	increaseErr error
}

func (f *admissionStore) WalletBalance(int) (int, error)               { return f.balance, nil }
func (*admissionStore) ReserveWallet(int, int) (bool, error)           { return true, nil }
func (*admissionStore) ReserveToken(domain.Account, int) (bool, error) { return true, nil }
func (*admissionStore) TokenRemaining(string) int                      { return 1000 }
func (*admissionStore) CreditToken(domain.Account, int) error          { return nil }
func (*admissionStore) HasSubscription(int) (bool, error)              { return true, nil }
func (*admissionStore) AllowsWalletOverflow(int) (bool, error)         { return false, nil }
func (*admissionStore) ReserveSubscription(_ string, _ int, _ string, amount int64) (domain.SubscriptionHold, error) {
	return domain.SubscriptionHold{ID: 1, PreConsumed: amount, AmountTotal: 1000, AmountUsedAfter: amount}, nil
}
func (f *admissionStore) DebitWallet(int, int) error          { return f.increaseErr }
func (*admissionStore) CreditWallet(int, int) error           { return nil }
func (f *admissionStore) AdjustSubscription(int, int64) error { return f.increaseErr }

func TestPreConsumeWithoutRequest(t *testing.T) {
	require.NoError(t, i18n.Init())
	withoutRequest, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, tc := range []struct {
		name    string
		context *gin.Context
	}{
		{"nil context", nil},
		{"nil request", withoutRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, balance := range []int{1000, 0} {
				f := &admissionStore{balance: balance}
				g := &Gateway{}
				g.usecases.Create = create_reservation.NewUsecase(f, func() int { return 0 }, quiet)
				info := &relaycommon.RelayInfo{}
				info.UserSetting.BillingPreference = "wallet_only"
				err := g.PreConsume(tc.context, 100, info)
				if balance == 0 {
					require.NotNil(t, err)
					assert.Equal(t, types.ErrorCodeInsufficientUserQuota, err.GetErrorCode())
					assert.Equal(t, 403, err.StatusCode)
					assert.Contains(t, err.Error(), "Insufficient user quota")
					continue
				}
				require.Nil(t, err)
				require.NotNil(t, info.Billing)
				assert.Equal(t, domain.Wallet, info.BillingSource)
				assert.Equal(t, 100, info.FinalPreConsumedQuota)
				assert.Equal(t, i18n.DefaultLang, info.Billing.(*Session).language())
			}
		})
	}
}

func TestReserveResolvesCapturedLanguageOnlyOnError(t *testing.T) {
	require.NoError(t, i18n.Init())
	lookups := 0
	// This test binary does not run main, the only production loader registrar.
	i18n.SetUserLangLoader(func(id int) string {
		assert.Equal(t, 7, id)
		lookups++
		return ""
	})
	t.Cleanup(func() { i18n.SetUserLangLoader(nil) })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request.Header.Set("Accept-Language", "ru")
	c.Set("id", 7)
	f := &admissionStore{balance: 1000}
	g := &Gateway{}
	g.usecases.Create = create_reservation.NewUsecase(f, func() int { return 0 }, quiet)
	g.usecases.Increase = increase_reservation.NewUsecase(f, quiet)
	info := &relaycommon.RelayInfo{}
	info.UserSetting.BillingPreference = "subscription_only"
	require.Nil(t, g.PreConsume(c, 100, info))
	assert.Zero(t, lookups, "successful admission must not load a language")
	require.NoError(t, info.Billing.Reserve(100))
	assert.Zero(t, lookups, "a successful no-op increase must not load a language")
	c.Set("id", 99)
	c.Request.Header.Set("Accept-Language", "en")
	f.increaseErr = errors.New("quota limit")
	for range 2 {
		err := info.Billing.Reserve(150)
		require.EqualError(t, err, "Недостаточно квоты подписки или подписка не настроена: quota limit")
		assert.Equal(t, 1, lookups, "language inputs are captured before Gin context reuse")
	}
}

func TestSessionProjectsSubscriptionAndOutcome(t *testing.T) {
	info := &relaycommon.RelayInfo{SubscriptionPostDelta: 12}
	s := &Session{info: info, reservation: &domain.Reservation{
		Source: domain.Subscription, PreConsumed: 150, ExtraReserved: 50,
		Subscription: domain.SubscriptionHold{ID: 2, PreConsumed: 100, AmountTotal: 1000, AmountUsedAfter: 200, PlanID: 6, PlanTitle: "Pro"},
		Outcome:      domain.Outcome{TargetQuota: 180, ReservedQuota: 150, FundingAppliedQuota: 180, TokenAppliedQuota: 150, SettlementStatus: "token_delta_failed", FailureStage: "token_delta"},
	}}
	s.syncRelayInfo()
	assert.Equal(t, domain.Subscription, info.BillingSource)
	assert.Equal(t, 150, info.FinalPreConsumedQuota)
	assert.Equal(t, 2, info.SubscriptionId)
	assert.Equal(t, int64(150), info.SubscriptionPreConsumed)
	assert.Zero(t, info.SubscriptionPostDelta)
	assert.Equal(t, int64(1000), info.SubscriptionAmountTotal)
	assert.Equal(t, int64(250), info.SubscriptionAmountUsedAfterPreConsume)
	assert.Equal(t, 6, info.SubscriptionPlanId)
	assert.Equal(t, "Pro", info.SubscriptionPlanTitle)
	assert.Equal(t, domain.Outcome{TargetQuota: 180, ReservedQuota: 150, FundingAppliedQuota: 180, TokenAppliedQuota: 150, SettlementStatus: "token_delta_failed", FailureStage: "token_delta"}, s.OpenRouterSettlementOutcome())
	s.reservation.Source = domain.Wallet
	s.syncRelayInfo()
	assert.Equal(t, domain.Wallet, info.BillingSource)
	assert.Zero(t, info.SubscriptionId)
	assert.Zero(t, info.SubscriptionPreConsumed)
	assert.Equal(t, 6, info.SubscriptionPlanId, "legacy wallet projection only clears the subscription ID and preconsume")
}

func TestSelectedGroupInvalidFXRetainsUnavailableStatus(t *testing.T) {
	for _, pure := range []float64{-1, math.NaN(), math.Inf(1), math.MaxFloat64} {
		snap := &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", GroupRatio: 1, EstimatedQuotaBeforeGroup: 100, EstimatedQuotaAfterGroup: 100}
		info := &relaycommon.RelayInfo{BillingFXFactor: 2, TieredBillingSnapshot: snap, PriceData: hosttypes.PriceData{GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: pure}}}
		err := PrepareTieredBillingForSelectedGroup(nil, info)
		require.NotNil(t, err)
		assert.Equal(t, 503, err.StatusCode)
		assert.Equal(t, types.ErrorCodeModelPriceError, err.GetErrorCode())
		assert.True(t, mappingfx.IsBillingFXError(err.Err))
		assert.Equal(t, 1.0, snap.GroupRatio)
		assert.Equal(t, 100, snap.EstimatedQuotaAfterGroup)
	}
}
func TestCostMappingWithoutGinRequest(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{OriginModelName: "fixture", PriceData: hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}}}
	prior := &common.QuotaClamp{Op: "prior", Kind: common.QuotaClampOverflow, Original: 1e20, Clamped: 2147483647}
	info.QuotaClamp = prior
	result := New().TextCost(ctx, info, &dto.Usage{PromptTokens: 1, TotalTokens: 1})
	assert.Equal(t, 1, result.Quota)
	assert.Same(t, prior, info.QuotaClamp)
	_, err := New().CaptureFX(nil)
	require.Error(t, err)
	assert.True(t, mappingfx.IsBillingFXError(err))
}
