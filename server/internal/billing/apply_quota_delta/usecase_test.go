package apply_quota_delta

import (
	"errors"
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

func TestDirectDeltaReportsPartialApplication(t *testing.T) {
	f := &ledgerFixture{wallet: 1000, token: 1000, tokenErr: errors.New("token failure")}
	result, err := NewUsecase(f).Execute(Input{Delta: 50})
	require.Error(t, err)
	assert.True(t, result.FundingApplied)
	assert.False(t, result.TokenApplied)
	assert.Equal(t, 950, f.wallet)
}
