package redeem_code

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/redemption/domain"
)

type ledger struct {
	steps                       []string
	code                        domain.Code
	lookup, cas, credit, commit error
	changed                     bool
	failure                     error
	mutation                    domain.Redemption
}

func (l *ledger) Transaction(run func(tx Tx) error) error {
	l.steps = append(l.steps, "begin")
	if err := run(l); err != nil {
		l.steps = append(l.steps, "rollback")
		return err
	}
	l.steps = append(l.steps, "commit")
	return l.commit
}

func (l *ledger) FindLocked(key string) (domain.Code, error) {
	l.steps = append(l.steps, "lookup:"+key)
	return l.code, l.lookup
}

func (l *ledger) MarkUsed(in domain.Redemption) (bool, error) {
	l.steps = append(l.steps, "cas")
	l.mutation = in
	return l.changed, l.cas
}

func (l *ledger) CreditWallet(user, quota int) error {
	l.steps = append(l.steps, "credit")
	return l.credit
}

func (l *ledger) Record(user int, code domain.Code) {
	l.steps = append(l.steps, "receipt")
}

func TestRedeemOrderAndFailureBoundaries(t *testing.T) {
	storage := errors.New("storage failure")
	cases := []struct {
		name                        string
		input                       Input
		code                        domain.Code
		lookup, cas, credit, commit error
		changed                     bool
		wantError, wantCause        error
		steps                       []string
	}{
		{name: "empty key before user", input: Input{}, wantError: domain.ErrMissingKey, steps: []string{}},
		{name: "zero user before wait", input: Input{Key: "key"}, wantError: domain.ErrUserID, steps: []string{}},
		{name: "lookup error hidden", input: Input{"key", 7}, lookup: storage, wantError: domain.ErrRedeemFailed,
			wantCause: domain.ErrInvalidCode, steps: []string{"wait", "begin", "lookup:key", "rollback", "failure"}},
		{name: "status before expiry", input: Input{"key", 7}, code: domain.Code{Status: domain.StatusDisabled, ExpiredTime: 99},
			wantError: domain.ErrRedeemFailed, wantCause: domain.ErrUsed, steps: []string{"wait", "begin", "lookup:key", "rollback", "failure"}},
		{name: "expiry before CAS", input: Input{"key", 7}, code: domain.Code{Status: domain.StatusEnabled, ExpiredTime: 99},
			wantError: domain.ErrRedeemFailed, wantCause: domain.ErrExpired, steps: []string{"wait", "begin", "lookup:key", "rollback", "failure"}},
		{name: "CAS zero before credit", input: Input{"key", 7}, code: domain.Code{ID: 11, Status: domain.StatusEnabled, Quota: 500},
			wantError: domain.ErrRedeemFailed, wantCause: domain.ErrUsed, steps: []string{"wait", "begin", "lookup:key", "cas", "rollback", "failure"}},
		{name: "CAS error before credit", input: Input{"key", 7}, code: domain.Code{Status: domain.StatusEnabled}, cas: storage,
			wantError: domain.ErrRedeemFailed, wantCause: storage, steps: []string{"wait", "begin", "lookup:key", "cas", "rollback", "failure"}},
		{name: "credit rollback", input: Input{"key", 7}, code: domain.Code{Status: domain.StatusEnabled}, changed: true, credit: storage,
			wantError: domain.ErrRedeemFailed, wantCause: storage, steps: []string{"wait", "begin", "lookup:key", "cas", "credit", "rollback", "failure"}},
		{name: "commit failure no cache or receipt", input: Input{"key", 7}, code: domain.Code{Status: domain.StatusEnabled}, changed: true,
			commit: storage, wantError: domain.ErrRedeemFailed, wantCause: storage,
			steps: []string{"wait", "begin", "lookup:key", "cas", "credit", "commit", "failure"}},
		{name: "expiry equality succeeds", input: Input{"key", 7}, code: domain.Code{ID: 11, Status: domain.StatusEnabled, Quota: 500, ExpiredTime: 100},
			changed: true, steps: []string{"wait", "begin", "lookup:key", "cas", "credit", "commit", "cache", "receipt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &ledger{steps: []string{}, code: tc.code, lookup: tc.lookup, cas: tc.cas, credit: tc.credit, commit: tc.commit, changed: tc.changed}
			u := NewUsecase(l, l, func(user, quota int) {
				require.Equal(t, 7, user)
				require.Equal(t, 500, quota)
				l.steps = append(l.steps, "cache")
			},
				func(err error) { l.failure = err; l.steps = append(l.steps, "failure") },
				Timing{Now: func() int64 { return 100 }, Wait: func() { l.steps = append(l.steps, "wait") }})
			quota, err := u.Execute(tc.input)
			if tc.wantError != nil {
				require.ErrorIs(t, err, tc.wantError)
				assert.Zero(t, quota)
			} else {
				require.NoError(t, err)
				assert.Equal(t, 500, quota)
				assert.Equal(t, domain.Redemption{ID: 11, UserID: 7, FromStatus: domain.StatusEnabled, ToStatus: domain.StatusUsed, RedeemedTime: 100},
					l.mutation)
			}
			if tc.wantCause != nil {
				assert.ErrorIs(t, l.failure, tc.wantCause)
			} else {
				assert.NoError(t, l.failure)
			}
			assert.Equal(t, tc.steps, l.steps)
		})
	}
}
