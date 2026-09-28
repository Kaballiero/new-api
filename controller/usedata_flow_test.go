package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flowQuotaResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    []model.FlowQuotaData `json:"data"`
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

	GetAllFlowQuotaDates(ctx)

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

	GetAllFlowQuotaDates(ctx)

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

	GetUserFlowQuotaDates(ctx)

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
	require.NoError(t, model.DB.Create(&model.QuotaData{UserID: 1, Username: "alice", TokenID: 11, ModelName: "gpt-old", CreatedAt: 900, Count: 3, Quota: 150, TokenUsed: 60}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{UserID: 1, Username: "alice", TokenID: 12, ModelName: "gpt-secondary", CreatedAt: 1100, Count: 4, Quota: 200, TokenUsed: 80}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/self?start_timestamp=1000&end_timestamp=2000&token_id=11", nil)

	GetUserQuotaDates(ctx)

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

	GetUserQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Empty(t, payload.Data)
}

func TestGetUserQuotaDatesRejectsNonPositiveTokenID(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/self?start_timestamp=1000&end_timestamp=2000&token_id=0", nil)

	GetUserQuotaDates(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestGetUserFlowQuotaDatesRejectsInvalidTimeRange(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=bad&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

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
		{"all", GetAllQuotaDates, "/api/data?start_timestamp=1000&end_timestamp=2000&granularity=quarter"},
		{"self", GetUserQuotaDates, "/api/data/self?start_timestamp=1000&end_timestamp=2000&granularity=quarter"},
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

	recorder := requestQuotaDates(t, GetUserQuotaDates, "/api/data/self?start_timestamp=1000&end_timestamp=2000", 1)

	require.Equal(t, http.StatusOK, recorder.Code)
	payload := decodeQuotaDatesResponse(t, recorder)
	require.True(t, payload.Success, payload.Message)
	assert.Equal(t, []quotaDateRow{
		{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: 1100, Count: 2, Quota: 100, TokenUsed: 40},
	}, payload.Data)
}

func TestGetUserQuotaDatesBucketsRowsByGranularity(t *testing.T) {
	setupFlowControllerTestDB(t)
	require.NoError(t, model.DB.Create(&model.QuotaData{UserID: 1, Username: "alice", TokenID: 11, ModelName: "gpt-a", CreatedAt: 90000, Count: 5, Quota: 250, TokenUsed: 90}).Error)

	recorder := requestQuotaDates(t, GetUserQuotaDates, "/api/data/self?start_timestamp=1000&end_timestamp=100000&granularity=day", 1)

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

	recorder := requestQuotaDates(t, GetAllQuotaDates, "/api/data?start_timestamp=1000&end_timestamp=2000&granularity=week", 1)

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

	recorder := requestQuotaDates(t, GetAllQuotaDates, "/api/data?start_timestamp=1577836800&end_timestamp=1896134400&granularity=month", 1)

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
			recorder := requestQuotaDates(t, GetAllQuotaDates, testCase.target, 1)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			payload := decodeQuotaDatesResponse(t, recorder)
			assert.False(t, payload.Success)
			assert.Equal(t, "invalid_params", payload.Code)
		})
	}

	t.Run("legacy path keeps its unvalidated behaviour", func(t *testing.T) {
		recorder := requestQuotaDates(t, GetAllQuotaDates, "/api/data?end_timestamp=1790000000", 1)

		require.Equal(t, http.StatusOK, recorder.Code)
		payload := decodeQuotaDatesResponse(t, recorder)
		require.True(t, payload.Success, payload.Message)
		assert.Equal(t, []quotaDateRow{
			{ModelName: "gpt-a", CreatedAt: 1100, Count: 2, Quota: 100, TokenUsed: 40},
			{ModelName: "gpt-b", CreatedAt: 1200, Count: 1, Quota: 70, TokenUsed: 30},
		}, payload.Data)
	})

	t.Run("legacy path still returns an empty list without end_timestamp", func(t *testing.T) {
		recorder := requestQuotaDates(t, GetAllQuotaDates, "/api/data?start_timestamp=1000", 1)

		require.Equal(t, http.StatusOK, recorder.Code)
		payload := decodeQuotaDatesResponse(t, recorder)
		require.True(t, payload.Success, payload.Message)
		assert.Empty(t, payload.Data)
	})
}
