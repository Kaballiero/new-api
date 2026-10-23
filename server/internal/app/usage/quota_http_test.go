package usage_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	app "github.com/QuantumNous/new-api/internal/app/usage"
	aggregate "github.com/QuantumNous/new-api/internal/usage/aggregate_usage_data"
	"github.com/QuantumNous/new-api/internal/usage/dto"
	"github.com/QuantumNous/new-api/internal/usage/transport"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
)

type flowQuotaResponse struct {
	Success bool                `json:"success"`
	Message string              `json:"message"`
	Data    []dto.FlowQuotaData `json:"data"`
}

func setupFlowControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.QuotaData{}))
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "default",
		ChannelID: 1,
		ModelName: "gpt-a",
		CreatedAt: 1100,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    2,
		Username:  "bob",
		NodeName:  "node-b",
		TokenID:   22,
		UseGroup:  "vip",
		ChannelID: 1,
		ModelName: "gpt-b",
		CreatedAt: 1200,
		Count:     1,
		Quota:     70,
		TokenUsed: 30,
	}).Error)
}

func decodeFlowQuotaResponse(t *testing.T, recorder *httptest.ResponseRecorder) flowQuotaResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	return payload
}

func TestGetAllFlowQuotaDatesUsesAdminDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=bob", nil)
	ctx.Request.Header.Set("Accept-Language", "zh-CN")

	app.NewHTTP().AdminFlow.Handle(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "bob", payload.Data[0].Username)
	require.Equal(t, "vip", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
	require.Empty(t, payload.Data[0].TokenName)
	require.Empty(t, payload.Data[0].NodeName)
}

