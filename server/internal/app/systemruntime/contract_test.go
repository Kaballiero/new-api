// System HTTP contracts characterize storage and public runtime effects.
package systemruntime_test

import (
	metricsstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/systemmetrics"
	metricsdomain "github.com/QuantumNous/new-api/internal/system/domain"

	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	perfmetrics "github.com/QuantumNous/new-api/internal/app/systemmetrics"
	"github.com/QuantumNous/new-api/internal/app/systemruntime"
	"github.com/QuantumNous/new-api/internal/contracttest"
	sf "github.com/QuantumNous/new-api/internal/contracttest/systemfixture"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	authtoken "github.com/QuantumNous/new-api/internal/adapter/identity/authtoken"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
)

type systemCase struct {
	Name, Kind, Method, Path, Actor, Body, Variant string
	Status                                         int
}

var routes = []systemCase{
	{Name: "setup_get", Method: "GET", Path: "/api/setup", Kind: "public"},
	{Name: "setup_post", Method: "POST", Path: "/api/setup", Kind: "public", Body: `{}`},
	{Name: "health", Method: "GET", Path: "/api/status/test", Kind: "admin"},
	{Name: "uptime", Method: "GET", Path: "/api/uptime/status", Kind: "public"},
	{Name: "metrics_summary", Method: "GET", Path: "/api/perf-metrics/summary", Kind: "public"},
	{Name: "metrics_detail", Method: "GET", Path: "/api/perf-metrics?model=fixture", Kind: "public"},
	{Name: "performance_stats", Method: "GET", Path: "/api/performance/stats", Kind: "root"},
	{Name: "performance_clear", Method: "DELETE", Path: "/api/performance/disk_cache", Kind: "root"},
	{Name: "performance_reset", Method: "POST", Path: "/api/performance/reset_stats", Kind: "root"},
	{Name: "performance_gc", Method: "POST", Path: "/api/performance/gc", Kind: "root"},
	{Name: "log_files", Method: "GET", Path: "/api/performance/logs", Kind: "root"},
	{Name: "log_cleanup", Method: "DELETE", Path: "/api/performance/logs?mode=by_count&value=1", Kind: "root"},
	{Name: "task_cleanup", Method: "POST", Path: "/api/system-task/log-cleanup?target_timestamp=946684799", Kind: "root"},
	{Name: "task_list", Method: "GET", Path: "/api/system-task/list", Kind: "root"},
	{Name: "task_current", Method: "GET", Path: "/api/system-task/current?type=unknown", Kind: "root"},
	{Name: "task_detail", Method: "GET", Path: "/api/system-task/unknown", Kind: "root", Status: 404},
	{Name: "instances", Method: "GET", Path: "/api/system-info/instances", Kind: "root"},
	{Name: "instances_delete", Method: "DELETE", Path: "/api/system-info/stale-instances", Kind: "root"},
	{Name: "instance_delete", Method: "DELETE", Path: "/api/system-info/instances/unknown", Kind: "root"},
}

func systemCases() []systemCase {
	cases := []systemCase{}
	for _, route := range routes {
		for _, actor := range []string{"anonymous", "key", "user", "admin", "root", "user_pat", "admin_pat", "root_pat"} {
			tc := route
			tc.Name = "access_" + route.Name + "_" + actor
			tc.Actor = actor
			tc.Kind = "access"
			tc.Status = route.Status
			if tc.Status == 0 {
				tc.Status = 200
			}
			if route.Kind != "public" {
				if actor == "anonymous" || actor == "key" {
					tc.Status = 401
				} else if route.Kind == "root" && actor != "root" && actor != "root_pat" {
					tc.Status = 403
				} else if route.Kind == "admin" && (actor == "user" || actor == "user_pat") {
					tc.Status = 403
				}
			}
			cases = append(cases, tc)
		}
	}
	for kind, variants := range map[string][]string{
		"setup": {"get_empty", "get_root", "get_complete", "guard", "malformed",
			"null", "array", "username_type", "body_limit", "password_8", "password_128",
			"username_12", "username_13", "username_multibyte", "empty_username", "mismatch",
			"short", "long", "unicode", "whitespace", "existing_root_invalid", "create_fault",
			"setup_fault", "options_fault", "hash_fault", "bcrypt_long", "repeat", "modes_none",
			"modes_self", "modes_demo", "modes_both", "startup_empty", "startup_root", "startup_complete",
			"startup_create_fault"},
		"tasks": {"empty", "all_types", "current_missing", "current_unknown",
			"current_pending", "current_running", "current_terminal", "missing_detail", "raw_json",
			"limit_bad", "limit_zero", "limit_negative", "limit_one", "limit_max", "limit_over",
			"cleanup_missing", "cleanup_bad", "cleanup_negative", "cleanup_first", "cleanup_existing",
			"read_fault"},
		"instances": {"empty", "list", "equal_boundary", "delete_all", "delete_single",
			"delete_online", "delete_missing", "delete_blank", "delete_spaces", "raw_info",
			"report_manual", "report_fallback", "report_repeat", "read_fault"},
		"performance": {"stats", "clear", "clear_missing", "clear_fault", "reset", "gc",
			"logs_disabled", "logs_empty", "logs_files", "logs_count", "logs_days", "logs_partial",
			"logs_bad_mode", "logs_bad_value", "logs_zero", "logs_unconfigured", "logs_fault"},

		"metrics": {"missing_model", "empty", "rows", "hot", "mixed", "inactive",
			"auto", "disabled", "hours_bad", "hours_zero", "hours_negative", "hours_max",
			"hours_over", "summary", "summary_ties", "nav_auth", "nav_disabled", "read_fault"},

		"uptime": {"empty", "null", "malformed", "missing_url", "missing_slug",
			"success", "multiple", "ui_disabled", "missing_values", "duplicate", "non200",
			"bad_json", "transport_error", "timeout", "redirect", "trailing_slashes"},
		"health": {"alive", "failure", "cached_failure", "cache_expired", "stats"},
		"status": {"setup_false", "setup_true", "uptime_disabled", "version"},
	} {
		for _, v := range variants {
			cases = append(cases, systemCase{Name: kind + "_" + v, Kind: kind, Variant: v})
		}
	}
	slices.SortFunc(cases, func(a, b systemCase) int { return strings.Compare(a.Name, b.Name) })
	return cases
}

