package delete_account

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOperation struct {
	target           domain.AdminTarget
	loadErr, hardErr error
	deleted, audited bool
}

func (op *fakeOperation) LoadOrigin(int) (domain.AdminTarget, error) { return op.target, op.loadErr }
func (op *fakeOperation) HardDelete(int) error                       { op.deleted = true; return op.hardErr }
func (op *fakeOperation) AuditDeleted(domain.AdminTarget)            { op.audited = true }

func TestDeleteAccount(t *testing.T) {
	storage := errors.New("unresolved financial operations")
	cases := []struct {
		name             string
		op               *fakeOperation
		actorRole        int
		err              error
		deleted, audited bool
	}{
		{name: "missing", op: &fakeOperation{loadErr: domain.ErrAccountNotFound}, actorRole: 100, err: domain.ErrAccountNotFound},
		// Unlike other admin scenarios, root cannot delete another root.
		{name: "root deletes root", op: &fakeOperation{target: domain.AdminTarget{ID: 1, Role: 100}}, actorRole: 100, err: domain.ErrManageForbidden},
		{name: "hard delete failure is internal", op: &fakeOperation{target: domain.AdminTarget{ID: 2, Role: 1}, hardErr: storage}, actorRole: 10, err: domain.ErrInternal, deleted: true},
		{name: "delete", op: &fakeOperation{target: domain.AdminTarget{ID: 2, Role: 1}}, actorRole: 10, deleted: true, audited: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewUsecase(tc.op).Execute(Input{ID: tc.op.target.ID, ActorRole: tc.actorRole})
			require.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.deleted, tc.op.deleted)
			assert.Equal(t, tc.audited, tc.op.audited)
		})
	}
}
