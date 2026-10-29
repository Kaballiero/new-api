package usage

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/getapi_aggregate"
	"github.com/QuantumNous/new-api/internal/usage/getapi_logs"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetAPIUsageDatabaseMatrix(t *testing.T) {
	runOnGetAPIUsageDialects(t, func(t *testing.T) {
		var version string
		switch common.MainDatabaseType() {
		case common.DatabaseTypeSQLite:
			require.NoError(t, model.DB.Raw("SELECT sqlite_version()").Scan(&version).Error)
		case common.DatabaseTypeMySQL:
			require.NoError(t, model.DB.Raw("SELECT VERSION()").Scan(&version).Error)
		case common.DatabaseTypePostgreSQL:
			require.NoError(t, model.DB.Raw("SHOW server_version").Scan(&version).Error)
		}
		t.Logf("database version: %s", version)
		start := time.Date(2026, 10, 1, 23, 0, 0, 0, time.UTC).Unix()
		end, now := start+7200, start+7200
		deleted := model.Token{UserId: 1, Key: "deleted-key", Name: "Deleted key"}
		require.NoError(t, model.DB.Create(&deleted).Error)
		require.NoError(t, model.DB.Delete(&deleted).Error)
		for _, row := range []model.QuotaData{
			{UserID: 1, TokenID: deleted.Id, CreatedAt: start, ModelName: "Model", Quota: 100, Count: 1, TokenUsed: 11},
			{UserID: 1, TokenID: deleted.Id, CreatedAt: start, ModelName: "Model", Quota: -30},
			{UserID: 1, CreatedAt: end, ModelName: "Model", Quota: 20, Count: 1, TokenUsed: 7},
			{UserID: 2, TokenID: 22, CreatedAt: end, ModelName: "Model", Quota: 50, Count: 1, TokenUsed: 5},
			{UserID: 3, CreatedAt: end, ModelName: "outside", Quota: 999},
			{UserID: 1, CreatedAt: start - 1, ModelName: "outside", Quota: 999},
			{UserID: 1, CreatedAt: end + 1, ModelName: "outside", Quota: 999},
		} {
			seedBucketQuotaData(t, row)
		}
		for _, row := range []model.Log{
			{Id: 10, UserId: 1, TokenId: deleted.Id, CreatedAt: now - 60, Type: 2, ModelName: "Model", TokenName: "old name", PromptTokens: 3, CompletionTokens: 4, ChannelId: 91, Other: `{"public":0,"admin_info":{"secret":true},"root_info":{"secret":true},"channel_id":91}`},
			{Id: 20, UserId: 1, TokenId: deleted.Id, CreatedAt: now - 59, Type: 6, ModelName: "Model", Quota: -30},
			{Id: 30, UserId: 2, TokenId: 22, CreatedAt: now, Type: 2, PromptTokens: 11},
			{Id: 40, UserId: 1, TokenId: deleted.Id, CreatedAt: now, Type: 2, ModelName: "Model", PromptTokens: 5, CompletionTokens: 6},
			{Id: 50, UserId: 1, CreatedAt: now - 61, Type: 2, ModelName: "Model"},
			{Id: 60, UserId: 1, CreatedAt: now, Type: 5, ModelName: "Model"},
			{Id: 70, UserId: 1, TokenId: deleted.Id, CreatedAt: now, Type: 2, ModelName: "Model%"},
			{Id: 80, UserId: 1, CreatedAt: end + 1, Type: 6},
			{Id: 90, UserId: 1, CreatedAt: start - 1, Type: 6},
		} {
			require.NoError(t, model.LOG_DB.Create(&row).Error)
		}
		agg := getapi_aggregate.NewUsecase(Store{}, func() int64 { return now })
		q := getapi_aggregate.GetAPIUsageAggregateRequest{UserIDs: []int{1, 2, 999}, StartTimestamp: &start, EndTimestamp: &end, GroupBy: "model"}
		result, err := agg.Execute(q)
		require.NoError(t, err)
		require.Len(t, result.Rows, 2)
		assert.Equal(t, 1, result.Rows[0].UserID)
		assert.Equal(t, int64(90), result.Rows[0].Quota)
		assert.Equal(t, int64(2), result.Rows[0].Count)
		assert.Equal(t, int64(18), result.Rows[0].TokenUsed)
		assert.Equal(t, 2, result.Rows[1].UserID)
		assert.Equal(t, int64(50), result.Rows[1].Quota)
		assert.Equal(t, []getapi_aggregate.GetAPIUsageRecent{{UserID: 1, RPM: 3, TPM: 18}, {UserID: 2, RPM: 1, TPM: 11}, {UserID: 999}}, result.Recent)
		q.TokenID = &deleted.Id
		result, err = agg.Execute(q)
		require.NoError(t, err)
		require.Len(t, result.Rows, 1)
		assert.Equal(t, int64(70), result.Rows[0].Quota)
		assert.Zero(t, result.Recent[1].RPM)
		q.UserIDs, q.TokenID = []int{999}, nil
		empty, err := agg.Execute(q)
		require.NoError(t, err)
		assert.NotNil(t, empty.Rows)
		assert.Empty(t, empty.Rows)
		assert.Equal(t, []getapi_aggregate.GetAPIUsageRecent{{UserID: 999}}, empty.Recent)
		q.UserIDs = []int{1, 2, 999}
		q.TokenID, q.GroupBy = nil, "token"
		result, err = agg.Execute(q)
		require.NoError(t, err)
		require.Len(t, result.Rows, 3)
		assert.Equal(t, 0, *result.Rows[0].TokenID)
		assert.Nil(t, result.Rows[0].TokenName)
		require.NotNil(t, result.Rows[0].TokenDeleted)
		assert.False(t, *result.Rows[0].TokenDeleted)
		assert.Equal(t, deleted.Id, *result.Rows[1].TokenID)
		require.NotNil(t, result.Rows[1].TokenName)
		assert.Equal(t, "Deleted key", *result.Rows[1].TokenName)
		assert.True(t, *result.Rows[1].TokenDeleted)
		assert.Nil(t, result.Recent)
		for _, grouping := range []string{"time", "time_model"} {
			for _, granularity := range []string{"hour", "day", "week", "month"} {
				q.GroupBy, q.Granularity = grouping, &granularity
				result, err = agg.Execute(q)
				require.NoError(t, err)
				expected := 2
				if granularity == "hour" || granularity == "day" {
					expected = 3
				}
				require.Len(t, result.Rows, expected)
				expectedStart := map[string]int64{
					"hour":  start,
					"day":   start - start%86400,
					"week":  time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC).Unix(),
					"month": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Unix(),
				}[granularity]
				assert.Equal(t, expectedStart, *result.Rows[0].CreatedAt)
				assert.Equal(t, grouping == "time_model", result.Rows[0].ModelName != nil)
			}
		}

		logs := getapi_logs.NewUsecase(Store{})
		lq := getapi_logs.GetAPIUsageLogsRequest{UserID: 1, StartTimestamp: &start, EndTimestamp: &end, Limit: 2, WithTotal: true}
		page, err := logs.Execute(lq)
		require.NoError(t, err)
		require.Len(t, page.Items, 2)
		assert.Equal(t, []int{70, 50}, []int{page.Items[0].ID, page.Items[1].ID})
		assert.True(t, page.HasMore)
		require.NotNil(t, page.Total)
		assert.Equal(t, int64(5), *page.Total)
		before := page.Items[1].ID
		lq.BeforeID = &before
		second, err := logs.Execute(lq)
		require.NoError(t, err)
		require.Len(t, second.Items, 2)
		assert.Equal(t, []int{40, 20}, []int{second.Items[0].ID, second.Items[1].ID})
		assert.True(t, second.HasMore)
		assert.Equal(t, int64(5), *second.Total)
		before = second.Items[1].ID
		third, err := logs.Execute(lq)
		require.NoError(t, err)
		require.Len(t, third.Items, 1)
		assert.Equal(t, 10, third.Items[0].ID)
		assert.False(t, third.HasMore)
		require.JSONEq(t, `{"public":0}`, third.Items[0].Other)
		wire, err := common.Marshal(third.Items[0])
		require.NoError(t, err)
		assert.NotContains(t, string(wire), "channel")
		assert.NotContains(t, string(wire), "admin_info")
		assert.NotContains(t, string(wire), "root_info")
		after := third.Items[0].ID
		lq.BeforeID, lq.AfterID, lq.WithTotal = nil, &after, false
		back, err := logs.Execute(lq)
		require.NoError(t, err)
		assert.Equal(t, second.Items, back.Items)
		assert.True(t, back.HasMore)
		assert.Nil(t, back.Total)
		wire, err = common.Marshal(back)
		require.NoError(t, err)
		assert.NotContains(t, string(wire), `"total"`)

		after = back.Items[0].ID
		back, err = logs.Execute(lq)
		require.NoError(t, err)
		assert.Equal(t, page.Items, back.Items)
		assert.False(t, back.HasMore)
		lq.AfterID, lq.TokenID, lq.Limit = nil, &deleted.Id, 100
		filtered, err := logs.Execute(lq)
		require.NoError(t, err)
		require.Len(t, filtered.Items, 4)
		modelName := "Model"
		lq.ModelName = &modelName
		filtered, err = logs.Execute(lq)
		require.NoError(t, err)
		require.Len(t, filtered.Items, 3)
		assert.Equal(t, "old name", filtered.Items[2].TokenName)
		lq.UserID = 2
		filtered, err = logs.Execute(lq)
		require.NoError(t, err)
		assert.Empty(t, filtered.Items)
		lq.UserID, lq.TokenID, lq.Types = 1, nil, []int{5}
		filtered, err = logs.Execute(lq)
		require.NoError(t, err)
		require.Len(t, filtered.Items, 1)
		assert.Equal(t, 60, filtered.Items[0].ID)

		overflow := make([]model.QuotaData, domain.MaxAggregateRows+1)
		for i := range overflow {
			overflow[i] = model.QuotaData{UserID: 4, CreatedAt: start, ModelName: fmt.Sprintf("m-%05d", i)}
		}
		require.NoError(t, model.DB.CreateInBatches(overflow, 100).Error)
		router := gin.New()
		router.POST("/aggregate", getapi_aggregate.NewHandler(agg).Handle)
		response := httptest.NewRecorder()
		body := fmt.Sprintf(`{"user_ids":[4],"start_timestamp":%d,"end_timestamp":%d,"group_by":"model"}`, start, end)
		router.ServeHTTP(response, httptest.NewRequest("POST", "/aggregate", strings.NewReader(body)))
		assert.Equal(t, http.StatusUnprocessableEntity, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), "GETAPI_USAGE_TOO_LARGE")
		require.NoError(t, model.DB.Delete(&overflow[len(overflow)-1]).Error)
		accepted, err := agg.Execute(getapi_aggregate.GetAPIUsageAggregateRequest{
			UserIDs: []int{4}, StartTimestamp: &start, EndTimestamp: &end, GroupBy: "model",
		})
		require.NoError(t, err)
		assert.Len(t, accepted.Rows, domain.MaxAggregateRows)
		for _, span := range []int64{7 * 86400, 93 * 86400} {
			boundary := start + span
			request := getapi_aggregate.GetAPIUsageAggregateRequest{
				UserIDs: []int{999}, StartTimestamp: &start, EndTimestamp: &boundary, GroupBy: "model",
			}
			if span == 7*86400 {
				granularity := "hour"
				request.GroupBy, request.Granularity = "time", &granularity
			}
			_, err := agg.Execute(request)
			require.NoError(t, err)
			_, err = logs.Execute(getapi_logs.GetAPIUsageLogsRequest{UserID: 999, StartTimestamp: &start, EndTimestamp: &boundary, Limit: 100})
			require.NoError(t, err)
		}

	})
}

