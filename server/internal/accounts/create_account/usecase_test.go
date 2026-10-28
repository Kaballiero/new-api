package create_account

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/accounts/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var roles = domain.AdminRoles{Common: 1, Admin: 10, Root: 100}

type fakeOperation struct {
	insertErr, reloadErr error
	inserted             NewAccount
	steps                []string
}

func (op *fakeOperation) CreateTransaction(run func(Mutation) error) error { return run(op) }
func (op *fakeOperation) Insert(account NewAccount) (domain.AdminTarget, error) {
	op.inserted = account
	return domain.AdminTarget{ID: 9, Username: account.Username, Role: account.Role}, op.insertErr
}
func (op *fakeOperation) SetPermissions(int, map[string]map[string]bool) error {
	op.steps = append(op.steps, "permissions")
	return nil
}
func (op *fakeOperation) ClearAuthorization(int) error { return nil }
func (op *fakeOperation) ReloadPolicy() error {
	op.steps = append(op.steps, "reload")
	return op.reloadErr
}
func (op *fakeOperation) FinishInsert()                   { op.steps = append(op.steps, "finish") }
func (op *fakeOperation) AuditCreated(domain.AdminTarget) { op.steps = append(op.steps, "audit") }

func TestCreateAccount(t *testing.T) {
	storage := errors.New("duplicate key")
	cases := []struct {
		name      string
		op        *fakeOperation
		input     Input
		actorRole int
		err       error
		steps     []string
	}{
		{name: "same role", op: &fakeOperation{}, input: Input{Username: "ann", Role: roles.Admin}, actorRole: roles.Admin, err: domain.ErrCreateForbidden},
		{name: "insert failure stays unclassified", op: &fakeOperation{insertErr: storage}, input: Input{Username: "ann", Role: roles.Common}, actorRole: roles.Admin, err: storage},
		{name: "common user", op: &fakeOperation{}, input: Input{Username: "ann", Role: roles.Common}, actorRole: roles.Admin, steps: []string{"finish", "audit"}},
		{name: "admin with permissions", op: &fakeOperation{}, input: Input{Username: "ann", Role: roles.Admin, Permissions: map[string]map[string]bool{"user": {"read": true}}}, actorRole: roles.Root, steps: []string{"permissions", "reload", "finish", "audit"}},
		{name: "policy reload failure", op: &fakeOperation{reloadErr: storage}, input: Input{Username: "ann", Role: roles.Admin, Permissions: map[string]map[string]bool{}}, actorRole: roles.Root, err: storage, steps: []string{"permissions", "reload"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewUsecase(tc.op, roles).Execute(tc.input, tc.actorRole)
			require.ErrorIs(t, err, tc.err)
			if tc.err != nil {
				assert.NotErrorIs(t, err, domain.ErrInternal)
			}
			assert.Equal(t, tc.steps, tc.op.steps)
		})
	}
}

func TestCreateAccountDefaultsDisplayName(t *testing.T) {
	op := &fakeOperation{}
	require.NoError(t, NewUsecase(op, roles).Execute(Input{Username: "ann", Password: "secret"}, roles.Admin))
	assert.Equal(t, NewAccount{Username: "ann", Password: "secret", DisplayName: "ann"}, op.inserted)
}
