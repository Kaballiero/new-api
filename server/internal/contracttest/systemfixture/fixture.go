// Package systemfixture supplies portable system-contract capture and process isolation.
package systemfixture

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

var manifest = flag.Bool("system-manifest", false, "list system cases without fixtures")
var preflight = flag.Bool("system-preflight", false, "capture without oracle IO")
var originCheck = flag.Bool("system-origin-check", false, "validate origin before fixtures")

func init() { fixture.RejectCandidateUpdate() }

// Run gives every case its own process, private databases and process-global state.
func Run(t *testing.T, root, group, directory string, names []string, execute func(*testing.T, string) any) {
	t.Helper()
	updating := flag.Lookup("update").Value.String() == "true"
	if updating || *originCheck {
		fixture.CheckRecording(t)
	}
	if *originCheck {
		return
	}
	require.NotEmpty(t, names)
	unique := map[string]bool{}
	for _, n := range names {
		require.False(t, unique[n], n)
		unique[n] = true
	}
	if *manifest {
		data, err := common.Marshal(names)
		require.NoError(t, err)
		fmt.Println("SYSTEM_MANIFEST " + group + " " + string(data))
		return
	}
	if name := os.Getenv("SYSTEM_CONTRACT_CASE"); name != "" {
		require.Contains(t, names, name)
		fixture.Compare(t, directory, name, execute(t, name), *preflight, updating)
		return
	}
	if path := os.Getenv("SYSTEM_CONTRACT_LOCALES"); path != "" {
		values := map[string]json.RawMessage{}
		selected := []string{"access_task_list_anonymous", "access_task_list_admin",
			"setup_guard", "setup_mismatch", "metrics_missing_model", "performance_clear"}
		for _, language := range []string{"default", "en", "ru", "zh-CN", "zh-TW", "unknown"} {
			for _, name := range selected {
				require.Contains(t, names, name)
				data := child(t, root, name, false, true, language)
				require.Len(t, data, 1)
				values[language+"/"+name] = json.RawMessage(data[0])
			}
		}
		data, err := common.Marshal(values)
		require.NoError(t, err)
		stream, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		require.NoError(t, err)
		_, err = stream.Write(data)
		require.NoError(t, err)
		require.NoError(t, stream.Close())
		return
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			child(t, root, name, updating, *preflight, "")
		})
	}
}

func child(t *testing.T, root, name string, updating, capture bool, language string) []string {
	t.Helper()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+root+"$", "-test.count=1", "-test.timeout=90s",
		"-update="+strconv.FormatBool(updating), "-system-preflight="+strconv.FormatBool(capture))
	command.Env = append(os.Environ(), "SYSTEM_CONTRACT_CASE="+name, "SYSTEM_CONTRACT_LANG="+language)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	found := []string{}
	for line := range strings.SplitSeq(string(output), "\n") {
		if value, ok := strings.CutPrefix(line, "TASKS_SNAPSHOT "+name+" "); ok {
			found = append(found, value)
			fmt.Println(line)
		}
		if strings.HasPrefix(line, "SYSTEM_ORDER ") {
			fmt.Println(line)
		}
	}
	if capture {
		require.Len(t, found, 1)
	}
	return found
}

// State retains raw persisted JSON and exact financial/security facts, without user password material.
func State(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"users": fixture.Rows(t, model.DB, "users", "id,username,role,status,display_name,access_token,quota,used_quota,request_count,auth_version,group",
			"id"),
		"options":   fixture.Rows(t, model.DB, "options", "*", "key"),
		"setup":     fixture.Rows(t, model.DB, "setups", "*", "id"),
		"tasks":     fixture.Rows(t, model.DB, "system_tasks", "*", "id"),
		"locks":     fixture.Rows(t, model.DB, "system_task_locks", "*", "type"),
		"instances": fixture.Rows(t, model.DB, "system_instances", "*", "node_name"),
		"metrics":   fixture.Rows(t, model.DB, "perf_metrics", "*", "id"),
		"logs":      fixture.Rows(t, model.LOG_DB, "logs", "*", "id"),
		"audit":     fixture.Rows(t, model.LOG_DB, "audit_logs", "*", "id"),
	}
}

// Canonical replaces only validated generated identities and explicitly owned fixture paths.
func Canonical(t *testing.T, value any, directories ...string) json.RawMessage {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	tasks := fixture.Rows(t, model.DB, "system_tasks", "id,task_id", "id")
	for _, row := range tasks {
		id := row["task_id"].(string)
		if strings.HasPrefix(id, "systask_") {
			require.Regexp(t, `^systask_[A-Za-z0-9]{32}$`, id)
			data = []byte(strings.ReplaceAll(string(data), id, fmt.Sprintf("systask_<%v>", row["id"])))
		}
	}
	for _, table := range []string{"logs", "audit_logs"} {
		fields := "id,request_id"
		if table == "audit_logs" {
			fields += ",event_id"
		}
		for _, row := range fixture.Rows(t, model.LOG_DB, table, fields, "id") {
			for _, key := range []string{"request_id", "event_id"} {
				id, _ := row[key].(string)
				if regexp.MustCompile(`^[0-9]{23}[A-Za-z0-9]{16}$`).MatchString(id) {
					require.True(t, strings.HasPrefix(id, "20000101"), "only the virtual fixture day may generate IDs")
					data = []byte(strings.ReplaceAll(string(data), id, fmt.Sprintf("<%s-%s-%v>", table, key, row["id"])))
				}
			}
		}
	}
	for _, row := range fixture.Rows(t, model.DB, "system_tasks", "locked_by", "id") {
		id, _ := row["locked_by"].(string)
		if strings.HasPrefix(id, "system-contract-node-") {
			require.Regexp(t, `^system-contract-node-[A-Za-z0-9]{8}$`, id)
			data = []byte(strings.ReplaceAll(string(data), id, "system-contract-node-<runner>"))
		}
	}
	for i, directory := range directories {
		require.True(t, filepath.IsAbs(directory))
		require.NotEqual(t, "/", directory)
		data = []byte(strings.ReplaceAll(string(data), directory, fmt.Sprintf("<fixture-dir-%d>", i)))
	}
	return data
}

// Order records the original permutation before an explicitly permitted order-only comparison.
func Order(t *testing.T, name string, objects []json.RawMessage) []json.RawMessage {
	t.Helper()
	data, err := common.Marshal(objects)
	require.NoError(t, err)
	fmt.Println("SYSTEM_ORDER " + name + " " + string(data))
	slices.SortFunc(objects, func(a, b json.RawMessage) int { return strings.Compare(string(a), string(b)) })
	return objects
}

// Open initializes the production SQLite dialect state without importing the HTTP/router graph.
func Open(t *testing.T) {
	t.Helper()
	fixture.InstallDBClock(t)
	fixture.Configure()
	oldMain, oldLog := model.DB, model.LOG_DB
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	common.SQLitePath = filepath.Join(t.TempDir(), "system.db") + "?_pragma=synchronous(OFF)"
	common.IsMasterNode = true
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	mainDB, logDB := model.DB, model.LOG_DB
	t.Cleanup(func() {
		pool, err := mainDB.DB()
		require.NoError(t, err)
		require.NoError(t, pool.Close())
		if logDB != mainDB {
			pool, err = logDB.DB()
			require.NoError(t, err)
			require.NoError(t, pool.Close())
		}
		model.DB, model.LOG_DB = oldMain, oldLog
	})
}
