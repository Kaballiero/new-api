package admin_disable_twofa

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/stretchr/testify/assert"
)

type fakeOperation struct {
	role              int
	loadErr, disable  error
	revokeErr         error
	actor             int
	disabled, revoked []int
	revokeReason      string
}

func (f *fakeOperation) LoadTarget(int) (int, error) { return f.role, f.loadErr }
func (f *fakeOperation) ActorRole() int              { return f.actor }
func (f *fakeOperation) RootRole() int               { return 100 }
func (f *fakeOperation) Disable(id int) error {
	f.disabled = append(f.disabled, id)
	return f.disable
}
func (f *fakeOperation) RevokeAll(id int, reason string) error {
	f.revoked, f.revokeReason = append(f.revoked, id), reason
	return f.revokeErr
}

func TestExecute(t *testing.T) {
	storage := errors.New("storage")
	cases := []struct {
		name    string
		op      fakeOperation
		want    error
		revoked bool
	}{
		{"missing user", fakeOperation{loadErr: domain.ErrTargetNotFound, actor: 100}, domain.ErrTargetNotFound, false},
		{"load failure", fakeOperation{loadErr: storage, actor: 100}, storage, false},
		{"same role", fakeOperation{role: 10, actor: 10}, domain.ErrTargetForbidden, false},
		{"not enabled", fakeOperation{role: 1, actor: 10, disable: domain.ErrTwoFANotEnabled}, domain.ErrTargetTwoFADisabled, false},
		{"disable failure", fakeOperation{role: 1, actor: 10, disable: storage}, storage, false},
		{"revoke failure", fakeOperation{role: 1, actor: 10, revokeErr: storage}, storage, true},
		{"disabled", fakeOperation{role: 1, actor: 10}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			err := NewUsecase(&op).Execute(7)
			if tc.want == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.want)
			}
			if tc.revoked {
				assert.Equal(t, []int{7}, op.revoked)
				assert.Equal(t, "admin_twofa_disabled", op.revokeReason)
			} else {
				assert.Empty(t, op.revoked)
			}
		})
	}
}
