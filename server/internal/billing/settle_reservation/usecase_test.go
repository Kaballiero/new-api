package settle_reservation

import (
	"errors"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/internal/billing/domain"
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

func TestReservationDeltaSettlement(t *testing.T) {
	for _, actual := range []int{0, 50, 100, 150} {
		t.Run(strconv.Itoa(actual), func(t *testing.T) {
			f := &ledgerFixture{wallet: 900, token: 900, tokenUsed: 100}
			r := &domain.Reservation{Source: domain.Wallet, PreConsumed: 100, TokenConsumed: 100, WalletConsumed: 100}
			u := NewUsecase(f, quiet)
			require.NoError(t, u.Execute(Input{Reservation: r, Actual: actual}))
			require.NoError(t, u.Execute(Input{Reservation: r, Actual: actual}))
			assert.Equal(t, 1000-actual, f.wallet)
			assert.Equal(t, 1000-actual, f.token)
			assert.False(t, r.NeedsRefund())
		})
	}
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
			f := &ledgerFixture{token: 900, tokenUsed: 100, subUsed: 100, subTotal: 1000}
			if tc.fundingFail {
				f.fundingErr = errors.New("subscription quota insufficient")
			}
			if tc.tokenFail {
				f.tokenErr = errors.New("injected token database error")
			}
			r := &domain.Reservation{Source: domain.Subscription, PreConsumed: 100, TokenConsumed: 100, Subscription: domain.SubscriptionHold{ID: 1, PreConsumed: 100}}
			u := NewUsecase(f, quiet)
			err := u.Execute(Input{Reservation: r, Actual: tc.target, RecordOutcome: true})
			if tc.fundingFail || tc.tokenFail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.status, r.Outcome.SettlementStatus)
			assert.Equal(t, tc.funding, r.Outcome.FundingAppliedQuota)
			assert.Equal(t, tc.token, r.Outcome.TokenAppliedQuota)
			assert.Equal(t, int64(tc.funding), f.subUsed)
			assert.Equal(t, 1000-tc.token, f.token)
			assert.False(t, r.NeedsRefund())
			events := len(f.events)
			require.NoError(t, u.Execute(Input{Reservation: r, Actual: tc.target, RecordOutcome: true}))
			assert.Len(t, f.events, events)
		})
	}
}

func TestOpenRouterSubscriptionOverrunKeepsAppliedHold(t *testing.T) {
	f := &ledgerFixture{subUsed: 50, subTotal: 100}
	r := &domain.Reservation{Source: domain.Subscription, PreConsumed: 50, TokenConsumed: 50, Subscription: domain.SubscriptionHold{ID: 1, PreConsumed: 50}}
	require.Error(t, NewUsecase(f, quiet).Execute(Input{Reservation: r, Actual: 150, RecordOutcome: true}))
	assert.Equal(t, int64(50), f.subUsed)
	assert.Equal(t, 50, r.Outcome.FundingAppliedQuota)
	assert.False(t, r.NeedsRefund())
}

func TestRealtimePartialLegRetry(t *testing.T) {
	f := &ledgerFixture{wallet: 800, token: 900, tokenUsed: 100, tokenErr: errors.New("token failure")}
	r := &domain.Reservation{Source: domain.Wallet, PreConsumed: 100, TokenConsumed: 100, WssFundingDirect: 100}
	u := NewUsecase(f, quiet)
	require.Error(t, u.Execute(Input{Reservation: r, Actual: 250, Realtime: true}))
	assert.Equal(t, 750, f.wallet)
	assert.True(t, r.FundingSettled)
	assert.False(t, r.NeedsRefund())
	f.tokenErr = nil
	require.NoError(t, u.Execute(Input{Reservation: r, Actual: 250, Realtime: true}))
	assert.Equal(t, 750, f.wallet)
	assert.Equal(t, 750, f.token)
}
