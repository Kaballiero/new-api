package reveal_keys_batch

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/apikeys/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReader struct{ tokens []domain.Token }

func (r fakeReader) KeysBatch([]int, int) ([]domain.Token, error) { return r.tokens, nil }

func TestRevealKeysBatch(t *testing.T) {
	cases := []struct {
		name      string
		ids       []int
		tokens    []domain.Token
		err       error
		params    map[string]any
		succeeded bool
	}{
		{name: "partial and duplicate", ids: []int{5, 5, 6, 999999}, tokens: []domain.Token{{Id: 5, Key: "full-key"}}, params: map[string]any{"count": 1, "returned_ids": []int{5}}, succeeded: true},
		{name: "no matches", ids: []int{6, 999999}, params: map[string]any{"count": 0, "returned_ids": []int{}}, succeeded: true},
		{name: "empty", ids: []int{}, err: domain.ErrBatchEmpty, params: map[string]any{}},
		{name: "too many", ids: make([]int, 101), err: domain.ErrBatchTooLarge, params: map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params, succeeded := map[string]any{}, false
			result, err := NewUsecase(fakeReader{tokens: tc.tokens}, func([]int) map[string]any { return params }, func() { succeeded = true }).Execute(tc.ids, 7)
			require.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.params, params)
			assert.Equal(t, tc.succeeded, succeeded)
			if tc.err == nil {
				assert.Len(t, result.Keys, len(tc.tokens))
			}
		})
	}
}
