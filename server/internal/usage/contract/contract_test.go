package contract_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/contracttest"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	readStart = int64(1706742000) // January 31, 2024, 23:00 UTC.
	readEnd   = int64(1706832000) // February 2, 2024, 00:00 UTC.
	readKey   = "wave3areadcontractkey000000000001"
)

var readManifest = flag.Bool("wave3a-manifest", false, "print the reviewed case manifest without opening a database")
var searchLocalePreflight = flag.Bool("wave3a-search-locale-preflight", false, "check pre-existing search error parity before recording")

type readCase struct {
	name, group, actor, method, path, body, locale string
	status                                         int
	failure                                        bool
}

func readCases() []readCase {
	var cases []readCase
	endpoints := []struct{ name, method, path, body, access string }{
		{"logs", "GET", "/api/log/", "", "admin"},
		{"user_logs", "GET", "/api/log/user/1", "", "admin"},
		{"query_logs", "POST", "/api/log/query", `{"user_ids":[1]}`, "admin"},
		{"own_logs", "GET", "/api/log/self", "", "user"},
		{"search", "GET", "/api/log/search", "", "deprecated_admin"},
		{"own_search", "GET", "/api/log/self/search", "", "deprecated_user"},
		{"stats", "GET", "/api/log/stat", "", "admin"},
		{"own_stats", "GET", "/api/log/self/stat", "", "user"},
		{"log_aggregate", "POST", "/api/log/aggregate", aggregateBody("time", "hour"), "admin"},
		{"usage_aggregate", "POST", "/api/usage/aggregate", aggregateBody("time", "hour"), "admin"},
		{"key_logs", "GET", "/api/log/token", "", "key"},
		{"data", "GET", dataPath("/api/data/"), "", "admin"},
		{"user_data", "GET", dataPath("/api/data/users"), "", "admin"},
		{"own_data", "GET", dataPath("/api/data/self"), "", "user"},
		{"flow", "GET", dataPath("/api/data/flow"), "", "admin"},
		{"own_flow", "GET", dataPath("/api/data/flow/self"), "", "user"},
		{"rankings", "GET", "/api/rankings", "", "public"},
	}
	for _, endpoint := range endpoints {
		for _, actor := range []string{"anonymous", "user", "admin", "root", "pat"} {
			allowed := endpoint.access == "public" ||
				(strings.Contains(endpoint.access, "user") && actor != "anonymous") ||
				(strings.Contains(endpoint.access, "admin") && (actor == "admin" || actor == "root"))
			cases = append(cases, readCase{
				name: "route_" + endpoint.name + "_" + actor, group: "routes", actor: actor,
				method: endpoint.method, path: endpoint.path, body: endpoint.body,
				failure: !allowed || strings.HasPrefix(endpoint.access, "deprecated"),
			})
		}
	}
	for _, actor := range []string{"key", "disabled_key", "expired_key", "empty_key"} {
		cases = append(cases, readCase{name: "key_" + actor, group: "logs", actor: actor, path: "/api/log/token",
			failure: actor == "disabled_key"})
	}
	logQueries := []struct{ name, query string }{
		{"all", ""}, {"consume", "type=2"}, {"error", "type=5"}, {"unknown_type", "type=99"},
		{"model_exact", "model_name=Model"}, {"model_pattern", "model_name=%25odel%25"},
		{"username_pattern", "username=contract-%25"}, {"token_name", "token_name=read-token"},
		{"channel", "channel=11"}, {"group", "group=paid"}, {"request", "request_id=read-101"},
		{"upstream_request", "upstream_request_id=upstream-102"},
		{"time_inclusive", fmt.Sprintf("start_timestamp=%d&end_timestamp=%d", readStart, readEnd)},
		{"single_timestamp", fmt.Sprintf("start_timestamp=%d&end_timestamp=%d", readStart, readStart)},
		{"page_two", "p=2&page_size=2"}, {"page_empty", "p=99&page_size=2"},
		{"page_alias_ps", "p=1&ps=2"}, {"page_alias_size", "size=2"},
		{"page_cap", "page_size=101"}, {"negative_page", "p=-1&page_size=2"},
		{"negative_size", "page_size=-1"}, {"empty", "request_id=absent"},
	}
	for _, item := range logQueries {
		for _, actor := range []string{"root", "user"} {
			path := "/api/log/"
			if actor == "user" {
				path = "/api/log/self"
			}
			cases = append(cases, readCase{name: "logs_" + actor + "_" + item.name,
				group: "logs", actor: actor, path: path + "?" + item.query})
		}
	}
	for _, actor := range []string{"user", "admin", "root"} {
		path := "/api/log/"
		if actor == "user" {
			path = "/api/log/self"
		}
		for _, value := range []string{"scopes", "legacy", "malformed", "null", "unchanged", "scoped_rejection"} {
			cases = append(cases, readCase{name: "visibility_" + actor + "_" + value,
				group: "logs", actor: actor, path: path + "?request_id=" + value})
		}
	}
	for _, item := range []struct{ name, query string }{
		{"owned", "token_id=11"}, {"literal_substring", "token_id=11&model_name=" + url.QueryEscape("EL_%!\\")},
		{"foreign", "token_id=12"}, {"deleted", "token_id=13"}, {"missing", "token_id=999"},
		{"zero", "token_id=0"}, {"negative", "token_id=-1"}, {"empty", "token_id="},
		{"invalid", "token_id=abc"},
	} {
		cases = append(cases, readCase{name: "own_token_" + item.name, group: "logs", actor: "user",
			path: "/api/log/self?" + item.query, failure: item.name != "owned" && item.name != "literal_substring"})
	}
	for _, pattern := range []struct{ name, value string }{
		{"consecutive", "%%"}, {"short", "%a%"}, {"too_many", "%ab%cd%"},
	} {
		cases = append(cases, readCase{name: "invalid_pattern_" + pattern.name, group: "logs",
			actor: "root", path: "/api/log/?model_name=" + url.QueryEscape(pattern.value), status: 500, failure: true})
	}
	cases = append(cases, readCase{name: "shared_db_logs", group: "shared_db", actor: "root",
		path: "/api/log/?request_id=scopes"})
	for _, endpoint := range []struct{ name, path, actor string }{
		{"logs", "/api/log/", "root"}, {"flow_root", dataPath("/api/data/flow"), "root"},
		{"flow_admin", dataPath("/api/data/flow"), "admin"},
	} {
		cases = append(cases, readCase{name: "cache_" + endpoint.name, group: "channel_cache",
			actor: endpoint.actor, path: endpoint.path})
	}
	cases = append(cases, strictLogCases()...)
	cases = append(cases, quotaCases()...)
	cases = append(cases, aggregateCases()...)
	for _, actor := range []string{"root", "user"} {
		for _, item := range []struct{ name, query string }{
			{"all", ""}, {"ignored_type", "type=5"}, {"historical", dataPath("")[1:]},
			{"model", "model_name=Model"}, {"model_pattern", "model_name=%25odel%25"},
			{"token", "token_name=read-token"}, {"channel", "channel=11"}, {"group", "group=paid"},
			{"username", "username=contract-admin"}, {"empty", "model_name=absent"},
		} {
			path := "/api/log/stat"
			if actor == "user" {
				path = "/api/log/self/stat"
			}
			cases = append(cases, readCase{name: "stats_" + actor + "_" + item.name,
				group: "stats", actor: actor, path: path + "?" + item.query})
		}
	}
	for _, period := range []string{"default", "empty", "today", "week", "month", "year", "invalid"} {
		path := "/api/rankings?period=" + period
		if period == "default" {
			path = "/api/rankings"
		}
		if period == "empty" {
			path = "/api/rankings?period="
		}
		cases = append(cases, readCase{name: "rankings_empty_" + period, group: "rankings_empty",
			actor: "anonymous", path: path, failure: period == "invalid"})
	}
	for _, period := range []string{"today", "week", "month", "year"} {
		cases = append(cases, readCase{name: "rankings_populated_" + period,
			group: "rankings_" + period, actor: "anonymous", path: "/api/rankings?period=" + period})
	}
	for _, mode := range []string{"disabled", "auth_anonymous", "auth_user"} {
		actor := "anonymous"
		if mode == "auth_user" {
			actor = "user"
		}
		cases = append(cases, readCase{name: "nav_" + mode, group: "nav_" + mode,
			actor: actor, path: "/api/rankings", failure: mode != "auth_user"})
	}
	for _, name := range []string{"query_null", "deprecated"} {
		path, method, body := "/api/log/search", "GET", ""
		if name == "query_null" {
			path, method, body = "/api/log/query", "POST", `{"user_ids":null}`
		}
		cases = append(cases, readCase{name: "zh_tw_" + name, group: "locale", actor: "root",
			path: path, method: method, body: body, locale: "zh-TW", failure: true})
	}
	for i := range cases {
		if cases[i].locale == "" {
			cases[i].locale = "zh-CN"
		}
	}
	return cases
}

