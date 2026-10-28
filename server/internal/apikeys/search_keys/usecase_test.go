package search_keys

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/apikeys/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeQuery struct {
	name, key     string
	countLimit    int
	offset, limit int
}

func (q *fakeQuery) NamePattern(pattern string) { q.name = pattern }
func (q *fakeQuery) KeyPattern(pattern string)  { q.key = pattern }
func (q *fakeQuery) Count(limit int) (int64, error) {
	q.countLimit = limit
	return 1, nil
}
func (q *fakeQuery) List(offset, limit int) ([]*domain.Token, error) {
	q.offset, q.limit = offset, limit
	return []*domain.Token{{Id: 1}}, nil
}

type fakeReader struct {
	count int64
	query *fakeQuery
}

func (r *fakeReader) Count(int) (int64, error) { return r.count, nil }
func (r *fakeReader) OpenSearch(int) Query {
	r.query = &fakeQuery{}
	return r.query
}

func search(reader *fakeReader, input Input) (Output, error) {
	return NewUsecase(reader, func() int { return 10 }, func(string) {}, func(string) {}).Execute(input)
}

func TestSearchKeysEscapesPatterns(t *testing.T) {
	reader := &fakeReader{}
	result, err := search(reader, Input{UserID: 7, Keyword: "literal_under!name", Key: "sk-ab%", Start: -5, Limit: 500})
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.Total)
	assert.Equal(t, "literal!_under!!name", reader.query.name)
	assert.Equal(t, "ab%", reader.query.key, "sk- prefix is removed")
	assert.Equal(t, 10, reader.query.countLimit)
	assert.Equal(t, 0, reader.query.offset)
	assert.Equal(t, 100, reader.query.limit)
}

func TestSearchKeysRejections(t *testing.T) {
	cases := []struct {
		name  string
		count int64
		input Input
		err   error
	}{
		{name: "fuzzy above key limit", count: 11, input: Input{Keyword: "ab%"}, err: domain.ErrFuzzySearchLimit},
		{name: "consecutive wildcards", input: Input{Keyword: "ab%%"}, err: domain.ErrSearchConsecutiveWildcards},
		{name: "too many wildcards", input: Input{Keyword: "%a%b%"}, err: domain.ErrSearchTooManyWildcards},
		{name: "short fuzzy keyword", input: Input{Key: "a%"}, err: domain.ErrSearchKeywordTooShort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := search(&fakeReader{count: tc.count}, tc.input)
			require.ErrorIs(t, err, tc.err)
		})
	}
}
