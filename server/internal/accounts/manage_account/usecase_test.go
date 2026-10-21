package manage_account

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var roles = domain.AdminRoles{Guest: 0, Common: 1, Admin: 10, Root: 100, Enabled: 1, Disabled: 2}

type fakeOperation struct {
	target                 domain.AdminTarget
	loadErr                error
	updateErr, deleteErr   error
	demoteErr, reloadErr   error
	steps                  []string
	audited                string
	role, status           int
	invalidateTokensFailed bool
}

func (op *fakeOperation) LoadUnscoped(int) (domain.AdminTarget, error) { return op.target, op.loadErr }
func (op *fakeOperation) UseRole(role int)                             { op.role = role }
func (op *fakeOperation) UseStatus(status int)                         { op.status = status }
func (op *fakeOperation) Update() (domain.AdminTarget, error) {
	op.steps = append(op.steps, "update")
	target := op.target
	if op.role != 0 {
		target.Role = op.role
	}
	if op.status != 0 {
		target.Status = op.status
	}
	return target, op.updateErr
}
func (op *fakeOperation) DemoteTransaction(run func(DemoteMutation) error) error {
	op.steps = append(op.steps, "demote")
	if op.demoteErr != nil {
		return op.demoteErr
	}
	return run(op)
}
func (op *fakeOperation) ClearAuthorization(int) error {
	op.steps = append(op.steps, "clear")
	return nil
}
func (op *fakeOperation) ReloadPolicy() error {
	op.steps = append(op.steps, "reload")
	return op.reloadErr
}
func (op *fakeOperation) PublishAuthCache(int) error {
	op.steps = append(op.steps, "publish")
	return nil
}
func (op *fakeOperation) RevokeSessions(int, string) error {
	op.steps = append(op.steps, "revoke")
	return nil
}
func (op *fakeOperation) Delete() error { op.steps = append(op.steps, "delete"); return op.deleteErr }
func (op *fakeOperation) InvalidateTokens(int) error {
	op.steps = append(op.steps, "invalidate")
	if op.invalidateTokensFailed {
		return errors.New("cache down")
	}
	return nil
}
func (op *fakeOperation) LogTokenInvalidationFailure(int, error) { op.steps = append(op.steps, "log") }
func (op *fakeOperation) AuditManaged(_ domain.AdminTarget, action string) {
	op.audited = action
}

func TestManageAccountRejections(t *testing.T) {
	storage := errors.New("connection reset")
	cases := []struct {
		name      string
		target    domain.AdminTarget
		loadErr   error
		actorRole int
		action    string
		err       error
	}{
		{name: "missing", loadErr: domain.ErrAccountNotFound, actorRole: roles.Root, action: "disable", err: domain.ErrAccountNotFound},
		{name: "load failure", loadErr: domain.Internal(storage), actorRole: roles.Root, action: "disable", err: storage},
		{name: "peer admin", target: domain.AdminTarget{ID: 2, Role: roles.Admin}, actorRole: roles.Admin, action: "disable", err: domain.ErrManageForbidden},
		{name: "disable root", target: domain.AdminTarget{ID: 1, Role: roles.Root}, actorRole: roles.Root, action: "disable", err: domain.ErrCannotDisableRoot},
		{name: "delete root", target: domain.AdminTarget{ID: 1, Role: roles.Root}, actorRole: roles.Root, action: "delete", err: domain.ErrCannotDeleteRoot},
		{name: "admin promotes", target: domain.AdminTarget{ID: 2, Role: roles.Common}, actorRole: roles.Admin, action: "promote", err: domain.ErrAdminCannotPromote},
		{name: "already admin", target: domain.AdminTarget{ID: 2, Role: roles.Admin}, actorRole: roles.Root, action: "promote", err: domain.ErrAlreadyAdmin},
		{name: "demote root", target: domain.AdminTarget{ID: 1, Role: roles.Root}, actorRole: roles.Root, action: "demote", err: domain.ErrCannotDemoteRoot},
		{name: "already common", target: domain.AdminTarget{ID: 2, Role: roles.Common}, actorRole: roles.Root, action: "demote", err: domain.ErrAlreadyCommon},
		{name: "unknown action", target: domain.AdminTarget{ID: 2, Role: roles.Common}, actorRole: roles.Root, action: "archive", err: domain.ErrInvalidAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := &fakeOperation{target: tc.target, loadErr: tc.loadErr}
			_, err := NewUsecase(op, roles).Execute(Input{ID: tc.target.ID, Action: tc.action}, tc.actorRole)
			require.ErrorIs(t, err, tc.err)
			assert.Empty(t, op.steps)
			assert.Empty(t, op.audited)
		})
	}
}

