package system

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/system/record"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type testSystemTaskPayload struct {
	TargetTimestamp int64 `json:"target_timestamp"`
	BatchSize       int   `json:"batch_size"`
}

type testSystemTaskState struct {
	Total     int64 `json:"total"`
	Processed int64 `json:"processed"`
	Progress  int   `json:"progress"`
	Remaining int64 `json:"remaining"`
}

func createLegacyPendingSystemTask(t *testing.T, taskType string) *record.SystemTask {
	t.Helper()
	taskID, err := testStore.generateSystemTaskID()
	require.NoError(t, err)
	task := &record.SystemTask{
		TaskID: taskID,
		Type:   taskType,
		Status: domain.SystemTaskStatusPending,
	}
	require.NoError(t, model.DB.Create(task).Error)
	return task
}

func TestSystemTaskCreateAndActiveLifecycle(t *testing.T) {
	truncateTables(t)

	payload := testSystemTaskPayload{TargetTimestamp: 1000, BatchSize: 100}
	state := testSystemTaskState{}
	task, err := testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, payload, state)
	require.NoError(t, err)
	require.NotNil(t, task.ActiveKey)
	assert.Equal(t, domain.SystemTaskTypeLogCleanup, *task.ActiveKey)

	var decodedPayload testSystemTaskPayload
	require.NoError(t, testStore.Decode(task.Payload, &decodedPayload))
	assert.Equal(t, payload, decodedPayload)

	activeTask, err := testStore.GetActiveSystemTask(domain.SystemTaskTypeLogCleanup)
	require.NoError(t, err)
	require.NotNil(t, activeTask)
	assert.Equal(t, task.TaskID, activeTask.TaskID)

	runnerID := "runner-a"
	claimedTask, claimed, err := testStore.ClaimSystemTask(task.ID, domain.SystemTaskTypeLogCleanup, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	err = testStore.FinishSystemTask(claimedTask.TaskID, runnerID, domain.SystemTaskStatusSucceeded, map[string]int64{"deleted_count": 0}, "")
	require.NoError(t, err)

	finishedTask, err := testStore.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, finishedTask)
	assert.Nil(t, finishedTask.ActiveKey)

	activeTask, err = testStore.GetActiveSystemTask(domain.SystemTaskTypeLogCleanup)
	require.NoError(t, err)
	require.Nil(t, activeTask)

	_, err = testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, payload, state)
	require.NoError(t, err)
}

func TestSystemTaskActiveKeyPreventsDuplicateActiveRun(t *testing.T) {
	truncateTables(t)

	payload := testSystemTaskPayload{TargetTimestamp: 1000, BatchSize: 100}
	task, err := testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, payload, testSystemTaskState{})
	require.NoError(t, err)
	_, err = testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, payload, testSystemTaskState{})
	require.Error(t, err)

	activeTask, err := testStore.GetActiveSystemTask(domain.SystemTaskTypeLogCleanup)
	require.NoError(t, err)
	require.NotNil(t, activeTask)
	assert.Equal(t, task.TaskID, activeTask.TaskID)
}

