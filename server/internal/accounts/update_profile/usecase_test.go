package update_profile

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	loadErr, saveErr, updateErr error
	saved                       *dto.UserSetting
	updated                     *SelfUpdateAccount
}

func (s *fakeStore) SelfPreferences(id int) (SelfPreferencesAccount, error) {
	return SelfPreferencesAccount{ID: id, Settings: dto.UserSetting{Language: "en"}}, s.loadErr
}
func (s *fakeStore) SaveSelfPreferences(_ int, settings dto.UserSetting) error {
	s.saved = &settings
	return s.saveErr
}
func (s *fakeStore) UpdateSelfAccount(account SelfUpdateAccount) error {
	s.updated = &account
	return s.updateErr
}

type fakeInput struct {
	account AccountInput
	err     error
}

func (i fakeInput) ReadAccount() (AccountInput, error) { return i.account, i.err }

type fakePasswords struct {
	result  PasswordResult
	err     error
	changed bool
}

func (p *fakePasswords) Change(AccountInput, int) (PasswordResult, error) {
	p.changed = true
	return p.result, p.err
}

type user int

func (u user) UserID() int { return int(u) }

func TestUpdateProfilePreferences(t *testing.T) {
	storage := errors.New("timeout")
	t.Run("sidebar wins over language", func(t *testing.T) {
		store := &fakeStore{}
		result, err := NewUsecase(store, fakeInput{}, &fakePasswords{}).Execute(map[string]any{"sidebar_modules": "{}", "language": "ru"}, user(7))
		require.NoError(t, err)
		assert.True(t, result.Preferences)
		assert.Equal(t, &dto.UserSetting{Language: "en", SidebarModules: "{}"}, store.saved)
	})
	t.Run("load failure stays unclassified", func(t *testing.T) {
		_, err := NewUsecase(&fakeStore{loadErr: storage}, fakeInput{}, &fakePasswords{}).Execute(map[string]any{"language": "ru"}, user(7))
		require.ErrorIs(t, err, storage)
		assert.NotErrorIs(t, err, domain.ErrSettingsSaveFailed)
	})
	t.Run("save failure", func(t *testing.T) {
		_, err := NewUsecase(&fakeStore{saveErr: storage}, fakeInput{}, &fakePasswords{}).Execute(map[string]any{"language": "ru"}, user(7))
		require.ErrorIs(t, err, domain.ErrSettingsSaveFailed)
	})
	t.Run("password request skips preferences", func(t *testing.T) {
		store, passwords := &fakeStore{}, &fakePasswords{}
		_, err := NewUsecase(store, fakeInput{account: AccountInput{Password: "new-secret"}}, passwords).Execute(map[string]any{"language": "ru", "password": "new-secret"}, user(7))
		require.NoError(t, err)
		assert.Nil(t, store.saved)
		assert.True(t, passwords.changed)
	})
}

func TestUpdateProfileAccount(t *testing.T) {
	storage := errors.New("duplicate username")
	cases := []struct {
		name      string
		input     fakeInput
		passwords *fakePasswords
		updateErr error
		err       error
		updated   bool
	}{
		{name: "undecodable", input: fakeInput{err: domain.ErrProfileInvalid}, passwords: &fakePasswords{}, err: domain.ErrProfileInvalid},
		{name: "invalid field", input: fakeInput{err: domain.ErrProfileInputFailed}, passwords: &fakePasswords{}, err: domain.ErrProfileInputFailed},
		{name: "password change failure", input: fakeInput{account: AccountInput{Password: "new-secret"}}, passwords: &fakePasswords{err: storage, result: PasswordResult{NotificationFailed: true}}, err: domain.ErrPasswordChangeFailed},
		{name: "profile update failure", input: fakeInput{account: AccountInput{Username: "bob"}}, passwords: &fakePasswords{}, updateErr: storage, err: storage, updated: true},
		{name: "profile update", input: fakeInput{account: AccountInput{Username: "bob", DisplayName: "Bob"}}, passwords: &fakePasswords{}, updated: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{updateErr: tc.updateErr}
			result, err := NewUsecase(store, tc.input, tc.passwords).Execute(map[string]any{"username": "bob"}, user(7))
			require.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.updated, store.updated != nil)
			assert.Equal(t, tc.passwords.result, result.Password)
		})
	}
}

func TestUpdateProfilePasswordFailureKeepsCause(t *testing.T) {
	cause := errors.New("proof expired")
	_, err := NewUsecase(&fakeStore{}, fakeInput{account: AccountInput{Password: "new-secret"}}, &fakePasswords{err: cause}).Execute(map[string]any{"password": "new-secret"}, user(7))
	require.ErrorIs(t, err, domain.ErrPasswordChangeFailed)
	assert.Equal(t, cause, errors.Unwrap(err))
}
