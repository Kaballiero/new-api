package list_getapi_keys

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeReader holds the keys that are not deleted, as storage would return them.
type fakeReader struct {
	tokens   []*domain.Token
	err      error
	countErr error
	calls    int
}

func (r *fakeReader) ListAfter(userID, afterID, limit int) ([]*domain.Token, error) {
	r.calls++
	var result []*domain.Token
	for _, token := range slices.Backward(r.tokens) {
		if token.UserId == userID && (afterID == 0 || token.Id < afterID) && len(result) < limit {
			result = append(result, token)
		}
	}
	return result, r.err
}
func (r *fakeReader) ListByIDs(ids []int) ([]*domain.Token, error) {
	r.calls++
	var result []*domain.Token
	for _, token := range slices.Backward(r.tokens) {
		if slices.Contains(ids, token.Id) {
			result = append(result, token)
		}
	}
	return result, r.err
}
func (r *fakeReader) Count(userID int) (int64, error) {
	var total int64
	for _, token := range r.tokens {
		if token.UserId == userID {
			total++
		}
	}
	return total, r.countErr
}

func fullKey(id int) string { return "secretkeyvalue" + strings.Repeat("x", id) + "tail" }

// keys returns tokens with ids 1..n in ascending order; odd ids belong to user 7, even ids to user 8.
func keys(n int) []*domain.Token {
	tokens := make([]*domain.Token, 0, n)
	for id := 1; id <= n; id++ {
		tokens = append(tokens, &domain.Token{Id: id, UserId: 7 + (id+1)%2, Key: fullKey(id), Name: "key", Status: domain.StatusEnabled, CreatedTime: 10, AccessedTime: 20, ExpiredTime: -1, RemainQuota: 30, UsedQuota: 40, UnlimitedQuota: true})
	}
	return tokens
}

func ptr(v int) *int { return &v }

func ids(items []Key) []int {
	result := make([]int, 0, len(items))
	for _, item := range items {
		result = append(result, item.ID)
	}
	return result
}

func TestListGetAPIKeysProjectsMaskedKey(t *testing.T) {
	expired := &domain.Token{Id: 3, UserId: 7, Key: "abcdEFGHijklMNOP", Name: "prod", Status: domain.StatusExpired, CreatedTime: 1, AccessedTime: 2, ExpiredTime: 3, RemainQuota: 4, UsedQuota: 5, AutoGroups: `["vip"]`, Group: "auto"}
	reader := &fakeReader{tokens: []*domain.Token{expired}}
	want := []Key{{ID: 3, UserID: 7, Name: "prod", MaskedKey: "abcd**********MNOP", Status: domain.StatusExpired, CreatedTime: 1, AccessedTime: 2, ExpiredTime: 3, RemainQuota: 4, UsedQuota: 5}}

	page, err := NewUsecase(reader).Execute(Input{UserID: ptr(7)})
	require.NoError(t, err)
	assert.Equal(t, Output{Items: want, Total: 1}, page)
	batch, err := NewUsecase(reader).Execute(Input{IDs: []int{3}})
	require.NoError(t, err)
	assert.Equal(t, Output{Items: want}, batch)

	// Neither the projection nor its JSON carries the full key.
	for _, value := range []any{page, Batch{Items: batch.Items}} {
		body, err := common.Marshal(value)
		require.NoError(t, err)
		assert.NotContains(t, string(body), expired.Key)
		assert.NotContains(t, string(body), `"key"`)
	}
}

func TestListGetAPIKeysUserModePagesWithoutGapsOrDuplicates(t *testing.T) {
	reader := &fakeReader{tokens: keys(10)} // user 7 owns 1,3,5,7,9
	usecase := NewUsecase(reader)

	first, err := usecase.Execute(Input{UserID: ptr(7), Limit: ptr(2)})
	require.NoError(t, err)
	assert.Equal(t, []int{9, 7}, ids(first.Items))
	require.NotNil(t, first.NextAfterID)
	assert.Equal(t, 7, *first.NextAfterID)
	assert.EqualValues(t, 5, first.Total)

	second, err := usecase.Execute(Input{UserID: ptr(7), AfterID: first.NextAfterID, Limit: ptr(2)})
	require.NoError(t, err)
	assert.Equal(t, []int{5, 3}, ids(second.Items))
	require.NotNil(t, second.NextAfterID)
	assert.Equal(t, 3, *second.NextAfterID)

	last, err := usecase.Execute(Input{UserID: ptr(7), AfterID: second.NextAfterID, Limit: ptr(2)})
	require.NoError(t, err)
	assert.Equal(t, []int{1}, ids(last.Items))
	assert.Nil(t, last.NextAfterID, "a short page is the last one")
	assert.EqualValues(t, 5, last.Total, "total does not depend on the cursor")

	// A full last page still returns a cursor; the following page is empty.
	full, err := usecase.Execute(Input{UserID: ptr(7), AfterID: ptr(5), Limit: ptr(2)})
	require.NoError(t, err)
	assert.Equal(t, []int{3, 1}, ids(full.Items))
	require.NotNil(t, full.NextAfterID)
	empty, err := usecase.Execute(Input{UserID: ptr(7), AfterID: full.NextAfterID, Limit: ptr(2)})
	require.NoError(t, err)
	assert.Equal(t, Output{Items: []Key{}, Total: 5}, empty)
}

