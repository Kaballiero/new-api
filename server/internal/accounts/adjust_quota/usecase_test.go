package adjust_quota

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	account                   QuotaAccount
	lockErr, updateErr, txErr error
	written                   *int
	increments                []int64
	cacheErr                  error
	cacheFailures             int
}

func (s *fakeStore) Transaction(run func(QuotaAdjustmentTx) error) error {
	if err := run(s); err != nil {
		return err
	}
	return s.txErr
}
func (s *fakeStore) LockAccount(int) (QuotaAccount, error) { return s.account, s.lockErr }
func (s *fakeStore) UpdateQuota(_ int, after int) error {
	s.written = &after
	return s.updateErr
}
func (s *fakeStore) Increment(_ int, delta int64) error {
	s.increments = append(s.increments, delta)
	return s.cacheErr
}
func (s *fakeStore) AdjustmentCacheFailed(int, error) { s.cacheFailures++ }

func TestAdjustQuotaRejections(t *testing.T) {
	storage := errors.New("deadlock")
	account := QuotaAccount{ID: 7, Username: "bob", Role: 1, Quota: 100}
	cases := []struct {
		name         string
		store        *fakeStore
		userID, role int
		mode         string
		value        int
		err          error
	}{
		{name: "zero add", store: &fakeStore{}, userID: 7, role: 100, mode: "add", value: 0, err: domain.ErrQuotaChangeZero},
		// The zero response wins even when the account id is invalid as well.
		{name: "zero subtract without id", store: &fakeStore{}, userID: 0, role: 100, mode: "subtract", value: -1, err: domain.ErrQuotaChangeZero},
		{name: "missing id", store: &fakeStore{}, userID: 0, role: 100, mode: "add", value: 5, err: domain.ErrQuotaAdjustmentInvalid},
		{name: "unknown mode", store: &fakeStore{}, userID: 7, role: 100, mode: "multiply", value: 0, err: domain.ErrQuotaAdjustmentInvalid},
		{name: "oversized value", store: &fakeStore{}, userID: 7, role: 100, mode: "override", value: common.MaxWalletQuota + 1, err: domain.ErrQuotaLimitExceeded},
		{name: "missing account", store: &fakeStore{lockErr: domain.ErrAccountNotFound}, userID: 7, role: 100, mode: "add", value: 5, err: domain.ErrAccountNotFound},
		{name: "peer role", store: &fakeStore{account: QuotaAccount{ID: 7, Role: 10}}, userID: 7, role: 10, mode: "add", value: 5, err: domain.ErrManageForbidden},
		{name: "overflow", store: &fakeStore{account: QuotaAccount{ID: 7, Role: 1, Quota: common.MaxWalletQuota}}, userID: 7, role: 100, mode: "add", value: 1, err: domain.ErrQuotaLimitExceeded},
		{name: "lock failure", store: &fakeStore{lockErr: storage}, userID: 7, role: 100, mode: "add", value: 5, err: domain.ErrInternal},
		{name: "update failure", store: &fakeStore{account: account, updateErr: storage}, userID: 7, role: 100, mode: "add", value: 5, err: domain.ErrInternal},
		{name: "commit failure", store: &fakeStore{account: account, txErr: storage}, userID: 7, role: 100, mode: "add", value: 5, err: domain.ErrInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adjustment, err := NewUsecase(tc.store, tc.store, tc.store, 100).Execute(tc.userID, tc.role, tc.mode, tc.value)
			require.ErrorIs(t, err, tc.err)
			assert.Nil(t, adjustment)
			assert.Empty(t, tc.store.increments)
		})
	}
}

func TestAdjustQuotaAppliesCommittedDifference(t *testing.T) {
	cases := []struct {
		mode        string
		value       int
		after       int
		written     bool
		increments  []int64
		cacheFailed bool
	}{
		{mode: "add", value: 50, after: 150, written: true, increments: []int64{50}},
		{mode: "subtract", value: 30, after: 70, written: true, increments: []int64{-30}},
		{mode: "override", value: 100, after: 100},
		{mode: "override", value: -20, after: -20, written: true, increments: []int64{-120}, cacheFailed: true},
	}
	for _, tc := range cases {
		store := &fakeStore{account: QuotaAccount{ID: 7, Username: "bob", Role: 1, Status: 1, Quota: 100}}
		if tc.cacheFailed {
			store.cacheErr = errors.New("redis down")
		}
		adjustment, err := NewUsecase(store, store, store, 100).Execute(7, 10, tc.mode, tc.value)
		require.NoError(t, err)
		assert.Equal(t, &UserQuotaAdjustment{UserID: 7, Username: "bob", Role: 1, Status: 1, Before: 100, After: tc.after}, adjustment)
		assert.Equal(t, tc.written, store.written != nil)
		assert.Equal(t, tc.increments, store.increments)
		assert.Equal(t, tc.cacheFailed, store.cacheFailures == 1)
	}
}
