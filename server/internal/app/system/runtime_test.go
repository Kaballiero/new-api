package system

import (
	"context"
	systemrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/system/record"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/system/enqueue_system_task"

	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"

	"github.com/QuantumNous/new-api/common"
	systemstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/system"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// withSystemTaskRegistry swaps the package registry for the given handlers for
// the duration of a test and restores the original registry afterward.
func withSystemTaskRegistry(t *testing.T, handlers ...domain.SystemTaskHandler) {
	t.Helper()
	systemTaskHandlersMu.Lock()
	saved := systemTaskHandlers
	systemTaskHandlers = map[string]domain.SystemTaskHandler{}
	for _, h := range handlers {
		systemTaskHandlers[h.Type()] = h
	}
	systemTaskHandlersMu.Unlock()
	t.Cleanup(func() {
		systemTaskHandlersMu.Lock()
		systemTaskHandlers = saved
		systemTaskHandlersMu.Unlock()
	})
}

type stubScheduledHandler struct {
	taskType string
	enabled  bool
	interval time.Duration
	onRun    func(ctx context.Context, task *domain.SystemTask, runnerID string)
}

type stubSystemTaskRunResult struct {
	taskID   string
	taskType string
	err      error
}

func (h *stubScheduledHandler) Type() string { return h.taskType }

func (h *stubScheduledHandler) Run(ctx context.Context, task *domain.SystemTask, runnerID string) {
	if h.onRun != nil {
		h.onRun(ctx, task, runnerID)
	}
}

func (h *stubScheduledHandler) Enabled() bool           { return h.enabled }
func (h *stubScheduledHandler) Interval() time.Duration { return h.interval }
func (h *stubScheduledHandler) NewPayload() any         { return nil }

func countSystemTasks(t *testing.T, taskType string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.SystemTask{}).Where("type = ?", taskType).Count(&count).Error)
	return count
}

func TestSystemTaskSchedulerCreatesWhenDueAndDedups(t *testing.T) {
	truncate(t)

	handler := &stubScheduledHandler{taskType: "test_scheduled", enabled: true, interval: time.Minute}
	withSystemTaskRegistry(t, handler)

	_ = Default().Schedule.Execute()
	require.Equal(t, int64(1), countSystemTasks(t, handler.taskType))

	// An active (pending) row already exists, so a second pass must not create
	// another row.
	_ = Default().Schedule.Execute()
	require.Equal(t, int64(1), countSystemTasks(t, handler.taskType))

	// Finish the run; with a fresh updated_at the next run is not due yet.
	latest, err := latestTask(handler.taskType)
	require.NoError(t, err)
	require.NotNil(t, latest)
	_, claimed, err := systemStoreSystemTaskTest.ClaimSystemTask(latest.ID, handler.taskType, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, systemStoreSystemTaskTest.FinishSystemTask(latest.TaskID, "runner-a", systemrecord.SystemTaskStatusSucceeded, nil, ""))

	_ = Default().Schedule.Execute()
	require.Equal(t, int64(1), countSystemTasks(t, handler.taskType))

	// Backdate the finished row beyond the interval -> the job becomes due again.
	require.NoError(t, model.DB.Model(&model.SystemTask{}).
		Where("task_id = ?", latest.TaskID).
		Update("updated_at", common.GetTimestamp()-120).Error)

	_ = Default().Schedule.Execute()
	require.Equal(t, int64(2), countSystemTasks(t, handler.taskType))
}

func TestSystemTaskSchedulerSkipsDisabled(t *testing.T) {
	truncate(t)

	handler := &stubScheduledHandler{taskType: "test_disabled", enabled: false, interval: time.Minute}
	withSystemTaskRegistry(t, handler)

	_ = Default().Schedule.Execute()
	assert.Equal(t, int64(0), countSystemTasks(t, handler.taskType))
}

func TestSystemTaskClaimPassDispatchesByType(t *testing.T) {
	truncate(t)

	ran := make(chan stubSystemTaskRunResult, 1)
	handler := &stubScheduledHandler{
		taskType: "test_dispatch",
		enabled:  true,
		interval: time.Minute,
		onRun: func(_ context.Context, task *domain.SystemTask, runnerID string) {
			ran <- stubSystemTaskRunResult{
				taskType: task.Type,
				err:      systemStoreSystemTaskTest.FinishSystemTask(task.TaskID, runnerID, systemrecord.SystemTaskStatusSucceeded, nil, ""),
			}
		},
	}
	withSystemTaskRegistry(t, handler)

	_, err := systemStoreSystemTaskTest.CreateSystemTask(handler.taskType, nil, nil)
	require.NoError(t, err)

	_ = Default().Claim.Execute("runner-dispatch")

	select {
	case got := <-ran:
		require.NoError(t, got.err)
		assert.Equal(t, handler.taskType, got.taskType)
	case <-time.After(2 * time.Second):
		t.Fatal("claimed task was not dispatched to its handler")
	}

	require.Eventually(t, func() bool {
		latest, err := latestTask(handler.taskType)
		return err == nil && latest != nil && latest.Status == systemrecord.SystemTaskStatusSucceeded
	}, 2*time.Second, 20*time.Millisecond)
}

