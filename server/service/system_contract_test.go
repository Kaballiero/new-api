package service_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	sf "github.com/QuantumNous/new-api/internal/contracttest/systemfixture"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func init() { fixture.RegisterUpdate(); fixture.RejectCandidateUpdate() }

func systemRuntimeCases() []string {
	names := []string{"registration", "registration_replace", "enqueue_new", "enqueue_active",
		"enqueue_unknown", "enqueue_create_fault", "cleanup_success", "cleanup_separate_logdb", "cleanup_cancel",
		"cleanup_invalid_payload", "cleanup_invalid_target", "cleanup_invalid_state",
		"cleanup_state_fault", "cleanup_finish_fault", "cleanup_no_progress", "scheduler_due",
		"scheduler_active", "scheduler_not_due", "scheduler_equal", "scheduler_disabled",
		"scheduler_query_fault", "scheduler_create_fault", "claim_earliest", "claim_concurrent_types",
		"claim_lock_busy", "claim_query_fault", "heartbeat_renew", "heartbeat_lost",
		"progress_throttle", "runner_follower_once", "runner_master_wakeup", "reporter_loop"}
	for _, kind := range []string{"log_cleanup", "channel_test", "model_update",
		"midjourney_poll", "async_task_poll", "provider_batch_poll", "pricing_catalog",
		"model_cost_fx"} {
		for _, variant := range []string{"idle", "disabled", "run", "cancelled"} {
			names = append(names, "handler_"+kind+"_"+variant)
		}
	}
	slices.Sort(names)
	return names
}

func TestSystemRuntimeContract(t *testing.T) {
	sf.Run(t, "TestSystemRuntimeContract", "runtime", "testdata/system", systemRuntimeCases(), runSystemRuntime)
}

type systemScheduled struct {
	kind    string
	enabled bool
	run     func(context.Context, *model.SystemTask, string)
}

func (h systemScheduled) Type() string            { return h.kind }
func (h systemScheduled) Enabled() bool           { return h.enabled }
func (h systemScheduled) Interval() time.Duration { return time.Minute }
func (h systemScheduled) NewPayload() any         { return map[string]bool{"scheduled": true} }
func (h systemScheduled) Run(ctx context.Context, task *model.SystemTask, runner string) {
	if h.run != nil {
		h.run(ctx, task, runner)
	}
}

type systemFXTransport struct {
	calls    atomic.Int64
	mu       sync.Mutex
	requests []map[string]any
}

func (h *systemFXTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	h.calls.Add(1)
	h.mu.Lock()
	h.requests = append(h.requests, map[string]any{"method": r.Method, "url": r.URL.String(), "headers": r.Header.Clone(), "body": r.Body != nil})
	h.mu.Unlock()
	if r.Context().Err() != nil {
		return nil, r.Context().Err()
	}
	if r.URL.Host != "fx.fixture" {
		return nil, errors.New("undeclared system handler outbound")
	}
	body := `{"Date":"2000-01-01T00:00:00Z","Valute":{"USD":{"CharCode":"USD","Value":90,"Nominal":1}}}`
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)),
		Header: make(http.Header), Request: r}, nil
}