func dataPath(path string) string {
	return fmt.Sprintf("%s?start_timestamp=%d&end_timestamp=%d", path, readStart, readEnd)
}

func aggregateBody(group, granularity string) string {
	return fmt.Sprintf(`{"user_ids":[2,1,1],"start_timestamp":%d,"end_timestamp":%d,"group_by":%q,"granularity":%q}`,
		readStart, readEnd, group, granularity)
}

func strictLogCases() []readCase {
	var cases []readCase
	for _, item := range []struct{ name, body string }{
		{"empty_ids", `{"user_ids":[]}`}, {"dedup", `{"user_ids":[2,1,1],"p":1,"page_size":2}`},
		{"missing_user", `{"user_ids":[999]}`}, {"unknown_field", `{"user_ids":[1],"unused":true}`},
		{"missing_ids", `{}`}, {"null", `{"user_ids":null}`}, {"null_extra", `{"user_ids":[1],"unused":null}`},
		{"negative_id", `{"user_ids":[-1]}`}, {"unsafe_id", `{"user_ids":[2147483648]}`},
		{"page_zero", `{"user_ids":[1],"p":0}`}, {"page_overflow", `{"user_ids":[1],"p":9223372036854775807}`},
		{"size_zero", `{"user_ids":[1],"page_size":0}`}, {"size_over", `{"user_ids":[1],"page_size":101}`},
		{"type_invalid", `{"user_ids":[1],"type":8}`}, {"negative_time", `{"user_ids":[1],"start_timestamp":-1}`},
		{"reversed_time", `{"user_ids":[1],"start_timestamp":2,"end_timestamp":1}`},
		{"bad_pattern", `{"user_ids":[1],"model_name":"%%"}`}, {"bad_json", `{`},
		{"invalid_utf8", "{\"user_ids\":[1],\"extra\":\"\xff\"}"},
		{"oversize", `{"user_ids":[1],"extra":"` + strings.Repeat("x", 256<<10) + `"}`},
		{"too_many_ids", `{"user_ids":[` + strings.Repeat("1,", 16384) + `1]}`},
		{"model_too_long", `{"user_ids":[1],"model_name":"` + strings.Repeat("a", (16<<10)+1) + `"}`},
	} {
		valid := slices.Contains([]string{"empty_ids", "dedup", "missing_user", "unknown_field"}, item.name)
		cases = append(cases, readCase{name: "strict_" + item.name, group: "strict", actor: "root",
			method: "POST", path: "/api/log/query", body: item.body, failure: !valid})
	}
	for _, item := range []struct{ name, path string }{
		{"user", "/api/log/user/1"}, {"zero_user", "/api/log/user/0"},
		{"large_user", "/api/log/user/2147483648"}, {"invalid_user", "/api/log/user/no"},
		{"repeat_page", "/api/log/user/1?p=1&p=2"}, {"repeat_time", "/api/log/user/1?start_timestamp=1&start_timestamp=2"},
		{"malformed_query", "/api/log/user/1?model_name=%ZZ"}, {"empty_page", "/api/log/user/1?p="},
	} {
		cases = append(cases, readCase{name: "strict_get_" + item.name, group: "strict", actor: "root",
			path: item.path, failure: item.name != "user"})
	}
	return cases
}

