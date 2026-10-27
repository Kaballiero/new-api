// Package contracttest runs HTTP contract tests of internal modules through the
// real API router: real middleware, real handlers and a fresh database. Goldens
// are recorded on the code before a migration and must stay unchanged afterwards
// (see internal/AGENTS.md, section "Тесты").
package contracttest

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	identitycore "github.com/QuantumNous/new-api/internal/app/identitycore"
)

// Same convention as relaykit/relayconvert golden tests.
var update = flag.Bool("update", false, "record contract golden files")

const goldenDir = "testdata/golden"

// Anonymous sends a request without credentials.
const Anonymous = ""

// Headers that belong to the contract when present: caching of secrets and limits.
var contractHeaders = []string{"Cache-Control", "Retry-After", "Specific_channel_version"}

type Env struct {
	DB     *gorm.DB
	engine *gin.Engine
	// Session bearer tokens of the fixture users.
	User, Admin, Root string
	// UserPAT is a personal access token of User.
	UserPAT string
	UserID  int
}

// MountRelay adds the production relay routes, including their authentication
// and distribution middleware. A stable request ID also pins errors that embed
// the ID in their message; the upstream itself is supplied by the test channel.
func (env *Env) MountRelay() {
	env.engine.Use(func(c *gin.Context) {
		const requestID = "billing-contract-request"
		c.Set(common.RequestIdKey, requestID)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), common.RequestIdKey, requestID))
		c.Header(common.RequestIdKey, requestID)
		c.Next()
	})
	router.SetRelayRouter(env.engine)
}

// MountTasks adds the production task surfaces in their normal registration order.
func (env *Env) MountTasks() {
	env.MountRelay()
	router.SetTaskPluginProtocolRouter(env.engine)
	router.SetVideoRouter(env.engine)
	router.SetTaskRouter(env.engine)
	env.engine.NoRoute(router.SetPluginRouter(env.engine))
}

// Route mounts the production middleware under test without contacting upstreams.
func (env *Env) Route(method, path string, handlers ...gin.HandlerFunc) {
	stableRequestID := func(c *gin.Context) { c.Set(common.RequestIdKey, "gateway-contract-request"); c.Next() }
	env.engine.Handle(method, path, append([]gin.HandlerFunc{stableRequestID}, handlers...)...)
}

// Serve sends a request with protocol-specific credential headers.
func (env *Env) Serve(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	env.engine.ServeHTTP(recorder, request)
	return recorder
}

// New creates a fresh SQLite database through the production model.InitDB and
// model.InitLogDB (full migrations and dialect setup), adds a common user, an
// admin and a root user with live dashboard sessions, applies the production
// rate-limit defaults and mounts the API router behind the production request-id
// and i18n middleware. Global state is restored when the test ends.
func New(t *testing.T) *Env {
	t.Helper()
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousSecret, previousPath, previousMaster, previousRedis := common.SessionSecret, common.SQLitePath, common.IsMasterNode, common.RedisEnabled
	previousGlobal, previousGlobalNum, previousGlobalDuration := common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration
	previousCritical, previousCriticalNum, previousCriticalDuration := common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration
	previousSearch, previousSearchNum, previousSearchDuration := common.SearchRateLimitEnable, common.SearchRateLimitNum, common.SearchRateLimitDuration
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	// A throwaway database needs no durability; without fsync the migrations take
	// milliseconds instead of minutes on slow disks.
	common.SQLitePath = filepath.Join(t.TempDir(), "contract.db") + "?_pragma=busy_timeout(30000)&_pragma=synchronous(OFF)&_txlock=immediate"
	common.IsMasterNode = true
	common.RedisEnabled = false
	common.SessionSecret = "contract-test-session-secret"
	// Production defaults from common/init.go. The in-memory limiter is shared by
	// all tests of one process, so contract tests run with -count=1.
	common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration = true, 360, 180
	common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = true, 20, 20*60
	common.SearchRateLimitEnable, common.SearchRateLimitNum, common.SearchRateLimitDuration = true, 10, 60
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	db := model.DB
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.SessionSecret, common.SQLitePath, common.IsMasterNode, common.RedisEnabled = previousSecret, previousPath, previousMaster, previousRedis
		common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration = previousGlobal, previousGlobalNum, previousGlobalDuration
		common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = previousCritical, previousCriticalNum, previousCriticalDuration
		common.SearchRateLimitEnable, common.SearchRateLimitNum, common.SearchRateLimitDuration = previousSearch, previousSearchNum, previousSearchDuration
	})

	env := &Env{DB: db, engine: gin.New(), UserID: 1, UserPAT: "contracttestpersonalaccesstoken1"}
	session := func(id int, username string, role int, pat *string) string {
		user := &model.User{Id: id, Username: username, Role: role, Status: common.UserStatusEnabled, Group: "default", AffCode: username, Quota: 1000000, AuthVersion: 1, AccessToken: pat}
		require.NoError(t, db.Create(user).Error)
		require.NoError(t, model.PublishUserAuthCache(user.Id))
		bundle, err := identitycore.CreateLoginSession(user.Id, "password", "127.0.0.1", "contract-test")
		require.NoError(t, err)
		return bundle.AccessToken
	}
	env.User = session(env.UserID, "contract-user", common.RoleCommonUser, &env.UserPAT)
	env.Admin = session(2, "contract-admin", common.RoleAdminUser, nil)
	env.Root = session(3, "contract-root", common.RoleRootUser, nil)
	env.engine.Use(middleware.RequestId(), middleware.I18n())
	router.SetApiRouter(env.engine)
	return env
}

