package admin_reset_passkey

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOperation struct {
	role                int
	actor               int
	loadErr, credential error
	deleted             []int
	revokeReason        string
}

func (f *fakeOperation) LoadTarget(id int) (int, string, int, error) {
	return id, "alice", f.role, f.loadErr
}
func (f *fakeOperation) ActorRole() int              { return f.actor }
func (f *fakeOperation) RootRole() int               { return 100 }
func (f *fakeOperation) RequireCredential(int) error { return f.credential }
func (f *fakeOperation) DeleteCredential(id int) error {
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeOperation) RevokeAll(_ int, reason string) error {
	f.revokeReason = reason
	return nil
}

func TestExecuteRejectsWithoutDeleting(t *testing.T) {
	storage := errors.New("storage")
	cases := []struct {
		name string
		op   fakeOperation
		want error
	}{
		{"load failure", fakeOperation{loadErr: storage, actor: 100}, storage},
		{"higher role", fakeOperation{role: 100, actor: 10}, domain.ErrTargetForbidden},
		{"no passkey", fakeOperation{role: 1, actor: 10, credential: domain.ErrPasskeyNotFound}, domain.ErrPasskeyMissing},
		{"credential failure", fakeOperation{role: 1, actor: 10, credential: storage}, storage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			_, err := NewUsecase(&op).Execute(5)
			assert.ErrorIs(t, err, tc.want)
			assert.Empty(t, op.deleted)
		})
	}
}

func TestExecuteResetsPasskey(t *testing.T) {
	op := fakeOperation{role: 1, actor: 10}
	output, err := NewUsecase(&op).Execute(5)
	require.NoError(t, err)
	assert.Equal(t, Output{ID: 5, Username: "alice"}, output)
	assert.Equal(t, []int{5}, op.deleted)
	assert.Equal(t, "admin_passkey_reset", op.revokeReason)
}