func quotaCases() []readCase {
	var cases []readCase
	for _, endpoint := range []struct{ name, path, actor string }{
		{"all", "/api/data/", "root"}, {"users", "/api/data/users", "admin"}, {"own", "/api/data/self", "user"},
	} {
		for _, granularity := range []string{"legacy", "hour", "day", "week", "month"} {
			path := dataPath(endpoint.path)
			if granularity != "legacy" {
				path += "&granularity=" + granularity
			}
			cases = append(cases, readCase{name: "data_" + endpoint.name + "_" + granularity,
				group: "data", actor: endpoint.actor, path: path})
		}
	}
	for _, item := range []struct {
		name, path, actor string
		failure           bool
	}{
		{"username", dataPath("/api/data/") + "&username=contract-user", "root", false},
		{"username_bucket", dataPath("/api/data/") + "&username=contract-user&granularity=hour", "root", false},
		{"empty", dataPath("/api/data/") + "&username=absent", "root", false},
		{"own_token", dataPath("/api/data/self") + "&token_id=11", "user", false},
		{"own_foreign_token", dataPath("/api/data/self") + "&token_id=12", "user", false},
		{"own_deleted_token", dataPath("/api/data/self") + "&token_id=13", "user", false},
		{"own_empty_token", dataPath("/api/data/self") + "&token_id=", "user", false},
		{"own_invalid_token", dataPath("/api/data/self") + "&token_id=no", "user", true},
		{"own_zero_token", dataPath("/api/data/self") + "&token_id=0", "user", true},
		{"bad_granularity", dataPath("/api/data/") + "&granularity=minute", "root", true},
		{"bucket_no_time", "/api/data/?granularity=hour", "root", true},
		{"legacy_no_time", "/api/data/", "root", false},
		{"bucket_reversed", "/api/data/?start_timestamp=200&end_timestamp=100&granularity=hour", "root", false},
		{"own_long", "/api/data/self?start_timestamp=1&end_timestamp=2592002", "user", true},
		{"own_reversed", "/api/data/self?start_timestamp=200&end_timestamp=100", "user", false},
		{"flow_root", dataPath("/api/data/flow"), "root", false},
		{"flow_admin", dataPath("/api/data/flow"), "admin", false},
		{"flow_user", dataPath("/api/data/flow/self"), "user", false},
		{"flow_username", dataPath("/api/data/flow") + "&username=contract-user", "admin", false},
		{"flow_empty", dataPath("/api/data/flow") + "&username=absent", "root", false},
		{"flow_missing_start", "/api/data/flow?end_timestamp=200", "root", true},
		{"flow_missing_end", "/api/data/flow?start_timestamp=100", "root", true},
		{"flow_reversed", "/api/data/flow?start_timestamp=200&end_timestamp=100", "root", true},
		{"flow_long", "/api/data/flow/self?start_timestamp=1&end_timestamp=2592002", "user", true},
	} {
		cases = append(cases, readCase{name: "quota_" + item.name, group: "data",
			actor: item.actor, path: item.path, failure: item.failure})
	}
	for _, boundary := range []struct {
		name       string
		start, end int64
	}{
		{"leap", 1709161200, 1709251200}, {"monday", 1709506800, 1709514000},
		{"year", 1735686000, 1735689600}, {"month120", 1577836800, 1893455999},
		{"month121", 1577836800, 1893456000},
	} {
		for _, granularity := range []string{"hour", "day", "week", "month"} {
			path := fmt.Sprintf("/api/data/?start_timestamp=%d&end_timestamp=%d&granularity=%s",
				boundary.start, boundary.end, granularity)
			cases = append(cases, readCase{name: "boundary_" + boundary.name + "_" + granularity,
				group: "data", actor: "root", path: path, failure: boundary.name == "month121" && granularity == "month"})
		}
	}
	return cases
}

func aggregateCases() []readCase {
	var cases []readCase
	for _, group := range []string{"time", "model", "time_model", "user"} {
		for _, granularity := range []string{"hour", "day", "week", "month"} {
			cases = append(cases, readCase{name: "aggregate_" + group + "_" + granularity, group: "aggregate",
				actor: "root", method: "POST", path: "/api/usage/aggregate", body: aggregateBody(group, granularity)})
		}
	}
	for _, endpoint := range []string{"log", "usage"} {
		path := "/api/" + endpoint + "/aggregate"
		for _, item := range []struct {
			name, body string
			failure    bool
		}{
			{"empty", `{"user_ids":[3],"start_timestamp":1,"end_timestamp":2,"group_by":"user","granularity":"day"}`, false},
			{"soft_deleted", `{"user_ids":[4],"start_timestamp":0,"end_timestamp":0,"group_by":"user","granularity":"day"}`, false},
			{"missing_user", `{"user_ids":[999],"start_timestamp":1,"end_timestamp":2,"group_by":"user","granularity":"day"}`, true},
			{"missing_time", `{"user_ids":[1],"group_by":"time","granularity":"day"}`, true},
			{"null_time", `{"user_ids":[1],"start_timestamp":null,"end_timestamp":2}`, true},
			{"zero_ids", `{"user_ids":[0],"start_timestamp":0,"end_timestamp":0}`, true},
			{"no_ids", `{"user_ids":[],"start_timestamp":0,"end_timestamp":0}`, true},
			{"unsafe_id", `{"user_ids":[9007199254740992],"start_timestamp":0,"end_timestamp":0}`, true},
			{"reversed", `{"user_ids":[1],"start_timestamp":2,"end_timestamp":1}`, true},
			{"unsafe_time", `{"user_ids":[1],"start_timestamp":0,"end_timestamp":9007199254740992}`, true},
			{"bad_json", `{`, true}, {"too_large", strings.Repeat(" ", (16<<10)+1), true},
			{"too_many_users", `{"user_ids":[` + strings.Repeat("1,", 100) + `1],"start_timestamp":0,"end_timestamp":0}`, true},
		} {
			cases = append(cases, readCase{name: endpoint + "_aggregate_" + item.name, group: "aggregate",
				actor: "root", method: "POST", path: path, body: item.body, failure: item.failure})
		}
	}
	for _, item := range []struct{ name, body string }{
		{"invalid_group", aggregateBody("bad", "day")}, {"missing_granularity", aggregateBody("user", "")},
		{"invalid_granularity", aggregateBody("model", "minute")},
		{"month_limit", `{"user_ids":[1],"start_timestamp":1577836800,"end_timestamp":1893456000,"group_by":"time","granularity":"month"}`},
	} {
		cases = append(cases, readCase{name: "aggregate_" + item.name, group: "aggregate", actor: "root",
			method: "POST", path: "/api/usage/aggregate", body: item.body, failure: true})
	}
	for _, name := range []string{"log_overflow", "usage_overflow", "row_limit", "response_limit", "exact_large", "bucket_large"} {
		path := "/api/usage/aggregate"
		method, body := "POST", aggregateBody("model", "day")
		if name == "log_overflow" {
			path = "/api/log/aggregate"
		}
		if name == "bucket_large" {
			method, body = "GET", ""
			path = dataPath("/api/data/") + "&granularity=hour"
		}
		cases = append(cases, readCase{name: "aggregate_special_" + name, group: "special_" + name,
			actor: "root", method: method, path: path, body: body, failure: name != "exact_large" && name != "bucket_large"})
	}
	return cases
}

