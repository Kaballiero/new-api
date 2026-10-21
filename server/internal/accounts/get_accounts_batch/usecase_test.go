package get_accounts_batch

import (
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReader struct {
	rows []domain.UserBatchRow
	err  error
}

func (r fakeReader) Batch([]int) ([]domain.UserBatchRow, error) { return r.rows, r.err }

type actor int

func (a actor) ActorRole() int { return int(a) }

func TestGetAccountsBatch(t *testing.T) {
	rows := []domain.UserBatchRow{{Id: 1, Role: 1}, {Id: 2, Role: 10}}
	cases := []struct {
		name   string
		reader fakeReader
		ids    []int
		actor  actor
		result Result
		err    error
	}{
		{name: "keeps request order", reader: fakeReader{rows: rows}, ids: []int{2, 3, 1}, actor: 100, result: Result{Users: []domain.UserBatchRow{rows[1], rows[0]}, SkippedIDs: []int{}, NotFoundIDs: []int{3}}},
		{name: "hides peers", reader: fakeReader{rows: rows}, ids: []int{1, 2}, actor: 10, result: Result{Users: []domain.UserBatchRow{rows[0]}, SkippedIDs: []int{2}, NotFoundIDs: []int{}}},
		{name: "only hidden", reader: fakeReader{rows: rows}, ids: []int{2}, actor: 10, err: domain.ErrViewForbidden},
		{name: "only missing", reader: fakeReader{rows: rows}, ids: []int{7}, actor: 100, err: domain.ErrAccountNotFound},
		{name: "storage failure", reader: fakeReader{err: errors.New("timeout")}, ids: []int{1}, actor: 100, err: domain.ErrInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := NewUsecase(tc.reader, 100).Execute(tc.ids, tc.actor)
			require.ErrorIs(t, err, tc.err)
			if tc.err == nil {
				assert.Equal(t, tc.result, result)
			}
		})
	}
}

func TestParseUserBatchIDs(t *testing.T) {
	ids, ok := ParseUserBatchIDs(" 3,1,3 ")
	require.True(t, ok)
	assert.Equal(t, []int{3, 1}, ids)
	for _, raw := range []string{"", "1,x", "0", strings.Repeat("1,", 100) + "1"} {
		_, ok := ParseUserBatchIDs(raw)
		assert.False(t, ok, raw)
	}
}