func TestGetAllFlowQuotaDatesUsesRootDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=alice", nil)
	ctx.Request.Header.Set("Accept-Language", "zh-CN")

	app.NewHTTP().AdminFlow.Handle(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "alice", payload.Data[0].Username)
	require.Equal(t, "node-a", payload.Data[0].NodeName)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRestrictsToAuthenticatedUser(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=1000&end_timestamp=2000", nil)
	ctx.Request.Header.Set("Accept-Language", "zh-CN")

	app.NewHTTP().OwnFlow.Handle(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Empty(t, payload.Data[0].Username)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Empty(t, payload.Data[0].ChannelName)
}

func TestGetUserQuotaDatesFiltersByOwnedTokenAndDateRange(t *testing.T) {
	setupFlowControllerTestDB(t)

	require.NoError(t, model.DB.Create(&model.Token{Id: 12, UserId: 1, Key: "sk-secondary", Name: "secondary"}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{UserID: 1, Username: "alice", TokenID: 11,
		ModelName: "gpt-old", CreatedAt: 900, Count: 3, Quota: 150, TokenUsed: 60}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{UserID: 1, Username: "alice", TokenID: 12,
		ModelName: "gpt-secondary", CreatedAt: 1100, Count: 4, Quota: 200, TokenUsed: 80}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/self?start_timestamp=1000&end_timestamp=2000&token_id=11", nil)
	ctx.Request.Header.Set("Accept-Language", "zh-CN")

	app.NewHTTP().OwnQuota.Handle(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "gpt-a", payload.Data[0].ModelName)
	require.Equal(t, 2, payload.Data[0].Count)
	require.Equal(t, 100, payload.Data[0].Quota)
	require.Equal(t, 40, payload.Data[0].TokenUsed)
}

func TestGetUserQuotaDatesDoesNotExposeForeignTokenUsage(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/self?start_timestamp=1000&end_timestamp=2000&token_id=22", nil)
	ctx.Request.Header.Set("Accept-Language", "zh-CN")

	app.NewHTTP().OwnQuota.Handle(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Empty(t, payload.Data)
}

func TestGetUserQuotaDatesRejectsNonPositiveTokenID(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/self?start_timestamp=1000&end_timestamp=2000&token_id=0", nil)
	ctx.Request.Header.Set("Accept-Language", "zh-CN")

	app.NewHTTP().OwnQuota.Handle(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestGetUserFlowQuotaDatesRejectsInvalidTimeRange(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=bad&end_timestamp=2000", nil)
	ctx.Request.Header.Set("Accept-Language", "zh-CN")

	app.NewHTTP().OwnFlow.Handle(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.Equal(t, "invalid start_timestamp", payload.Message)
}

type quotaDateRow struct {
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	ModelName string `json:"model_name"`
	CreatedAt int64  `json:"created_at"`
	TokenUsed int    `json:"token_used"`
	Count     int    `json:"count"`
	Quota     int    `json:"quota"`
}

type quotaDatesResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Code    string         `json:"code"`
	Data    []quotaDateRow `json:"data"`
}

func requestQuotaDates(t *testing.T, handler gin.HandlerFunc, target string, userID int) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", userID)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	ctx.Request.Header.Set("Accept-Language", "zh-CN")
	handler(ctx)
	return recorder
}

func decodeQuotaDatesResponse(t *testing.T, recorder *httptest.ResponseRecorder) quotaDatesResponse {
	t.Helper()
	var payload quotaDatesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload
}

func TestQuotaDatesRejectUnsupportedGranularity(t *testing.T) {
	setupFlowControllerTestDB(t)

	for _, testCase := range []struct {
		name    string
		handler gin.HandlerFunc
		target  string
	}{
		{"all", app.NewHTTP().AdminQuota.Handle, "/api/data?start_timestamp=1000&end_timestamp=2000&granularity=quarter"},
		{"self", app.NewHTTP().OwnQuota.Handle, "/api/data/self?start_timestamp=1000&end_timestamp=2000&granularity=quarter"},
		{"users", app.NewHTTP().UserQuota.Handle, "/api/data/users?start_timestamp=1000&end_timestamp=2000&granularity=quarter"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := requestQuotaDates(t, testCase.handler, testCase.target, 1)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			payload := decodeQuotaDatesResponse(t, recorder)
			assert.False(t, payload.Success)
			assert.Equal(t, "invalid_granularity", payload.Code)
			assert.Equal(t, "granularity must be one of hour, day, week, month", payload.Message)
		})
	}
}

func TestGetUserQuotaDatesKeepsLegacyRowsWithoutGranularity(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := requestQuotaDates(t, app.NewHTTP().OwnQuota.Handle, "/api/data/self?start_timestamp=1000&end_timestamp=2000", 1)

	require.Equal(t, http.StatusOK, recorder.Code)
	payload := decodeQuotaDatesResponse(t, recorder)
	require.True(t, payload.Success, payload.Message)
	assert.Equal(t, []quotaDateRow{
		{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: 1100, Count: 2, Quota: 100, TokenUsed: 40},
	}, payload.Data)
}

func TestGetUserQuotaDatesBucketsRowsByGranularity(t *testing.T) {
	setupFlowControllerTestDB(t)
	require.NoError(t, model.DB.Create(&model.QuotaData{UserID: 1, Username: "alice", TokenID: 11,
		ModelName: "gpt-a", CreatedAt: 90000, Count: 5, Quota: 250, TokenUsed: 90}).Error)

	recorder := requestQuotaDates(t, app.NewHTTP().OwnQuota.Handle, "/api/data/self?start_timestamp=1000&end_timestamp=100000&granularity=day", 1)

	require.Equal(t, http.StatusOK, recorder.Code)
	payload := decodeQuotaDatesResponse(t, recorder)
	require.True(t, payload.Success, payload.Message)
	assert.Equal(t, []quotaDateRow{
		{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: 0, Count: 2, Quota: 100, TokenUsed: 40},
		{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: 86400, Count: 5, Quota: 250, TokenUsed: 90},
	}, payload.Data)
}

func TestGetAllQuotaDatesBucketsRowsWithoutUserDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := requestQuotaDates(t, app.NewHTTP().AdminQuota.Handle, "/api/data?start_timestamp=1000&end_timestamp=2000&granularity=week", 1)

	require.Equal(t, http.StatusOK, recorder.Code)
	payload := decodeQuotaDatesResponse(t, recorder)
	require.True(t, payload.Success, payload.Message)
	assert.Equal(t, []quotaDateRow{
		{ModelName: "gpt-a", CreatedAt: -259200, Count: 2, Quota: 100, TokenUsed: 40},
		{ModelName: "gpt-b", CreatedAt: -259200, Count: 1, Quota: 70, TokenUsed: 30},
	}, payload.Data)
}

func TestGetAllQuotaDatesRejectsMonthSpanBeyondCap(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := requestQuotaDates(t, app.NewHTTP().AdminQuota.Handle, "/api/data?start_timestamp=1577836800&end_timestamp=1896134400&granularity=month",
		1)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	payload := decodeQuotaDatesResponse(t, recorder)
	assert.False(t, payload.Success)
	assert.Equal(t, "month_span_exceeded", payload.Code)
}

func TestGetAllQuotaDatesRejectsNonPositiveRangeOnlyWhenBucketed(t *testing.T) {
	setupFlowControllerTestDB(t)

	for _, testCase := range []struct {
		name   string
		target string
	}{
		{"missing start_timestamp", "/api/data?end_timestamp=1790000000&granularity=month"},
		{"missing end_timestamp", "/api/data?start_timestamp=1000&granularity=day"},
		{"non-numeric start_timestamp", "/api/data?start_timestamp=abc&end_timestamp=2000&granularity=day"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := requestQuotaDates(t, app.NewHTTP().AdminQuota.Handle, testCase.target, 1)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			payload := decodeQuotaDatesResponse(t, recorder)
			assert.False(t, payload.Success)
			assert.Equal(t, "invalid_params", payload.Code)
		})
	}

	t.Run("legacy path keeps its unvalidated behaviour", func(t *testing.T) {
		recorder := requestQuotaDates(t, app.NewHTTP().AdminQuota.Handle, "/api/data?end_timestamp=1790000000", 1)

		require.Equal(t, http.StatusOK, recorder.Code)
		payload := decodeQuotaDatesResponse(t, recorder)
		require.True(t, payload.Success, payload.Message)
		assert.Equal(t, []quotaDateRow{
			{ModelName: "gpt-a", CreatedAt: 1100, Count: 2, Quota: 100, TokenUsed: 40},
			{ModelName: "gpt-b", CreatedAt: 1200, Count: 1, Quota: 70, TokenUsed: 30},
		}, payload.Data)
	})

	t.Run("legacy path still returns an empty list without end_timestamp", func(t *testing.T) {
		recorder := requestQuotaDates(t, app.NewHTTP().AdminQuota.Handle, "/api/data?start_timestamp=1000", 1)

		require.Equal(t, http.StatusOK, recorder.Code)
		payload := decodeQuotaDatesResponse(t, recorder)
		require.True(t, payload.Success, payload.Message)
		assert.Empty(t, payload.Data)
	})
}

// The /api/data/users rows carry the legacy dimension set of that endpoint —
// username and the bucket start only — so the raw JSON keys are asserted rather
// than decoded into a struct that would hide an extra or a missing field.
func decodeQuotaDatesRows(t *testing.T, recorder *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var payload struct {
		Success bool             `json:"success"`
		Message string           `json:"message"`
		Data    []map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	return payload.Data
}

func TestGetQuotaDatesByUserKeepsLegacyRowsWithoutGranularity(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := requestQuotaDates(t, app.NewHTTP().UserQuota.Handle, "/api/data/users?start_timestamp=1000&end_timestamp=2000", 1)

	require.Equal(t, http.StatusOK, recorder.Code)
	rows := decodeQuotaDatesRows(t, recorder)
	assert.Equal(t, []map[string]any{
		{"id": float64(0), "user_id": float64(0), "username": "alice", "model_name": "", "created_at": float64(1100),
			"use_group": "", "token_id": float64(0), "channel_id": float64(0), "node_name": "",
			"token_used": float64(40), "count": float64(2), "quota": float64(100)},
		{"id": float64(0), "user_id": float64(0), "username": "bob", "model_name": "", "created_at": float64(1200),
			"use_group": "", "token_id": float64(0), "channel_id": float64(0), "node_name": "",
			"token_used": float64(30), "count": float64(1), "quota": float64(70)},
	}, rows)
}

func TestGetQuotaDatesByUserBucketsRowsWithoutModelDimension(t *testing.T) {
	setupFlowControllerTestDB(t)
	require.NoError(t, model.DB.Create(&model.QuotaData{UserID: 1, Username: "alice", TokenID: 11,
		ModelName: "gpt-b", CreatedAt: 1100, Count: 3, Quota: 30, TokenUsed: 10}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{UserID: 1, Username: "alice", TokenID: 11,
		ModelName: "gpt-a", CreatedAt: 90000, Count: 5, Quota: 250, TokenUsed: 90}).Error)

	recorder := requestQuotaDates(t, app.NewHTTP().UserQuota.Handle, "/api/data/users?start_timestamp=1000&end_timestamp=100000&granularity=day", 1)

	require.Equal(t, http.StatusOK, recorder.Code)
	rows := decodeQuotaDatesRows(t, recorder)
	assert.Equal(t, []map[string]any{
		{"username": "alice", "created_at": float64(0), "token_used": float64(50), "count": float64(5), "quota": float64(130)},
		{"username": "bob", "created_at": float64(0), "token_used": float64(30), "count": float64(1), "quota": float64(70)},
		{"username": "alice", "created_at": float64(86400), "token_used": float64(90), "count": float64(5), "quota": float64(250)},
	}, rows)
}

func TestGetQuotaDatesByUserRejectsMonthSpanBeyondCap(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := requestQuotaDates(t, app.NewHTTP().UserQuota.Handle, "/api/data/users?start_timestamp=1577836800&end_timestamp=1896134400&granularity=month",

		1)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	payload := decodeQuotaDatesResponse(t, recorder)
	assert.False(t, payload.Success)
	assert.Equal(t, "month_span_exceeded", payload.Code)
}

func TestGetQuotaDatesByUserRejectsNonPositiveRangeOnlyWhenBucketed(t *testing.T) {
	setupFlowControllerTestDB(t)

	for _, testCase := range []struct {
		name   string
		target string
	}{
		{"missing start_timestamp", "/api/data/users?end_timestamp=1790000000&granularity=month"},
		{"missing end_timestamp", "/api/data/users?start_timestamp=1000&granularity=day"},
		{"non-numeric start_timestamp", "/api/data/users?start_timestamp=abc&end_timestamp=2000&granularity=day"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := requestQuotaDates(t, app.NewHTTP().UserQuota.Handle, testCase.target, 1)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			payload := decodeQuotaDatesResponse(t, recorder)
			assert.False(t, payload.Success)
			assert.Equal(t, "invalid_params", payload.Code)
		})
	}

	t.Run("legacy path keeps its unvalidated behaviour", func(t *testing.T) {
		recorder := requestQuotaDates(t, app.NewHTTP().UserQuota.Handle, "/api/data/users?end_timestamp=1790000000", 1)

		require.Equal(t, http.StatusOK, recorder.Code)
		payload := decodeQuotaDatesResponse(t, recorder)
		require.True(t, payload.Success, payload.Message)
		assert.Equal(t, []quotaDateRow{
			{Username: "alice", CreatedAt: 1100, Count: 2, Quota: 100, TokenUsed: 40},
			{Username: "bob", CreatedAt: 1200, Count: 1, Quota: 70, TokenUsed: 30},
		}, payload.Data)
	})
}

func TestQuotaDatesContractMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := ""
			if dialect != "sqlite" {
				dsn = os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
				if dsn == "" {
					t.Skip("host-only: database DSN not configured")
				}
			}
			db, _ := newAuditTestDatabase(t, dialect, dsn)
			previousDB, previousLog := model.DB, model.LOG_DB
			model.DB, model.LOG_DB = db, db
			previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
			kind := common.DatabaseTypeSQLite
			if dialect == "mysql" {
				kind = common.DatabaseTypeMySQL
			}
			if dialect == "postgres" {
				kind = common.DatabaseTypePostgreSQL
			}
			common.SetDatabaseTypes(kind, kind)
			t.Cleanup(func() {
				model.DB, model.LOG_DB = previousDB, previousLog
				common.SetDatabaseTypes(previousMainType, previousLogType)
			})
			require.NoError(t, db.AutoMigrate(&model.QuotaData{}))

			versionSQL := "SELECT version()"
			if dialect == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
			t.Logf("database version: %s", version)

			fixtures := []model.QuotaData{
				{UserID: 1, Username: "alice", ModelName: "a", TokenID: 11, CreatedAt: 1709168400, Count: 1, Quota: 10, TokenUsed: 100},
				{UserID: 1, Username: "alice", ModelName: "a", TokenID: 12, CreatedAt: 1709172000, Count: 2, Quota: 20, TokenUsed: 200},
				{UserID: 1, Username: "alice", ModelName: "a", TokenID: 11, CreatedAt: 1709211600, Count: 4, Quota: 40, TokenUsed: 400},
				{UserID: 1, Username: "alice", ModelName: "a", TokenID: 11, CreatedAt: 1709251200, Count: 8, Quota: 80, TokenUsed: 800},
				{UserID: 1, Username: "alice", ModelName: "b", TokenID: 11, CreatedAt: 1709168400, Count: 16, Quota: 160, TokenUsed: 1600},
				{UserID: 2, Username: "bob", ModelName: "a", TokenID: 22, CreatedAt: 1709168400, Count: 32, Quota: 320, TokenUsed: 3200},
				{UserID: 2, Username: "bob", ModelName: "b", TokenID: 22, CreatedAt: 1709211600, Count: 0, Quota: 0, TokenUsed: 0},
			}
			for _, row := range fixtures {
				require.NoError(t, db.Create(&row).Error)
			}
			for _, timestamp := range []int64{1709164800, 1709254800} {
				row := model.QuotaData{UserID: 1, Username: "alice", ModelName: "a", TokenID: 11, CreatedAt: timestamp, Count: 999, Quota: 999, TokenUsed: 999}
				require.NoError(t, db.Create(&row).Error)
			}
			for _, zone := range []string{"UTC", "offset"} {
				t.Run(zone, func(t *testing.T) {
					switch dialect {
					case "mysql":
						offset := "+00:00"
						if zone == "offset" {
							offset = "+05:30"
						}
						require.NoError(t, db.Exec("SET time_zone = ?", offset).Error)
						var mode string
						require.NoError(t, db.Raw("SELECT @@sql_mode").Scan(&mode).Error)
						require.Contains(t, mode, "ONLY_FULL_GROUP_BY")
					case "postgres":
						if zone == "offset" {
							require.NoError(t, db.Exec("SET TIME ZONE 'Pacific/Honolulu'").Error)
						} else {
							require.NoError(t, db.Exec("SET TIME ZONE 'UTC'").Error)
						}
					}
					for _, endpoint := range []struct {
						name, path, filter string
						handler            gin.HandlerFunc
					}{
						{"all", "/api/data/", "", app.NewHTTP().AdminQuota.Handle},
						{"username", "/api/data/", "&username=alice", app.NewHTTP().AdminQuota.Handle},
						{"users", "/api/data/users", "", app.NewHTTP().UserQuota.Handle},
						{"self", "/api/data/self", "", app.NewHTTP().OwnQuota.Handle},
						{"token", "/api/data/self", "&token_id=11", app.NewHTTP().OwnQuota.Handle},
						{"foreign_token", "/api/data/self", "&token_id=22", app.NewHTTP().OwnQuota.Handle},
					} {
						t.Run(endpoint.name, func(t *testing.T) {
							target := endpoint.path + "?start_timestamp=1709168400&end_timestamp=1709251200" + endpoint.filter
							legacy := requestQuotaDates(t, endpoint.handler, target, 1)
							empty := requestQuotaDates(t, endpoint.handler, target+"&granularity=", 1)
							require.Equal(t, http.StatusOK, legacy.Code)
							assert.JSONEq(t, legacy.Body.String(), empty.Body.String())
							for _, granularity := range []string{"hour", "day", "week", "month"} {
								t.Run(granularity, func(t *testing.T) {
									expectedByKey := map[string]quotaDateRow{}
									for _, row := range fixtures {
										if (endpoint.name == "username" || endpoint.name == "self" || endpoint.name == "token" || endpoint.name == "foreign_token") && row.UserID != 1 {
											continue
										}
										if endpoint.name == "token" && row.TokenID != 11 {
											continue
										}
										if endpoint.name == "foreign_token" && row.TokenID != 22 {
											continue
										}
										bucket := row.CreatedAt
										switch granularity {
										case "day":
											bucket = 1709164800
											if row.CreatedAt == 1709251200 {
												bucket = 1709251200
											}
										case "week":
											bucket = 1708905600
										case "month":
											bucket = 1706745600
											if row.CreatedAt == 1709251200 {
												bucket = 1709251200
											}
										}
										value := quotaDateRow{CreatedAt: bucket}
										if endpoint.name != "users" {
											value.ModelName = row.ModelName
										}
										if endpoint.name != "all" {
											value.Username = row.Username
										}
										if endpoint.name != "all" && endpoint.name != "users" {
											value.UserID = row.UserID
										}
										key := fmt.Sprintf("%d/%s/%s/%d", value.UserID, value.Username, value.ModelName, bucket)
										previous := expectedByKey[key]
										value.Count = previous.Count + row.Count
										value.Quota = previous.Quota + row.Quota
										value.TokenUsed = previous.TokenUsed + row.TokenUsed
										expectedByKey[key] = value
									}
									expected := make([]quotaDateRow, 0, len(expectedByKey))
									for _, row := range expectedByKey {
										expected = append(expected, row)
									}
									recorder := requestQuotaDates(t, endpoint.handler, target+"&granularity="+granularity, 1)
									require.Equal(t, http.StatusOK, recorder.Code)
									payload := decodeQuotaDatesResponse(t, recorder)
									require.True(t, payload.Success, payload.Message)
									assert.ElementsMatch(t, expected, payload.Data)
									assert.True(t, sort.SliceIsSorted(payload.Data, func(i, j int) bool {
										first, second := payload.Data[i], payload.Data[j]
										if first.CreatedAt != second.CreatedAt {
											return first.CreatedAt < second.CreatedAt
										}
										if endpoint.name == "users" {
											return first.Username < second.Username
										}
										return first.ModelName < second.ModelName
									}))
									t.Logf("response %s/%s legacy_rows=%d aggregate_rows=%d legacy_bytes=%d aggregate_bytes=%d",
										endpoint.name, granularity, len(decodeQuotaDatesResponse(t, legacy).Data), len(payload.Data),
										legacy.Body.Len(), recorder.Body.Len())
									if output := os.Getenv("USAGE_CONTRACT_OUTPUT_DIR"); output != "" && dialect == "sqlite" && zone == "UTC" {
										require.NoError(t, os.MkdirAll(output, 0700))
										require.NoError(t, os.WriteFile(filepath.Join(output, endpoint.name+"-legacy.json"), legacy.Body.Bytes(), 0600))
										require.NoError(t, os.WriteFile(filepath.Join(output, endpoint.name+"-"+granularity+".json"), recorder.Body.Bytes(), 0600))
									}
								})
							}
							for _, suffix := range []string{"&granularity=hour", "&granularity=day", "&granularity=week", "&granularity=month"} {
								noRows := requestQuotaDates(t, endpoint.handler, strings.Replace(target, "1709251200", "1709168000", 1)+suffix, 1)
								require.Equal(t, http.StatusOK, noRows.Code)
								assert.Empty(t, decodeQuotaDatesResponse(t, noRows).Data)
							}
						})
					}
				})
			}
		})
	}
}

