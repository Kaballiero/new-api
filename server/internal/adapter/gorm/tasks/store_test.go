package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/model"
)

func newTaskTestStore(t *testing.T) (Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&record.Task{}))
	return NewStore(func() *gorm.DB { return db }), db
}

func insertTask(t *testing.T, db *gorm.DB, task *record.Task) {
	t.Helper()
	task.CreatedAt = time.Now().Unix()
	task.UpdatedAt = time.Now().Unix()
	require.NoError(t, db.Create(task).Error)
}

func TestGetTaskForProtocolObservationScopesOwnerAndPlatform(t *testing.T) {
	store, db := newTaskTestStore(t)
	task := &record.Task{
		TaskID:   "task_protocol_scope",
		UserId:   7,
		Platform: "plugin-a",
		Status:   record.TaskStatusInProgress,
	}
	insertTask(t, db, task)

	got, exists, err := store.GetTaskForProtocolObservation(context.Background(), 7, "plugin-a", task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, task.ID, got.ID)

	for _, query := range []struct {
		userID   int
		platform string
	}{
		{userID: 8, platform: "plugin-a"},
		{userID: 7, platform: "plugin-b"},
	} {
		got, exists, err = store.GetTaskForProtocolObservation(context.Background(), query.userID, constant.TaskPlatform(query.platform), task.TaskID)
		require.NoError(t, err)
		assert.False(t, exists)
		assert.Nil(t, got)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = store.GetTaskForProtocolObservation(cancelled, 7, "plugin-a", task.TaskID)
	require.ErrorIs(t, err, context.Canceled)
}

func TestUpdateWithStatus_Win(t *testing.T) {
	store, db := newTaskTestStore(t)

	task := &record.Task{
		TaskID:   "task_cas_win",
		Status:   record.TaskStatusInProgress,
		Progress: "50%",
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, db, task)

	task.Status = record.TaskStatusSuccess
	task.Progress = "100%"
	won, err := store.UpdateTaskWithStatus(task, record.TaskStatusInProgress)
	require.NoError(t, err)
	assert.True(t, won)

	var reloaded record.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, record.TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
}

func TestUpdateWithStatus_Lose(t *testing.T) {
	store, db := newTaskTestStore(t)

	task := &record.Task{
		TaskID: "task_cas_lose",
		Status: record.TaskStatusFailure,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, db, task)

	task.Status = record.TaskStatusSuccess
	won, err := store.UpdateTaskWithStatus(task, record.TaskStatusInProgress) // wrong fromStatus
	require.NoError(t, err)
	assert.False(t, won)

	var reloaded record.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, record.TaskStatusFailure, reloaded.Status) // unchanged
}

