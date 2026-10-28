package change_group

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/accounts/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	roles               map[int]int
	rolesErr, updateErr error
	updated             []int
}

func (s *fakeStore) RolesByIDs([]int) (map[int]int, error) { return s.roles, s.rolesErr }
func (s *fakeStore) UpdateGroup(ids []int, _ string) (int64, error) {
	s.updated = ids
	return int64(len(ids)), s.updateErr
}

func TestChangeGroup(t *testing.T) {
	storage := errors.New("deadlock")
	exists := func(group string) bool { return group == "vip" }
	tooMany := make([]int, domain.MaxBulkGroupBatch+1)
	cases := []struct {
		name   string
		store  *fakeStore
		input  Input
		result BulkGroupResult
		err    error
	}{
		{name: "empty", store: &fakeStore{}, input: Input{Group: "vip", ActorRole: 100}, err: domain.ErrInvalidGroupChange},
		{name: "too many", store: &fakeStore{}, input: Input{UserIDs: tooMany, Group: "vip", ActorRole: 100}, err: domain.ErrGroupBatchTooLarge},
		{name: "unknown group", store: &fakeStore{}, input: Input{UserIDs: []int{1}, Group: "gold", ActorRole: 100}, err: domain.ErrGroupDoesNotExist},
		{name: "all missing", store: &fakeStore{roles: map[int]int{}}, input: Input{UserIDs: []int{1}, Group: "vip", ActorRole: 100}, err: domain.ErrGroupUsersNotFound},
		{name: "all forbidden", store: &fakeStore{roles: map[int]int{1: 10}}, input: Input{UserIDs: []int{1, 2}, Group: "vip", ActorRole: 10}, err: domain.ErrGroupChangeForbidden},
		{name: "roles failure", store: &fakeStore{rolesErr: storage}, input: Input{UserIDs: []int{1}, Group: "vip", ActorRole: 100}, err: domain.ErrGroupChangeFailed},
		{name: "update failure", store: &fakeStore{roles: map[int]int{1: 1}, updateErr: storage}, input: Input{UserIDs: []int{1}, Group: "vip", ActorRole: 100}, err: domain.ErrGroupChangeFailed},
		{name: "partial", store: &fakeStore{roles: map[int]int{1: 1, 2: 10}}, input: Input{UserIDs: []int{1, 2, 3}, Group: "vip", ActorRole: 10}, result: BulkGroupResult{Updated: 1, UpdatedIDs: []int{1}, SkippedIDs: []int{2}, NotFoundIDs: []int{3}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := NewUsecase(tc.store, exists, 100).Execute(tc.input)
			require.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.result, result)
		})
	}
}

func TestChangeGroupReportsMissingGroup(t *testing.T) {
	_, err := NewUsecase(&fakeStore{}, func(string) bool { return false }, 100).Execute(Input{UserIDs: []int{1}, Group: "gold"})
	var group *domain.GroupError
	require.ErrorAs(t, err, &group)
	assert.Equal(t, "gold", group.Group)
}