func TestQuotaDatesAccessAndSelfRestrictions(t *testing.T) {
	user, pat := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.AutoMigrate(&model.QuotaData{}))
	router := gin.New()
	router.GET("/api/data/", middleware.AdminAuth(), app.NewHTTP().AdminQuota.Handle)
	router.GET("/api/data/users", middleware.AdminAuth(), app.NewHTTP().UserQuota.Handle)
	router.GET("/api/data/self", middleware.UserAuth(), app.NewHTTP().OwnQuota.Handle)
	for _, path := range []string{"/api/data/", "/api/data/users", "/api/data/self"} {
		assert.Equal(t, http.StatusUnauthorized, auditRequest(router, http.MethodGet, path+"?granularity=day", "").Code)
	}
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleCommonUser).Error)
	for _, path := range []string{"/api/data/", "/api/data/users"} {
		assert.Equal(t, http.StatusForbidden, auditRequest(router, http.MethodGet, path+"?granularity=day", pat).Code)
	}

	require.NoError(t, model.DB.Create(&model.QuotaData{UserID: user.Id, Username: user.Username,
		ModelName: "own", TokenID: 11, CreatedAt: 1100, Count: 2, Quota: 20, TokenUsed: 200}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{UserID: user.Id + 1, Username: "foreign",
		ModelName: "foreign", TokenID: 22, CreatedAt: 1100, Count: 99, Quota: 990, TokenUsed: 9900}).Error)
	for _, granularity := range []string{"hour", "day", "week", "month"} {
		response := auditRequest(router, http.MethodGet, "/api/data/self?start_timestamp=1000&end_timestamp=2000&granularity="+granularity, pat)
		require.Equal(t, http.StatusOK, response.Code)
		ownRows := decodeQuotaDatesResponse(t, response).Data
		require.Len(t, ownRows, 1)
		assert.Equal(t, user.Id, ownRows[0].UserID)
		assert.Equal(t, "own", ownRows[0].ModelName)
		assert.Equal(t, 20, ownRows[0].Quota)
		foreign := auditRequest(router, http.MethodGet, "/api/data/self?start_timestamp=1000&end_timestamp=2000&token_id=22&granularity="+granularity, pat)
		require.Equal(t, http.StatusOK, foreign.Code)
		assert.Empty(t, decodeQuotaDatesResponse(t, foreign).Data)

		for _, testCase := range []struct{ query, code string }{
			{"start_timestamp=1000&end_timestamp=2593001", "time_span_exceeded"},
			{"start_timestamp=1000&end_timestamp=2000&token_id=0", "invalid_token_id"},
			{"start_timestamp=1000&end_timestamp=2000&token_id=-1", "invalid_token_id"},
			{"start_timestamp=1000&end_timestamp=2000&token_id=abc", "invalid_token_id"},
		} {
			response := auditRequest(router, http.MethodGet, "/api/data/self?"+testCase.query+"&granularity="+granularity, pat)
			require.Equal(t, http.StatusBadRequest, response.Code)
			assert.Equal(t, testCase.code, decodeQuotaDatesResponse(t, response).Code)
		}
	}
}