func TestSystemTaskClaimPassDispatchesEarliestPendingByType(t *testing.T) {
	truncate(t)

	ran := make(chan stubSystemTaskRunResult, 2)
	handlerA := &stubScheduledHandler{
		taskType: "test_dispatch_a",
		enabled:  true,
		interval: time.Minute,
		onRun: func(_ context.Context, task *domain.SystemTask, runnerID string) {
			ran <- stubSystemTaskRunResult{
				taskID: task.TaskID,
				err:    systemStoreSystemTaskTest.FinishSystemTask(task.TaskID, runnerID, systemrecord.SystemTaskStatusSucceeded, nil, ""),
			}
		},
	}
	handlerB := &stubScheduledHandler{
		taskType: "test_dispatch_b",
		enabled:  true,
		interval: time.Minute,
		onRun: func(_ context.Context, task *domain.SystemTask, runnerID string) {
			ran <- stubSystemTaskRunResult{
				taskID: task.TaskID,
				err:    systemStoreSystemTaskTest.FinishSystemTask(task.TaskID, runnerID, systemrecord.SystemTaskStatusSucceeded, nil, ""),
			}
		},
	}
	withSystemTaskRegistry(t, handlerA, handlerB)

	firstA, err := systemStoreSystemTaskTest.CreateSystemTask(handlerA.taskType, nil, nil)
	require.NoError(t, err)
	secondTaskID, err := legacyTaskID()
	require.NoError(t, err)
	secondA := &model.SystemTask{
		TaskID: secondTaskID,
		Type:   handlerA.taskType,
		Status: systemrecord.SystemTaskStatusPending,
	}
	require.NoError(t, model.DB.Create(secondA).Error)
	firstB, err := systemStoreSystemTaskTest.CreateSystemTask(handlerB.taskType, nil, nil)
	require.NoError(t, err)

	_ = Default().Claim.Execute("runner-dispatch")

	got := map[string]bool{}
	for range 2 {
		select {
		case result := <-ran:
			require.NoError(t, result.err)
			got[result.taskID] = true
		case <-time.After(2 * time.Second):
			t.Fatal("claimed tasks were not dispatched to their handlers")
		}
	}

	assert.True(t, got[firstA.TaskID])
	assert.True(t, got[firstB.TaskID])
	assert.False(t, got[secondA.TaskID])

	require.Eventually(t, func() bool {
		reloaded, err := systemStoreSystemTaskTest.GetSystemTaskByTaskID(secondA.TaskID)
		return err == nil && reloaded != nil && reloaded.Status == systemrecord.SystemTaskStatusPending
	}, 2*time.Second, 20*time.Millisecond)
}

func TestEnqueueSystemTaskReportsCreatedAndExistingActive(t *testing.T) {
	truncate(t)

	queued, err := Default().Enqueue.Execute(enqueue_system_task.Input{Type: "test_enqueue", Payload: map[string]bool{"manual": true}})
	first, created := queued.Task, queued.Created
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, first)

	reused, err := Default().Enqueue.Execute(enqueue_system_task.Input{Type: "test_enqueue"})
	existing, created := reused.Task, reused.Created
	require.NoError(t, err)
	require.False(t, created)
	require.NotNil(t, existing)
	assert.Equal(t, first.TaskID, existing.TaskID)

	_, claimed, err := systemStoreSystemTaskTest.ClaimSystemTask(first.ID, first.Type, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, systemStoreSystemTaskTest.FinishSystemTask(first.TaskID, "runner-a", systemrecord.SystemTaskStatusSucceeded, nil, ""))

	next, err := Default().Enqueue.Execute(enqueue_system_task.Input{Type: "test_enqueue"})
	second, created := next.Task, next.Created
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, second)
	assert.NotEqual(t, first.TaskID, second.TaskID)
}

// BatchContractHandlers and scheduler entrypoints expose test-only seams to the
// external Batch characterization; production has no new registration locator.

var systemStoreSystemTaskTest = systemstorage.Store{DB: func() *gorm.DB { return model.DB }}

func truncate(t *testing.T) { taskfixture.WithSQLite(t); _ = Default() }

func latestTask(kind string) (*domain.SystemTask, error) {
	rows, err := systemStoreSystemTaskTest.GetLatestSystemTasks([]string{kind})
	return rows[kind], err
}

func legacyTaskID() (string, error) { return "systask_" + common.GetRandomString(32), nil }

// Default uses the same assembly body; an earlier caller replacement must survive lazy assembly.
func TestSystemTaskRegistrationBeforeAssemblyKeepsReplacement(t *testing.T) {
	withSystemTaskRegistry(t)
	custom := &stubScheduledHandler{taskType: "log_cleanup"}
	RegisterSystemTaskHandler(custom)
	_ = newRuntime()
	handlers := handlers()
	require.Len(t, handlers, 1)
	require.Same(t, custom, handlers[0])
}