// Each group has its own process: real router rate limiters, pricing/ranking
// caches and model globals cannot leak across fixtures or repeat runs.
func TestUsageReadContract(t *testing.T) {
	if flag.Lookup("update").Value.String() == "true" {
		require.Equal(t, "6b0f439a432ab129817e4e41ce323e6bd3c3c0c3", os.Getenv("WAVE3A_ORIGIN_RECORD"),
			"host-only recording requires the pinned origin archive")
		directory, err := os.Getwd()
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(directory, "/dev/shm/wave3a-origin-main."), "never update on the branch")
	}
	if *searchLocalePreflight {
		checkSearchLocaleParity(t)
		return
	}
	if *readManifest {
		for _, name := range readGoldenNames() {
			fmt.Println("WAVE3A_GOLDEN " + name + ".golden.json")
		}
		return
	}
	if destination := os.Getenv("WAVE3A_DEFAULT_EVIDENCE"); destination != "" {
		writeDefaultLocaleEvidence(t, destination)
		return
	}
	group := os.Getenv("WAVE3A_READ_GROUP")
	if group == "" {
		groups := []string{"jobs", "cleanup", "failures"}
		for _, tc := range readCases() {
			if !slices.Contains(groups, tc.group) {
				groups = append(groups, tc.group)
			}
		}
		for _, name := range groups {
			t.Run(name, func(t *testing.T) { runReadProcess(t, name) })
		}
		return
	}
	switch group {
	case "jobs":
		runQuotaAggregation(t)
	case "cleanup":
		runLogCleanup(t)
	case "failures":
		runReadFailures(t)
	default:
		runReadRequests(t, group)
	}
}

func runReadProcess(t *testing.T, group string) {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestUsageReadContract$", "-test.count=1",
		"-test.timeout=5m", "-update="+flag.Lookup("update").Value.String())
	command.Env = append(os.Environ(), "WAVE3A_READ_GROUP="+group, "TZ=UTC")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func readGoldenNames() []string {
	names := []string{"usage_read_jobs", "usage_read_cleanup", "usage_read_failures"}
	for _, tc := range readCases() {
		names = append(names, "usage_read_"+tc.name)
	}
	slices.Sort(names)
	return names
}

func newReadEnvironment(t *testing.T) *contracttest.Env {
	t.Helper()
	env := contracttest.New(t)
	oldMemory, oldBatch := common.MemoryCacheEnabled, common.BatchUpdateEnabled
	common.MemoryCacheEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() { common.MemoryCacheEnabled, common.BatchUpdateEnabled = oldMemory, oldBatch })
	// Both databases contain logs: reading the wrong handle produces valid but
	// observably different data rather than an incidental missing-table error.
	logDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "read-log.db")), &gorm.Config{})
	require.NoError(t, err)
	connection, err := logDB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, connection.Close()) })
	previousLog := model.LOG_DB
	model.LOG_DB = logDB
	t.Cleanup(func() { model.LOG_DB = previousLog })
	require.NoError(t, logDB.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	require.NoError(t, env.DB.Create(&model.Log{Id: 9001, UserId: 1, Type: model.LogTypeConsume,
		CreatedAt: readStart, Quota: 999999, ModelName: "wrong-main-db", RequestId: "main-sentinel"}).Error)
	for _, channel := range []model.Channel{
		{Id: 11, Name: "read-channel", Key: "fixture", Type: 1, Status: 1},
		{Id: 12, Name: "other-channel", Key: "fixture", Type: 1, Status: 1},
	} {
		require.NoError(t, env.DB.Create(&channel).Error)
	}
	for _, token := range []model.Token{
		{Id: 11, UserId: 1, Name: "read-token", Key: readKey, Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true},
		{Id: 12, UserId: 2, Name: "foreign-token", Key: "foreignreadkey", Status: common.TokenStatusEnabled, ExpiredTime: -1},
		{Id: 13, UserId: 1, Name: "deleted-token", Key: "deletedreadkey", Status: common.TokenStatusEnabled},
		{Id: 14, UserId: 1, Name: "disabled-token", Key: "disabledreadkey", Status: common.TokenStatusDisabled},
		{Id: 15, UserId: 1, Name: "expired-token", Key: "expiredreadkey", Status: common.TokenStatusEnabled, ExpiredTime: 1},
		{Id: 16, UserId: 1, Name: "empty-token", Key: "emptyreadkey", Status: common.TokenStatusEnabled, ExpiredTime: -1},
	} {
		require.NoError(t, env.DB.Create(&token).Error)
	}
	require.NoError(t, env.DB.Delete(&model.Token{}, 13).Error)
	require.NoError(t, env.DB.Create(&model.User{Id: 4, Username: "deleted-user", AffCode: "deleted-user"}).Error)
	require.NoError(t, env.DB.Delete(&model.User{}, 4).Error)
	seedReadLogs(t)
	seedReadQuota(t, env.DB)
	// The invalid-token handler branch is unreachable behind successful
	// TokenAuthReadOnly; characterize it as an explicit transport boundary.
	env.Route(http.MethodGet, "/wave3a/key-no-context", controller.GetLogByKey)
	return env
}

func seedReadLogs(t *testing.T) {
	t.Helper()
	metadata := []struct{ name, other string }{
		{"scopes", `{"public_id":9007199254740993,"admin_info":{"admin_id":9007199254740995},` +
			`"root_info":{"generation":18446744073709551615},"audit_info":{"method":"POST"}}`},
		{"legacy", `{"channel_id":11,"channel_name":"secret","channel_type":1,"reject_reason":"legacy","admin_info":null}`},
		{"malformed", `{not-json`}, {"null", `null`},
		{"unchanged", `{ "public_id" : 9007199254740993, "ratio":1.00 }`},
		{"scoped_rejection", `{"reject_reason":"legacy","admin_info":{"reject_reason":"scoped"},"root_info":{}}`},
	}
	for i, item := range metadata {
		row := model.Log{Id: 101 + i, UserId: 1, Username: "contract-user", CreatedAt: readStart + int64(i%2),
			Type: model.LogTypeConsume, TokenId: 11, TokenName: "read-token", ChannelId: 11,
			ModelName: "Model", Group: "paid", Quota: i + 1, PromptTokens: 10 + i, CompletionTokens: 2,
			RequestId: item.name, UpstreamRequestId: fmt.Sprintf("upstream-%d", 101+i), Other: item.other}
		require.NoError(t, model.LOG_DB.Create(&row).Error)
	}
	for _, row := range []model.Log{
		{Id: 201, UserId: 1, Username: "contract-user", CreatedAt: readEnd, Type: model.LogTypeError,
			ModelName: "model", TokenId: 13, TokenName: "deleted-token", ChannelId: 999, RequestId: "read-101"},
		{Id: 202, UserId: 2, Username: "contract-admin", CreatedAt: readStart - 1, Type: model.LogTypeConsume,
			ModelName: "Model", TokenId: 12, TokenName: "foreign-token", ChannelId: 12, Quota: 17, RequestId: "admin-row"},
		{Id: 203, UserId: 1, Username: "contract-user", CreatedAt: readEnd + 1, Type: model.LogTypeConsume,
			ModelName: "Model_%!\\suffix", TokenId: 11, TokenName: "read-token", Quota: 31, RequestId: "literal-row"},
	} {
		require.NoError(t, model.LOG_DB.Create(&row).Error)
	}
}