func TestQuotaDatesAdminRangeErrors(t *testing.T) {
	setupFlowControllerTestDB(t)
	for _, endpoint := range []struct {
		path    string
		handler gin.HandlerFunc
	}{
		{"/api/data/", app.NewHTTP().AdminQuota.Handle}, {"/api/data/users", app.NewHTTP().UserQuota.Handle},
	} {
		for _, testCase := range []struct{ query, code string }{
			{"end_timestamp=1790000000&granularity=month", "invalid_params"},
			{"start_timestamp=1000&granularity=day", "invalid_params"},
			{"start_timestamp=abc&end_timestamp=2000&granularity=day", "invalid_params"},
			{"start_timestamp=1577836800&end_timestamp=1896134400&granularity=month", "month_span_exceeded"},
		} {
			response := requestQuotaDates(t, endpoint.handler, endpoint.path+"?"+testCase.query, 1)
			require.Equal(t, http.StatusBadRequest, response.Code)
			payload := decodeQuotaDatesResponse(t, response)
			assert.False(t, payload.Success)
			assert.Equal(t, testCase.code, payload.Code)
		}
		for _, query := range []string{"end_timestamp=1790000000", "start_timestamp=abc&end_timestamp=2000", "start_timestamp=1000"} {
			legacy := requestQuotaDates(t, endpoint.handler, endpoint.path+"?"+query, 1)
			empty := requestQuotaDates(t, endpoint.handler, endpoint.path+"?"+query+"&granularity=", 1)
			require.Equal(t, http.StatusOK, legacy.Code)
			assert.True(t, decodeQuotaDatesResponse(t, legacy).Success)
			assert.JSONEq(t, legacy.Body.String(), empty.Body.String())
		}
	}
}

