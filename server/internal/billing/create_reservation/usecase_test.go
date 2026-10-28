package create_reservation

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/billing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ledgerFixture struct {
	wallet, token, tokenUsed, walletHeld, tokenHeld int
	subscription, overflow                          bool
	subUsed, subTotal                               int64
	tokenErr, queryErr, reserveErr                  error
	events                                          []string
}

func (f *ledgerFixture) WalletBalance(int) (int, error) { return f.wallet, f.queryErr }

func (f *ledgerFixture) ReserveWallet(_ int, n int) (bool, error) {
	f.events = append(f.events, "reserve wallet")
	if f.reserveErr != nil {
		return false, f.reserveErr
	}
	if f.wallet-f.walletHeld < n {
		return false, nil
	}
	f.wallet -= n
	return true, nil
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

func (f *ledgerFixture) ReserveToken(a domain.Account, n int) (bool, error) {
	f.events = append(f.events, "reserve token")
	if f.tokenErr != nil {
		return false, f.tokenErr
	}
	if !a.Unlimited && f.token-f.tokenHeld < n {
		return false, nil
	}
	f.token -= n
	f.tokenUsed += n
	return true, nil
}

func (f *ledgerFixture) TokenRemaining(string) int { return f.token }

func (f *ledgerFixture) HasSubscription(int) (bool, error) { return f.subscription, f.queryErr }

func (f *ledgerFixture) AllowsWalletOverflow(int) (bool, error) { return f.overflow, f.queryErr }

func (f *ledgerFixture) ReserveSubscription(_ string, _ int, _ string, int64Amount int64) (domain.SubscriptionHold, error) {
	f.events = append(f.events, "reserve subscription")
	if !f.subscription {
		return domain.SubscriptionHold{}, errors.New("no active subscription")
	}
	if f.subTotal-f.subUsed < int64Amount {
		return domain.SubscriptionHold{}, errors.New("subscription quota insufficient")
	}
	f.subUsed += int64Amount
	return domain.SubscriptionHold{ID: 1, PreConsumed: int64Amount, AmountTotal: f.subTotal, AmountUsedAfter: f.subUsed}, nil
}

func quiet(string) {}

func fixtureInput() Input {
	return Input{Account: domain.Account{UserID: 1, TokenID: 1, TokenKey: "fixture"}, RequestID: "request", Quota: 100, Preference: "wallet_only"}
}

func TestReservationPreferencesAndInitialWriteOrder(t *testing.T) {
	for _, tc := range []struct {
		name, preference, source string
		wallet                   int
		subscription, overflow   bool
		subTotal                 int64
		wantError                bool
	}{
		{"wallet", "wallet_only", domain.Wallet, 1000, false, false, 0, false},
		{"subscription", "subscription_only", domain.Subscription, 1000, true, false, 1000, false},
		{"wallet first fallback", "wallet_first", domain.Subscription, 0, true, false, 1000, false},
		{"subscription overflow", "subscription_first", domain.Wallet, 1000, true, true, 50, false},
		{"overflow denied", "subscription_first", domain.Subscription, 1000, true, false, 50, true},
		{"no subscription", "subscription_first", domain.Wallet, 1000, false, false, 0, false},
		{"strict subscription", "subscription_only", domain.Subscription, 1000, false, false, 0, true},
		{"unknown default", "unknown", domain.Subscription, 1000, true, false, 1000, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &ledgerFixture{wallet: tc.wallet, token: 1000, subscription: tc.subscription, overflow: tc.overflow, subTotal: tc.subTotal}
			input := fixtureInput()
			input.Preference = tc.preference
			r, err := NewUsecase(f, func() int { return 0 }, quiet).Execute(input)
			if tc.wantError {
				require.Error(t, err)
				assert.Equal(t, 1000, f.token)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.source, r.Source)
			assert.Equal(t, 100, r.PreConsumed)
			assert.Equal(t, 900, f.token)
			if tc.source == domain.Subscription {
				assert.Equal(t, int64(100), f.subUsed)
			} else {
				assert.Equal(t, tc.wallet-100, f.wallet)
			}
			if tc.name == "wallet" {
				assert.Equal(t, []string{"reserve token", "reserve wallet"}, f.events)
			}
		})
	}
}

func TestReservationTrustAndHeldAdmission(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		wallet, token, walletHeld, tokenHeld int
		unlimited, force                     bool
		trusted, fail                        bool
	}{
		{"trusted", 1000, 1000, 0, 0, false, false, true, false},
		{"unlimited trusted", 1000, 0, 0, 0, true, false, true, false},
		{"forced", 1000, 1000, 0, 0, false, true, false, false},
		{"wallet threshold", 500, 1000, 0, 0, false, false, false, false},
		{"token threshold", 1000, 500, 0, 0, false, false, false, false},
		{"held wallet", 1000, 1000, 950, 0, false, true, false, true},
		{"held token", 1000, 1000, 0, 950, false, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &ledgerFixture{wallet: tc.wallet, token: tc.token, walletHeld: tc.walletHeld, tokenHeld: tc.tokenHeld}
			input := fixtureInput()
			input.Account.Unlimited, input.TokenQuota, input.Force = tc.unlimited, tc.token, tc.force
			r, err := NewUsecase(f, func() int { return 500 }, quiet).Execute(input)
			if tc.fail {
				require.Error(t, err)
				assert.Equal(t, tc.wallet, f.wallet)
				assert.Equal(t, tc.token, f.token)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.trusted, r.Trusted)
			hold := 100
			if tc.trusted {
				hold = 0
			}
			assert.Equal(t, hold, r.PreConsumed)
		})
	}
}

func TestInitialTechnicalFailureDoesNotFallback(t *testing.T) {
	for _, query := range []bool{false, true} {
		failure := errors.New("injected storage failure")
		f := &ledgerFixture{wallet: 1000, token: 1000, subscription: true, subTotal: 1000}
		if query {
			f.queryErr = failure
		} else {
			f.tokenErr = failure
		}
		input := fixtureInput()
		input.Preference = "wallet_first"
		_, err := NewUsecase(f, func() int { return 0 }, quiet).Execute(input)
		require.ErrorIs(t, err, failure)
		assert.Equal(t, int64(0), f.subUsed)
		assert.NotContains(t, f.events, "reserve subscription")
	}
}

func TestSubscriptionZeroEstimateStillReservesOne(t *testing.T) {
	f := &ledgerFixture{token: 1000, subscription: true, subTotal: 1000}
	input := fixtureInput()
	input.Preference, input.Quota = "subscription_only", 0
	r, err := NewUsecase(f, func() int { return 500 }, quiet).Execute(input)
	require.NoError(t, err)
	assert.Equal(t, 1, r.PreConsumed)
	assert.Equal(t, int64(1), r.Subscription.PreConsumed)
	assert.Equal(t, 999, f.token)
	assert.Equal(t, int64(1), f.subUsed)
}
