package transfer_affiliate_quota

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTransfer struct {
	balances          AffiliateBalances
	saved             *AffiliateBalances
	committed, rolled bool
}

func (f *fakeTransfer) Begin() (AffiliateTransferTx, error)      { return f, nil }
func (f *fakeTransfer) LockBalances() (AffiliateBalances, error) { return f.balances, nil }
func (f *fakeTransfer) SaveBalances(b AffiliateBalances) error {
	f.saved = &b
	return nil
}
func (f *fakeTransfer) Commit() error   { f.committed = true; return nil }
func (f *fakeTransfer) Rollback() error { f.rolled = true; return nil }

type display struct{}

func (display) MinimumQuota() float64    { return 500 }
func (display) FormatQuota(q int) string { return "$1.00" }

func TestTransferAffiliateQuota(t *testing.T) {
	t.Run("below minimum", func(t *testing.T) {
		transfer := &fakeTransfer{}
		err := TransferAffiliateQuota(499, transfer, display{}, common.QuotaFromFloat)
		var amount *domain.AmountError
		require.ErrorAs(t, err, &amount)
		assert.ErrorIs(t, err, domain.ErrTransferBelowMinimum)
		assert.Equal(t, "$1.00", amount.Amount)
		assert.Nil(t, transfer.saved)
	})
	t.Run("insufficient affiliate quota", func(t *testing.T) {
		transfer := &fakeTransfer{balances: AffiliateBalances{Quota: 10, AffiliateQuota: 499}}
		require.ErrorIs(t, TransferAffiliateQuota(500, transfer, display{}, common.QuotaFromFloat), domain.ErrInviteQuotaInsufficient)
		assert.Nil(t, transfer.saved)
		assert.True(t, transfer.rolled)
	})
	t.Run("moves the quota", func(t *testing.T) {
		transfer := &fakeTransfer{balances: AffiliateBalances{Quota: 10, AffiliateQuota: 800}}
		require.NoError(t, TransferAffiliateQuota(500, transfer, display{}, common.QuotaFromFloat))
		assert.Equal(t, &AffiliateBalances{Quota: 510, AffiliateQuota: 300}, transfer.saved)
		assert.True(t, transfer.committed)
	})
}

type fakeLoader struct {
	err      error
	transfer error
	quota    *int
}

func (l *fakeLoader) LoadAffiliateAccount(int) (AffiliateAccountTransfer, error) { return l, l.err }
func (l *fakeLoader) TransferQuota(quota int) error                              { l.quota = &quota; return l.transfer }

type quotaInput struct {
	quota int
	err   error
}

func (i quotaInput) ReadQuota() (int, error) { return i.quota, i.err }

func TestTransferAffiliateQuotaStages(t *testing.T) {
	loadErr, inputErr := errors.New("user not found"), errors.New("invalid json")
	cases := []struct {
		name       string
		loader     *fakeLoader
		input      quotaInput
		err        error
		dispatched bool
	}{
		{name: "load failure", loader: &fakeLoader{err: loadErr}, input: quotaInput{quota: 5}, err: loadErr},
		{name: "input failure", loader: &fakeLoader{}, input: quotaInput{err: inputErr}, err: inputErr},
		{name: "rejected transfer", loader: &fakeLoader{transfer: domain.ErrInviteQuotaInsufficient}, input: quotaInput{quota: 5}, err: domain.ErrAffiliateTransferFailed, dispatched: true},
		{name: "transfer", loader: &fakeLoader{}, input: quotaInput{quota: 5}, dispatched: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewUsecase(tc.loader).Execute(7, tc.input)
			require.ErrorIs(t, err, tc.err)
			if tc.err != nil && tc.err != domain.ErrAffiliateTransferFailed {
				assert.NotErrorIs(t, err, domain.ErrAffiliateTransferFailed)
			}
			assert.Equal(t, tc.dispatched, tc.loader.quota != nil)
		})
	}
}