func TestAggregateEndpointsValidateScopeAndAccess(t *testing.T) {
	setupFlowControllerTestDB(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	for _, fixture := range []struct {
		id, role  int
		name, pat string
	}{
		{1, common.RoleCommonUser, "aggregate-member", "aggregate-test-member-pat"},
		{2, common.RoleAdminUser, "aggregate-admin", "aggregate-test-admin-pat"},
	} {
		pat := fixture.pat
		require.NoError(t, db.Create(&model.User{Id: fixture.id, Username: fixture.name, Role: fixture.role,
			Status: common.UserStatusEnabled, AccessToken: &pat, AuthVersion: 1, AffCode: fixture.name}).Error)
	}
	engine := gin.New()
	engine.POST("/api/log/aggregate", middleware.AdminAuth(), app.NewHTTP().LogAggregate.Handle)
	engine.POST("/api/usage/aggregate", middleware.AdminAuth(), app.NewHTTP().UsageAggregate.Handle)
	for _, path := range []string{"/api/log/aggregate", "/api/usage/aggregate"} {
		valid := `{"user_ids":[1],"start_timestamp":1000,"end_timestamp":2000,"granularity":"hour","group_by":"user"}`
		for _, check := range []struct {
			name, body, pat string
			status          int
		}{
			{"anonymous", valid, "", http.StatusUnauthorized},
			{"ordinary", valid, "aggregate-test-member-pat", http.StatusForbidden},
			{"admin", valid, "aggregate-test-admin-pat", http.StatusOK},
			{"empty", `{"user_ids":[],"start_timestamp":1000,"end_timestamp":2000}`, "aggregate-test-admin-pat", http.StatusBadRequest},
			{"missing", `{}`, "aggregate-test-admin-pat", http.StatusBadRequest},
			{"invalid", `{"user_ids":[0],"start_timestamp":1000,"end_timestamp":2000}`, "aggregate-test-admin-pat", http.StatusBadRequest},
			{"fractional", `{"user_ids":[1.5],"start_timestamp":1000,"end_timestamp":2000}`, "aggregate-test-admin-pat", http.StatusBadRequest},
			{"unknown", `{"user_ids":[999],"start_timestamp":1000,"end_timestamp":2000,"granularity":"hour","group_by":"user"}`,
				"aggregate-test-admin-pat", http.StatusNotFound},
			{"oversize", strings.Repeat(" ", 16<<10) + valid, "aggregate-test-admin-pat", http.StatusRequestEntityTooLarge},
		} {
			t.Run(path+"/"+check.name, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(check.body))
				request.Header.Set("Accept-Language", "zh-CN")
				request.Header.Set("Content-Type", "application/json")
				if check.pat != "" {
					request.Header.Set("Authorization", "Bearer "+check.pat)
				}
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, request)
				assert.Equal(t, check.status, response.Code, response.Body.String())
				if path == "/api/usage/aggregate" && check.name == "admin" {
					var body struct {
						Data []aggregate.Row `json:"data"`
					}
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
					require.Len(t, body.Data, 1)
					assert.Equal(t, int64(100), body.Data[0].Quota)
					assert.Equal(t, 1, *body.Data[0].UserID)
				}
			})
		}
	}
}

