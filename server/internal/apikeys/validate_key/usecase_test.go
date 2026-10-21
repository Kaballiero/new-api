package validate_key

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errMissing = errors.New("record not found")

type fakeStore struct {
	token    *domain.Token
	err      error
	statuses []int
}

func (s *fakeStore) Lookup(string) (*domain.Token, error) { return s.token, s.err }
func (s *fakeStore) UpdateStatus(status int) error {
	s.statuses = append(s.statuses, status)
	return nil
}
func (s *fakeStore) IsMissing(err error) bool { return errors.Is(err, errMissing) }

func validate(store *fakeStore, cacheEnabled bool, key string) error {
	return NewUsecase(store, func() int64 { return 1700000000 }, func() bool { return cacheEnabled }, func(string) {}).Execute(key)
}

func TestValidateKey(t *testing.T) {
	enabled := func() *domain.Token {
		return &domain.Token{Status: domain.StatusEnabled, ExpiredTime: -1, RemainQuota: 25}
	}
	t.Run("not provided", func(t *testing.T) {
		require.ErrorIs(t, validate(&fakeStore{}, false, ""), domain.ErrKeyNotProvided)
	})
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, validate(&fakeStore{token: enabled()}, false, "key"))
	})
	t.Run("disabled", func(t *testing.T) {
		token := enabled()
		token.Status = 2
		require.ErrorIs(t, validate(&fakeStore{token: token}, false, "key"), domain.ErrKeyInvalid)
	})
	t.Run("expired is persisted without cache", func(t *testing.T) {
		token := enabled()
		token.ExpiredTime = 1600000000
		store := &fakeStore{token: token}
		require.ErrorIs(t, validate(store, false, "key"), domain.ErrKeyInvalid)
		assert.Equal(t, []int{domain.StatusExpired}, store.statuses)
	})
	t.Run("exhausted is persisted without cache", func(t *testing.T) {
		token := enabled()
		token.RemainQuota = 0
		store := &fakeStore{token: token}
		require.ErrorIs(t, validate(store, false, "key"), domain.ErrKeyInvalid)
		assert.Equal(t, []int{domain.StatusExhausted}, store.statuses)
	})
	t.Run("cache keeps stored status", func(t *testing.T) {
		token := enabled()
		token.RemainQuota = 0
		store := &fakeStore{token: token}
		require.ErrorIs(t, validate(store, true, "key"), domain.ErrKeyInvalid)
		assert.Empty(t, store.statuses)
	})
	t.Run("unlimited key ignores quota", func(t *testing.T) {
		token := enabled()
		token.RemainQuota, token.UnlimitedQuota = 0, true
		require.NoError(t, validate(&fakeStore{token: token}, false, "key"))
	})
	t.Run("missing key", func(t *testing.T) {
		require.ErrorIs(t, validate(&fakeStore{err: errMissing}, false, "key"), domain.ErrKeyInvalid)
	})
	t.Run("lookup failure", func(t *testing.T) {
		err := validate(&fakeStore{err: errors.New("connection reset")}, false, "key")
		require.ErrorIs(t, err, domain.ErrInternal)
		assert.Equal(t, "connection reset", errors.Unwrap(err).Error())
	})
}