func seedReadQuota(t *testing.T, db *gorm.DB) {
	t.Helper()
	for i, timestamp := range []int64{
		readStart - 1, readStart, readStart + 3599, readStart + 3600, readEnd, readEnd + 1,
		1709161200, 1709251199, 1709251200, 1709506800, 1709510400, 1735686000, 1735689600,
	} {
		row := model.QuotaData{Id: 101 + i, UserID: 1, Username: "contract-user", ModelName: "Model",
			CreatedAt: timestamp, UseGroup: "paid", TokenID: 11, ChannelID: 11, NodeName: "read-node",
			Count: i + 1, Quota: (i + 1) * 7, TokenUsed: (i + 1) * 11}
		require.NoError(t, db.Create(&row).Error)
	}
	for _, row := range []model.QuotaData{
		{Id: 201, UserID: 1, Username: "contract-user", ModelName: "model", CreatedAt: readStart,
			UseGroup: "paid", TokenID: 13, ChannelID: 999, NodeName: "other-node", Count: 2, Quota: 17, TokenUsed: 19},
		{Id: 202, UserID: 2, Username: "contract-admin", ModelName: "Model ", CreatedAt: readEnd,
			UseGroup: "free", TokenID: 12, ChannelID: 12, NodeName: "other-node", Count: 3, Quota: 23, TokenUsed: 29},
		{Id: 203, UserID: 1, Username: "old-username", ModelName: "legacy", CreatedAt: readStart,
			Count: 97, Quota: 101, TokenUsed: 103},
	} {
		require.NoError(t, db.Create(&row).Error)
	}
}

func readBearer(env *contracttest.Env, actor string) string {
	switch actor {
	case "user":
		return env.User
	case "admin":
		return env.Admin
	case "root":
		return env.Root
	case "pat":
		return env.UserPAT
	case "key":
		return "sk-" + readKey
	case "disabled_key":
		return "sk-disabledreadkey"
	case "expired_key":
		return "sk-expiredreadkey"
	case "empty_key":
		return "sk-emptyreadkey"
	}
	return ""
}

func sendRead(env *contracttest.Env, tc readCase) *httptest.ResponseRecorder {
	return env.Serve(readHTTPRequest(env, tc))
}

func readHTTPRequest(env *contracttest.Env, tc readCase) *http.Request {
	method := tc.method
	if method == "" {
		method = http.MethodGet
	}
	request := httptest.NewRequest(method, tc.path, strings.NewReader(tc.body))
	if bearer := readBearer(env, tc.actor); bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	if tc.body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	locale := tc.locale
	if locale == "" {
		locale = "zh-CN"
	}
	if locale != "default" {
		request.Header.Set("Accept-Language", locale)
	}
	request.Header.Set("Origin", "https://usage-contract.example")
	return request
}

// Audit every golden case, not only error cases. The same request constructor
// feeds the production router; default/en/ru belong exclusively to A5 evidence.
func TestUsageReadParityLocales(t *testing.T) {
	env := &contracttest.Env{User: "fixture-user", Admin: "fixture-admin", Root: "fixture-root", UserPAT: "fixture-pat"}
	audited := map[string]bool{"usage_read_jobs": true, "usage_read_cleanup": true}
	fmt.Println("WAVE3A_LOCALE usage_read_jobs no-http")
	fmt.Println("WAVE3A_LOCALE usage_read_cleanup no-http")
	for _, tc := range readCases() {
		require.Contains(t, []string{"zh-CN", "zh-TW"}, tc.locale, tc.name)
		request := readHTTPRequest(env, tc)
		assert.Equal(t, tc.locale, request.Header.Get("Accept-Language"), tc.name)
		name := "usage_read_" + tc.name
		audited[name] = true
		fmt.Printf("WAVE3A_LOCALE %s %s\n", name, request.Header.Get("Accept-Language"))
	}
	for _, tc := range readFailureCases() {
		assert.Equal(t, "zh-CN", tc.locale)
		assert.Equal(t, "zh-CN", readHTTPRequest(env, tc).Header.Get("Accept-Language"))
	}
	audited["usage_read_failures"] = true
	fmt.Println("WAVE3A_LOCALE usage_read_failures zh-CN-five-http-requests")
	require.Len(t, audited, len(readGoldenNames()))
	for _, name := range readGoldenNames() {
		assert.True(t, audited[name], "golden case missing from locale audit: %s", name)
	}
	// The intentional default/en/ru path remains separate and unmodified.
	for _, locale := range []string{"default", "en", "ru"} {
		request := readHTTPRequest(env, readCase{path: "/api/log/search", locale: locale})
		expected := locale
		if locale == "default" {
			expected = ""
		}
		assert.Equal(t, expected, request.Header.Get("Accept-Language"))
	}
}

// Invalid patterns return before any database query. This explicit boundary
// preflight detects old-main/branch drift without writing or normalizing an oracle.
func checkSearchLocaleParity(t *testing.T) {
	require.NoError(t, i18n.Init())
	expected := map[string]string{
		"invalid_pattern_consecutive": "搜索模式中不允许包含连续的 % 通配符",
		"invalid_pattern_short":       "使用模糊搜索时，关键词长度至少为 2 个字符",
		"invalid_pattern_too_many":    "搜索模式中最多允许包含 2 个 % 通配符",
	}
	for _, tc := range readCases() {
		message, included := expected[tc.name]
		if !included {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = readHTTPRequest(&contracttest.Env{}, tc)
			require.Equal(t, "zh-CN", c.Request.Header.Get("Accept-Language"))
			middleware.I18n()(c)
			controller.GetAllLogs(c)
			var envelope struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
			assert.Equal(t, http.StatusInternalServerError, response.Code)
			assert.Equal(t, "internal_error", envelope.Code)
			assert.Equal(t, message, envelope.Message, "explicit zh-CN must match the pinned main response")
		})
	}
}