func TestAggregateResponseSizeIsAnErrorInsteadOfPartialSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	name := strings.Repeat("x", 4<<20)
	transport.RespondAggregate(ctx, []aggregate.Row{{ModelName: &name}}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, response.Code)
	var body struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.False(t, body.Success)
	assert.Equal(t, "aggregate_response_too_large", body.Code)
}

// Username and the other unsupported dimensions must not become new filters.
func TestBucketedQuotaIgnoresUnsupportedQueryFilters(t *testing.T) {
	setupFlowControllerTestDB(t)
	handlers := app.NewHTTP()
	for _, tc := range []struct {
		name, path, ignored string
		handler             gin.HandlerFunc
		want                int
	}{
		{"users", "/api/data/users", "&token_id=999", handlers.UserQuota.Handle, 2},
		{"self", "/api/data/self", "", handlers.OwnQuota.Handle, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := tc.path + "?start_timestamp=1000&end_timestamp=2000&granularity=hour"
			baseline := requestQuotaDates(t, tc.handler, query, 1)
			payload := decodeQuotaDatesResponse(t, baseline)
			require.True(t, payload.Success)
			require.Len(t, payload.Data, tc.want)
			withIgnored := requestQuotaDates(t, tc.handler,
				query+"&username=other&channel=999&group=other&model=other&model_name=other"+tc.ignored, 1)
			assert.Equal(t, baseline.Code, withIgnored.Code)
			assert.Equal(t, baseline.Body.String(), withIgnored.Body.String())
		})
	}
}