func TestSystemHTTPContract(t *testing.T) {
	cases := systemCases()
	names := make([]string, len(cases))
	byName := map[string]systemCase{}
	for i, tc := range cases {
		names[i] = tc.Name
		byName[tc.Name] = tc
	}
	sf.Run(t, "TestSystemHTTPContract", "http", "../../system/contract/testdata/golden", names, func(t *testing.T, name string) any { return runSystem(t, byName[name]) })
}

type uptimeTransport struct {
	mu      sync.Mutex
	calls   []map[string]any
	variant string
}

func (u *uptimeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u.mu.Lock()
	u.calls = append(u.calls, map[string]any{"method": r.Method, "url": r.URL.String(), "headers": r.Header.Clone(), "body": r.Body != nil})
	u.mu.Unlock()
	if r.URL.Host != "uptime.fixture" {
		return nil, errors.New("undeclared system outbound")
	}
	if u.variant == "transport_error" {
		return nil, errors.New("fixture transport failed")
	}
	if u.variant == "timeout" {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	status := 200
	body := `{"publicGroupList":[{"id":1,"name":"Public","monitorList":[{"id":7,"name":"Primary"},{"id":8,"name":"Secondary"}]},{"id":2,"name":"Empty","monitorList":[]}]}`
	if strings.Contains(r.URL.Path, "heartbeat/") {
		body = `{"heartbeatList":{"7":[{"status":1},{"status":0}],"8":[]},"uptimeList":{"7_24":99.5,"8_24":0}}`
	}
	if u.variant == "missing_values" && strings.Contains(r.URL.Path, "heartbeat/") {
		body = `{}`
	}
	if u.variant == "duplicate" && !strings.Contains(r.URL.Path, "heartbeat/") {
		body = `{"publicGroupList":[{"name":"Public","monitorList":[{"id":7,"name":"Primary"},{"id":7,"name":"Again"}]}]}`
	}
	if u.variant == "non200" {
		status = 503
		body = "unavailable"
	}
	if u.variant == "bad_json" {
		body = "{"
	}
	response := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}
	if u.variant == "redirect" && !strings.HasPrefix(r.URL.Path, "/redirected") {
		response.StatusCode = 302
		response.Header.Set("Location", "/redirected"+r.URL.Path)
	}
	return response, nil
}