func TestUpdateWithStatus_ConcurrentWinner(t *testing.T) {
	store, db := newTaskTestStore(t)

	task := &record.Task{
		TaskID: "task_cas_race",
		Status: record.TaskStatusInProgress,
		Quota:  1000,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, db, task)

	const goroutines = 5
	wins := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			t := &record.Task{}
			*t = record.Task{
				ID:       task.ID,
				TaskID:   task.TaskID,
				Status:   record.TaskStatusSuccess,
				Progress: "100%",
				Quota:    task.Quota,
				Data:     json.RawMessage(`{}`),
			}
			t.CreatedAt = task.CreatedAt
			t.UpdatedAt = time.Now().Unix()
			won, err := store.UpdateTaskWithStatus(t, record.TaskStatusInProgress)
			if err == nil {
				wins[idx] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, w := range wins {
		if w {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one goroutine should win the CAS")
}

func TestUpdateWithStatus_PersistsPluginStateAndPollFailures(t *testing.T) {
	store, db := newTaskTestStore(t)

	task := &record.Task{
		TaskID: "task_cas_plugin_state",
		Status: record.TaskStatusInProgress,
		Data:   json.RawMessage(`{}`),
		PrivateData: record.TaskPrivateData{
			PluginState:  json.RawMessage(`{"req_key":"old"}`),
			PollFailures: 1,
		},
	}
	insertTask(t, db, task)

	task.PrivateData.PluginState = json.RawMessage(`{"req_key":"new"}`)
	task.PrivateData.PollFailures = 4
	won, err := store.UpdateTaskWithStatus(task, record.TaskStatusInProgress)
	require.NoError(t, err)
	require.True(t, won)

	var reloaded record.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, record.TaskStatusInProgress, reloaded.Status)
	assert.JSONEq(t, `{"req_key":"new"}`, string(reloaded.PrivateData.PluginState))
	assert.Equal(t, 4, reloaded.PrivateData.PollFailures)
}

// TestTasksPersistence exercises the same claim and rollback boundaries on each
// configured real engine. PostgreSQL connections are isolated at startup.
func TestTasksPersistence(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			store, db := newTaskPersistenceStore(t, engine)
			for _, source := range []string{"wallet", "subscription"} {
				t.Run(source, func(t *testing.T) {
					user := model.User{Username: "task-" + source, AffCode: "task-" + source, Quota: 1000, UsedQuota: 100, AuthVersion: 1}
					require.NoError(t, db.Create(&user).Error)
					channel := model.Channel{Name: "task-" + source, UsedQuota: 100}
					require.NoError(t, db.Create(&channel).Error)
					token := model.Token{UserId: user.Id, Key: "task-" + source, RemainQuota: 1000, UsedQuota: 100}
					require.NoError(t, db.Create(&token).Error)
					sub := model.UserSubscription{UserId: user.Id, PlanId: 1, AmountTotal: 1000, AmountUsed: 100}
					require.NoError(t, db.Create(&sub).Error)
					task := &record.Task{
						TaskID: "task-" + source, UserId: user.Id, ChannelId: channel.Id,
						Status: record.TaskStatusSuccess, Progress: "100%", Quota: 100,
						PrivateData: record.TaskPrivateData{
							BillingSource: source, SubscriptionId: sub.Id, TokenId: token.Id,
							BillingContext: &record.TaskBillingContext{SettlementMode: record.TaskSettlementOpenRouterCostV1},
							Reconciliation: &record.TaskReconciliation{ReservedQuota: 100, TargetQuota: 70, SettlementPending: true, Required: true},
						},
						Data: json.RawMessage(`{"billing_status":"pending"}`),
					}
					require.NoError(t, store.InsertTask(t.Context(), task))
					// A failed final token leg must roll back the claim, funding and counters.
					task.PrivateData.TokenId = token.Id + 10000
					require.NoError(t, store.UpdateTask(task))
					_, err := store.ApplyTaskCostSettlement(task)
					require.Error(t, err)
					var saved record.Task
					require.NoError(t, db.First(&saved, task.ID).Error)
					assert.Equal(t, 100, saved.Quota)
					assert.False(t, saved.PrivateData.Reconciliation.MainApplied)
					require.NoError(t, db.First(&sub, sub.Id).Error)
					require.NoError(t, db.First(&user, user.Id).Error)
					require.NoError(t, db.First(&channel, channel.Id).Error)
					assert.Equal(t, int64(100), sub.AmountUsed)
					assert.Equal(t, 1000, user.Quota)
					assert.Equal(t, 100, user.UsedQuota)
					assert.Equal(t, int64(100), channel.UsedQuota)

					task.PrivateData.TokenId = token.Id
					require.NoError(t, store.UpdateTask(task))
					key, err := store.ApplyTaskCostSettlement(task)
					require.NoError(t, err)
					assert.Equal(t, token.Key, key)
					require.NoError(t, db.First(&sub, sub.Id).Error)
					require.NoError(t, db.First(&user, user.Id).Error)
					require.NoError(t, db.First(&channel, channel.Id).Error)
					require.NoError(t, db.First(&token, token.Id).Error)
					wantWallet, wantSubscription := 1030, int64(100)
					if source == "subscription" {
						wantWallet, wantSubscription = 1000, 70
					}
					assert.Equal(t, wantWallet, user.Quota)
					assert.Equal(t, wantSubscription, sub.AmountUsed)
					assert.Equal(t, 70, user.UsedQuota)
					assert.Equal(t, int64(70), channel.UsedQuota)
					assert.Equal(t, 1030, token.RemainQuota)
					assert.Equal(t, 70, token.UsedQuota)
					assert.Equal(t, 70, task.Quota)
					assert.True(t, task.PrivateData.Reconciliation.MainApplied)

					_, err = store.ApplyTaskCostSettlement(task)
					require.Error(t, err, "an already applied claim must not transfer money again")
					require.NoError(t, store.SyncTaskCostSettlementCache(task, key))
					require.NoError(t, store.FinalizeTaskCostSettlement(task))
					assert.True(t, task.PrivateData.Reconciliation.Finalized)
					assert.False(t, task.PrivateData.Reconciliation.Required)
					assert.False(t, task.PrivateData.Reconciliation.SettlementPending)
					require.NoError(t, db.First(&saved, task.ID).Error)
					assert.True(t, saved.PrivateData.Reconciliation.Finalized)
					require.NoError(t, db.First(&token, token.Id).Error)
					assert.Equal(t, 1030, token.RemainQuota)
					assert.Equal(t, 70, token.UsedQuota)
				})
			}
		})
	}
}