// Do sends a request with the given bearer token. A non-empty body is sent as JSON.
func (env *Env) Do(method, path, bearer, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if bearer != Anonymous {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	recorder := httptest.NewRecorder()
	env.engine.ServeHTTP(recorder, request)
	return recorder
}

// AuditLog returns all audit log rows in insertion order as generic JSON values.
func (env *Env) AuditLog(t *testing.T) any {
	t.Helper()
	var rows []model.AuditLog
	require.NoError(t, model.LOG_DB.Order("id").Find(&rows).Error)
	wire, err := common.Marshal(rows)
	require.NoError(t, err)
	var value any
	require.NoError(t, common.Unmarshal(wire, &value))
	return value
}

// Golden compares the status, contract headers and body of a response with
// testdata/golden/<name>.golden.json. An empty body is recorded as null and a
// non-JSON body as a string.
func Golden(t *testing.T, name string, recorder *httptest.ResponseRecorder, dynamicKeys ...string) {
	t.Helper()
	var body any
	if raw := recorder.Body.Bytes(); len(raw) != 0 && common.Unmarshal(raw, &body) != nil {
		body = string(raw)
	}
	headers := map[string]any{}
	for _, header := range contractHeaders {
		if value := recorder.Header().Get(header); value != "" {
			headers[header] = value
		}
	}
	GoldenValue(t, name, map[string]any{"status": recorder.Code, "headers": headers, "body": body}, dynamicKeys...)
}

// GoldenValue compares any JSON value with testdata/golden/<name>.golden.json.
// Values of the dynamic keys are masked at any depth: letters and digits of a
// string become "x" while length and separators stay (a masked key and a full
// key differ), numbers become "__number__" and booleans "__bool__"; objects and
// arrays keep their shape. Record with -update only on code before migration.
func GoldenValue(t *testing.T, name string, value any, dynamicKeys ...string) {
	t.Helper()
	snapshot, err := common.Marshal(normalize(value, dynamicKeys))
	require.NoError(t, err)
	snapshot, err = common.IndentJson(snapshot)
	require.NoError(t, err)
	snapshot = append(snapshot, '\n')

	path := filepath.Join(goldenDir, name+".golden.json")
	if *update {
		require.NoError(t, os.MkdirAll(goldenDir, 0o755))
		require.NoError(t, os.WriteFile(path, snapshot, 0o644))
	}
	expected, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file; record it with -update on the code before migration")
	assert.Equal(t, string(expected), string(snapshot), "HTTP contract changed: %s", name)
}

func normalize(value any, dynamicKeys []string) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if slices.Contains(dynamicKeys, key) {
				typed[key] = mask(nested)
				continue
			}
			typed[key] = normalize(nested, dynamicKeys)
		}
	case []any:
		for index, nested := range typed {
			typed[index] = normalize(nested, dynamicKeys)
		}
	}
	return value
}

func mask(value any) any {
	switch typed := value.(type) {
	case string:
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return 'x'
			}
			return r
		}, typed)
	case float64:
		return "__number__"
	case bool:
		return "__bool__"
	case map[string]any:
		for key, nested := range typed {
			typed[key] = mask(nested)
		}
	case []any:
		for index, nested := range typed {
			typed[index] = mask(nested)
		}
	}
	return value
}