func runSystem(t *testing.T, tc systemCase) any {
	fixture.InstallDBClock(t)
	env := contracttest.New(t)
	fixture.Configure()
	constant.AnonymousRequestBodyLimitKB = 1
	common.StartTime = fixture.Epoch
	common.NodeName = "system-contract-node"
	common.NodeNameSource = common.NodeNameSourceManual
	common.NodeNameManuallyConfigured = true
	constant.Setup = true
	common.OptionMap = map[string]string{"HeaderNavModules": "", "SidebarModulesAdmin": ""}
	console_setting.GetConsoleSetting().UptimeKumaGroups = ""
	*common.LogDir = ""
	cacheDir := t.TempDir()
	common.SetDiskCacheConfig(common.DiskCacheConfig{Path: cacheDir, Enabled: true, ThresholdMB: 1, MaxSizeMB: 2})
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: true, CPUThreshold: 90, MemoryThreshold: 90, DiskThreshold: 95})
	transport := &uptimeTransport{variant: tc.Variant}
	http.DefaultTransport = transport
	identities := map[string]identitydomain.AuthIdentity{}
	for name, token := range map[string]string{"user": env.User, "admin": env.Admin, "root": env.Root} {
		id, err := authtoken.ParseAccessToken(token)
		require.NoError(t, err)
		identities[name] = id
	}
	for id, pat := range map[int]string{2: "system-admin-pat", 3: "system-root-pat"} {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id=?", id).Update("access_token", pat).Error)
	}
	var result any
	synctest.Test(t, func(t *testing.T) {
		tokens := map[string]string{"anonymous": "", "key": "sk-system-key", "user_pat": env.UserPAT,
			"admin_pat": "system-admin-pat", "root_pat": "system-root-pat"}
		for name, id := range identities {
			token, _, err := authtoken.IssueAccessToken(id)
			require.NoError(t, err)
			tokens[name] = token
		}
		actor := tc.Actor
		if actor == "" {
			actor = "root"
		}
		if tc.Kind == "metrics" && (tc.Variant == "nav_auth" || tc.Variant == "nav_disabled") {
			actor = "anonymous"
		}
		observations := []any{}
		extra := map[string]any{}
		request := func(method, path, body string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Accept-Language", "zh-CN")
			if lang := os.Getenv("SYSTEM_CONTRACT_LANG"); lang != "" {
				if lang == "default" {
					r.Header.Del("Accept-Language")
				} else {
					r.Header.Set("Accept-Language", lang)
				}
			}
			if body != "" {
				r.Header.Set("Content-Type", "application/json")
			}
			if token := tokens[actor]; token != "" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
			response := env.Serve(r)
			raw := canonicalBody(t, path, response.Body.String())
			headers := map[string]string{}
			for _, name := range []string{"Content-Type", "Cache-Control", "Retry-After", "Location", "Vary", "ETag"} {
				if value := response.Header().Get(name); value != "" {
					headers[name] = value
				}
			}
			observations = append(observations, map[string]any{"status": response.Code, "headers": headers, "body": raw})
			return response
		}
		switch tc.Kind {
		case "access":
			assert.Equal(t, tc.Status, request(tc.Method, tc.Path, tc.Body).Code)
		case "setup":
			setupCase(t, tc, request, extra)
		case "tasks":
			tasksCase(t, tc, request)
		case "instances":
			instancesCase(t, tc, request, extra)
		case "performance":
			performanceCase(t, tc, cacheDir, request, extra)
		case "metrics":
			metricsCase(t, tc, request)
		case "uptime":
			uptimeCase(t, tc, request)
		case "health":
			switch tc.Variant {
			case "failure", "cached_failure", "cache_expired":
				if tc.Variant != "failure" {
					request("GET", "/api/status/test", "")
				}
				oldDB := model.DB
				oldLog := model.LOG_DB
				env.Route("GET", "/system-health-closed", func(c *gin.Context) {
					pool, err := oldDB.DB()
					require.NoError(t, err)
					require.NoError(t, pool.Close())
					controller.TestStatus(c)
				})
				// Restore the live handle before collecting persisted state.
				if tc.Variant == "cache_expired" {
					time.Sleep(11 * time.Second)
				}
				response := request("GET", "/system-health-closed", "")
				if tc.Variant == "cached_failure" {
					assert.Equal(t, 200, response.Code)
				} else {
					assert.Equal(t, 503, response.Code)
				}
				reopened, err := gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{})
				require.NoError(t, err)
				model.DB = reopened
				if oldLog == oldDB {
					model.LOG_DB = reopened
				}
				t.Cleanup(func() { pool, err := reopened.DB(); require.NoError(t, err); require.NoError(t, pool.Close()) })
			case "stats":
				env.MountRelay()
				env.Route("GET", "/system-stats-probe", func(c *gin.Context) { c.JSON(200, middleware.GetStats()) })
				request("GET", "/system-stats-probe", "")
				request("GET", "/api/status/test", "")
			default:
				request("GET", "/api/status/test", "")
			}
		case "status":
			constant.Setup = tc.Variant != "setup_false"
			if tc.Variant == "uptime_disabled" {
				console_setting.GetConsoleSetting().UptimeKumaEnabled = false
			}
			if tc.Variant == "version" {
				common.Version = "system-fixture-version"
			}
			request("GET", "/api/status", "")
		default:
			t.Fatalf("unknown kind %s", tc.Kind)
		}
		synctest.Wait()
		state := sf.State(t)
		for _, row := range state["instances"].([]map[string]any) {
			row["info"] = canonicalInstanceInfo(t, row["info"].(string))
			if tc.Variant == "report_fallback" {
				hostname, err := os.Hostname()
				require.NoError(t, err)
				require.Equal(t, hostname, row["node_name"])
				row["node_name"] = "<fixture-hostname>"
			}
		}
		state["observations"] = observations
		state["extra"] = extra
		state["runtime"] = map[string]any{"setup": constant.Setup, "self": operation_setting.SelfUseModeEnabled,
			"demo": operation_setting.DemoSiteEnabled, "cache": common.GetDiskCacheStats()}
		transport.mu.Lock()
		calls := append([]map[string]any{}, transport.calls...)
		transport.mu.Unlock()
		rawCalls := make([]json.RawMessage, len(calls))
		for i, c := range calls {
			data, err := common.Marshal(c)
			require.NoError(t, err)
			rawCalls[i] = data
		}
		if len(calls) > 0 {
			rawCalls = sf.Order(t, tc.Name, rawCalls)
		}
		state["outbound"] = rawCalls
		result = sf.Canonical(t, state, cacheDir)
	})
	return result
}

