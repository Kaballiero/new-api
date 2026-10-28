package delete_key

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/apikeys/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errMissing = errors.New("record not found")

type fakeStore struct{ deleted *domain.Token }

func (s *fakeStore) Get(id, userID int) (*domain.Token, error) {
	if id != 5 || userID != 7 {
		return nil, errMissing
	}
	return &domain.Token{Id: 5, UserId: 7, Name: "owned"}, nil
}
func (s *fakeStore) IsMissing(err error) bool { return errors.Is(err, errMissing) }
func (s *fakeStore) Delete(t *domain.Token) error {
	s.deleted = t
	return nil
}

func TestDeleteKey(t *testing.T) {
	cases := []struct {
		name      string
		id        int
		err       error
		params    map[string]any
		succeeded bool
	}{
		{name: "delete", id: 5, params: map[string]any{"id": 5, "name": "owned"}, succeeded: true},
		{name: "foreign or missing", id: 999999, err: domain.ErrKeyNotFound, params: map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, params, succeeded := &fakeStore{}, map[string]any{}, false
			_, err := NewUsecase(store, func() map[string]any { return params }, func() { succeeded = true }).Execute(Input{ID: tc.id, UserID: 7})
			require.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.params, params)
			assert.Equal(t, tc.succeeded, succeeded)
			assert.Equal(t, tc.succeeded, store.deleted != nil)
		})
	}
}
