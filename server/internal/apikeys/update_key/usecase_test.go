package update_key

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/apikeys/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errMissing = errors.New("record not found")

type fakeStore struct {
	stored    *domain.Token
	updateErr error
	updated   *domain.Token
	// others are the names of the other keys of the user that are not deleted.
	others   []string
	nameRule bool
}

func (s *fakeStore) Get(id, userID int) (*domain.Token, error) {
	if s.stored == nil || s.stored.Id != id || s.stored.UserId != userID {
		return nil, errMissing
	}
	copy := *s.stored
	return &copy, nil
}
func (s *fakeStore) IsMissing(err error) bool { return errors.Is(err, errMissing) }
func (s *fakeStore) Update(t *domain.Token) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updated = t
	return nil
}
func (s *fakeStore) UpdateWithUniqueName(t *domain.Token) error {
	s.nameRule = true
	if domain.NameTaken(s.others, t.Name) {
		return domain.ErrNameDuplicate
	}
	return s.Update(t)
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
	params    map[string]any
	succeeded bool
}

func newRun(store *fakeStore) *run {
	r := &run{params: map[string]any{}}
	groups := domain.AutoGroupRules{Max: func() int { return 2 }, UserGroup: func() (string, error) { return "default", nil }, Selectable: func(string, string) bool { return true }, Encode: store.SetAutoGroups}
	r.usecase = NewUsecase(store, groups, func() int { return 1000 }, func() int64 { return 1700000000 }, func() map[string]any { return r.params }, func() { r.succeeded = true })
	return r
}

func owned() *domain.Token {
	empty := ""
	return &domain.Token{Id: 5, UserId: 7, Name: "owned", Status: domain.StatusEnabled, ExpiredTime: -1, RemainQuota: 100, UnlimitedQuota: true, Group: "auto", CrossGroupRetry: true, AllowIps: &empty, AutoGroups: `["default"]`}
}

func TestUpdateKeyAuditsChangedFields(t *testing.T) {
	empty := ""
	cases := []struct {
		name    string
		token   domain.Token
		changed []string
	}{
		{name: "normalized update", token: domain.Token{Id: 5, Name: "renamed", ExpiredTime: -1, RemainQuota: 200, UnlimitedQuota: true, Group: "default", CrossGroupRetry: true, AllowIps: &empty}, changed: []string{"name", "remain_quota", "group", "cross_group_retry", "auto_groups"}},
		{name: "unchanged update", token: domain.Token{Id: 5, Name: "owned", ExpiredTime: -1, RemainQuota: 100, UnlimitedQuota: true, Group: "auto", CrossGroupRetry: true, AllowIps: &empty}, changed: []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{stored: owned()}
			r := newRun(store)
			token, err := r.usecase.Execute(Input{UserID: 7, Token: tc.token})
			require.NoError(t, err)
			assert.Same(t, store.updated, token)
			assert.Equal(t, map[string]any{"id": 5, "name": tc.token.Name, "changed_fields": tc.changed}, r.params)
			assert.True(t, r.succeeded)
		})
	}
}

func TestUpdateKeyStatusOnly(t *testing.T) {
	store := &fakeStore{stored: owned()}
	store.stored.Status = 2
	r := newRun(store)
	token, err := r.usecase.Execute(Input{UserID: 7, StatusOnly: "true", Token: domain.Token{Id: 5, Status: domain.StatusEnabled, Name: "ignored"}})
	require.NoError(t, err)
	assert.Equal(t, "owned", token.Name, "status_only changes only the status")
	assert.Equal(t, map[string]any{"id": 5, "name": "owned", "from": 2, "to": 1}, r.params)
}

func TestUpdateKeyRejections(t *testing.T) {
	cases := []struct {
		name   string
		stored func(*domain.Token)
		token  domain.Token
		err    error
		params map[string]any
	}{
		{name: "foreign update", token: domain.Token{Id: 6, Name: "forged-name", UnlimitedQuota: true}, err: domain.ErrKeyNotFound, params: map[string]any{"id": 6}},
		{name: "expired enable", stored: func(t *domain.Token) { t.Status, t.ExpiredTime = domain.StatusExpired, 1600000000 }, token: domain.Token{Id: 5, Status: domain.StatusEnabled}, err: domain.ErrExpiredCannotEnable, params: map[string]any{"id": 5, "name": "owned"}},
		{name: "exhausted enable", stored: func(t *domain.Token) { t.Status, t.RemainQuota, t.UnlimitedQuota = domain.StatusExhausted, 0, false }, token: domain.Token{Id: 5, Status: domain.StatusEnabled}, err: domain.ErrExhaustedCannotEnable, params: map[string]any{"id": 5, "name": "owned"}},
		{name: "negative quota", token: domain.Token{Id: 5, RemainQuota: -1}, err: domain.ErrQuotaNegative, params: map[string]any{"id": 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{stored: owned()}
			if tc.stored != nil {
				tc.stored(store.stored)
			}
			r := newRun(store)
			_, err := r.usecase.Execute(Input{UserID: 7, StatusOnly: "true", Token: tc.token})
			require.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.params, r.params)
			assert.False(t, r.succeeded)
			assert.Nil(t, store.updated)
		})
	}
}