func canonicalBody(t *testing.T, path, raw string) string {
	t.Helper()
	if strings.HasPrefix(path, "/api/system-info/instances") {
		raw = canonicalInstanceInfo(t, raw)
	}
	if strings.HasPrefix(path, "/api/performance/stats") && strings.Contains(raw, `"cache_stats"`) {
		var decoded map[string]any
		require.NoError(t, common.UnmarshalJsonStr(raw, &decoded))
		require.True(t, decoded["success"].(bool))
		for _, key := range []string{"alloc", "total_alloc", "sys", "num_gc", "num_goroutine", "total", "free", "used", "used_percent"} {
			pattern := regexp.MustCompile(`("` + key + `":)([0-9]+(?:\.[0-9]+)?)`)
			raw = pattern.ReplaceAllString(raw, "${1}0")
		}
		raw = regexp.MustCompile(`("is_running_in_container":)(true|false)`).ReplaceAllString(raw, "${1}false")
	}
	if strings.HasPrefix(path, "/api/perf-metrics/summary") && strings.Contains(raw, `"models"`) {
		// The production comparator has no tie-breaker; preserve raw model objects and only order equal-count groups.
		var envelope struct {
			Success bool `json:"success"`
			Data    struct {
				Models []json.RawMessage `json:"models"`
			} `json:"data"`
		}
		require.NoError(t, common.UnmarshalJsonStr(raw, &envelope))
		if len(envelope.Data.Models) > 1 && os.Getenv("SYSTEM_CONTRACT_CASE") == "metrics_summary_ties" {
			ordered := sf.Order(t, "metrics_summary_ties", envelope.Data.Models)
			data, err := common.Marshal(ordered)
			require.NoError(t, err)
			start := strings.Index(raw, `"models":[`) + len(`"models":`)
			require.Greater(t, start, 0)
			decoder := common.NewJSONStreamDecoder(strings.NewReader(raw[start:]))
			var original json.RawMessage
			require.NoError(t, decoder.Decode(&original))
			end := start + int(decoder.InputOffset())
			raw = raw[:start] + string(data) + raw[end:]
			require.Contains(t, raw, `"success":true`)
		}
	}
	return raw
}

