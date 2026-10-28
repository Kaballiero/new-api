package core_test

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	cachefixture "github.com/QuantumNous/new-api/internal/contracttest/cachefixture"
	model "github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertUsersForPaginationTest(t *testing.T, total int) {
	t.Helper()
	for id := 1; id <= total; id++ {
		user := &model.User{
			Id:          id,
			Username:    fmt.Sprintf("user%02d", id),
			Password:    "password123",
			DisplayName: fmt.Sprintf("User %02d", id),
			Email:       fmt.Sprintf("user%02d@example.com", id),
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
			Group:       "default",
			AffCode:     fmt.Sprintf("aff%02d", id),
		}
		require.NoError(t, model.DB.Create(user).Error)
	}
}

func collectUserIDs(users []*model.User) []int {
	ids := make([]int, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.Id)
	}
	return ids
}

func TestGetAllUsersSortsBeforePagination(t *testing.T) {
	cachefixture.Open(t)
	insertUsersForPaginationTest(t, 42)

	pageOne, total, err := accountstorage.SQL().GetAllUsers(&common.PageInfo{Page: 1, PageSize: 20}, accountrecord.NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}, collectUserIDs(pageOne))

	pageTwo, total, err := accountstorage.SQL().GetAllUsers(&common.PageInfo{Page: 2, PageSize: 20}, accountrecord.NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}, collectUserIDs(pageTwo))

	pageThree, total, err := accountstorage.SQL().GetAllUsers(&common.PageInfo{Page: 3, PageSize: 20}, accountrecord.NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{41, 42}, collectUserIDs(pageThree))
}

func TestSearchUsersSortsBeforePagination(t *testing.T) {
	cachefixture.Open(t)
	insertUsersForPaginationTest(t, 42)

	users, total, err := accountstorage.SQL().SearchUsers("user", "", nil, nil, 20, 20, accountrecord.NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}, collectUserIDs(users))
}