func newTaskPersistenceStore(t *testing.T, engine string) (Store, *gorm.DB) {
	t.Helper()
	var db, logDB *gorm.DB
	if engine == "sqlite" {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		pool, err := db.DB()
		require.NoError(t, err)
		pool.SetMaxOpenConns(1)
		t.Cleanup(func() { assert.NoError(t, pool.Close()) })
		logDB = db
	} else {
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("host-only: TEST_POSTGRES_DSN is not configured")
		}
		config, err := pgx.ParseConfig(dsn)
		require.NoError(t, err)
		config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		admin := stdlib.OpenDB(*config.Copy())
		t.Cleanup(func() { assert.NoError(t, admin.Close()) })
		schema := "wave5a_tasks_" + strings.ReplaceAll(common.GetUUID(), "-", "")
		for _, name := range []string{schema, schema + "_log"} {
			quoted := pgx.Identifier{name}.Sanitize()
			_, err := admin.ExecContext(t.Context(), "CREATE SCHEMA "+quoted)
			require.NoError(t, err)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, err := admin.ExecContext(ctx, "DROP SCHEMA "+quoted+" CASCADE")
				assert.NoError(t, err)
			})
		}
		for i, name := range []string{schema, schema + "_log"} {
			isolated := config.Copy()
			if isolated.RuntimeParams == nil {
				isolated.RuntimeParams = make(map[string]string)
			}
			isolated.RuntimeParams["search_path"] = name
			pool := stdlib.OpenDB(*isolated)
			t.Cleanup(func() { assert.NoError(t, pool.Close()) })
			opened, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
			require.NoError(t, err)
			// Hold both probes concurrently: every connection must have the same schema.
			first, err := pool.Conn(t.Context())
			require.NoError(t, err)
			t.Cleanup(func() { _ = first.Close() })
			second, err := pool.Conn(t.Context())
			require.NoError(t, err)
			t.Cleanup(func() { _ = second.Close() })
			for _, conn := range []*sql.Conn{first, second} {
				var actual string
				require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT current_schema()").Scan(&actual))
				require.Equal(t, name, actual)
			}
			require.NoError(t, first.Close())
			require.NoError(t, second.Close())
			if i == 0 {
				db = opened
			} else {
				logDB = opened
			}
		}
	}
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	model.DB, model.LOG_DB = db, logDB
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	mainType := common.DatabaseTypeSQLite
	if engine == "postgres" {
		mainType = common.DatabaseTypePostgreSQL
	}
	common.SetDatabaseTypes(mainType, mainType)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMain, oldLog)
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
	})
	versionQuery := "SELECT sqlite_version()"
	if engine == "postgres" {
		versionQuery = "SELECT version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database version: %s", version)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&record.Task{}, &record.Midjourney{}, &model.User{}, &model.Channel{}, &model.Token{}, &model.UserSubscription{}))
	}
	return NewStore(func() *gorm.DB { return db }), db
}

func TestPollingClaimReturnsTimestampObservedByCompletionHook(t *testing.T) {
	store, db := newTaskTestStore(t)
	db.Config.NowFunc = func() time.Time { return time.Unix(300, 0) }
	row := &record.Task{TaskID: "task-poll-timestamp", Status: record.TaskStatusInProgress, Progress: "30%"}
	require.NoError(t, store.InsertTask(t.Context(), row))
	require.NoError(t, db.Model(row).UpdateColumn("updated_at", int64(100)).Error)
	row.UpdatedAt = 100
	task := record.ToDomain(row)
	task.Status = domain.TaskStatusSuccess
	won, err := (PollingStore{Store: store}).Transition(task, domain.TaskStatusInProgress)
	require.NoError(t, err)
	require.True(t, won)
	assert.Equal(t, int64(300), task.UpdatedAt)
}