func readHTTPState(response *httptest.ResponseRecorder) map[string]any {
	headers := make(map[string]string)
	for _, name := range []string{
		"Content-Type", "Cache-Control", "Retry-After", "Specific_channel_version",
		"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Expose-Headers", "Vary",
	} {
		if value := response.Header().Get(name); value != "" {
			headers[name] = value
		}
	}
	return map[string]any{"status": response.Code, "headers": headers, "body": response.Body.String()}
}

func runReadRequests(t *testing.T, group string) {
	env := newReadEnvironment(t)
	configureReadGroup(t, env, group)
	before := readDataState(t, env.DB)
	matched := false
	for _, tc := range readCases() {
		if tc.group != group {
			continue
		}
		matched = true
		t.Run(tc.name, func(t *testing.T) {
			response := sendRead(env, tc)
			var envelope struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope), response.Body.String())
			assert.Equal(t, !tc.failure, envelope.Success, response.Body.String())
			if tc.status != 0 {
				assert.Equal(t, tc.status, response.Code)
			} else if !tc.failure {
				assert.Equal(t, http.StatusOK, response.Code)
			}
			if strings.HasPrefix(group, "rankings_") && group != "rankings_empty" {
				snapshotRankings(t, env, tc, response)
				return
			}
			contracttest.GoldenValue(t, "usage_read_"+tc.name, readHTTPState(response))
		})
	}
	require.True(t, matched, "unknown fixture group: %s", group)
	assert.Equal(t, before, readDataState(t, env.DB), "read endpoints changed logs/quota_data")
}

func readDataState(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var mainLogs, logRows []model.Log
	var quota []model.QuotaData
	require.NoError(t, db.Order("id").Find(&mainLogs).Error)
	require.NoError(t, model.LOG_DB.Order("id").Find(&logRows).Error)
	require.NoError(t, db.Order("id").Find(&quota).Error)
	encoded, err := common.Marshal(map[string]any{"main_logs": mainLogs, "logs": logRows, "quota_data": quota})
	require.NoError(t, err)
	return string(encoded)
}

func configureReadGroup(t *testing.T, env *contracttest.Env, group string) {
	t.Helper()
	if group == "shared_db" {
		model.LOG_DB = env.DB
		seedReadLogs(t)
	}
	if group == "channel_cache" {
		common.MemoryCacheEnabled = true
		model.InitChannelCache()
	}
	if group == "stats" {
		row := model.Log{Id: 301, UserId: 1, Username: "contract-user", ModelName: "Model", TokenName: "read-token",
			TokenId: 11, ChannelId: 11, Group: "paid", CreatedAt: time.Now().Unix() - 5,
			Type: model.LogTypeConsume, Quota: 41, PromptTokens: 43, CompletionTokens: 47, RequestId: "recent-stat"}
		require.NoError(t, model.LOG_DB.Create(&row).Error)
	}
	if strings.HasPrefix(group, "nav_") {
		value := `{"rankings":{"enabled":true,"requireAuth":true}}`
		if group == "nav_disabled" {
			value = `{"rankings":false}`
		}
		common.OptionMapRWMutex.Lock()
		wasNil := common.OptionMap == nil
		if wasNil {
			common.OptionMap = make(map[string]string)
		}
		previous, existed := common.OptionMap["HeaderNavModules"]
		common.OptionMap["HeaderNavModules"] = value
		common.OptionMapRWMutex.Unlock()
		t.Cleanup(func() {
			common.OptionMapRWMutex.Lock()
			defer common.OptionMapRWMutex.Unlock()
			if wasNil {
				common.OptionMap = nil
				return
			}
			if existed {
				common.OptionMap["HeaderNavModules"] = previous
			} else {
				delete(common.OptionMap, "HeaderNavModules")
			}
		})
	}
	if strings.HasPrefix(group, "rankings_") && group != "rankings_empty" {
		seedRankingContract(t, env.DB, strings.TrimPrefix(group, "rankings_"))
	}
	if !strings.HasPrefix(group, "special_") {
		return
	}
	name := strings.TrimPrefix(group, "special_")
	if name == "log_overflow" {
		require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("id = ?", 101).
			Update("quota", int64(9007199254740991)).Error)
		return
	}
	require.NoError(t, env.DB.Where("1 = 1").Delete(&model.QuotaData{}).Error)
	switch name {
	case "usage_overflow":
		for _, quota := range []int{9007199254740991, 1} {
			require.NoError(t, env.DB.Create(&model.QuotaData{UserID: 1, ModelName: "overflow",
				CreatedAt: readStart, Quota: quota}).Error)
		}
	case "row_limit":
		rows := make([]model.QuotaData, 10001)
		for i := range rows {
			rows[i] = model.QuotaData{UserID: 1, ModelName: fmt.Sprintf("limit-%05d", i), CreatedAt: readStart, Quota: 1}
		}
		require.NoError(t, env.DB.CreateInBatches(rows, 100).Error)
	case "response_limit":
		require.NoError(t, env.DB.Create(&model.QuotaData{UserID: 1, ModelName: strings.Repeat("x", 4<<20),
			CreatedAt: readStart, Quota: 1}).Error)
	case "exact_large":
		require.NoError(t, env.DB.Create(&model.QuotaData{UserID: 1, ModelName: "exact",
			CreatedAt: readStart, Quota: 9007199254740991, Count: 2147483649, TokenUsed: 9007199254740991}).Error)
	case "bucket_large":
		require.NoError(t, env.DB.Create(&model.QuotaData{UserID: 1, ModelName: "exact",
			CreatedAt: readStart, Quota: 9007199254740993, Count: 2147483649, TokenUsed: 9007199254740993}).Error)
	}
}

