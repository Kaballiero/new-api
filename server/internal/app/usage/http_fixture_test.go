package usage_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"

	sqlmysql "github.com/go-sql-driver/mysql"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	authz "github.com/QuantumNous/new-api/internal/app/authorizationcore"
	"github.com/QuantumNous/new-api/model"
)

func httpPostgres(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	admin := stdlib.OpenDB(*config.Copy())
	t.Cleanup(func() { assert.NoError(t, admin.Close()) })
	schema := "wave3a_http_" + common.GetUUID()
	identifier := pgx.Identifier{schema}.Sanitize()
	_, err = admin.ExecContext(t.Context(), "CREATE SCHEMA "+identifier)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := admin.ExecContext(ctx, "DROP SCHEMA "+identifier+" CASCADE")
		assert.NoError(t, err)
	})
	config.RuntimeParams["search_path"] = schema
	connection := stdlib.OpenDB(*config)
	t.Cleanup(func() { assert.NoError(t, connection.Close()) })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), &gorm.Config{})
	require.NoError(t, err)
	var actual, version string
	require.NoError(t, db.Raw("SELECT current_schema()").Scan(&actual).Error)
	require.Equal(t, schema, actual)
	require.NoError(t, db.Raw("SHOW server_version").Scan(&version).Error)
	t.Logf("PostgreSQL %s, isolated schema %s", version, schema)
	return db
}

func newAuditTestDatabase(t *testing.T, kind, dsn string) (*gorm.DB, string) {
	t.Helper()
	if kind == "postgres" {
		return httpPostgres(t, dsn), dsn
	}
	if kind == "sqlite" {
		path := t.TempDir() + "/audit.db"
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		require.NoError(t, err)
		return db, path
	}
	require.NotEmpty(t, dsn)
	name := fmt.Sprintf("newapi_audit_%d", time.Now().UnixNano())
	var original, isolated gorm.Dialector
	var newDSN string
	if kind == "mysql" {
		config, err := sqlmysql.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "tcp", config.Net)
		host, _, err := net.SplitHostPort(config.Addr)
		require.NoError(t, err)
		require.True(t, net.ParseIP(host).IsLoopback(), "database tests only permit loopback instances")
		original = mysql.Open(dsn)
		config.DBName = name
		newDSN = config.FormatDSN()
		isolated = mysql.Open(newDSN)
	} else {
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.True(t, net.ParseIP(parsed.Hostname()).IsLoopback(), "database tests only permit loopback instances")
		parsed.Path = "/" + name
		newDSN = parsed.String()
		if kind == "clickhouse" {
			original = clickhouse.Open(dsn)
			isolated = clickhouse.Open(newDSN)
		} else {
			original = postgres.Open(dsn)
			isolated = postgres.Open(newDSN)
		}
	}
	admin, err := gorm.Open(original, &gorm.Config{})
	require.NoError(t, err)
	// No IF NOT EXISTS: a collision fails before any test data can be written.
	createSQL := "CREATE DATABASE " + name
	if kind == "mysql" {
		createSQL += " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
	}
	require.NoError(t, admin.Exec(createSQL).Error)
	sqlDB, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	t.Cleanup(func() { assert.NoError(t, admin.Exec("DROP DATABASE "+name).Error) })
	db, err := gorm.Open(isolated, &gorm.Config{})
	require.NoError(t, err)
	t.Logf("isolated database: %s (%s)", name, kind)
	t.Cleanup(func() {
		connection, err := db.DB()
		if err == nil {
			_ = connection.Close()
		}
	})
	return db, newDSN
}

func setupModelListControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &channelrecord.Ability{}, &model.Model{}, &model.Vendor{}))
	previousDB, previousLog := model.DB, model.LOG_DB
	previousMain, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLog
		common.SetDatabaseTypes(previousMain, previousLogType)
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemory
	})
	return db
}

func setupAccessTokenAudit(t *testing.T) (*model.User, string) {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.Log{}, &model.AuditLog{}, &model.CasbinRule{}, &model.AuthzRole{}))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true
	require.NoError(t, authz.Init(db))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.IsMasterNode = previousMaster
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled = previousRedis
	})
	token := "legacy-opaque-token"
	user := &model.User{Username: "audit-owner", Password: "placeholder", Role: common.RoleAdminUser,
		Status: common.UserStatusEnabled, Group: "default", AccessToken: &token, AuthVersion: 1,
		AffCode: "audit-owner"}
	require.NoError(t, db.Create(user).Error)
	return user, token
}

func auditRequest(router http.Handler, method, path, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(`{"secret":"body-must-not-be-logged"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", "audit-test-client")
	request.RemoteAddr = "192.0.2.8:4567"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

type restErrorEnvelope struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decodeRestError(t *testing.T, response *httptest.ResponseRecorder) restErrorEnvelope {
	t.Helper()
	var result restErrorEnvelope
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	return result
}