func setupCase(t *testing.T, tc systemCase, request func(string, string, string) *httptest.ResponseRecorder, extra map[string]any) {
	constant.Setup = false
	if !slices.Contains([]string{"get_root", "get_complete", "guard", "existing_root_invalid", "startup_root", "startup_complete", "startup_create_fault"}, tc.Variant) {
		require.NoError(t, model.DB.Unscoped().Where("role=?", common.RoleRootUser).Delete(&model.User{}).Error)
	}
	if strings.HasPrefix(tc.Variant, "get_") {
		constant.Setup = tc.Variant == "get_complete"
		request("GET", "/api/setup", "")
		return
	}
	if tc.Variant == "guard" {
		constant.Setup = true
		request("POST", "/api/setup", "{")
		return
	}
	if strings.HasPrefix(tc.Variant, "startup_") {
		if tc.Variant == "startup_complete" {
			require.NoError(t, model.DB.Create(&model.Setup{Version: "older", InitializedAt: fixture.Epoch - 5}).Error)
		}
		if tc.Variant == "startup_create_fault" {
			createFault(t, "setups")
		}
		systemruntime.CheckSetup()
		request("GET", "/api/setup", "")
		return
	}
	username, password, confirmation := "initial-root", "password123", "password123"
	self, demo := false, false
	switch tc.Variant {
	case "username_12":
		username = "abcdefghijkl"
	case "username_13":
		username = "abcdefghijklm"
	case "username_multibyte":
		username = strings.Repeat("я", 7)
	case "empty_username":
		username = ""
	case "mismatch":
		confirmation = "different"
	case "password_8":
		password = "12345678"
		confirmation = password
	case "password_128":
		password = strings.Repeat("x", 128)
		confirmation = password
	case "short":
		password = "1234567"
		confirmation = password
	case "long":
		password = strings.Repeat("x", 129)
		confirmation = password
	case "unicode":
		password = strings.Repeat("я", 8)
		confirmation = password
	case "whitespace":
		password = "        "
		confirmation = password
	case "existing_root_invalid":
		username = strings.Repeat("x", 13)
		password = "x"
		confirmation = "different"
	case "create_fault":
		createFault(t, "users")
	case "setup_fault":
		createFault(t, "setups")
	case "options_fault":
		createFault(t, "options")
	case "hash_fault":
		t.Setenv("ACCOUNT_PASSWORD_HASH_ALGORITHM", "unsupported")
	case "bcrypt_long":
		t.Setenv("ACCOUNT_PASSWORD_HASH_ALGORITHM", "bcrypt")
		password = strings.Repeat("x", 73)
		confirmation = password
	case "modes_self":
		self = true
	case "modes_demo":
		demo = true
	case "modes_both":
		self, demo = true, true
	}
	body, err := common.Marshal(map[string]any{"username": username, "password": password,
		"confirmPassword": confirmation, "SelfUseModeEnabled": self, "DemoSiteEnabled": demo})
	require.NoError(t, err)
	switch tc.Variant {
	case "malformed":
		body = []byte("{")
	case "null":
		body = []byte("null")
	case "array":
		body = []byte("[]")
	case "username_type":
		body = []byte(`{"username":1}`)
	case "body_limit":
		body = []byte(strings.Repeat("x", 1025))
	}
	request("POST", "/api/setup", string(body))
	if tc.Variant == "repeat" {
		request("POST", "/api/setup", string(body))
	}
	request("GET", "/api/setup", "")
	request("GET", "/api/status", "")
	if tc.Variant != "existing_root_invalid" {
		var rows []model.User
		require.NoError(t, model.DB.Where("role=?", common.RoleRootUser).Find(&rows).Error)
		for _, row := range rows {
			require.NotEqual(t, password, row.Password)
			assert.True(t, common.ValidatePasswordAndHash(password, row.Password))
			require.True(t, strings.HasPrefix(row.Password, "$argon2id$") || strings.HasPrefix(row.Password, "$2"))
			parts := strings.Split(row.Password, "$")
			shape := map[string]any{"algorithm": parts[1], "verified": true}
			if parts[1] == "argon2id" {
				require.Len(t, parts, 6)
				salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
				require.NoError(t, err)
				key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
				require.NoError(t, err)
				shape["version"], shape["parameters"], shape["salt_bytes"], shape["key_bytes"] = parts[2], parts[3], len(salt), len(key)
			} else {
				require.Len(t, parts, 4)
				shape["cost"], shape["encoded_length"] = parts[2], len(parts[3])
			}
			extra["password_hash"] = shape
			all := fixture.Rows(t, model.DB, "users", "*", "id")
			for _, raw := range all {
				if raw["id"] == int64(row.Id) {
					delete(raw, "password")
					extra["created_root_row"] = raw
				}
			}
		}
	}
}

func createFault(t *testing.T, table string) {
	t.Helper()
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("system_fixture_fault", func(db *gorm.DB) {
		if db.Statement.Table == table {
			db.AddError(errors.New("fixture create rejected"))
		}
	}))
}