func TestGetAPIUsageValidationHTTP(t *testing.T) {
	router := gin.New()
	router.POST("/aggregate", getapi_aggregate.NewHandler(getapi_aggregate.NewUsecase(nil, nil)).Handle)
	router.POST("/logs", getapi_logs.NewHandler(getapi_logs.NewUsecase(nil)).Handle)
	for _, tc := range []struct{ path, body string }{
		{"aggregate", `{}`},
		{"aggregate", `{"user_ids":[1,1],"start_timestamp":0,"end_timestamp":1,"group_by":"model"}`},
		{"aggregate", `{"user_ids":[0],"start_timestamp":0,"end_timestamp":1,"group_by":"model"}`},
		{"aggregate", `{"user_ids":[],"start_timestamp":0,"end_timestamp":1,"group_by":"model"}`},
		{"aggregate", `{"user_ids":[1],"start_timestamp":2,"end_timestamp":1,"group_by":"model"}`},
		{"aggregate", `{"user_ids":[1],"start_timestamp":0,"end_timestamp":8035201,"group_by":"model"}`},
		{"aggregate", `{"user_ids":[1],"start_timestamp":0,"end_timestamp":604801,"group_by":"time","granularity":"hour"}`},
		{"aggregate", `{"user_ids":[1],"start_timestamp":0,"end_timestamp":1,"group_by":"time"}`},
		{"aggregate", `{"user_ids":[1],"start_timestamp":0,"end_timestamp":1,"group_by":"time_model","granularity":"bad"}`},
		{"aggregate", `{"user_ids":[1],"start_timestamp":0,"end_timestamp":1,"group_by":"model","granularity":"day"}`},
		{"aggregate", `{"user_ids":[1],"start_timestamp":0,"end_timestamp":1,"group_by":"token","granularity":""}`},
		{"aggregate", `{"user_ids":[1],"token_id":0,"start_timestamp":0,"end_timestamp":1,"group_by":"model"}`},
		{"aggregate", `{"user_ids":[1],"start_timestamp":0,"end_timestamp":1,"group_by":"user"}`},
		{"logs", `{"user_id":1,"start_timestamp":0,"end_timestamp":1,"limit":1,"before_id":1,"after_id":2}`},
		{"logs", `{"user_id":1,"start_timestamp":2,"end_timestamp":1,"limit":1}`},
		{"logs", `{"user_id":1,"start_timestamp":0,"end_timestamp":8035201,"limit":1}`},
		{"logs", `{"user_id":1,"start_timestamp":0,"end_timestamp":1,"limit":101}`},
		{"logs", `{"user_id":1,"start_timestamp":0,"end_timestamp":1,"limit":0}`},
		{"logs", `{"user_id":0,"start_timestamp":0,"end_timestamp":1,"limit":1}`},
		{"logs", `{"user_id":1,"start_timestamp":0,"end_timestamp":1,"limit":1,"before_id":0}`},
		{"logs", `{"user_id":1,"start_timestamp":0,"end_timestamp":1,"limit":1,"after_id":-1}`},
		{"logs", `{"user_id":1,"start_timestamp":0,"end_timestamp":1,"limit":1,"types":[]}`},
		{"logs", `{"user_id":1,"start_timestamp":0,"end_timestamp":1,"limit":1,"types":[8]}`},
		{"logs", `bad json`},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("POST", "/"+tc.path, strings.NewReader(tc.body)))
		assert.Equal(t, http.StatusBadRequest, response.Code, tc.body)
		assert.Contains(t, response.Body.String(), "GETAPI_USAGE_INVALID_QUERY")
	}
	ids := make([]int, 101)
	for i := range ids {
		ids[i] = i + 1
	}
	body, err := common.Marshal(map[string]any{"user_ids": ids, "start_timestamp": 0, "end_timestamp": 1, "group_by": "model"})
	require.NoError(t, err)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", "/aggregate", strings.NewReader(string(body))))
	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func TestGetAPIUsageLogsClickHouseUnsupported(t *testing.T) {
	previous := common.LogDatabaseType()
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)
	t.Cleanup(func() { common.SetLogDatabaseType(previous) })
	router := gin.New()
	router.POST("/logs", getapi_logs.NewHandler(getapi_logs.NewUsecase(Store{})).Handle)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", "/logs", strings.NewReader(`{"user_id":1,"start_timestamp":0,"end_timestamp":1,"limit":1}`)))
	assert.Equal(t, http.StatusNotImplemented, response.Code)
	assert.Contains(t, response.Body.String(), "GETAPI_USAGE_UNSUPPORTED")
}

func runOnGetAPIUsageDialects(t *testing.T, run func(*testing.T)) {
	t.Helper()
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			dsn := ""
			if kind != "sqlite" {
				dsn = os.Getenv("TEST_" + strings.ToUpper(kind) + "_DSN")
				if dsn == "" {
					t.Skip("host-only: database DSN not configured")
				}
				setupReadDatabase(t, kind, dsn)
				run(t)
				return
			}
			mainDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			logDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			for _, db := range []*gorm.DB{mainDB, logDB} {
				connection, err := db.DB()
				require.NoError(t, err)
				connection.SetMaxOpenConns(1)
				t.Cleanup(func() { assert.NoError(t, connection.Close()) })
			}
			require.NoError(t, mainDB.AutoMigrate(&model.QuotaData{}, &model.Token{}))
			require.NoError(t, logDB.AutoMigrate(&model.Log{}))
			previousDB, previousLogDB := model.DB, model.LOG_DB
			previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
			model.DB, model.LOG_DB = mainDB, logDB
			common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
			t.Cleanup(func() {
				model.DB, model.LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousMain, previousLog)
			})
			run(t)
		})
	}
}
