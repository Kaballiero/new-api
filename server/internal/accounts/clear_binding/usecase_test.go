package clear_binding

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/accounts/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOperation struct {
	role, actor       int
	loadErr, clearErr error
	cleared, audited  bool
}

func (op *fakeOperation) LoadTarget(int) (int, error) { return op.role, op.loadErr }
func (op *fakeOperation) ActorRole() int              { return op.actor }
func (op *fakeOperation) RootRole() int               { return 100 }
func (op *fakeOperation) Clear(string) error          { op.cleared = true; return op.clearErr }
func (op *fakeOperation) AuditCurrent(string)         { op.audited = true }

func TestClearBinding(t *testing.T) {
	missing, invalid := errors.New("record not found"), errors.New("invalid binding type")
	cases := []struct {
		name             string
		op               *fakeOperation
		err              error
		cleared, audited bool
	}{
		// A missing account is reported with its own text, not as a 404.
		{name: "missing", op: &fakeOperation{loadErr: missing, actor: 100}, err: missing},
		{name: "peer admin", op: &fakeOperation{role: 10, actor: 10}, err: domain.ErrBindingForbidden},
		{name: "unknown binding", op: &fakeOperation{role: 1, actor: 10, clearErr: invalid}, err: invalid, cleared: true},
		{name: "root clears root", op: &fakeOperation{role: 100, actor: 100}, cleared: true, audited: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewUsecase(tc.op).Execute(Input{ID: 3, BindingType: "github"})
			require.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.cleared, tc.op.cleared)
			assert.Equal(t, tc.audited, tc.op.audited)
		})
	}
}
