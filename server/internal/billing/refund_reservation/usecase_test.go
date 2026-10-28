package refund_reservation

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/billing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ledgerFixture struct {
	wallet, token, tokenUsed  int
	subUsed, subTotal         int64
	fundingErr, tokenErr      error
	refundErrors, refundCalls int
	events                    []string
}

func (f *ledgerFixture) CreditWallet(_ int, n int) error {
	f.events = append(f.events, "credit wallet")
	if f.fundingErr != nil {
		return f.fundingErr
	}
	f.wallet += n
	return nil
}

func (f *ledgerFixture) RefundSubscription(string) error {
	f.refundCalls++
	f.events = append(f.events, "refund subscription")
	if f.refundCalls <= f.refundErrors {
		return errors.New("refund failed")
	}
	f.subUsed -= 100
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

func TestRefundQueuesWholeHoldOnce(t *testing.T) {
	f := &ledgerFixture{wallet: 850, token: 850, tokenUsed: 150}
	r := &domain.Reservation{Source: domain.Wallet, PreConsumed: 150, TokenConsumed: 150, WalletConsumed: 150, ExtraReserved: 50}
	var queued []func()
	u := NewUsecase(f, func(job func()) { queued = append(queued, job) }, func(time.Duration) {}, quiet)
	require.NoError(t, u.Execute(r))
	require.NoError(t, u.Execute(r))
	require.Len(t, queued, 1)
	queued[0]()
	assert.Equal(t, 1000, f.wallet)
	assert.Equal(t, 1000, f.token)

	queued = nil
	r = &domain.Reservation{Source: domain.Wallet, PreConsumed: 100, TokenConsumed: 100, WalletConsumed: 100, Settled: true}
	require.NoError(t, u.Execute(r))
	assert.Empty(t, queued, "settled funding must not be refunded")
}

func TestRefundRetryAndExtraReservation(t *testing.T) {
	for _, failures := range []int{0, 2, 3} {
		f := &ledgerFixture{subUsed: 150, subTotal: 1000, token: 850, tokenUsed: 150, refundErrors: failures}
		r := &domain.Reservation{Source: domain.Subscription, PreConsumed: 150, TokenConsumed: 150, ExtraReserved: 50, Subscription: domain.SubscriptionHold{ID: 1, PreConsumed: 100}}
		var delays []time.Duration
		u := NewUsecase(f, func(job func()) { job() }, func(delay time.Duration) { delays = append(delays, delay) }, quiet)
		require.NoError(t, u.Execute(r))
		require.NoError(t, u.Execute(r))
		assert.Equal(t, min(failures+1, 3), f.refundCalls)
		if failures > 0 {
			assert.Equal(t, []time.Duration{200 * time.Millisecond, 400 * time.Millisecond}, delays)
		} else {
			assert.Empty(t, delays)
		}
		remaining := int64(0)
		if failures == 3 {
			remaining = 100
		}
		assert.Equal(t, remaining, f.subUsed)
		assert.Equal(t, 1000, f.token)
	}
}

func TestWalletRefundFailureIsNotRetriedAndStillReturnsToken(t *testing.T) {
	f := &ledgerFixture{wallet: 900, token: 900, tokenUsed: 100, fundingErr: errors.New("wallet credit failure")}
	r := &domain.Reservation{Source: domain.Wallet, PreConsumed: 100, TokenConsumed: 100, WalletConsumed: 100}
	u := NewUsecase(f, func(job func()) { job() }, func(time.Duration) { require.FailNow(t, "wallet refund must not retry") }, quiet)
	require.NoError(t, u.Execute(r))
	require.NoError(t, u.Execute(r))
	assert.Equal(t, []string{"credit wallet", "credit token"}, f.events)
	assert.Equal(t, 900, f.wallet)
	assert.Equal(t, 1000, f.token)
}