func tasksCase(t *testing.T, tc systemCase, request func(string, string, string) *httptest.ResponseRecorder) {
	if tc.Variant != "empty" {
		for i, kind := range []string{"log_cleanup", "channel_test", "model_update",
			"midjourney_poll", "async_task_poll", "provider_batch_poll", "pricing_catalog",
			"model_cost_fx", "unknown"} {
			status := model.SystemTaskStatusSucceeded
			active := (*string)(nil)
			if tc.Variant == "current_pending" {
				status = model.SystemTaskStatusPending
				key := kind
				active = &key
			}
			if tc.Variant == "current_running" {
				status = model.SystemTaskStatusRunning
				key := kind
				active = &key
			}
			task := model.SystemTask{ID: int64(i + 1), TaskID: fmt.Sprintf("fixture-task-%d",
				i), Type: kind, Status: status, ActiveKey: active, CreatedAt: fixture.Epoch - int64(i),
				UpdatedAt: fixture.Epoch, Payload: `{"manual":false}`, State: `{"progress":50}`,
				Result: `{"count":0}`}
			if tc.Variant == "raw_json" {
				task.Payload = "broken"
				task.State = ""
				task.Result = "null"
			}
			require.NoError(t, model.DB.Create(&task).Error)
		}
	}
	path := "/api/system-task/list"
	switch tc.Variant {
	case "current_missing":
		path = "/api/system-task/current"
	case "current_unknown":
		path = "/api/system-task/current?type=absent"
	case "current_pending", "current_running", "current_terminal":
		path = "/api/system-task/current?type=channel_test"
	case "missing_detail":
		path = "/api/system-task/absent"
	case "raw_json":
		path = "/api/system-task/fixture-task-0"
	case "limit_bad":
		path += "?limit=bad"
	case "limit_zero":
		path += "?limit=0"
	case "limit_negative":
		path += "?limit=-1"
	case "limit_one":
		path += "?limit=1"
	case "limit_max":
		path += "?limit=100"
	case "limit_over":
		path += "?limit=101"
	case "cleanup_missing", "cleanup_bad", "cleanup_negative", "cleanup_first", "cleanup_existing":
		target := ""
		switch tc.Variant {
		case "cleanup_bad":
			target = "bad"
		case "cleanup_negative":
			target = "-1"
		case "cleanup_first", "cleanup_existing":
			target = "946684799"
		}
		request("POST", "/api/system-task/log-cleanup?target_timestamp="+target, "")
		if tc.Variant == "cleanup_existing" {
			request("POST", "/api/system-task/log-cleanup?target_timestamp="+target, "")
		}
		path = "/api/system-task/current?type=log_cleanup"
	case "read_fault":
		require.NoError(t, model.DB.Migrator().DropTable(&model.SystemTask{}))
		request("GET", path, "")
		require.NoError(t, model.DB.AutoMigrate(&model.SystemTask{}))
		return
	}
	request("GET", path, "")
}

func instancesCase(t *testing.T, tc systemCase, request func(string, string, string) *httptest.ResponseRecorder, extra map[string]any) {
	if strings.HasPrefix(tc.Variant, "report_") {
		if tc.Variant == "report_fallback" {
			common.NodeName = ""
		}
		require.NoError(t, systemruntime.SystemContractReportInstance())
		if tc.Variant == "report_repeat" {
			time.Sleep(time.Second)
			require.NoError(t, systemruntime.SystemContractReportInstance())
		}
		hostname, err := os.Hostname()
		require.NoError(t, err)
		var rows []model.SystemInstance
		require.NoError(t, model.DB.Find(&rows).Error)
		require.Len(t, rows, 1)
		var info map[string]any
		require.NoError(t, common.UnmarshalJsonStr(rows[0].Info, &info))
		require.Equal(t, hostname, info["host"].(map[string]any)["hostname"])
		info["host"].(map[string]any)["hostname"] = "<fixture-hostname>"
		info["runtime"].(map[string]any)["goos"] = "<goos>"
		info["runtime"].(map[string]any)["goarch"] = "<goarch>"
		resources := info["resources"].(map[string]any)
		for _, key := range []string{"total_bytes", "used_bytes", "free_bytes", "used_percent"} {
			v := resources["storage"].(map[string]any)[key]
			require.GreaterOrEqual(t, v.(float64), float64(0))
			resources["storage"].(map[string]any)[key] = 0
		}
		if tc.Variant == "report_fallback" {
			require.Equal(t, hostname, rows[0].NodeName)
			info["node"].(map[string]any)["name"] = "<fixture-hostname>"
			rows[0].NodeName = "<fixture-hostname>"
		}
		extra["reported_identity_checked"] = true
	} else if tc.Variant != "empty" {
		for i, age := range []int64{0, 90, 91, 200} {
			data := `{"schema_version":1,"zero":0,"empty":{}}`
			if tc.Variant == "raw_info" {
				data = []string{"", "null", "broken", "[]"}[i]
			}
			require.NoError(t, model.DB.Create(&model.SystemInstance{NodeName: fmt.Sprintf("node-%d",
				i), Info: data, StartedAt: fixture.Epoch - 1000, LastSeenAt: fixture.Epoch - age,
				CreatedAt: fixture.Epoch - 2000, UpdatedAt: fixture.Epoch - age}).Error)
		}
	}
	switch tc.Variant {
	case "delete_all":
		request("DELETE", "/api/system-info/stale-instances", "")
	case "delete_single":
		request("DELETE", "/api/system-info/instances/node-2", "")
	case "delete_online":
		request("DELETE", "/api/system-info/instances/node-1", "")
	case "delete_missing":
		request("DELETE", "/api/system-info/instances/missing", "")
	case "delete_blank":
		request("DELETE", "/api/system-info/instances/%20", "")
	case "delete_spaces":
		request("DELETE", "/api/system-info/instances/%20node-2%20", "")
	case "read_fault":
		require.NoError(t, model.DB.Migrator().DropTable(&model.SystemInstance{}))
		request("GET", "/api/system-info/instances", "")
		require.NoError(t, model.DB.AutoMigrate(&model.SystemInstance{}))
		return
	}
	request("GET", "/api/system-info/instances", "")
}