func TestUpdateKeyStorageFailureKeepsStoredName(t *testing.T) {
	store := &fakeStore{stored: owned(), updateErr: errors.New("write failed")}
	r := newRun(store)
	_, err := r.usecase.Execute(Input{UserID: 7, Token: domain.Token{Id: 5, Name: "failed-rename", UnlimitedQuota: true}})
	require.ErrorIs(t, err, domain.ErrInternal)
	assert.Equal(t, "write failed", err.Error())
	assert.Equal(t, map[string]any{"id": 5, "name": "owned"}, r.params)
	assert.False(t, r.succeeded)
}

func TestUpdateKeyAppliesAutoGroupsOnlyWhenSet(t *testing.T) {
	store := &fakeStore{stored: owned()}
	r := newRun(store)
	token, err := r.usecase.Execute(Input{UserID: 7, Token: domain.Token{Id: 5, Name: "owned", UnlimitedQuota: true, Group: "auto"}, AutoGroups: []string{"vip"}, AutoGroupsSet: true})
	require.NoError(t, err)
	assert.Equal(t, `["vip"]`, token.AutoGroups)

	store = &fakeStore{stored: owned()}
	token, err = newRun(store).usecase.Execute(Input{UserID: 7, Token: domain.Token{Id: 5, Name: "owned", UnlimitedQuota: true, Group: "auto"}})
	require.NoError(t, err)
	assert.Equal(t, `["default"]`, token.AutoGroups, "absent auto_groups keeps stored groups")
}

func TestUpdateKeyRenameKeepsNameUnique(t *testing.T) {
	cases := []struct {
		name     string
		others   []string
		input    string
		err      error
		saved    string
		nameRule bool
	}{
		{name: "duplicate", others: []string{"Prod"}, input: "Prod", err: domain.ErrNameDuplicate, nameRule: true},
		{name: "duplicate in another case", others: []string{"Prod"}, input: "pROD", err: domain.ErrNameDuplicate, nameRule: true},
		{name: "duplicate after trimming", others: []string{" prod "}, input: "  Prod\t", err: domain.ErrNameDuplicate, nameRule: true},
		{name: "unique name is trimmed", others: []string{"Prod"}, input: "  staging  ", saved: "staging", nameRule: true},
		{name: "own name skips the rule", others: []string{"owned"}, input: " owned ", saved: "owned"},
		{name: "own name in another case", others: []string{"other"}, input: "OWNED", saved: "OWNED", nameRule: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{stored: owned(), others: tc.others}
			r := newRun(store)
			token, err := r.usecase.Execute(Input{UserID: 7, Token: domain.Token{Id: 5, Name: tc.input, ExpiredTime: -1, UnlimitedQuota: true, Group: "auto"}})
			assert.Equal(t, tc.nameRule, store.nameRule)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.NotErrorIs(t, err, domain.ErrInternal)
				assert.Nil(t, store.updated)
				assert.False(t, r.succeeded)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.saved, token.Name)
			assert.Equal(t, tc.saved, r.params["name"])
			assert.True(t, r.succeeded)
		})
	}
}

func TestUpdateKeyStatusOnlyIgnoresNameRule(t *testing.T) {
	store := &fakeStore{stored: owned(), others: []string{"taken"}}
	store.stored.Status = domain.StatusDisabled
	token, err := newRun(store).usecase.Execute(Input{UserID: 7, StatusOnly: "true", Token: domain.Token{Id: 5, Status: domain.StatusEnabled, Name: "taken"}})
	require.NoError(t, err)
	assert.False(t, store.nameRule)
	assert.Equal(t, "owned", token.Name)
	assert.Equal(t, domain.StatusEnabled, token.Status)
}