func TestListGetAPIKeysUserModeDefaultsAndBounds(t *testing.T) {
	reader := &fakeReader{tokens: keys(250)} // user 7 owns 125 keys
	page, err := NewUsecase(reader).Execute(Input{UserID: ptr(7)})
	require.NoError(t, err)
	assert.Len(t, page.Items, 100, "limit defaults to 100")
	assert.Equal(t, 249, page.Items[0].ID)
	require.NotNil(t, page.NextAfterID)
	assert.Equal(t, 51, *page.NextAfterID)
	assert.EqualValues(t, 125, page.Total)

	single, err := NewUsecase(reader).Execute(Input{UserID: ptr(8), Limit: ptr(1)})
	require.NoError(t, err)
	assert.Equal(t, []int{250}, ids(single.Items))

	unknown, err := NewUsecase(reader).Execute(Input{UserID: ptr(999)})
	require.NoError(t, err)
	assert.Equal(t, Output{Items: []Key{}, Total: 0}, unknown)
	body, err := common.Marshal(unknown)
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[],"next_after_id":null,"total":0}`, string(body))
}

func TestListGetAPIKeysIDsMode(t *testing.T) {
	reader := &fakeReader{tokens: keys(10)}
	batch, err := NewUsecase(reader).Execute(Input{IDs: []int{2, 9, 404, 5}})
	require.NoError(t, err)
	assert.Equal(t, []int{9, 5, 2}, ids(batch.Items), "keys of any user; unknown ids are absent")
	assert.Nil(t, batch.NextAfterID)
	assert.Zero(t, batch.Total)

	hundred := make([]int, 100)
	for i := range hundred {
		hundred[i] = i + 1
	}
	batch, err = NewUsecase(&fakeReader{tokens: keys(150)}).Execute(Input{IDs: hundred})
	require.NoError(t, err)
	assert.Len(t, batch.Items, 100)

	missing, err := NewUsecase(reader).Execute(Input{IDs: []int{404}})
	require.NoError(t, err)
	body, err := common.Marshal(Batch{Items: missing.Items})
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[]}`, string(body))
}

func TestListGetAPIKeysRejectsInvalidQuery(t *testing.T) {
	tooMany := make([]int, 101)
	for i := range tooMany {
		tooMany[i] = i + 1
	}
	cases := []struct {
		name  string
		input Input
	}{
		{"no mode", Input{}},
		{"cursor without user", Input{AfterID: ptr(5), Limit: ptr(10)}},
		{"both modes", Input{UserID: ptr(7), IDs: []int{1}}},
		{"zero user", Input{UserID: ptr(0)}},
		{"negative user", Input{UserID: ptr(-7)}},
		{"zero cursor", Input{UserID: ptr(7), AfterID: ptr(0)}},
		{"negative cursor", Input{UserID: ptr(7), AfterID: ptr(-1)}},
		{"zero limit", Input{UserID: ptr(7), Limit: ptr(0)}},
		{"limit above 100", Input{UserID: ptr(7), Limit: ptr(101)}},
		{"empty ids", Input{IDs: []int{}}},
		{"more than 100 ids", Input{IDs: tooMany}},
		{"duplicate ids", Input{IDs: []int{1, 2, 1}}},
		{"zero id", Input{IDs: []int{0}}},
		{"negative id", Input{IDs: []int{3, -3}}},
		{"ids with cursor", Input{IDs: []int{1}, AfterID: ptr(5)}},
		{"ids with limit", Input{IDs: []int{1}, Limit: ptr(5)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeReader{tokens: keys(3)}
			_, err := NewUsecase(reader).Execute(tc.input)
			require.ErrorIs(t, err, domain.ErrListQueryInvalid)
			assert.Zero(t, reader.calls, "invalid queries do not reach storage")
		})
	}
}

func TestListGetAPIKeysStorageFailures(t *testing.T) {
	failure := errors.New("database is down")
	for _, input := range []Input{{UserID: ptr(7)}, {IDs: []int{1}}} {
		_, err := NewUsecase(&fakeReader{tokens: keys(3), err: failure}).Execute(input)
		require.ErrorIs(t, err, domain.ErrInternal)
		require.ErrorIs(t, err, failure)
	}
	_, err := NewUsecase(&fakeReader{tokens: keys(3), countErr: failure}).Execute(Input{UserID: ptr(7)})
	require.ErrorIs(t, err, domain.ErrInternal)
}
