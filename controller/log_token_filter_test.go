package controller

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type selfLogFilterDatabase struct {
	name  string
	dsn   string
	type_ common.DatabaseType
}

func setupSelfLogTokenFilterTest(t *testing.T, database selfLogFilterDatabase) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	db, _ := newAuditTestDatabase(t, database.name, database.dsn)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(database.type_, database.type_)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		common.RedisEnabled = previousRedisEnabled
	})
	return db
}

func selfLogFilterDatabases(t *testing.T) []selfLogFilterDatabase {
	t.Helper()
	databases := []selfLogFilterDatabase{{name: "sqlite", type_: common.DatabaseTypeSQLite}}
	for _, database := range []struct {
		name  string
		env   string
		type_ common.DatabaseType
	}{
		{name: "postgres", env: "TEST_POSTGRES_DSN", type_: common.DatabaseTypePostgreSQL},
		{name: "mysql", env: "TEST_MYSQL_DSN", type_: common.DatabaseTypeMySQL},
	} {
		if dsn := os.Getenv(database.env); dsn != "" {
			databases = append(databases, selfLogFilterDatabase{name: database.name, dsn: dsn, type_: database.type_})
		}
	}
	return databases
}

func seedSelfLogTokenFilterData(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "owner", Password: "Password12", Group: "default", AffCode: "owner"}).Error)
	require.NoError(t, db.Create(&model.User{Id: 2, Username: "other", Password: "Password12", Group: "default", AffCode: "other"}).Error)
	for _, token := range []*model.Token{
		{Id: 11, UserId: 1, Name: "duplicate", Key: "owner-token-a", Status: common.TokenStatusEnabled, ExpiredTime: -1},
		{Id: 12, UserId: 1, Name: "duplicate", Key: "owner-token-unused", Status: common.TokenStatusEnabled, ExpiredTime: -1},
		{Id: 13, UserId: 1, Name: "renamed", Key: "owner-token-renamed", Status: common.TokenStatusEnabled, ExpiredTime: -1},
		{Id: 21, UserId: 2, Name: "duplicate", Key: "other-token", Status: common.TokenStatusEnabled, ExpiredTime: -1},
	} {
		require.NoError(t, db.Create(token).Error)
	}
	for _, log := range []*model.Log{
		{UserId: 1, TokenId: 11, TokenName: "duplicate", ModelName: "alpha", CreatedAt: 100},
		{UserId: 1, TokenId: 11, TokenName: "duplicate", ModelName: "beta", CreatedAt: 200},
		{UserId: 1, TokenId: 11, TokenName: "duplicate", ModelName: "alpha", CreatedAt: 300},
		{UserId: 1, TokenId: 11, TokenName: "duplicate", ModelName: "alpha", CreatedAt: 400},
		{UserId: 1, TokenId: 13, TokenName: "before-rename", ModelName: "alpha", CreatedAt: 500},
		{UserId: 2, TokenId: 21, TokenName: "duplicate", ModelName: "alpha", CreatedAt: 600},
	} {
		require.NoError(t, db.Create(log).Error)
	}
}

func TestGetUserLogsByTokenIdFiltersBeforeCountAndPagination(t *testing.T) {
	for _, database := range selfLogFilterDatabases(t) {
		t.Run(database.name, func(t *testing.T) {
			db := setupSelfLogTokenFilterTest(t, database)
			seedSelfLogTokenFilterData(t, db)

			firstPage, total, err := model.GetUserLogsByTokenId(1, 11, model.LogTypeUnknown, 150, 450, "alpha", "", 0, 1, "", "", "")
			require.NoError(t, err)
			require.Equal(t, int64(2), total)
			require.Len(t, firstPage, 1)
			assert.Equal(t, 11, firstPage[0].TokenId)
			assert.Equal(t, int64(400), firstPage[0].CreatedAt)

			secondPage, total, err := model.GetUserLogsByTokenId(1, 11, model.LogTypeUnknown, 150, 450, "alpha", "", 1, 1, "", "", "")
			require.NoError(t, err)
			require.Equal(t, int64(2), total)
			require.Len(t, secondPage, 1)
			assert.Equal(t, 11, secondPage[0].TokenId)
			assert.Equal(t, int64(300), secondPage[0].CreatedAt)

			renamedTokenLogs, total, err := model.GetUserLogsByTokenId(1, 13, model.LogTypeUnknown, 0, 0, "", "", 0, 20, "", "", "")
			require.NoError(t, err)
			require.Equal(t, int64(1), total)
			require.Len(t, renamedTokenLogs, 1)
			assert.Equal(t, "before-rename", renamedTokenLogs[0].TokenName)
		})
	}
}

