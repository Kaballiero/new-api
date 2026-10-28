// Package taskfixture supplies portable task-contract fixtures without router or service imports.
package taskfixture

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	dbclock "github.com/QuantumNous/new-api/internal/app/dbclock"
	"github.com/QuantumNous/new-api/model"

	sqliteclock "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const Origin = "6b0f439a432ab129817e4e41ce323e6bd3c3c0c3"
const Epoch int64 = 946684800

// RegisterUpdate shares the conventional flag when a package does not link contracttest/env.go.
func RegisterUpdate() {
	if flag.Lookup("update") == nil {
		flag.Bool("update", false, "record task contracts only on verified origin")
	}
}

// CheckRecording rejects a candidate update before DB, provider or fixture IO.
func CheckRecording(t *testing.T) {
	t.Helper()
	require.Equal(t, Origin, os.Getenv("TASKS_CONTRACT_ORIGIN_RECORD"))
	root, err := filepath.EvalSymlinks(os.Getenv("TASKS_CONTRACT_ORIGIN_DIR"))
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(root))
	cwd, err := os.Getwd()
	require.NoError(t, err)
	relative, err := filepath.Rel(root, cwd)
	require.NoError(t, err)
	require.False(t, relative == ".." || strings.HasPrefix(relative, "../"))
	marker, err := os.ReadFile(filepath.Join(root, ".tasks-origin"))
	require.NoError(t, err)
	require.Equal(t, Origin+"\n", string(marker))
	require.NoFileExists(t, filepath.Join(root, "server/go.mod"))
	var hashes map[string]string
	data, err := os.ReadFile(filepath.Join(root, ".tasks-source.json"))
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(data, &hashes))
	require.NotEmpty(t, hashes)
	for name, digest := range hashes {
		require.False(t, filepath.IsAbs(name) || strings.Contains(name, ".."))
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Equal(t, digest, fmt.Sprintf("%x", sha256.Sum256(data)), name)
	}
}

// Configure disables external persistence and fixes deployment facts for this child process.
func Configure() {
	common.RedisEnabled, common.BatchUpdateEnabled, common.MemoryCacheEnabled = false, false, false
	common.LogConsumeEnabled, common.DataExportEnabled = true, false
	common.NodeName, common.Version = "tasks-contract-node", "contract"
	common.QuotaPerUnit = 500000
}

// Open creates private SQLite main/log databases for package-local ticks and accounting.
func Open(t *testing.T) *gorm.DB {
	t.Helper()
	require.NoError(t, i18n.Init())
	InstallDBClock(t)
	Configure()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	open := func(name string) *gorm.DB {
		db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name)+"?_pragma=synchronous(OFF)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		t.Cleanup(func() { pool, err := db.DB(); require.NoError(t, err); require.NoError(t, pool.Close()) })
		return db
	}
	model.DB, model.LOG_DB = open("main.db"), open("logs.db")
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Task{}, &model.Midjourney{},
		&model.UserSubscription{}, &model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}, &model.TopUp{}, &model.SubscriptionOrder{},
		&model.SystemTask{}, &model.SystemTaskLock{}, &identityrecord.UserSession{}, &model.Log{}, &model.AuditLog{}))
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	return model.DB
}

// Rows retains database numbers as integers and JSON columns as their original strings.
func Rows(t *testing.T, db *gorm.DB, table, columns, order string) []map[string]any {
	t.Helper()
	selected := make([]clause.Column, 0)
	for column := range strings.SplitSeq(columns, ",") {
		selected = append(selected, clause.Column{Name: column, Raw: column == "*"})
	}
	rows, err := db.Table(table).Clauses(clause.Select{Columns: selected}).Order(order).Rows()
	require.NoError(t, err)
	defer rows.Close()
	names, err := rows.Columns()
	require.NoError(t, err)
	result := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(names))
		pointers := make([]any, len(names))
		for i := range values {
			pointers[i] = &values[i]
		}
		require.NoError(t, rows.Scan(pointers...))
		item := make(map[string]any, len(names))
		for i, name := range names {
			if raw, ok := values[i].([]byte); ok {
				item[name] = string(raw)
			} else {
				item[name] = values[i]
			}
		}
		result = append(result, item)
	}
	require.NoError(t, rows.Err())
	return result
}

func State(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"tasks": Rows(t, model.DB, "tasks", "*", "id"), "midjourneys": Rows(t, model.DB, "midjourneys", "*", "id"),
		"users":               Rows(t, model.DB, "users", "id,username,quota,used_quota,request_count,auth_version,status,group,setting,email,stripe_customer", "id"),
		"sessions":            Rows(t, model.DB, "user_sessions", "user_id,version,user_auth_version,status,revoked_at,revoked_reason", "user_id"),
		"tokens":              Rows(t, model.DB, "tokens", "id,user_id,status,remain_quota,used_quota,unlimited_quota", "id"),
		"channels":            Rows(t, model.DB, "channels", "id,type,status,used_quota", "id"),
		"subscriptions":       Rows(t, model.DB, "user_subscriptions", "*", "id"),
		"subscription_orders": Rows(t, model.DB, "subscription_orders", "*", "id"),
		"ledger":              Rows(t, model.DB, "subscription_pre_consume_records", "*", "id"),
		"top_ups":             Rows(t, model.DB, "top_ups", "*", "id"),
		"logs":                Rows(t, model.LOG_DB, "logs", "*", "id"),
		"audit":               Rows(t, model.LOG_DB, "audit_logs", "*", "id"),
		"main_logs":           Rows(t, model.DB, "logs", "*", "id"),
	}
}