func TestSystemTaskLockPreventsConcurrentClaim(t *testing.T) {
	truncateTables(t)

	payload := testSystemTaskPayload{TargetTimestamp: 1000, BatchSize: 100}
	task, err := testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, payload, testSystemTaskState{})
	require.NoError(t, err)
	secondTask := createLegacyPendingSystemTask(t, domain.SystemTaskTypeLogCleanup)

	claimedTask, claimed, err := testStore.ClaimSystemTask(task.ID, domain.SystemTaskTypeLogCleanup, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	_, claimed, err = testStore.ClaimSystemTask(secondTask.ID, domain.SystemTaskTypeLogCleanup, "runner-b", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.False(t, claimed)

	assert.Equal(t, "runner-a", claimedTask.LockedBy)

	reloadedSecond, err := testStore.GetSystemTaskByTaskID(secondTask.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloadedSecond)
	assert.Equal(t, domain.SystemTaskStatusPending, reloadedSecond.Status)
}

func TestExpiredSystemTaskLockFailsOldRunAndClaimsLegacyPendingRun(t *testing.T) {
	truncateTables(t)

	first, err := testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)
	_, claimed, err := testStore.ClaimSystemTask(first.ID, domain.SystemTaskTypeLogCleanup, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, model.DB.Model(&record.SystemTaskLock{}).
		Where("task_id = ?", first.TaskID).
		Update("locked_until", common.GetTimestamp()-1).Error)

	second := createLegacyPendingSystemTask(t, domain.SystemTaskTypeLogCleanup)
	claimedTask, claimed, err := testStore.ClaimSystemTask(second.ID, domain.SystemTaskTypeLogCleanup, "runner-b", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	assert.Equal(t, second.TaskID, claimedTask.TaskID)
	assert.Equal(t, "runner-b", claimedTask.LockedBy)

	reloadedFirst, err := testStore.GetSystemTaskByTaskID(first.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloadedFirst)
	assert.Equal(t, domain.SystemTaskStatusFailed, reloadedFirst.Status)
	assert.Equal(t, "task lease expired", reloadedFirst.Error)
	assert.Nil(t, reloadedFirst.ActiveKey)
}

func TestExpireStaleSystemTaskLockFailsOldRunAndAllowsNewRun(t *testing.T) {
	truncateTables(t)

	first, err := testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)
	_, claimed, err := testStore.ClaimSystemTask(first.ID, domain.SystemTaskTypeLogCleanup, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, model.DB.Model(&record.SystemTaskLock{}).
		Where("task_id = ?", first.TaskID).
		Update("locked_until", common.GetTimestamp()-1).Error)

	require.NoError(t, testStore.ExpireStaleSystemTaskLocks(common.GetTimestamp()))

	reloadedFirst, err := testStore.GetSystemTaskByTaskID(first.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloadedFirst)
	assert.Equal(t, domain.SystemTaskStatusFailed, reloadedFirst.Status)
	assert.Equal(t, "task lease expired", reloadedFirst.Error)
	assert.Nil(t, reloadedFirst.ActiveKey)

	var lockCount int64
	require.NoError(t, model.DB.Model(&record.SystemTaskLock{}).Where("task_id = ?", first.TaskID).Count(&lockCount).Error)
	assert.Equal(t, int64(0), lockCount)

	second, err := testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)
	require.NotEqual(t, first.TaskID, second.TaskID)
}