func TestGetUserLogsTokenIdValidationAndOwnership(t *testing.T) {
	for _, database := range selfLogFilterDatabases(t) {
		t.Run(database.name, func(t *testing.T) {
			db := setupSelfLogTokenFilterTest(t, database)
			seedSelfLogTokenFilterData(t, db)

			for _, tokenId := range []string{"", "abc", "0", "-1", "999999999999999999999999"} {
				t.Run("invalid_"+tokenId, func(t *testing.T) {
					ctx, recorder := newSelfLogContext("/api/log/self?token_id=" + tokenId)
					GetUserLogs(ctx)
					require.Equal(t, http.StatusBadRequest, recorder.Code)
					assert.Equal(t, "invalid_params", decodeRestError(t, recorder).Code)
				})
			}

			for _, tokenId := range []string{"21", "999"} {
				t.Run("inaccessible_"+tokenId, func(t *testing.T) {
					ctx, recorder := newSelfLogContext("/api/log/self?token_id=" + tokenId)
					GetUserLogs(ctx)
					require.Equal(t, http.StatusNotFound, recorder.Code)
					assert.Equal(t, "token_not_found", decodeRestError(t, recorder).Code)
				})
			}

			ctx, recorder := newSelfLogContext("/api/log/self?token_id=12")
			GetUserLogs(ctx)
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool `json:"success"`
				Data    struct {
					Total int         `json:"total"`
					Items []model.Log `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.True(t, response.Success)
			assert.Zero(t, response.Data.Total)
			assert.Empty(t, response.Data.Items)
		})
	}
}

func newSelfLogContext(target string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	ctx.Set("id", 1)
	return ctx, recorder
}

func TestTokenUsageModelSubstringFilter(t *testing.T) {
	databases := selfLogFilterDatabases(t)
	if dsn := os.Getenv("TEST_CLICKHOUSE_DSN"); dsn != "" {
		databases = append(databases, selfLogFilterDatabase{name: "clickhouse", dsn: dsn, type_: common.DatabaseTypeClickHouse})
	}
	for _, database := range databases {
		t.Run(database.name, func(t *testing.T) {
			mainDatabase := database
			if database.type_ == common.DatabaseTypeClickHouse {
				mainDatabase = selfLogFilterDatabase{name: "sqlite", type_: common.DatabaseTypeSQLite}
			}
			db := setupSelfLogTokenFilterTest(t, mainDatabase)
			seedSelfLogTokenFilterData(t, db)
			if database.type_ == common.DatabaseTypeClickHouse {
				_, dsn := newAuditTestDatabase(t, database.name, database.dsn)
				previousMaster := common.IsMasterNode
				common.IsMasterNode = true
				t.Cleanup(func() { common.IsMasterNode = previousMaster })
				t.Setenv("LOG_SQL_DSN", dsn)
				t.Setenv("LOG_SQL_CLICKHOUSE_TTL_DAYS", "0")
				require.NoError(t, model.InitLogDB())
				db = model.LOG_DB
			}
			for _, log := range []*model.Log{
				{UserId: 1, TokenId: 11, ModelName: "gpt-4o", CreatedAt: 700, Type: model.LogTypeConsume},
				{UserId: 1, TokenId: 11, ModelName: "vendor/GPT-4.1", CreatedAt: 800, Type: model.LogTypeConsume},
				{UserId: 1, TokenId: 11, ModelName: "claude-sonnet", CreatedAt: 900, Type: model.LogTypeConsume},
				{UserId: 1, TokenId: 11, ModelName: `custom%_!\model`, CreatedAt: 1000, Type: model.LogTypeConsume},
				{UserId: 1, TokenId: 13, ModelName: "gpt-4o", CreatedAt: 800, Type: model.LogTypeConsume},
				{UserId: 2, TokenId: 21, ModelName: "gpt-4o", CreatedAt: 800, Type: model.LogTypeConsume},
				{UserId: 1, TokenId: 11, ModelName: "gpt-4o", CreatedAt: 1100, Type: model.LogTypeConsume},
				{UserId: 1, TokenId: 11, ModelName: "gpt-4o", CreatedAt: 850, Type: model.LogTypeError},
			} {
				require.NoError(t, db.Create(log).Error)
			}
			for _, tc := range []struct {
				query string
				page  string
				total int
				model string
			}{
				{query: "gpt", page: "1", total: 2, model: "vendor/GPT-4.1"},
				{query: "GpT", page: "2", total: 2, model: "gpt-4o"},
				{query: "4.1", page: "1", total: 1, model: "vendor/GPT-4.1"},
				{query: "g", page: "1", total: 2, model: "vendor/GPT-4.1"},
				{query: "%", page: "1", total: 1, model: `custom%_!\model`},
				{query: "_", page: "1", total: 1, model: `custom%_!\model`},
				{query: `!\`, page: "1", total: 1, model: `custom%_!\model`},
				{query: "missing", page: "1"},
				{query: "", page: "1", total: 4, model: `custom%_!\model`},
			} {
				t.Run(tc.query+"/"+tc.page, func(t *testing.T) {
					ctx, recorder := newSelfLogContext("/api/log/self?token_id=11&type=2&start_timestamp=700&end_timestamp=1000&page_size=1&p=" + tc.page + "&model_name=" + url.QueryEscape(tc.query))
					GetUserLogs(ctx)
					require.Equal(t, http.StatusOK, recorder.Code)
					var response struct {
						Success bool `json:"success"`
						Data    struct {
							Total int         `json:"total"`
							Items []model.Log `json:"items"`
						} `json:"data"`
					}
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
					require.True(t, response.Success)
					assert.Equal(t, tc.total, response.Data.Total)
					if tc.total == 0 {
						assert.Empty(t, response.Data.Items)
						return
					}
					require.Len(t, response.Data.Items, 1)
					assert.Equal(t, tc.model, response.Data.Items[0].ModelName)
					assert.Equal(t, 11, response.Data.Items[0].TokenId)
				})
			}
		})
	}
}

func TestAdminLogUserQueryContract(t *testing.T) {
	databases := selfLogFilterDatabases(t)
	if dsn := os.Getenv("TEST_CLICKHOUSE_DSN"); dsn != "" {
		databases = append(databases, selfLogFilterDatabase{name: "clickhouse", dsn: dsn, type_: common.DatabaseTypeClickHouse})
	}
	for _, database := range databases {
		t.Run(database.name, func(t *testing.T) {
			previousDB, previousLogDB := model.DB, model.LOG_DB
			previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
			mainDB, _ := newAuditTestDatabase(t, "sqlite", "")
			logDB, _ := newAuditTestDatabase(t, database.name, database.dsn)
			model.DB, model.LOG_DB = mainDB, logDB
			common.SetDatabaseTypes(common.DatabaseTypeSQLite, database.type_)
			t.Cleanup(func() {
				model.DB, model.LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousMain, previousLog)
			})
			require.NoError(t, mainDB.Exec("CREATE TABLE channels (id INTEGER PRIMARY KEY, name TEXT)").Error)
			require.NoError(t, mainDB.Exec("INSERT INTO channels (id, name) VALUES (7, 'channel-seven')").Error)
			if database.name == "clickhouse" {
				require.NoError(t, logDB.Exec("CREATE TABLE logs (user_id Int32, created_at Int64, type Int32, username String, model_name String, token_name String, channel_id Int32, other String, request_id String) ENGINE = MergeTree ORDER BY (created_at, request_id)").Error)
			} else {
				require.NoError(t, logDB.AutoMigrate(&model.Log{}))
			}
			for index := range 150 {
				row := map[string]any{
					"user_id": index%2 + 1, "created_at": int64(100 + index/2), "type": model.LogTypeConsume,
					"username": "historical-name", "model_name": "alpha_one", "token_name": "api-key-name", "channel_id": 7,
					"other":      `{"admin_info":{"quota_saturation":{"source":"test"}},"root_info":{"diagnostic":"root-only"}}`,
					"request_id": fmt.Sprintf("request-%03d", index+1),
				}
				require.NoError(t, logDB.Table("logs").Create(row).Error)
			}
			for _, row := range []map[string]any{
				{"user_id": 3, "created_at": int64(1000), "type": model.LogTypeConsume, "model_name": "alpha_one", "username": "outsider", "request_id": "outsider", "token_name": "", "channel_id": 0, "other": "{}"},
				{"user_id": 1, "created_at": int64(1000), "type": model.LogTypeError, "model_name": "beta", "username": "current-name", "request_id": "error", "token_name": "", "channel_id": 0, "other": "{}"},
			} {
				require.NoError(t, logDB.Table("logs").Create(row).Error)
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				role := common.RoleAdminUser
				if c.GetHeader("Test-Role") == "root" {
					role = common.RoleRootUser
				}
				c.Set("role", role)
			})
			router.GET("/api/log/user/:user_id", GetAdminUserLogs)
			router.POST("/api/log/query", QueryAdminLogs)
			router.GET("/api/log/", GetAllLogs)
			router.GET("/api/log/self", func(c *gin.Context) { c.Set("id", 1); GetUserLogs(c) })
			requestPage := func(method, path, body, role string) (int, common.PageInfo, []model.Log) {
				t.Helper()
				request := httptest.NewRequest(method, path, strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Test-Role", role)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				var envelope struct {
					Success bool `json:"success"`
					Data    struct {
						Page     int         `json:"page"`
						PageSize int         `json:"page_size"`
						Total    int         `json:"total"`
						Items    []model.Log `json:"items"`
					} `json:"data"`
				}
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
				if recorder.Code == http.StatusOK {
					require.True(t, envelope.Success)
				}
				return recorder.Code, common.PageInfo{Page: envelope.Data.Page, PageSize: envelope.Data.PageSize, Total: envelope.Data.Total}, envelope.Data.Items
			}
			t.Run("global-pages-rename-order-and-formatting", func(t *testing.T) {
				var seen []string
				for page := 1; page <= 3; page++ {
					status, info, logs := requestPage("POST", "/api/log/query", fmt.Sprintf(`{"user_ids":[2,1,2],"type":2,"p":%d,"page_size":50}`, page), "")
					assert.Equal(t, http.StatusOK, status)
					assert.Equal(t, 150, info.Total)
					require.Len(t, logs, 50)
					for index, log := range logs {
						position := (page-1)*50 + index
						assert.Equal(t, fmt.Sprintf("request-%03d", 150-position), log.RequestId)
						assert.Equal(t, "historical-name", log.Username)
						assert.Equal(t, "channel-seven", log.ChannelName)
						assert.NotContains(t, log.Other, "root-only")
						if database.name == "clickhouse" {
							assert.Equal(t, position+1, log.Id)
						} else {
							assert.Equal(t, 150-position, log.Id)
						}
						seen = append(seen, log.RequestId)
					}
				}
				assert.Len(t, seen, 150)
				unique := types.NewSet[string]()
				for _, requestID := range seen {
					unique.Add(requestID)
				}
				assert.Equal(t, 150, unique.Len())
				_, _, rootLogs := requestPage("POST", "/api/log/query", `{"user_ids":[1],"type":2}`, "root")
				require.NotEmpty(t, rootLogs)
				assert.Contains(t, rootLogs[0].Other, "root-only")
			})
			t.Run("filters-defaults-and-empty", func(t *testing.T) {
				status, info, logs := requestPage("GET", "/api/log/user/1?type=2&start_timestamp=110&end_timestamp=112&model_name=alpha%25", "", "")
				assert.Equal(t, http.StatusOK, status)
				assert.Equal(t, common.PageInfo{Page: 1, PageSize: 10, Total: 3}, info)
				require.Len(t, logs, 3)
				for _, log := range logs {
					assert.Equal(t, 1, log.UserId)
				}
				for _, body := range []string{`{"user_ids":[]}`, `{"user_ids":[999]}`, `{"user_ids":[1,2],"model_name":"absent"}`} {
					status, info, logs := requestPage("POST", "/api/log/query", body, "")
					assert.Equal(t, http.StatusOK, status)
					assert.Zero(t, info.Total)
					assert.Empty(t, logs)
				}
				_, info, _ = requestPage("POST", "/api/log/query", `{"user_ids":[1,2],"type":2,"model_name":"alpha_one"}`, "")
				assert.Equal(t, 150, info.Total)
				_, oldInfo, _ := requestPage("GET", "/api/log/?type=2&username=historical-name&page_size=50", "", "")
				assert.Equal(t, 150, oldInfo.Total)
				_, selfInfo, _ := requestPage("GET", "/api/log/self?type=2&page_size=50", "", "")
				assert.Equal(t, 75, selfInfo.Total)
			})
			t.Run("equal-clickhouse-sort-keys-have-no-identity-guarantee", func(t *testing.T) {
				for _, userID := range []int{1, 2} {
					require.NoError(t, logDB.Table("logs").Create(map[string]any{"user_id": userID, "created_at": int64(3000), "type": model.LogTypeConsume, "username": "tie-user", "model_name": "tie-model", "token_name": "", "channel_id": 0, "other": "{}", "request_id": "duplicate-tie"}).Error)
				}
				status, info, logs := requestPage("POST", "/api/log/query", `{"user_ids":[1,2],"type":2,"model_name":"tie-model"}`, "")
				assert.Equal(t, http.StatusOK, status)
				assert.Equal(t, 2, info.Total)
				require.Len(t, logs, 2)
				assert.ElementsMatch(t, []int{1, 2}, []int{logs[0].UserId, logs[1].UserId})
			})
			t.Run("large-list-and-invalid-input", func(t *testing.T) {
				ids := make([]int, 16384)
				for index := range ids {
					ids[index] = index + 1
				}
				body, err := common.Marshal(AdminLogQueryRequest{UserIDs: ids, Type: 2, Page: 1, PageSize: 50})
				require.NoError(t, err)
				status, info, _ := requestPage("POST", "/api/log/query", string(body), "")
				assert.Equal(t, http.StatusOK, status)
				assert.Equal(t, 153, info.Total)
				maximumIDs := make([]int, 16384)
				rows := make([]map[string]any, len(maximumIDs))
				longModel := strings.Repeat("'\\", 8192)
				for index := range maximumIDs {
					maximumIDs[index] = math.MaxInt32 - index
					modelName := "bulk-model"
					rows[index] = map[string]any{"user_id": maximumIDs[index], "created_at": int64(2000), "type": model.LogTypeConsume, "username": "bulk-user", "model_name": modelName, "token_name": "", "channel_id": 0, "other": "{}", "request_id": fmt.Sprintf("bulk-%05d", index+1)}
				}
				if database.name == "clickhouse" {
					connection, err := logDB.DB()
					require.NoError(t, err)
					transaction, err := connection.Begin()
					require.NoError(t, err)
					statement, err := transaction.Prepare("INSERT INTO logs (user_id, created_at, type, username, model_name, token_name, channel_id, other, request_id)")
					require.NoError(t, err)
					for _, row := range rows {
						_, err = statement.Exec(row["user_id"], row["created_at"], row["type"], row["username"], row["model_name"], row["token_name"], row["channel_id"], row["other"], row["request_id"])
						require.NoError(t, err)
					}
					require.NoError(t, transaction.Commit())
					require.NoError(t, statement.Close())
				} else {
					require.NoError(t, logDB.Table("logs").CreateInBatches(rows, 500).Error)
				}
				body, err = common.Marshal(AdminLogQueryRequest{UserIDs: maximumIDs, Type: 2, Page: 164, PageSize: 100})
				require.NoError(t, err)
				status, info, maximumLogs := requestPage("POST", "/api/log/query", string(body), "")
				assert.Equal(t, http.StatusOK, status)
				assert.Equal(t, 16384, info.Total)
				require.Len(t, maximumLogs, 84)
				assert.Equal(t, "bulk-00084", maximumLogs[0].RequestId)
				assert.Equal(t, "bulk-00001", maximumLogs[len(maximumLogs)-1].RequestId)
				assert.Equal(t, math.MaxInt32, maximumLogs[len(maximumLogs)-1].UserId)
				body, err = common.Marshal(AdminLogQueryRequest{UserIDs: maximumIDs, Type: 2, Page: 1, PageSize: 100, ModelName: longModel})
				require.NoError(t, err)
				status, info, maximumLogs = requestPage("POST", "/api/log/query", string(body), "")
				assert.Equal(t, http.StatusOK, status)
				assert.Zero(t, info.Total)
				assert.Empty(t, maximumLogs)
				invalid := []string{`{`, `{}`, `null`, `[]`, `{"user_ids":null}`, `{"user_ids":[0]}`, `{"user_ids":[-1]}`, `{"user_ids":[1.5]}`, `{"user_ids":["1"]}`, `{"user_ids":[2147483648]}`, `{"user_ids":[18446744073686646784]}`, `{"user_ids":[null]}`, `{"user_ids":[],"p":0}`, `{"user_ids":[],"p":null}`, `{"user_ids":[],"p":9223372036854775807,"page_size":100}`, `{"user_ids":[],"page_size":101}`, `{"user_ids":[],"page_size":0}`, `{"user_ids":[],"type":8}`, `{"user_ids":[],"type":-1}`, `{"user_ids":[],"start_timestamp":-1}`, `{"user_ids":[],"start_timestamp":2,"end_timestamp":1}`, `{"user_ids":[]} {}`, `{"user_ids":[],"model_name":"` + strings.Repeat("a", 16385) + `"}`, strings.Repeat(" ", 262145) + `{}`}
				body, err = common.Marshal(AdminLogQueryRequest{UserIDs: append(ids, 1), Page: 1, PageSize: 10})
				require.NoError(t, err)
				invalid = append(invalid, string(body))
				invalid = append(invalid, `{"user_ids":[1],"model_name":"%"}`, `{"user_ids":[],"model_name":"%"}`, `{"user_ids":[1],"model_name":"a%"}`, `{"user_ids":[1],"model_name":"abc%%"}`, `{"user_ids":[1],"model_name":"ab%cd%ef%"}`)
				for index, body := range invalid {
					status, _, _ := requestPage("POST", "/api/log/query", body, "")
					assert.Equalf(t, http.StatusBadRequest, status, "case %d", index)
				}
				for _, path := range []string{"0", "-1", "1.5", "2147483648", "1?p=0", "1?p=x", "1?page_size=0", "1?page_size=1.5", "1?start_timestamp=x", "1?end_timestamp=-1", "1?type=8", "1?p=9223372036854775807&page_size=100", "1?model_name=%25", "1?p=1;unexpected=2", "1?model_name=%ZZ"} {
					status, _, _ := requestPage("GET", "/api/log/user/"+path, "", "")
					assert.Equal(t, http.StatusBadRequest, status, path)
				}
			})
			t.Run("database-failure-is-not-an-empty-success", func(t *testing.T) {
				require.NoError(t, logDB.Exec("DROP TABLE logs").Error)
				status, _, _ := requestPage("POST", "/api/log/query", `{"user_ids":[1]}`, "")
				assert.Equal(t, http.StatusInternalServerError, status)
			})
		})
	}
}

func TestAdminLogUserQueryGuard(t *testing.T) {
	user, token := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleCommonUser).Error)
	router := gin.New()
	router.GET("/api/log/user/:user_id", middleware.AdminAuth(), GetAdminUserLogs)
	router.POST("/api/log/query", middleware.AdminAuth(), QueryAdminLogs)
	for _, endpoint := range []struct{ method, path string }{{"GET", "/api/log/user/1"}, {"POST", "/api/log/query"}} {
		for _, bearer := range []string{"", token} {
			request := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{"user_ids":[]}`))
			if bearer != "" {
				request.Header.Set("Authorization", "Bearer "+bearer)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if bearer == "" {
				assert.Equal(t, http.StatusUnauthorized, response.Code)
			} else {
				assert.Equal(t, http.StatusForbidden, response.Code)
			}
		}
	}
}
