package initialize_authorization

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/authorization/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	roles     []string
	reset     []string
	upsertErr error
}

func (s *fakeStore) UpsertRole(spec domain.RoleSpec) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.roles = append(s.roles, spec.Key)
	return nil
}
func (s *fakeStore) ResetRolePolicies(subjects []string) error {
	s.reset = subjects
	return nil
}

type fakePolicy struct{ added [][]string }

func (p *fakePolicy) AddPolicy(subject, resource, action, effect string) (bool, error) {
	p.added = append(p.added, []string{subject, resource, action, effect})
	return true, nil
}

type fakeEngine struct {
	published bool
	policy    *fakePolicy
}

func (e *fakeEngine) CreateAndPublish() error {
	e.published = true
	return nil
}
func (e *fakeEngine) CurrentPolicy() Policy {
	if e.policy == nil {
		return nil
	}
	return e.policy
}

func catalog() domain.Catalog {
	return domain.Catalog{Resources: domain.BuiltInResources(), BuiltInRoles: domain.BuiltInRoles()}
}

// Mirrors app/authorizationcore TestInitSeedsBuiltInRolesAndPoliciesOnce and
// TestInitOnSlaveOnlyLoadsPolicies: the master seeds roles and the admin
// baseline (root is implicit), a follower only loads the policy.
func TestInitializeAuthorization(t *testing.T) {
	store, engine := &fakeStore{}, &fakeEngine{policy: &fakePolicy{}}
	require.NoError(t, NewUsecase(catalog, func() bool { return true }, store, engine).Execute())
	assert.Equal(t, []string{domain.BuiltInRoleRoot, domain.BuiltInRoleAdmin}, store.roles)
	assert.Equal(t, []string{domain.RoleSubject(domain.BuiltInRoleRoot), domain.RoleSubject(domain.BuiltInRoleAdmin)}, store.reset)
	assert.True(t, engine.published)
	require.Len(t, engine.policy.added, len(catalog().PermissionsForRole(domain.BuiltInRoleAdmin)))
	for _, rule := range engine.policy.added {
		assert.Equal(t, domain.RoleSubject(domain.BuiltInRoleAdmin), rule[0])
		assert.Equal(t, domain.EffectAllow, rule[3])
	}

	store, engine = &fakeStore{}, &fakeEngine{policy: &fakePolicy{}}
	require.NoError(t, NewUsecase(catalog, func() bool { return false }, store, engine).Execute())
	assert.Empty(t, store.roles)
	assert.Nil(t, store.reset)
	assert.True(t, engine.published)
	assert.Empty(t, engine.policy.added)
}

func TestInitializeAuthorizationErrors(t *testing.T) {
	err := NewUsecase(catalog, func() bool { return true }, &fakeStore{}, &fakeEngine{}).Execute()
	require.ErrorIs(t, err, domain.ErrEnforcerNotInitialized)

	cause := errors.New("roles table unavailable")
	engine := &fakeEngine{policy: &fakePolicy{}}
	err = NewUsecase(catalog, func() bool { return true }, &fakeStore{upsertErr: cause}, engine).Execute()
	require.ErrorIs(t, err, domain.ErrInternal)
	require.ErrorIs(t, err, cause)
	assert.EqualError(t, err, "roles table unavailable")
	assert.False(t, engine.published)
}
