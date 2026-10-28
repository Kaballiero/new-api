package revoke_token

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	token    string
	clearErr error
	cleared  bool
}

func (s *fakeStore) Transaction(action func(Transaction) error) error { return action(s) }
func (s *fakeStore) LockToken(int) (string, error)                    { return s.token, nil }
func (s *fakeStore) Clear(int) error {
	if s.clearErr != nil {
		return s.clearErr
	}
	s.cleared = true
	return nil
}

func execute(store *fakeStore) (Output, []string, error) {
	var events []string
	output, err := NewUsecase(store, domain.Fingerprint, func(ref string) { events = append(events, ref) }).Execute(Input{UserID: 4})
	return output, events, err
}

// Mirrors controller TestAccessTokenLifecycleAndLateRequests and
// TestPersonalAccessTokenRevocationRollsBackOnWriteFailure.
func TestRevokeToken(t *testing.T) {
	store := &fakeStore{token: "pat"}
	output, events, err := execute(store)
	require.NoError(t, err)
	assert.True(t, store.cleared)
	assert.Equal(t, Output{TokenRef: domain.Fingerprint("pat")}, output)
	assert.Equal(t, []string{domain.Fingerprint("pat")}, events)

	store = &fakeStore{}
	output, events, err = execute(store)
	require.NoError(t, err)
	assert.False(t, store.cleared, "repeated revocation is idempotent")
	assert.Empty(t, output.TokenRef)
	assert.Empty(t, events)
}

func TestRevokeTokenWriteFailure(t *testing.T) {
	cause := errors.New("personal access token write failure")
	output, events, err := execute(&fakeStore{token: "pat", clearErr: cause})
	require.ErrorIs(t, err, domain.ErrInternal)
	require.ErrorIs(t, err, cause)
	assert.Equal(t, domain.Fingerprint("pat"), output.TokenRef, "the attempted generation reference is retained")
	assert.Empty(t, events, "a failed revoke is not audited")
}