func TestFindEarliestPendingSystemTasks(t *testing.T) {
	truncateTables(t)

	empty, err := testStore.FindEarliestPendingSystemTasks(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	firstA, err := testStore.CreateSystemTask("type_a", nil, nil)
	require.NoError(t, err)
	ignoredB, err := testStore.CreateSystemTask("type_b", nil, nil)
	require.NoError(t, err)
	_, claimed, err := testStore.ClaimSystemTask(ignoredB.ID, "type_b", "runner-b", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, testStore.FinishSystemTask(ignoredB.TaskID, "runner-b", domain.SystemTaskStatusFailed, nil, "failed"))
	firstB, err := testStore.CreateSystemTask("type_b", nil, nil)
	require.NoError(t, err)
	ignoredC, err := testStore.CreateSystemTask("type_c", nil, nil)
	require.NoError(t, err)
	_, claimed, err = testStore.ClaimSystemTask(ignoredC.ID, "type_c", "runner-c", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, testStore.FinishSystemTask(ignoredC.TaskID, "runner-c", domain.SystemTaskStatusFailed, nil, "failed"))

	tasks, err := testStore.FindEarliestPendingSystemTasks([]string{"type_a", "type_b", "type_c", "missing"})
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	assert.Equal(t, firstA.TaskID, tasks["type_a"].TaskID)
	assert.Equal(t, firstB.TaskID, tasks["type_b"].TaskID)
	assert.Nil(t, tasks["type_c"])
	assert.Nil(t, tasks["missing"])
}

func TestGetLatestSystemTask(t *testing.T) {
	truncateTables(t)

	latest, err := latestTask(domain.SystemTaskTypeChannelTest)
	require.NoError(t, err)
	require.Nil(t, latest)

	first, err := testStore.CreateSystemTask(domain.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	runnerID := "runner-a"
	_, claimed, err := testStore.ClaimSystemTask(first.ID, domain.SystemTaskTypeChannelTest, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, testStore.FinishSystemTask(first.TaskID, runnerID, domain.SystemTaskStatusSucceeded, nil, ""))

	second, err := testStore.CreateSystemTask(domain.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	latest, err = latestTask(domain.SystemTaskTypeChannelTest)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, second.TaskID, latest.TaskID)
}

func TestGetLatestSystemTasks(t *testing.T) {
	truncateTables(t)

	empty, err := testStore.GetLatestSystemTasks(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	firstA, err := testStore.CreateSystemTask("type_a", nil, nil)
	require.NoError(t, err)
	firstB, err := testStore.CreateSystemTask("type_b", nil, nil)
	require.NoError(t, err)
	_, claimed, err := testStore.ClaimSystemTask(firstA.ID, "type_a", "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, testStore.FinishSystemTask(firstA.TaskID, "runner-a", domain.SystemTaskStatusSucceeded, nil, ""))
	secondA, err := testStore.CreateSystemTask("type_a", nil, nil)
	require.NoError(t, err)

	tasks, err := testStore.GetLatestSystemTasks([]string{"type_a", "type_b", "missing"})
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	assert.NotEqual(t, firstA.TaskID, tasks["type_a"].TaskID)
	assert.Equal(t, secondA.TaskID, tasks["type_a"].TaskID)
	assert.Equal(t, firstB.TaskID, tasks["type_b"].TaskID)
	assert.Nil(t, tasks["missing"])
}

func TestRenewSystemTaskLock(t *testing.T) {
	truncateTables(t)

	task, err := testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)

	runnerID := "runner-a"
	_, claimed, err := testStore.ClaimSystemTask(task.ID, domain.SystemTaskTypeLogCleanup, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	newLockUntil := common.GetTimestamp() + 600
	require.NoError(t, testStore.RenewSystemTaskLock(task.TaskID, runnerID, newLockUntil))

	var lock record.SystemTaskLock
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&lock).Error)
	assert.Equal(t, newLockUntil, lock.LockedUntil)

	// A different runner cannot renew a lease it does not hold.
	assert.ErrorIs(t, testStore.RenewSystemTaskLock(task.TaskID, "runner-b", common.GetTimestamp()+600), domain.ErrSystemTaskLockLost)

	// After the task finishes it is no longer running, so renew fails.
	require.NoError(t, testStore.FinishSystemTask(task.TaskID, runnerID, domain.SystemTaskStatusSucceeded, nil, ""))
	assert.ErrorIs(t, testStore.RenewSystemTaskLock(task.TaskID, runnerID, common.GetTimestamp()+600), domain.ErrSystemTaskLockLost)
}

func TestFinishSystemTaskRetainsExecutor(t *testing.T) {
	truncateTables(t)

	task, err := testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)

	runnerID := "node-1-abc123"
	_, claimed, err := testStore.ClaimSystemTask(task.ID, domain.SystemTaskTypeLogCleanup, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, testStore.FinishSystemTask(task.TaskID, runnerID, domain.SystemTaskStatusSucceeded, nil, ""))

	reloaded, err := testStore.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.Equal(t, domain.SystemTaskStatusSucceeded, reloaded.Status)
	assert.Equal(t, runnerID, reloaded.LockedBy, "executor-of-record must be retained for history")

	var lockCount int64
	require.NoError(t, model.DB.Model(&record.SystemTaskLock{}).Where("task_id = ?", task.TaskID).Count(&lockCount).Error)
	assert.Equal(t, int64(0), lockCount)
}

func TestSystemTaskUpdatesRequireCurrentLock(t *testing.T) {
	truncateTables(t)

	task, err := testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)

	runnerID := "runner-a"
	_, claimed, err := testStore.ClaimSystemTask(task.ID, domain.SystemTaskTypeLogCleanup, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, model.DB.Model(&record.SystemTaskLock{}).
		Where("task_id = ?", task.TaskID).
		Updates(map[string]any{"locked_by": "runner-b"}).Error)

	assert.ErrorIs(t, testStore.UpdateSystemTaskState(task.TaskID, runnerID, testSystemTaskState{Progress: 10}), domain.ErrSystemTaskLockLost)
	assert.ErrorIs(t, testStore.FinishSystemTask(task.TaskID, runnerID, domain.SystemTaskStatusSucceeded, nil, ""), domain.ErrSystemTaskLockLost)
}