// Navigation fixtures run before main initializes options on the pinned
// baseline. They must also restore a pre-existing option map and its values.
func TestUsageReadNavigationOptionFixture(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options map[string]string
	}{
		{name: "nil"},
		{name: "unrelated", options: map[string]string{"unrelated": "preserved"}},
		{name: "configured", options: map[string]string{"HeaderNavModules": "original", "unrelated": "preserved"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.OptionMapRWMutex.Lock()
			previous := common.OptionMap
			common.OptionMap = maps.Clone(tc.options)
			common.OptionMapRWMutex.Unlock()
			t.Cleanup(func() {
				common.OptionMapRWMutex.Lock()
				common.OptionMap = previous
				common.OptionMapRWMutex.Unlock()
			})
			for _, group := range []string{"nav_disabled", "nav_auth_anonymous", "nav_auth_user"} {
				t.Run(group, func(t *testing.T) {
					configureReadGroup(t, nil, group)
					common.OptionMapRWMutex.RLock()
					observed := maps.Clone(common.OptionMap)
					common.OptionMapRWMutex.RUnlock()
					expected := `{"rankings":{"enabled":true,"requireAuth":true}}`
					if group == "nav_disabled" {
						expected = `{"rankings":false}`
					}
					assert.Equal(t, expected, observed["HeaderNavModules"])
					assert.Equal(t, tc.options["unrelated"], observed["unrelated"])
				})
				common.OptionMapRWMutex.RLock()
				restored := maps.Clone(common.OptionMap)
				common.OptionMapRWMutex.RUnlock()
				assert.Equal(t, tc.options, restored, "fixture cleanup must restore the original nil/map state")
			}
		})
	}
}

func seedRankingContract(t *testing.T, db *gorm.DB, period string) {
	t.Helper()
	require.NoError(t, db.Where("1 = 1").Delete(&model.QuotaData{}).Error)
	duration := map[string]time.Duration{
		"today": 24 * time.Hour, "week": 7 * 24 * time.Hour,
		"month": 30 * 24 * time.Hour, "year": 365 * 24 * time.Hour,
	}[period]
	// The two rows are safely inside each rolling window. Their actual bucket
	// timestamps/labels are asserted exactly below, never masked in a golden.
	now := time.Now().UTC().Truncate(time.Second)
	for i := range 23 {
		name := fmt.Sprintf("rank-%02d", i)
		vendorID := 100 + i%7
		if i < 7 {
			require.NoError(t, db.Create(&model.Vendor{Id: vendorID, Name: fmt.Sprintf("Vendor-%d", i),
				Icon: fmt.Sprintf("Icon-%d", i), Status: 1}).Error)
		}
		require.NoError(t, db.Create(&model.Model{ModelName: name, VendorID: vendorID, Status: 1}).Error)
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: 11, Enabled: true}).Error)
		for _, row := range []model.QuotaData{
			{UserID: 1, ModelName: name, CreatedAt: now.Add(-2 * time.Hour).Unix(), TokenUsed: 3000 - i*50},
			{UserID: 1, ModelName: name, CreatedAt: now.Add(-duration - 2*time.Hour).Unix(), TokenUsed: 1000 + i*25},
		} {
			require.NoError(t, db.Create(&row).Error)
		}
	}
	// No matching pricing entry: the owner must fall back to Unknown.
	require.NoError(t, db.Create(&model.QuotaData{UserID: 1, ModelName: "unknown-ranking-model",
		CreatedAt: now.Add(-2 * time.Hour).Unix(), TokenUsed: 7}).Error)
}

func snapshotRankings(t *testing.T, env *contracttest.Env, tc readCase, response *httptest.ResponseRecorder) {
	t.Helper()
	var envelope map[string]any
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, true, envelope["success"])
	result, ok := envelope["data"].(map[string]any)
	require.True(t, ok)
	models, ok := result["models"].([]any)
	require.True(t, ok)
	require.Len(t, models, 20)
	first, ok := models[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "rank-00", first["model_name"])
	assert.Equal(t, float64(3000), first["total_tokens"])
	assert.Equal(t, "Vendor-0", first["vendor"])
	assert.Equal(t, "Icon-0", first["vendor_icon"])
	require.NotEmpty(t, result["top_movers"])
	require.NotEmpty(t, result["top_droppers"])
	require.NotEmpty(t, result["vendors"])
	var rows []model.QuotaData
	require.NoError(t, model.DB.Where("model_name = ?", "rank-00").Order("created_at DESC").Find(&rows).Error)
	require.Len(t, rows, 2)
	bucketSize := int64(86400)
	layout := "Jan 2"
	if strings.HasSuffix(tc.group, "today") {
		bucketSize, layout = 3600, "15:04"
	} else if strings.HasSuffix(tc.group, "year") {
		bucketSize = 604800
	}
	bucket := rows[0].CreatedAt / bucketSize * bucketSize
	expectedTimestamp := time.Unix(bucket, 0).UTC().Format(time.RFC3339)
	expectedLabel := time.Unix(bucket, 0).Format(layout)
	for _, name := range []string{"models_history", "vendor_share_history"} {
		history, ok := result[name].(map[string]any)
		require.True(t, ok)
		require.Equal(t, float64(1), history["buckets"])
		points, ok := history["points"].([]any)
		require.True(t, ok)
		require.NotEmpty(t, points)
		for _, value := range points {
			point, ok := value.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, expectedTimestamp, point["ts"])
			assert.Equal(t, expectedLabel, point["label"])
			// Every real timestamp/label is asserted above. The static oracle
			// pins all remaining fields; no wildcard or dynamic mask is used.
			delete(point, "ts")
			delete(point, "label")
		}
	}
	state := readHTTPState(response)
	state["body"] = envelope
	contracttest.GoldenValue(t, "usage_read_"+tc.name, state)
	// A cache hit must preserve the whole wire response, including real times.
	assert.Equal(t, response.Body.String(), sendRead(env, tc).Body.String())
}

func runQuotaAggregation(t *testing.T) {
	env := newReadEnvironment(t)
	model.CacheQuotaDataLock.Lock()
	previous := model.CacheQuotaData
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = previous
		model.CacheQuotaDataLock.Unlock()
	})
	params := model.QuotaDataLogParams{UserID: 1, Username: "contract-user", ModelName: "job-model",
		CreatedAt: readStart + 17, UseGroup: "paid", TokenID: 11, ChannelID: 11, NodeName: "node-a", Quota: 7, TokenUsed: 11}
	model.LogQuotaData(params)
	model.LogQuotaData(params)
	for dimension := range 7 {
		changed := params
		switch dimension {
		case 0:
			changed.UserID = 2
		case 1:
			changed.Username = "renamed"
		case 2:
			changed.ModelName = "other-job-model"
		case 3:
			changed.UseGroup = "free"
		case 4:
			changed.TokenID = 12
		case 5:
			changed.ChannelID = 12
		case 6:
			changed.NodeName = "node-b"
		}
		model.LogQuotaData(changed)
	}
	model.SaveQuotaDataCache()
	first := quotaJobRows(t, env.DB)
	model.LogQuotaData(params)
	model.SaveQuotaDataCache()
	second := quotaJobRows(t, env.DB)
	model.SaveQuotaDataCache()
	assert.Equal(t, second, quotaJobRows(t, env.DB), "empty repeat flush changed persistent aggregates")
	model.CacheQuotaDataLock.Lock()
	assert.Empty(t, model.CacheQuotaData)
	model.CacheQuotaDataLock.Unlock()
	contracttest.GoldenValue(t, "usage_read_jobs", map[string]any{"first_flush": first, "second_flush": second})
}