func performanceCase(t *testing.T, tc systemCase, cacheDir string, request func(string,
	string, string) *httptest.ResponseRecorder, extra map[string]any) {
	common.ResetDiskCacheStats()
	common.IncrementDiskCacheHits()
	common.IncrementMemoryCacheHits()
	switch tc.Variant {
	case "stats":
		require.NoError(t, os.MkdirAll(common.GetDiskCacheDir(), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(common.GetDiskCacheDir(), "fixture.tmp"), []byte("12345"), 0600))
		request("GET", "/api/performance/stats", "")
	case "clear", "clear_missing", "clear_fault":
		dir := common.GetDiskCacheDir()
		if tc.Variant != "clear_missing" {
			require.NoError(t, os.MkdirAll(dir, 0755))
			old := filepath.Join(dir, "old.tmp")
			require.NoError(t, os.WriteFile(old, []byte("123"), 0600))
			require.NoError(t, os.Chtimes(old, time.Now().Add(-11*time.Minute), time.Now().Add(-11*time.Minute)))
			common.IncrementDiskFiles(3)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "fresh.tmp"), []byte("12"), 0600))
		}
		if tc.Variant == "clear_fault" {
			require.NoError(t, os.RemoveAll(dir))
			require.NoError(t, os.WriteFile(dir, []byte("file"), 0600))
		}
		request("DELETE", "/api/performance/disk_cache", "")
		if tc.Variant == "clear" {
			_, err := os.Stat(filepath.Join(dir, "old.tmp"))
			assert.True(t, os.IsNotExist(err))
			_, err = os.Stat(filepath.Join(dir, "fresh.tmp"))
			require.NoError(t, err)
		}
	case "reset":
		common.IncrementDiskFiles(7)
		request("POST", "/api/performance/reset_stats", "")
		assert.Equal(t, int64(1), common.GetDiskCacheStats().ActiveDiskFiles)
	case "gc":
		request("POST", "/api/performance/gc", "")
	default:
		dir := filepath.Join(cacheDir, "logs")
		require.NoError(t, os.MkdirAll(dir, 0755))
		*common.LogDir = dir
		if tc.Variant == "logs_disabled" || tc.Variant == "logs_unconfigured" {
			*common.LogDir = ""
		}
		if slices.Contains([]string{"logs_files", "logs_count", "logs_days", "logs_partial"}, tc.Variant) {
			for i, name := range []string{"oneapi-20200101.log", "oneapi-20000102.log", "oneapi-20000101.log", "other.log", "oneapi-skip.txt"} {
				p := filepath.Join(dir, name)
				require.NoError(t, os.WriteFile(p, []byte(strings.Repeat("x", i+1)), 0600))
				when := time.Now().AddDate(0, 0, -i)
				require.NoError(t, os.Chtimes(p, when, when))
			}
			require.NoError(t, os.Mkdir(filepath.Join(dir, "oneapi-directory.log"), 0755))
		}
		path := "/api/performance/logs"
		method := "GET"
		switch tc.Variant {
		case "logs_count":
			method = "DELETE"
			path += "?mode=by_count&value=1"
		case "logs_days":
			method = "DELETE"
			path += "?mode=by_days&value=1"
		case "logs_partial":
			require.NoError(t, os.Chmod(dir, 0555))
			defer func() { require.NoError(t, os.Chmod(dir, 0755)) }()
			method = "DELETE"
			path += "?mode=by_count&value=1"
		case "logs_bad_mode":
			method = "DELETE"
			path += "?mode=bad&value=1"
		case "logs_bad_value":
			method = "DELETE"
			path += "?mode=by_count&value=bad"
		case "logs_zero":
			method = "DELETE"
			path += "?mode=by_count&value=0"
		case "logs_unconfigured":
			method = "DELETE"
			path += "?mode=by_count&value=1"
		case "logs_fault":
			require.NoError(t, os.Remove(dir))
			require.NoError(t, os.WriteFile(dir, []byte("file"), 0600))
		}
		request(method, path, "")
		if method == "DELETE" && *common.LogDir != "" {
			request("GET", "/api/performance/logs", "")
		}
		if tc.Variant != "logs_fault" {
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			files := []string{}
			for _, e := range entries {
				files = append(files, e.Name())
			}
			extra["files"] = files
		}
	}
}