func TestSystemTaskUpdatesRequireUnexpiredLock(t *testing.T) {
	truncateTables(t)

	task, err := testStore.CreateSystemTask(domain.SystemTaskTypeLogCleanup, nil, nil)
	require.NoError(t, err)

	runnerID := "runner-a"
	_, claimed, err := testStore.ClaimSystemTask(task.ID, domain.SystemTaskTypeLogCleanup, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, model.DB.Model(&record.SystemTaskLock{}).
		Where("task_id = ?", task.TaskID).
		Update("locked_until", common.GetTimestamp()-1).Error)

	assert.ErrorIs(t, testStore.UpdateSystemTaskState(task.TaskID, runnerID, testSystemTaskState{Progress: 10}), domain.ErrSystemTaskLockLost)
	assert.ErrorIs(t, testStore.FinishSystemTask(task.TaskID, runnerID, domain.SystemTaskStatusSucceeded, nil, ""), domain.ErrSystemTaskLockLost)

	reloaded, err := testStore.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.Equal(t, domain.SystemTaskStatusRunning, reloaded.Status)
	assert.Empty(t, reloaded.State)
}

func TestUpdateSystemTaskStateIdenticalPayloadDoesNotLoseLock(t *testing.T) {
	// SQLite reports matched rows for unchanged UPDATEs, so this case passed
	// even before the fix. The MySQL regression is covered by
	// TestUpdateSystemTaskStateIdenticalPayloadDoesNotLoseLockConfiguredDatabases.
	truncateTables(t)
	runUpdateSystemTaskStateIdenticalPayloadKeepsLock(t, domain.SystemTaskTypeLogCleanup)
}

func runUpdateSystemTaskStateIdenticalPayloadKeepsLock(t *testing.T, taskType string) {
	t.Helper()
	// Two persists in the same second so MySQL's unchanged-row UPDATE returns 0.

	task, err := testStore.CreateSystemTask(taskType, nil, nil)
	require.NoError(t, err)

	runnerID := "runner-a"
	_, claimed, err := testStore.ClaimSystemTask(task.ID, taskType, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	state := testSystemTaskState{Total: 10, Processed: 10, Progress: 100, Remaining: 0}
	require.NoError(t, testStore.UpdateSystemTaskState(task.TaskID, runnerID, state))
	require.NoError(t, testStore.UpdateSystemTaskState(task.TaskID, runnerID, state), "identical state persist must not be treated as lock loss")

	require.NoError(t, testStore.FinishSystemTask(task.TaskID, runnerID, domain.SystemTaskStatusSucceeded, map[string]int64{"deleted_count": 10}, ""))
	finished, err := testStore.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, finished)
	assert.Equal(t, domain.SystemTaskStatusSucceeded, finished.Status)
}

var testStore = Store{DB: func() *gorm.DB { return model.DB }}

func truncateTables(t *testing.T) { taskfixture.WithSQLite(t) }