func TestManageAccountWrites(t *testing.T) {
	storage := errors.New("connection reset")
	common := domain.AdminTarget{ID: 2, Username: "bob", Role: roles.Common, Status: roles.Enabled}
	admin := domain.AdminTarget{ID: 3, Username: "ann", Role: roles.Admin, Status: roles.Enabled}

	t.Run("disable invalidates tokens and audits", func(t *testing.T) {
		op := &fakeOperation{target: common, invalidateTokensFailed: true}
		result, err := NewUsecase(op, roles).Execute(Input{ID: 2, Action: "disable"}, roles.Admin)
		require.NoError(t, err)
		assert.Equal(t, Result{Action: "disable", UserID: 2, Role: roles.Common, Status: roles.Disabled}, result)
		assert.Equal(t, []string{"update", "invalidate", "log"}, op.steps)
		assert.Equal(t, "disable", op.audited)
	})
	t.Run("ordinary update failure is internal", func(t *testing.T) {
		op := &fakeOperation{target: common, updateErr: storage}
		_, err := NewUsecase(op, roles).Execute(Input{ID: 2, Action: "enable"}, roles.Admin)
		require.ErrorIs(t, err, domain.ErrInternal)
		assert.EqualError(t, err, "connection reset")
		assert.Empty(t, op.audited)
	})
	t.Run("delete reports deletion", func(t *testing.T) {
		op := &fakeOperation{target: common}
		result, err := NewUsecase(op, roles).Execute(Input{ID: 2, Action: "delete"}, roles.Admin)
		require.NoError(t, err)
		assert.Equal(t, Result{Action: "delete", UserID: 2, Deleted: true}, result)
		assert.Equal(t, []string{"delete", "invalidate"}, op.steps)
	})
	t.Run("delete failure is internal", func(t *testing.T) {
		op := &fakeOperation{target: common, deleteErr: storage}
		_, err := NewUsecase(op, roles).Execute(Input{ID: 2, Action: "delete"}, roles.Admin)
		require.ErrorIs(t, err, domain.ErrInternal)
		assert.Equal(t, []string{"delete"}, op.steps)
	})
	t.Run("demote runs post-commit effects in order", func(t *testing.T) {
		op := &fakeOperation{target: admin}
		result, err := NewUsecase(op, roles).Execute(Input{ID: 3, Action: "demote"}, roles.Root)
		require.NoError(t, err)
		assert.Equal(t, roles.Common, result.Role)
		assert.Equal(t, []string{"demote", "update", "clear", "reload", "publish", "revoke", "invalidate"}, op.steps)
	})
	t.Run("demote failures stay unclassified", func(t *testing.T) {
		for _, op := range []*fakeOperation{{target: admin, demoteErr: storage}, {target: admin, reloadErr: storage}} {
			_, err := NewUsecase(op, roles).Execute(Input{ID: 3, Action: "demote"}, roles.Root)
			require.ErrorIs(t, err, storage)
			assert.NotErrorIs(t, err, domain.ErrInternal)
			assert.Empty(t, op.audited)
		}
	})
}
