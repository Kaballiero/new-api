package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type userBatchEnvelope struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Data    struct {
		Users       []map[string]any `json:"users"`
		SkippedIDs  []int            `json:"skipped_ids"`
		NotFoundIDs []int            `json:"not_found_ids"`
	} `json:"data"`
}

func TestGetUsersBatchResultsAndVisibility(t *testing.T) {
	db := openUserControllerTestDB(t)
	first := seedUser(t, db, "batchfirst", common.RoleCommonUser, "default")
	second := seedUser(t, db, "batchsecond", common.RoleCommonUser, "default")
	admin := seedUser(t, db, "batchadmin", common.RoleAdminUser, "default")
	deleted := seedUser(t, db, "batchdeleted", common.RoleCommonUser, "default")
	require.NoError(t, db.Model(first).Updates(map[string]any{"quota": 123, "used_quota": 45}).Error)
	require.NoError(t, db.Delete(deleted).Error)

	cases := []struct {
		name       string
		ids        string
		role       int
		status     int
		userIDs    []int
		skippedIDs []int
		missingIDs []int
		code       string
	}{
		{"order, deduplication, missing and forbidden", fmt.Sprintf("%d,%d,%d,%d,99999,%d", second.Id, admin.Id, first.Id, second.Id, first.Id), common.RoleAdminUser, 200, []int{second.Id, first.Id}, []int{admin.Id}, []int{99999}, ""},
		{"all forbidden", fmt.Sprintf("%d", admin.Id), common.RoleAdminUser, 403, nil, nil, nil, "permission_denied"},
		{"all missing includes deleted", fmt.Sprintf("%d,99999", deleted.Id), common.RoleAdminUser, 404, nil, nil, nil, "user_not_found"},
		{"root can see admin", fmt.Sprintf("%d", admin.Id), common.RoleRootUser, 200, []int{admin.Id}, []int{}, []int{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, rec := newRestContext(t, http.MethodGet, "/api/user/batch?ids="+tc.ids, nil, nil, tc.role)
			GetUsersBatch(ctx)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			var response userBatchEnvelope
			require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &response))
			assert.Equal(t, tc.code, response.Code)
			assert.Equal(t, tc.status == 200, response.Success)
			if tc.status != 200 {
				return
			}
			ids := make([]int, 0, len(response.Data.Users))
			for _, user := range response.Data.Users {
				assert.Len(t, user, 7)
				for _, field := range []string{"id", "status", "quota", "used_quota", "request_count", "group", "DeletedAt"} {
					assert.Contains(t, user, field)
				}
				ids = append(ids, int(user["id"].(float64)))
			}
			assert.Equal(t, tc.userIDs, ids)
			assert.Equal(t, tc.skippedIDs, response.Data.SkippedIDs)
			assert.Equal(t, tc.missingIDs, response.Data.NotFoundIDs)
			if tc.name == "order, deduplication, missing and forbidden" {
				assert.Equal(t, float64(123), response.Data.Users[1]["quota"])
				assert.Equal(t, float64(45), response.Data.Users[1]["used_quota"])
			}
		})
	}
}

func TestGetUsersBatchMatchesSingleUserFields(t *testing.T) {
	db := openUserControllerTestDB(t)
	user := seedUser(t, db, "batchparity", common.RoleCommonUser, "default")
	require.NoError(t, db.Model(user).Updates(map[string]any{"quota": 30, "used_quota": 11, "request_count": 7}).Error)

	batchContext, batchRecorder := newRestContext(t, http.MethodGet, fmt.Sprintf("/api/user/batch?ids=%d", user.Id), nil, nil, common.RoleRootUser)
	GetUsersBatch(batchContext)
	require.Equal(t, http.StatusOK, batchRecorder.Code)
	var batch userBatchEnvelope
	require.NoError(t, common.Unmarshal(batchRecorder.Body.Bytes(), &batch))
	require.Len(t, batch.Data.Users, 1)

	singleContext, singleRecorder := newRestContext(t, http.MethodGet, fmt.Sprintf("/api/user/%d", user.Id), nil, gin.Params{{Key: "id", Value: fmt.Sprint(user.Id)}}, common.RoleRootUser)
	GetUser(singleContext)
	require.Equal(t, http.StatusOK, singleRecorder.Code)
	var single struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(singleRecorder.Body.Bytes(), &single))
	for _, field := range []string{"id", "status", "quota", "used_quota", "request_count", "group", "DeletedAt"} {
		assert.Equal(t, single.Data[field], batch.Data.Users[0][field], field)
	}
}

func TestGetUsersBatchInvalidInputAndDatabaseError(t *testing.T) {
	db := openUserControllerTestDB(t)
	for _, ids := range []string{"", "0", "-1", "abc", "1,", "1,,2", "999999999999999999999999", strings.Repeat("1,", 100) + "1"} {
		t.Run(ids, func(t *testing.T) {
			ctx, rec := newRestContext(t, http.MethodGet, "/api/user/batch?ids="+ids, nil, nil, common.RoleAdminUser)
			GetUsersBatch(ctx)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, "invalid_params", decodeRestError(t, rec).Code)
		})
	}
	require.NoError(t, db.Migrator().DropTable(&model.User{}))
	ctx, rec := newRestContext(t, http.MethodGet, "/api/user/batch?ids=1", nil, nil, common.RoleAdminUser)
	GetUsersBatch(ctx)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "internal_error", decodeRestError(t, rec).Code)
}

func TestGetUsersBatchRequiresAdminAuth(t *testing.T) {
	openUserControllerTestDB(t)
	router := gin.New()
	router.GET("/api/user/batch", middleware.AdminAuth(), GetUsersBatch)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/user/batch?ids=1", nil))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}