// Supplied PostgreSQL DSNs are always scoped to a private schema on every connection.
func TestSystemPersistence(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialect gorm.Dialector
			switch engine {
			case "sqlite":
				dialect = sqlite.Open(filepath.Join(t.TempDir(), "system.db"))
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				dialect = postgres.Open(pgfixture.ScopedDSN(t, dsn))
			}
			db, err := gorm.Open(dialect, &gorm.Config{})
			require.NoError(t, err)
			pool, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, pool.Close()) })
			if engine == "postgres" {
				pgfixture.ProbePool(t, db)
			}
			query := "SELECT version()"
			if engine == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Log("database version:", version)
			require.NoError(t, db.AutoMigrate(&record.Setup{}, &record.SystemTask{}, &record.SystemTaskLock{}, &record.SystemInstance{}))
			store := Store{DB: func() *gorm.DB { return db }}
			require.NoError(t, store.CreateSetup("previous", 100))
			task, err := store.CreateSystemTask("persistence", map[string]any{"empty": map[string]any{}, "zero": 0}, nil)
			require.NoError(t, err)
			_, err = store.CreateSystemTask("persistence", nil, nil)
			require.Error(t, err, "active-key uniqueness")
			claimed, won, err := store.ClaimSystemTask(task.ID, task.Type, "owner", common.GetTimestamp()+60)
			require.NoError(t, err)
			require.True(t, won)
			_, won, err = store.ClaimSystemTask(task.ID, task.Type, "other", common.GetTimestamp()+60)
			require.NoError(t, err)
			require.False(t, won)
			state := map[string]int{"progress": 0}
			require.NoError(t, store.UpdateSystemTaskState(claimed.TaskID, "owner", state))
			require.NoError(t, store.UpdateSystemTaskState(claimed.TaskID, "owner", state))
			require.ErrorIs(t, store.UpdateSystemTaskState(claimed.TaskID, "other", state), domain.ErrSystemTaskLockLost)
			require.NoError(t, store.FinishSystemTask(claimed.TaskID, "owner", domain.SystemTaskStatusSucceeded, map[string]any{}, ""))
			require.NoError(t, store.UpsertSystemInstance("node", map[string]any{}, 1, 100))
			require.NoError(t, store.UpsertSystemInstance("node", nil, 2, 101))
			var original record.SystemInstance
			require.NoError(t, db.First(&original).Error)
			require.NoError(t, db.AutoMigrate(&record.Setup{}, &record.SystemTask{}, &record.SystemTaskLock{}, &record.SystemInstance{}))
			require.NoError(t, db.AutoMigrate(&record.Setup{}, &record.SystemTask{}, &record.SystemTaskLock{}, &record.SystemInstance{}))
			reopened, err := gorm.Open(dialect, &gorm.Config{})
			require.NoError(t, err)
			reopenedPool, err := reopened.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, reopenedPool.Close()) })
			if engine == "postgres" {
				pgfixture.ProbePool(t, reopened)
			}
			store = Store{DB: func() *gorm.DB { return reopened }}
			saved, err := store.GetSystemTaskByTaskID(task.TaskID)
			require.NoError(t, err)
			assert.Equal(t, domain.SystemTaskStatusSucceeded, saved.Status)
			assert.Nil(t, saved.ActiveKey)
			assert.Equal(t, "{}", saved.Result)
			assert.Equal(t, int64(100), *store.SetupTime())
			rows, err := store.ListSystemInstances()
			require.NoError(t, err)
			require.Len(t, rows, 1)
			assert.Equal(t, domain.SystemInstanceStatusOnline, domain.InstanceStatus(rows[0].LastSeenAt, 191))
			assert.Empty(t, rows[0].Info)
			deleted, err := store.DeleteStaleSystemInstance("node", 191)
			require.NoError(t, err)
			assert.False(t, deleted)
			deleted, err = store.DeleteStaleSystemInstance("node", 192)
			require.NoError(t, err)
			assert.True(t, deleted)
			require.NoError(t, store.UpsertSystemInstance("node", map[string]any{}, 2, 102))
			var after record.SystemInstance
			require.NoError(t, reopened.First(&after).Error)
			assert.NotZero(t, original.CreatedAt)
			assert.NotZero(t, after.CreatedAt)
			next, err := store.CreateSystemTask("persistence", nil, nil)
			require.NoError(t, err)
			assert.Greater(t, next.ID, task.ID)
		})
	}
}

func latestTask(kind string) (*domain.SystemTask, error) {
	rows, err := testStore.GetLatestSystemTasks([]string{kind})
	return rows[kind], err
}
