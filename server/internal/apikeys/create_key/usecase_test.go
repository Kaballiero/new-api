package create_key

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/apikeys/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	count     int64
	countErr  error
	insertErr error
	inserted  *domain.Token
}

func (s *fakeStore) Count(int) (int64, error) { return s.count, s.countErr }
func (s *fakeStore) Insert(t *domain.Token) error {
	if s.insertErr != nil {
		return s.insertErr
	}
	t.Id = 41
	s.inserted = t
	return nil
}
func (s *fakeStore) SetAutoGroups(t *domain.Token, groups []string) error {
	t.AutoGroups = ""
	if len(groups) > 0 {
		t.AutoGroups = `["` + groups[0] + `"]`
	}
	return nil
}

type run struct {
	usecase   Usecase
	store     *fakeStore
	params    map[string]any
	succeeded bool
}

func newRun(store *fakeStore, generate func() (string, error)) *run {
	r := &run{store: store, params: map[string]any{}}
	groups := domain.AutoGroupRules{
		Max:        func() int { return 2 },
		UserGroup:  func() (string, error) { return "default", nil },
		Selectable: func(_, group string) bool { return group != "vip" },
		Encode:     store.SetAutoGroups,
	}
	r.usecase = NewUsecase(store, groups, func() int { return 1000 }, func() int { return 3 }, func() int64 { return 1700000000 }, generate, func(string) {}, func() map[string]any { return r.params }, func() { r.succeeded = true })
	return r
}

func generateKey() (string, error) { return "generated-key", nil }

func TestCreateKeyStoresCleanTokenAndAudits(t *testing.T) {
	r := newRun(&fakeStore{}, generateKey)
	token, err := r.usecase.Execute(Input{UserID: func() int { return 7 }, Token: domain.Token{Id: 99, Name: "created", Key: "client-key", ExpiredTime: -1, UnlimitedQuota: true, Group: "default", CrossGroupRetry: true, UsedQuota: 5}})
	require.NoError(t, err)
	assert.Equal(t, 41, token.Id)
	assert.Equal(t, 7, token.UserId)
	assert.Equal(t, "generated-key", token.Key, "client key and id are ignored")
	assert.Zero(t, token.UsedQuota)
	assert.False(t, token.CrossGroupRetry, "cross group retry only applies to the auto group")
	assert.Equal(t, int64(1700000000), token.CreatedTime)
	assert.Equal(t, map[string]any{"name": "created", "id": 41}, r.params)
	assert.True(t, r.succeeded)
}

func TestCreateKeyRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		store  fakeStore
		token  domain.Token
		groups []string
		err    error
		max    int
		params map[string]any
	}{
		{name: "long name", token: domain.Token{Name: string(make([]byte, 51))}, err: domain.ErrNameTooLong, params: map[string]any{}},
		{name: "negative quota", token: domain.Token{Name: "attempt", RemainQuota: -1}, err: domain.ErrQuotaNegative, params: map[string]any{"name": "attempt"}},
		{name: "quota above maximum", token: domain.Token{Name: "attempt", RemainQuota: 1001}, err: domain.ErrQuotaExceedMax, max: 1000, params: map[string]any{"name": "attempt"}},
		{name: "count limit", store: fakeStore{count: 3}, token: domain.Token{Name: "attempt", UnlimitedQuota: true}, err: domain.ErrKeyCountLimit, max: 3, params: map[string]any{"name": "attempt"}},
		{name: "too many auto groups", token: domain.Token{Name: "attempt", UnlimitedQuota: true, Group: "auto"}, groups: []string{"a", "b", "c"}, err: domain.ErrAutoGroupsTooMany, max: 2, params: map[string]any{"name": "attempt"}},
		{name: "duplicate auto group", token: domain.Token{Name: "attempt", UnlimitedQuota: true, Group: "auto"}, groups: []string{"a", "a"}, err: domain.ErrAutoGroupDuplicate, params: map[string]any{"name": "attempt"}},
		{name: "unselectable auto group", token: domain.Token{Name: "attempt", UnlimitedQuota: true, Group: "auto"}, groups: []string{"vip"}, err: domain.ErrAutoGroupInvalid, params: map[string]any{"name": "attempt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRun(&tc.store, generateKey)
			_, err := r.usecase.Execute(Input{UserID: func() int { return 7 }, Token: tc.token, AutoGroups: tc.groups, AutoGroupsSet: tc.groups != nil})
			require.ErrorIs(t, err, tc.err)
			if tc.max != 0 {
				var limit *domain.LimitError
				require.ErrorAs(t, err, &limit)
				assert.Equal(t, tc.max, limit.Max)
			}
			assert.Equal(t, tc.params, r.params)
			assert.False(t, r.succeeded)
			assert.Nil(t, tc.store.inserted)
		})
	}
}

func TestCreateKeyStorageAndGenerationFailures(t *testing.T) {
	storage := errors.New("disk full")
	r := newRun(&fakeStore{insertErr: storage}, generateKey)
	_, err := r.usecase.Execute(Input{UserID: func() int { return 7 }, Token: domain.Token{Name: "attempt", UnlimitedQuota: true}})
	require.ErrorIs(t, err, domain.ErrInternal)
	require.ErrorIs(t, err, storage)
	assert.Equal(t, "disk full", err.Error())
	assert.False(t, r.succeeded)

	r = newRun(&fakeStore{}, func() (string, error) { return "", errors.New("no entropy") })
	_, err = r.usecase.Execute(Input{UserID: func() int { return 7 }, Token: domain.Token{Name: "attempt", UnlimitedQuota: true}})
	require.ErrorIs(t, err, domain.ErrKeyGenerate)
}

func TestCreateKeyStoresAutoGroups(t *testing.T) {
	r := newRun(&fakeStore{}, generateKey)
	token, err := r.usecase.Execute(Input{UserID: func() int { return 7 }, Token: domain.Token{Name: "auto", UnlimitedQuota: true, Group: "auto", CrossGroupRetry: true}, AutoGroups: []string{"default"}, AutoGroupsSet: true})
	require.NoError(t, err)
	assert.Equal(t, `["default"]`, token.AutoGroups)
	assert.True(t, token.CrossGroupRetry)
}