func runSystemRuntime(t *testing.T, name string) any {
	sf.Open(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Setup{}, &model.Option{}, &model.SystemInstance{}, &model.PerfMetric{}, &model.ModelCostFXRow{}))
	if name == "cleanup_separate_logdb" {
		previousLog := model.LOG_DB
		logDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "separate-log.db")+"?_pragma=synchronous(OFF)"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, logDB.AutoMigrate(&model.Log{}, &model.AuditLog{}))
		model.LOG_DB = logDB
		t.Cleanup(func() {
			model.LOG_DB = previousLog
			pool, err := logDB.DB()
			require.NoError(t, err)
			require.NoError(t, pool.Close())
		})
	}
	common.StartTime = fixture.Epoch
	common.NodeName = "system-contract-node"
	constant.UpdateTask = true
	operation_setting.GetMonitorSetting().AutoTestChannelEnabled = true
	service.SystemContractFXEndpoint(t, "https://fx.fixture/daily_json.js")
	transport := &systemFXTransport{}
	http.DefaultTransport = transport
	var output any
	synctest.Test(t, func(t *testing.T) {
		extra := map[string]any{}
		ctx := context.Background()
		switch {
		case name == "registration" || name == "registration_replace":
			controller.RegisterScheduledSystemTasks()
			if name == "registration_replace" {
				controller.RegisterScheduledSystemTasks()
			}
			types := []string{}
			for _, handler := range service.SystemContractHandlers() {
				types = append(types, handler.Type())
			}
			slices.Sort(types)
			require.Equal(t, []string{"async_task_poll", "channel_test", "log_cleanup",
				"midjourney_poll", "model_cost_fx", "model_update", "pricing_catalog", "provider_batch_poll"},
				types)
			extra["types"] = types
		case strings.HasPrefix(name, "handler_"):
			suffix := strings.TrimPrefix(name, "handler_")
			kind, variant := "", ""
			for _, v := range []string{"idle", "disabled", "run", "cancelled"} {
				if strings.HasSuffix(suffix, "_"+v) {
					kind = strings.TrimSuffix(suffix, "_"+v)
					variant = v
				}
			}
			if variant == "disabled" {
				constant.UpdateTask = false
			}
			controller.RegisterScheduledSystemTasks()
			var handler service.SystemTaskHandler
			for _, h := range service.SystemContractHandlers() {
				if h.Type() == kind {
					handler = h
				}
			}
			require.NotNil(t, handler)
			extra["update_task"] = constant.UpdateTask
			if scheduled, ok := handler.(service.ScheduledSystemTaskHandler); ok {
				if variant == "disabled" {
					constant.UpdateTask = false
					operation_setting.GetMonitorSetting().AutoTestChannelEnabled = false
					t.Setenv("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", "false")
					t.Setenv("PRICING_CATALOG_TASK_ENABLED", "false")
				}
				extra["enabled"] = scheduled.Enabled()
				extra["interval_seconds"] = scheduled.Interval().Seconds()
				extra["payload"] = scheduled.NewPayload()
			} else {
				extra["scheduled"] = false
			}
			if variant == "run" || variant == "cancelled" {
				var payload any
				if kind == "log_cleanup" {
					payload = service.LogCleanupPayload{TargetTimestamp: fixture.Epoch, BatchSize: 2}
				}
				task, err := model.CreateSystemTask(kind, payload, nil)
				require.NoError(t, err)
				claimed, won, err := model.ClaimSystemTask(task.ID, kind, "fixture-runner", fixture.Epoch+60)
				require.NoError(t, err)
				require.True(t, won)
				runCtx, cancel := context.WithCancel(ctx)
				if variant == "cancelled" {
					cancel()
				}
				handler.Run(runCtx, claimed, "fixture-runner")
				cancel()
			}
		case strings.HasPrefix(name, "enqueue_"):
			if name == "enqueue_create_fault" {
				require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("system-create-fault", func(db *gorm.DB) {
					if db.Statement.Table == "system_tasks" {
						db.AddError(errors.New("fixture task create failed"))
					}
				}))
			}
			kind := "fixture"
			if name == "enqueue_unknown" {
				kind = "unknown"
			}
			task, created, err := service.EnqueueSystemTask(kind, map[string]bool{"manual": true})
			extra["created"], extra["error"] = created, errorText(err)
			if name == "enqueue_active" {
				again, created, err := service.EnqueueSystemTask(kind, nil)
				require.NoError(t, err)
				require.NotNil(t, task)
				assert.Equal(t, task.TaskID, again.TaskID)
				assert.False(t, created)
				extra["second_created"] = created
			}
		case strings.HasPrefix(name, "cleanup_"):
			if name == "cleanup_separate_logdb" {
				require.NoError(t, model.DB.Create(&model.Log{Id: 900, CreatedAt: fixture.Epoch - 10, Type: 2, Content: "main database sentinel"}).Error)
			}
			for i, at := range []int64{fixture.Epoch - 3, fixture.Epoch - 2, fixture.Epoch - 1, fixture.Epoch, fixture.Epoch + 1} {
				require.NoError(t, model.LOG_DB.Create(&model.Log{Id: i + 1, CreatedAt: at, Type: 2, Content: "fixture"}).Error)
			}
			task, err := model.CreateSystemTask("log_cleanup", service.LogCleanupPayload{TargetTimestamp: fixture.Epoch, BatchSize: 2}, nil)
			require.NoError(t, err)
			task, won, err := model.ClaimSystemTask(task.ID, task.Type, "fixture-runner", fixture.Epoch+60)
			require.NoError(t, err)
			require.True(t, won)
			switch name {
			case "cleanup_invalid_payload":
				task.Payload = "{"
			case "cleanup_invalid_target":
				task.Payload = `{"target_timestamp":0}`
			case "cleanup_invalid_state":
				task.State = "{"
			case "cleanup_state_fault":
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("system-update-fault",
					func(db *gorm.DB) { db.AddError(errors.New("fixture task update failed")) }))
			case "cleanup_finish_fault":
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("system-update-fault", func(db *gorm.DB) {
					if updates, ok := db.Statement.Dest.(map[string]any); ok && updates["status"] != nil {
						db.AddError(errors.New("fixture finish failed"))
					}
				}))
			case "cleanup_no_progress":
				require.NoError(t, model.LOG_DB.Callback().Delete().Before("gorm:delete").Register("system-no-delete", func(db *gorm.DB) {
					db.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "1=0"}}})
				}))
			}
			runCtx, cancel := context.WithCancel(ctx)
			if name == "cleanup_cancel" {
				cancel()
			}
			service.SystemContractCleanup(runCtx, task, "fixture-runner")
			if name == "cleanup_separate_logdb" {
				rows := fixture.Rows(t, model.DB, "logs", "*", "id")
				require.Len(t, rows, 1)
				assert.Equal(t, "main database sentinel", rows[0]["content"])
				extra["main_logs"] = rows
			}
			cancel()
		case strings.HasPrefix(name, "scheduler_"):
			h := systemScheduled{kind: "fixture", enabled: name != "scheduler_disabled"}
			service.SystemContractRegistry(t, h)
			if name == "scheduler_query_fault" {
				require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("system-query-fault",
					func(db *gorm.DB) { db.AddError(errors.New("fixture query failed")) }))
			}
			if name == "scheduler_create_fault" {
				require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("system-create-fault",
					func(db *gorm.DB) { db.AddError(errors.New("fixture create failed")) }))
			}
			if name == "scheduler_active" {
				_, err := model.CreateSystemTask(h.kind, nil, nil)
				require.NoError(t, err)
			}
			if name == "scheduler_not_due" || name == "scheduler_equal" {
				at := fixture.Epoch - 59
				if name == "scheduler_equal" {
					at--
				}
				require.NoError(t, model.DB.Create(&model.SystemTask{TaskID: "prior-task",
					Type: h.kind, Status: model.SystemTaskStatusSucceeded, UpdatedAt: at, CreatedAt: at}).Error)
			}
			service.SystemContractSchedule()
			service.SystemContractSchedule()
		case strings.HasPrefix(name, "claim_"):
			complete := func(_ context.Context, task *model.SystemTask, runner string) {
				require.NoError(t, model.FinishSystemTask(task.TaskID, runner, model.SystemTaskStatusSucceeded, map[string]int{"done": 1}, ""))
			}
			first, second := complete, complete
			if name == "claim_concurrent_types" {
				aEntered, bFinished := make(chan struct{}), make(chan struct{})
				first = func(ctx context.Context, task *model.SystemTask, runner string) {
					close(aEntered)
					<-bFinished
					complete(ctx, task, runner)
				}
				second = func(ctx context.Context, task *model.SystemTask, runner string) {
					<-aEntered
					complete(ctx, task, runner)
					close(bFinished)
				}
			}
			service.SystemContractRegistry(t, systemScheduled{kind: "a", enabled: true, run: first}, systemScheduled{kind: "b", enabled: true, run: second})
			for i, kind := range []string{"a", "a", "b"} {
				require.NoError(t, model.DB.Create(&model.SystemTask{ID: int64(i + 1), TaskID: fmt.Sprintf("pending-%d",
					i), Type: kind, Status: model.SystemTaskStatusPending}).Error)
			}
			if name == "claim_lock_busy" {
				require.NoError(t, model.DB.Create(&model.SystemTaskLock{Type: "a", TaskID: "other", LockedBy: "other", LockedUntil: fixture.Epoch + 60}).Error)
			}
			if name == "claim_query_fault" {
				require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("system-query-fault", func(db *gorm.DB) {
					if db.Statement.Table == "system_tasks" {
						db.AddError(errors.New("fixture query failed"))
					}
				}))
			}
			service.SystemContractClaim("fixture-runner")
			synctest.Wait()
		case strings.HasPrefix(name, "heartbeat_") || name == "progress_throttle":
			task, err := model.CreateSystemTask("fixture", nil, nil)
			require.NoError(t, err)
			task, won, err := model.ClaimSystemTask(task.ID, task.Type, "fixture-runner", fixture.Epoch+60)
			require.NoError(t, err)
			require.True(t, won)
			if name == "progress_throttle" {
				report := service.NewSystemTaskProgressReporter(task, "fixture-runner")
				report(1, 10)
				report(2, 10)
				time.Sleep(2 * time.Second)
				report(3, 10)
				report(10, 10)
			} else {
				service.SystemContractHeartbeat(task, "fixture-runner", func(ctx context.Context) {
					if name == "heartbeat_lost" {
						require.NoError(t, model.DB.Where("type=?", task.Type).Delete(&model.SystemTaskLock{}).Error)
					}
					time.Sleep(21 * time.Second)
					extra["cancelled"] = ctx.Err() != nil
				})
			}
		case name == "runner_follower_once":
			service.SystemContractResetRunner()
			common.IsMasterNode = false
			service.StartSystemTaskRunner()
			common.IsMasterNode = true
			service.StartSystemTaskRunner()
			extra["started_on_follower"] = true
		case name == "runner_master_wakeup":
			service.SystemContractResetRunner()
			common.IsMasterNode = true
			var ran atomic.Int64
			h := systemScheduled{kind: "fixture", enabled: false, run: func(_ context.Context, task *model.SystemTask, runner string) {
				ran.Add(1)
				require.NoError(t, model.FinishSystemTask(task.TaskID, runner, model.SystemTaskStatusSucceeded, nil, ""))
			}}
			service.SystemContractRegistry(t, h)
			var passes atomic.Int64
			require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("system-stop-runner", func(db *gorm.DB) {
				if db.Statement.Table == "system_task_locks" && passes.Add(1) == 2 {
					_ = db.Rollback().Error
					runtime.Goexit()
				}
			}))
			service.StartSystemTaskRunner()
			synctest.Wait()
			_, _, err := service.EnqueueSystemTask("fixture", nil)
			require.NoError(t, err)
			synctest.Wait()
			time.Sleep(31 * time.Second)
			synctest.Wait()
			extra["runs"] = ran.Load()
			assert.Equal(t, int64(1), ran.Load())
		case name == "reporter_loop":
			var calls atomic.Int64
			require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("system-stop-reporter", func(db *gorm.DB) {
				if db.Statement.Table == "system_instances" && calls.Add(1) == 3 {
					_ = db.Rollback().Error
					runtime.Goexit()
				}
			}))
			service.StartSystemInstanceReporter()
			service.StartSystemInstanceReporter()
			synctest.Wait()
			time.Sleep(61 * time.Second)
			synctest.Wait()
			var instance model.SystemInstance
			require.NoError(t, model.DB.First(&instance).Error)
			assert.Equal(t, fixture.Epoch, instance.CreatedAt)
			assert.Equal(t, fixture.Epoch+30, instance.LastSeenAt)
			extra["report_created"], extra["report_last_seen"] = instance.CreatedAt, instance.LastSeenAt
			extra["reports"] = calls.Load()
			assert.Equal(t, int64(3), calls.Load())
			require.NoError(t, model.DB.Where("1=1").Delete(&model.SystemInstance{}).Error)
		default:
			t.Fatalf("unknown runtime case %s", name)
		}
		synctest.Wait()
		// Fault callbacks protect the exercised operation, not the snapshot reads.
		for _, name := range []string{"system-query-fault", "system-stop-runner"} {
			_ = model.DB.Callback().Query().Remove(name)
		}
		state := sf.State(t)
		state["extra"] = extra
		state["outbound_calls"] = transport.calls.Load()
		state["outbound"] = transport.requests
		output = sf.Canonical(t, state)
	})
	return output
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
