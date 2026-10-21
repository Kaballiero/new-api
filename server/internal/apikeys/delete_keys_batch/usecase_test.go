package delete_keys_batch

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct{ deleted int }

func (s fakeStore) DeleteBatch([]int, int) (int, error) { return s.deleted, nil }

func TestDeleteKeysBatch(t *testing.T) {
	params, succeeded := map[string]any{}, false
	result, err := NewUsecase(fakeStore{deleted: 1}, func([]int) map[string]any { return params }, func() { succeeded = true }).Execute([]int{5, 5, 6, 999999}, 7)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Count)
	assert.Equal(t, map[string]any{"count": 1}, params)
	assert.True(t, succeeded)

	params, succeeded = map[string]any{}, false
	_, err = NewUsecase(fakeStore{}, func([]int) map[string]any { return params }, func() { succeeded = true }).Execute([]int{}, 7)
	require.ErrorIs(t, err, domain.ErrBatchEmpty)
	assert.Empty(t, params)
	assert.False(t, succeeded)
}