func quotaJobRows(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var rows []model.QuotaData
	require.NoError(t, db.Where("model_name IN ?", []string{"job-model", "other-job-model"}).
		Order("user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name").Find(&rows).Error)
	require.Len(t, rows, 8)
	for i := range rows {
		// Map traversal can allocate different physical IDs; dimensions and all
		// aggregates are the business contract, physical insertion order is not.
		rows[i].Id = 0
	}
	encoded, err := common.Marshal(rows)
	require.NoError(t, err)
	return string(encoded)
}

func runLogCleanup(t *testing.T) {
	env := newReadEnvironment(t)
	ctx := context.Background()
	cutoff := readStart + 1
	before, err := model.CountOldLog(ctx, cutoff)
	require.NoError(t, err)
	deleted, err := model.DeleteOldLogBatch(ctx, cutoff, 2)
	require.NoError(t, err)
	after, err := model.CountOldLog(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, before-deleted, after)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = model.DeleteOldLogBatch(cancelled, cutoff, 2)
	require.ErrorIs(t, err, context.Canceled)
	var mainCount int64
	require.NoError(t, env.DB.Model(&model.Log{}).Count(&mainCount).Error)
	assert.EqualValues(t, 1, mainCount)
	contracttest.GoldenValue(t, "usage_read_cleanup", map[string]any{
		"before": before, "deleted": deleted, "after": after, "state": readDataState(t, env.DB),
	})
}

func runReadFailures(t *testing.T) {
	env := newReadEnvironment(t)
	fixtureError := errors.New("usage read fixture storage failure")
	callback := func(db *gorm.DB) {
		if db.Statement.Table == "logs" {
			db.AddError(fixtureError)
		}
	}
	require.NoError(t, model.LOG_DB.Callback().Query().Before("gorm:query").Register("wave3a:read", callback))
	require.NoError(t, model.LOG_DB.Callback().Row().Before("gorm:row").Register("wave3a:row", callback))
	t.Cleanup(func() {
		assert.NoError(t, model.LOG_DB.Callback().Query().Remove("wave3a:read"))
		assert.NoError(t, model.LOG_DB.Callback().Row().Remove("wave3a:row"))
	})
	state := make(map[string]any)
	for _, tc := range readFailureCases() {
		response := sendRead(env, tc)
		var envelope struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
		require.False(t, envelope.Success, response.Body.String())
		state[tc.name] = readHTTPState(response)
	}
	contracttest.GoldenValue(t, "usage_read_failures", state)
}

func readFailureCases() []readCase {
	return []readCase{
		{name: "admin", actor: "admin", path: "/api/log/", locale: "zh-CN"},
		{name: "own", actor: "user", path: "/api/log/self", locale: "zh-CN"},
		{name: "key", actor: "key", path: "/api/log/token", locale: "zh-CN"},
		{name: "stats", actor: "root", path: "/api/log/stat", locale: "zh-CN"},
		{name: "own_stats", actor: "user", path: "/api/log/self/stat", locale: "zh-CN"},
	}
}

// A5: default-locale evidence is a separate write-once host artifact, not a
// golden that would falsely require Chinese defaults after localization.
func writeDefaultLocaleEvidence(t *testing.T, destination string) {
	env := newReadEnvironment(t)
	state := make(map[string]any)
	for _, locale := range []string{"default", "en", "ru", "zh-CN", "zh-TW"} {
		for _, tc := range []readCase{
			{name: "deprecated", actor: "root", path: "/api/log/search"},
			{name: "search_consecutive", actor: "root", path: "/api/log/?model_name=" + url.QueryEscape("%%")},
			{name: "search_short", actor: "root", path: "/api/log/?model_name=" + url.QueryEscape("%a%")},
			{name: "search_too_many", actor: "root", path: "/api/log/?model_name=" + url.QueryEscape("%ab%cd%")},
			{name: "invalid_token_boundary", actor: "anonymous", path: "/wave3a/key-no-context"},
			{name: "own_span", actor: "user", path: "/api/data/self?start_timestamp=1&end_timestamp=2592002"},
			{name: "flow_span", actor: "user", path: "/api/data/flow/self?start_timestamp=1&end_timestamp=2592002"},
			{name: "invalid_params", actor: "root", method: "POST", path: "/api/log/query", body: `{"user_ids":null}`},
		} {
			tc.locale = locale
			state[locale+"_"+tc.name] = readHTTPState(sendRead(env, tc))
		}
	}
	callback := func(db *gorm.DB) {
		if db.Statement.Table == "logs" {
			db.AddError(errors.New("usage default-locale fixture storage failure"))
		}
	}
	require.NoError(t, model.LOG_DB.Callback().Query().Before("gorm:query").Register("wave3a:locale-query", callback))
	require.NoError(t, model.LOG_DB.Callback().Row().Before("gorm:row").Register("wave3a:locale-row", callback))
	t.Cleanup(func() {
		assert.NoError(t, model.LOG_DB.Callback().Query().Remove("wave3a:locale-query"))
		assert.NoError(t, model.LOG_DB.Callback().Row().Remove("wave3a:locale-row"))
	})
	for _, locale := range []string{"default", "en", "ru", "zh-CN", "zh-TW"} {
		for _, tc := range []readCase{
			{name: "query_failed", actor: "user", path: "/api/log/self"},
			{name: "stats_failed", actor: "root", path: "/api/log/stat"},
		} {
			tc.locale = locale
			state[locale+"_"+tc.name] = readHTTPState(sendRead(env, tc))
		}
	}
	encoded, err := common.Marshal(state)
	require.NoError(t, err)
	encoded, err = common.IndentJson(encoded)
	require.NoError(t, err)
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err, "default-locale evidence must never be overwritten")
	_, err = output.Write(append(encoded, '\n'))
	require.NoError(t, err)
	require.NoError(t, output.Close())
}
