package increase_reservation

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/billing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ledgerFixture struct {
	wallet, token, tokenUsed, walletHeld, tokenHeld int
	subUsed, subTotal                               int64
	fundingErr, tokenErr, reserveErr                error
	events                                          []string
}

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

func quiet(string) {}

func TestOpenRouterTrustedTransitionReservesAtomically(t *testing.T) {
	f := &ledgerFixture{wallet: 120, token: 1000}
	r := &domain.Reservation{Source: domain.Wallet, Trusted: true}
	u := NewUsecase(f, quiet)
	require.NoError(t, u.Execute(Input{Reservation: r, Target: 100, AtomicWallet: true}))
	assert.False(t, r.Trusted)
	require.Error(t, u.Execute(Input{Reservation: r, Target: 150, AtomicWallet: true}))
	assert.Equal(t, 100, r.PreConsumed)
	assert.Equal(t, 20, f.wallet)
	assert.Equal(t, 900, f.token)
	require.NoError(t, u.Execute(Input{Reservation: r, Target: 50, AtomicWallet: true}))
	assert.Equal(t, 100, r.PreConsumed)
}

func TestIncreaseRollbackAndLegacyDebt(t *testing.T) {
	for _, source := range []string{domain.Wallet, domain.Subscription} {
		t.Run(source, func(t *testing.T) {
			f := &ledgerFixture{wallet: 20, token: 0, subUsed: 100, subTotal: 1000}
			r := &domain.Reservation{Source: source, PreConsumed: 100, TokenConsumed: 100, WalletConsumed: 100, Subscription: domain.SubscriptionHold{ID: 1, PreConsumed: 100}}
			u := NewUsecase(f, quiet)
			require.Error(t, u.Execute(Input{Reservation: r, Target: 150}))
			assert.Equal(t, 20, f.wallet)
			assert.Equal(t, int64(100), f.subUsed)
			assert.Equal(t, 100, r.PreConsumed)
			f.token = 1000
			f.events = nil
			require.NoError(t, u.Execute(Input{Reservation: r, Target: 150}))
			if source == domain.Wallet {
				assert.Equal(t, []string{"debit wallet", "reserve token"}, f.events)
				assert.Equal(t, -30, f.wallet)
			} else {
				assert.Equal(t, []string{"adjust subscription", "reserve token"}, f.events)
				assert.Equal(t, int64(150), f.subUsed)
			}
		})
	}
}
