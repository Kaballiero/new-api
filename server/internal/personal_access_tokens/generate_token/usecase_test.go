package generate_token

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	existing  int64
	updateErr error
	updated   map[int]string
}

func (s *fakeStore) CountToken(string) (int64, error) { return s.existing, nil }
func (s *fakeStore) Update(id int, token string) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updated[id] = token
	return nil
}

type audited struct {
	id  int
	ref string
}

func run(store *fakeStore, generate func() (string, error)) (Output, []audited, error) {
	var events []audited
	output, err := NewUsecase(store, generate, domain.Fingerprint, func(id int, ref string) { events = append(events, audited{id, ref}) }).Execute(Input{UserID: 5})
	return output, events, err
}

func TestGenerateTokenStoresAndAuditsGeneration(t *testing.T) {
	store := &fakeStore{updated: map[int]string{}}
	output, events, err := run(store, func() (string, error) { return "fresh-token", nil })
	require.NoError(t, err)
	assert.Equal(t, Output{Token: "fresh-token"}, output)
	assert.Equal(t, map[int]string{5: "fresh-token"}, store.updated)
	assert.Equal(t, []audited{{5, domain.Fingerprint("fresh-token")}}, events)
}

func TestGenerateTokenFailures(t *testing.T) {
	store := &fakeStore{existing: 1, updated: map[int]string{}}
	_, events, err := run(store, func() (string, error) { return "taken", nil })
	require.ErrorIs(t, err, domain.ErrTokenDuplicate)
	assert.Empty(t, store.updated, "a colliding token is never written")
	assert.Empty(t, events)

	cause := errors.New("entropy unavailable")
	_, _, err = run(&fakeStore{updated: map[int]string{}}, func() (string, error) { return "", cause })
	require.ErrorIs(t, err, domain.ErrInternal)
	require.ErrorIs(t, err, cause)

	cause = errors.New("write unavailable")
	_, events, err = run(&fakeStore{updateErr: cause}, func() (string, error) { return "fresh-token", nil })
	require.ErrorIs(t, err, domain.ErrInternal)
	assert.EqualError(t, err, "write unavailable")
	assert.Empty(t, events, "a failed write is not audited")
}
