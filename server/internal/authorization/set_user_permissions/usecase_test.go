package set_user_permissions

import (
	"errors"
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/internal/authorization/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePolicy keeps casbin rules as [subject, resource, action, effect].
type fakePolicy struct {
	rules     [][]string
	removeErr error
}

func (p *fakePolicy) GetFilteredPolicy(index int, values ...string) ([][]string, error) {
	var result [][]string
	for _, rule := range p.rules {
		if slices.Equal(rule[index:index+len(values)], values) {
			result = append(result, rule)
		}
	}
	return result, nil
}
func (p *fakePolicy) AddPolicy(subject, resource, action, effect string) (bool, error) {
	p.rules = append(p.rules, []string{subject, resource, action, effect})
	return true, nil
}
func (p *fakePolicy) RemoveFilteredPolicy(index int, values ...string) (bool, error) {
	if p.removeErr != nil {
		return false, p.removeErr
	}
	p.rules = slices.DeleteFunc(p.rules, func(rule []string) bool { return slices.Equal(rule[index:index+len(values)], values) })
	return true, nil
}

func catalog() domain.Catalog {
	return domain.Catalog{Resources: domain.BuiltInResources(), BuiltInRoles: domain.BuiltInRoles()}
}

// Mirrors app/authorizationcore TestSetUserPermissionsStoresOnlyOverrides: only values
// that differ from the admin baseline are stored, previous overrides of the
// resource are replaced and unknown resources are ignored.
func TestSetUserPermissionsStoresOnlyOverrides(t *testing.T) {
	admin := domain.RoleSubject(domain.BuiltInRoleAdmin)
	user := domain.UserSubject(7)
	policy := &fakePolicy{rules: [][]string{
		{admin, domain.ResourceChannel, domain.ActionRead, domain.EffectAllow},
		{user, domain.ResourceChannel, domain.ActionWrite, domain.EffectAllow},
	}}
	err := NewUsecase(catalog, func() Policy { return policy }).Execute(7, domain.PermissionsMap{
		domain.ResourceChannel: {domain.ActionRead: true, domain.ActionOperate: true, domain.ActionSecretView: false},
		"unknown":              {domain.ActionRead: true},
	})
	require.NoError(t, err)
	assert.Equal(t, [][]string{
		{admin, domain.ResourceChannel, domain.ActionRead, domain.EffectAllow},
		{user, domain.ResourceChannel, domain.ActionOperate, domain.EffectAllow},
	}, policy.rules)
}

func TestSetUserPermissionsErrors(t *testing.T) {
	err := NewUsecase(catalog, func() Policy { return nil }).Execute(7, domain.PermissionsMap{})
	require.ErrorIs(t, err, domain.ErrEnforcerNotInitialized)
	assert.EqualError(t, err, "authz enforcer is not initialized")

	cause := errors.New("policy store unavailable")
	policy := &fakePolicy{removeErr: cause}
	err = NewUsecase(catalog, func() Policy { return policy }).Execute(7, domain.PermissionsMap{domain.ResourceChannel: {domain.ActionRead: true}})
	require.ErrorIs(t, err, domain.ErrInternal)
	require.ErrorIs(t, err, cause)
	assert.EqualError(t, err, "policy store unavailable")
	assert.Empty(t, policy.rules)
}
