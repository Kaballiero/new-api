package update_account

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/accounts/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var roles = domain.AdminRoles{Guest: 0, Common: 1, Admin: 10, Root: 100}

type fakeOperation struct {
	origin                   domain.AdminTarget
	loadErr, editErr         error
	reloadErr, revokeErr     error
	nextAuthVersion          int64
	steps                    []string
	permissionsSet, cleared  bool
	reloadAccountErr         error
	requested, reloadAccount *domain.User
}

func (op *fakeOperation) LoadOrigin(int) (domain.AdminTarget, error) { return op.origin, op.loadErr }
func (op *fakeOperation) UseOriginRole(int)                          {}
func (op *fakeOperation) EditTransaction(run func(Mutation) error) error {
	if op.editErr != nil {
		return op.editErr
	}
	return run(op)
}
func (op *fakeOperation) EnsureUsernameAvailable(string, int) error { return nil }
func (op *fakeOperation) Edit(bool) (int64, error)                  { return op.nextAuthVersion, nil }
func (op *fakeOperation) SetPermissions(int, map[string]map[string]bool) error {
	op.permissionsSet = true
	return nil
}
func (op *fakeOperation) ClearAuthorization(int) error { op.cleared = true; return nil }
func (op *fakeOperation) ReloadPolicy() error {
	op.steps = append(op.steps, "reload")
	return op.reloadErr
}
func (op *fakeOperation) RevokeSessions(int, string) error {
	op.steps = append(op.steps, "revoke")
	return op.revokeErr
}
func (op *fakeOperation) PublishAuthCache(int) error {
	op.steps = append(op.steps, "publish")
	return nil
}
func (op *fakeOperation) AuditUpdated(domain.AdminTarget) {
	op.steps = append(op.steps, "audit")
}
func (op *fakeOperation) ReloadAccount(int) (*domain.User, error) {
	return op.reloadAccount, op.reloadAccountErr
}
func (op *fakeOperation) RequestedAccount() *domain.User { return op.requested }

func TestUpdateAccountRejections(t *testing.T) {
	storage := errors.New("connection reset")
	cases := []struct {
		name      string
		op        *fakeOperation
		input     Input
		actorRole int
		err       error
	}{
		{name: "missing", op: &fakeOperation{loadErr: domain.ErrAccountNotFound}, input: Input{ID: 2}, actorRole: roles.Root, err: domain.ErrAccountNotFound},
		{name: "role change", op: &fakeOperation{origin: domain.AdminTarget{ID: 2, Role: roles.Common}}, input: Input{ID: 2, RequestedRole: roles.Admin}, actorRole: roles.Root, err: domain.ErrRoleChangeNotAllowed},
		{name: "peer admin", op: &fakeOperation{origin: domain.AdminTarget{ID: 2, Role: roles.Admin}}, input: Input{ID: 2}, actorRole: roles.Admin, err: domain.ErrManageForbidden},
		{name: "username taken", op: &fakeOperation{origin: domain.AdminTarget{ID: 2, Role: roles.Common}, editErr: domain.ErrUsernameTaken}, input: Input{ID: 2}, actorRole: roles.Root, err: domain.ErrUsernameTaken},
		{name: "write failure", op: &fakeOperation{origin: domain.AdminTarget{ID: 2, Role: roles.Common}, editErr: storage}, input: Input{ID: 2}, actorRole: roles.Root, err: storage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewUsecase(tc.op, roles).Execute(tc.input, tc.actorRole)
			require.ErrorIs(t, err, tc.err)
			assert.NotContains(t, tc.op.steps, "audit")
		})
	}
}

func TestUpdateAccountEffects(t *testing.T) {
	origin := domain.AdminTarget{ID: 2, Role: roles.Common, AuthVersion: 4}
	t.Run("advanced auth version revokes sessions", func(t *testing.T) {
		op := &fakeOperation{origin: origin, nextAuthVersion: 5, reloadAccount: &domain.User{Id: 2, Password: "hash"}}
		user, err := NewUsecase(op, roles).Execute(Input{ID: 2, Permissions: map[string]map[string]bool{}}, roles.Root)
		require.NoError(t, err)
		assert.Equal(t, []string{"reload", "revoke", "publish", "audit"}, op.steps)
		assert.True(t, op.cleared, "permissions of a common user are cleared")
		assert.Empty(t, user.Password)
	})
	t.Run("unchanged auth version keeps sessions and falls back to the request", func(t *testing.T) {
		op := &fakeOperation{origin: origin, nextAuthVersion: 4, reloadAccountErr: errors.New("gone"), requested: &domain.User{Id: 2, Username: "bob", Password: "plain"}}
		user, err := NewUsecase(op, roles).Execute(Input{ID: 2}, roles.Admin)
		require.NoError(t, err)
		assert.Equal(t, []string{"publish", "audit"}, op.steps)
		assert.Equal(t, "bob", user.Username)
		assert.Empty(t, user.Password)
	})
	t.Run("post-commit failure is unclassified", func(t *testing.T) {
		revoke := errors.New("redis down")
		op := &fakeOperation{origin: origin, nextAuthVersion: 5, revokeErr: revoke}
		_, err := NewUsecase(op, roles).Execute(Input{ID: 2}, roles.Root)
		require.ErrorIs(t, err, revoke)
		assert.NotErrorIs(t, err, domain.ErrInternal)
		assert.NotContains(t, op.steps, "audit")
	})
}