func metricsCase(t *testing.T, tc systemCase, request func(string, string, string) *httptest.ResponseRecorder) {
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"active":2}`))
	if !slices.Contains([]string{"missing_model", "empty", "nav_auth", "nav_disabled"}, tc.Variant) {
		for i, group := range []string{"default", "active", "inactive", "auto"} {
			require.NoError(t, metricsstorage.UpsertPerfMetric(&model.PerfMetric{ModelName: "fixture",
				Group: group, BucketTs: fixture.Epoch - 3600, RequestCount: 2, SuccessCount: 1,
				TotalLatencyMs: int64(200 + i), TtftSumMs: 40, TtftCount: 1, OutputTokens: 20,
				GenerationMs: 1000}))
		}
	}
	if tc.Variant == "hot" || tc.Variant == "mixed" {
		perfmetrics.Record(metricsdomain.Sample{Model: "fixture", Group: "active", LatencyMs: 300,
			TtftMs: 20, HasTtft: true, Success: true, OutputTokens: 30, GenerationMs: 200})
	}
	path := "/api/perf-metrics?model=fixture"
	switch tc.Variant {
	case "missing_model":
		path = "/api/perf-metrics"
	case "inactive":
		path += "&group=inactive"
	case "auto":
		path += "&group=auto"
	case "disabled":
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("perf_metrics_setting"), map[string]string{"enabled": "false"}))
	case "hours_bad":
		path += "&hours=bad"
	case "hours_zero":
		path += "&hours=0"
	case "hours_negative":
		path += "&hours=-1"
	case "hours_max":
		path += "&hours=720"
	case "hours_over":
		path += "&hours=721"
	case "summary", "summary_ties":
		path = "/api/perf-metrics/summary"
		if tc.Variant == "summary_ties" {
			require.NoError(t, metricsstorage.UpsertPerfMetric(&model.PerfMetric{ModelName: "second",
				Group: "default", BucketTs: fixture.Epoch - 3600, RequestCount: 6, SuccessCount: 4,
				TotalLatencyMs: 500, GenerationMs: 2000, OutputTokens: 17}))
		}
	case "nav_auth":
		common.OptionMap["HeaderNavModules"] = `{"pricing":{"enabled":true,"requireAuth":true}}`
	case "nav_disabled":
		common.OptionMap["HeaderNavModules"] = `{"pricing":false}`
	case "read_fault":
		require.NoError(t, model.DB.Migrator().DropTable(&model.PerfMetric{}))
		request("GET", path, "")
		require.NoError(t, model.DB.AutoMigrate(&model.PerfMetric{}))
		return
	}
	request("GET", path, "")
}

func uptimeCase(t *testing.T, tc systemCase, request func(string, string, string) *httptest.ResponseRecorder) {
	groups := []map[string]any{{"url": "https://uptime.fixture/", "slug": "public", "categoryName": "First"}}
	switch tc.Variant {
	case "empty":
		groups = nil
	case "null":
		console_setting.GetConsoleSetting().UptimeKumaGroups = "null"
		request("GET", "/api/uptime/status", "")
		return
	case "malformed":
		console_setting.GetConsoleSetting().UptimeKumaGroups = "{"
		request("GET", "/api/uptime/status", "")
		return
	case "missing_url":
		delete(groups[0], "url")
	case "missing_slug":
		delete(groups[0], "slug")
	case "multiple":
		groups = append(groups, map[string]any{"url": "https://uptime.fixture", "slug": "second", "categoryName": "Second"})
	case "ui_disabled":
		console_setting.GetConsoleSetting().UptimeKumaEnabled = false
	case "trailing_slashes":
		groups[0]["url"] = "https://uptime.fixture///"
	}
	data, err := common.Marshal(groups)
	require.NoError(t, err)
	console_setting.GetConsoleSetting().UptimeKumaGroups = string(data)
	r := request("GET", "/api/uptime/status", "")
	assert.Equal(t, 200, r.Code)
	assert.Contains(t, r.Body.String(), `"success":true`)
}

// canonicalInstanceInfo validates volatile resource values before masking those exact lexemes.
func canonicalInstanceInfo(t *testing.T, raw string) string {
	t.Helper()
	if !strings.Contains(raw, `"hostname":`) {
		return raw
	}
	hostname, err := os.Hostname()
	require.NoError(t, err)
	for _, key := range []string{"hostname", "node_name", "name"} {
		old, err := common.Marshal(hostname)
		require.NoError(t, err)
		raw = strings.ReplaceAll(raw, `"`+key+`":`+string(old), `"`+key+`":"<fixture-hostname>"`)
	}
	for _, key := range []string{"goos", "goarch"} {
		raw = regexp.MustCompile(`("`+key+`":)"[^"]*"`).ReplaceAllString(raw, `${1}"<runtime>"`)
	}
	for _, key := range []string{"total_bytes", "used_bytes", "free_bytes", "used_percent"} {
		raw = regexp.MustCompile(`("`+key+`":)([0-9]+(?:\.[0-9]+)?)`).ReplaceAllString(raw, "${1}0")
	}
	return raw
}