var generatedTaskID = regexp.MustCompile(`^task_[A-Za-z0-9]{32}$`)

// BindTaskID verifies a generated public identity before substituting its exact occurrences.
func BindTaskID(t *testing.T, value string) (string, string) {
	t.Helper()
	require.Regexp(t, generatedTaskID, value)
	return value, "task_<generated>"
}

// Compare emits raw preflight evidence or compares an immutable oracle. It never records candidate data.
func Compare(t *testing.T, directory, name string, value any, preflight, updating bool) {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	data, err = common.IndentJson(data)
	require.NoError(t, err)
	data = append(data, '\n')
	if preflight {
		fmt.Println("TASKS_SNAPSHOT " + name + " " + stringMustCompact(t, data))
		return
	}
	target := filepath.Join(directory, name+".golden.json")
	if updating {
		CheckRecording(t)
		require.NoError(t, os.MkdirAll(directory, 0755))
		stream, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		require.NoError(t, err, "refusing to overwrite oracle")
		_, err = stream.Write(data)
		require.NoError(t, err)
		require.NoError(t, stream.Close())
	}
	expected, err := os.ReadFile(target)
	require.NoError(t, err, "host must record the pinned origin first")
	require.Equal(t, string(expected), string(data), name)
}

func stringMustCompact(t *testing.T, data []byte) string {
	t.Helper()
	// RawMessage avoids integer conversion while producing a single evidence line.
	compact, err := common.Marshal(json.RawMessage(data))
	require.NoError(t, err)
	return string(compact)
}

// StableRequestIDs validates generated identities before replacing their exact occurrences.
// No monetary, status, payload, or content field is rewritten by key.
func StableRequestIDs(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	pattern := regexp.MustCompile(`^[0-9]{23}[A-Za-z0-9]{16}$`)
	for _, table := range []string{"logs", "audit_logs"} {
		columns := "id,request_id"
		if table == "audit_logs" {
			columns += ",event_id"
		}
		for _, row := range Rows(t, model.LOG_DB, table, columns, "id") {
			for _, field := range []string{"request_id", "event_id"} {
				identity, _ := row[field].(string)
				if field == "event_id" && table == "audit_logs" {
					require.Regexp(t, pattern, identity)
				}
				if pattern.MatchString(identity) {
					require.True(t, strings.HasPrefix(identity, common.GetTimeString()))
					data = []byte(strings.ReplaceAll(string(data), identity, fmt.Sprintf("<%s-%s-%v>", table, field, row["id"])))
				}
			}
		}
	}
	return json.RawMessage(data)
}

// RejectCandidateUpdate runs during test initialization, before an existing TestMain can open a database.
func RejectCandidateUpdate() {
	updating := false
	for _, argument := range os.Args[1:] {
		name, value, assigned := strings.Cut(strings.TrimLeft(argument, "-"), "=")
		if name != "update" {
			continue
		}
		if !assigned {
			updating = true
			continue
		}
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			updating = true
		}
	}
	if !updating {
		return
	}
	root, err := filepath.EvalSymlinks(os.Getenv("TASKS_CONTRACT_ORIGIN_DIR"))
	cwd, cwdErr := os.Getwd()
	if err != nil || cwdErr != nil || os.Getenv("TASKS_CONTRACT_ORIGIN_RECORD") != Origin || !strings.HasPrefix(cwd, root+string(filepath.Separator)) {
		panic("task contracts: candidate recording rejected before fixtures")
	}
	marker, err := os.ReadFile(filepath.Join(root, ".tasks-origin"))
	if err != nil || string(marker) != Origin+"\n" {
		panic("task contracts: candidate recording rejected before fixtures")
	}
}

var clockOnce sync.Once
var clockError error
var clockCalls atomic.Int64

// InstallDBClock uses the existing SQLite UDF registration seam, never production clock hooks.
func InstallDBClock(t *testing.T) {
	t.Helper()
	clockOnce.Do(func() {
		clockError = sqliteclock.RegisterScalarFunction("strftime", 2, func(_ *sqliteclock.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) != 2 || args[0] != "%s" || args[1] != "now" {
				return nil, fmt.Errorf("unexpected task DB clock expression")
			}
			clockCalls.Add(1)
			return strconv.FormatInt(time.Now().Unix(), 10), nil
		})
	})
	require.NoError(t, clockError)
}

func AssertDBClock(t *testing.T) {
	t.Helper()
	before := clockCalls.Load()
	var actual int64
	require.NoError(t, model.DB.Raw("SELECT strftime('%s','now')").Scan(&actual).Error)
	require.Equal(t, time.Now().Unix(), actual)
	require.Greater(t, clockCalls.Load(), before)
	require.Equal(t, time.Now().Unix(), dbclock.Now())
}
