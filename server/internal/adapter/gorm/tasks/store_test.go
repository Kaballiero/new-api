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

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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

func TestPluginTaskAccessPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	logDSN := os.Getenv("TEST_LOG_POSTGRES_DSN")
	require.NotEmpty(t, logDSN, "separate log PostgreSQL DSN must be explicitly configured")
	db := pgfixture.Open(t, dsn)
	logs := pgfixture.Open(t, logDSN)
	var version string
	require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	t.Log("database version:", version)
	var mainSchema, logSchema string
	require.NoError(t, db.Raw("SELECT current_schema()").Scan(&mainSchema).Error)
	require.NoError(t, logs.Raw("SELECT current_schema()").Scan(&logSchema).Error)
	assert.NotEqual(t, mainSchema, logSchema)
	require.NoError(t, db.AutoMigrate(&record.Task{}))
	store := NewStore(func() *gorm.DB { return db })
	task := record.Task{TaskID: "task_plugin_pg",
		Platform:  "plugin",
		UserId:    1,
		ChannelId: 71,
		Status:    record.TaskStatusQueued,
		Data:      []byte(`{"task_id":"upstream"}`)}
	task.PrivateData.UpstreamTaskID = "upstream"
	require.NoError(t, db.Create(&task).Error)
	found, exists, err := store.GetByTaskId(1, task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, task.TaskID, found.TaskID)
	_, exists, err = store.GetByTaskId(2, task.TaskID)
	require.NoError(t, err)
	assert.False(t, exists)
	rows, err := store.GetByTaskIdsForPlatforms(1, []constant.TaskPlatform{"other", "plugin"}, []string{task.TaskID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	observed, exists, err := store.GetTaskForProtocolObservation(context.Background(), 1, "plugin", task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	_, exists, err = store.GetTaskForProtocolObservation(context.Background(), 1, "other", task.TaskID)
	require.NoError(t, err)
	assert.False(t, exists)
	observed.PrivateData.ResponsesBackground = true
	require.NoError(t, store.MarkResponsesBackground(observed))
	require.NoError(t, db.AutoMigrate(&record.Task{}))
	require.NoError(t, db.AutoMigrate(&record.Task{}))
	saved, exists, err := store.GetByTaskId(1, task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	assert.True(t, saved.PrivateData.ResponsesBackground)
	assert.Equal(t, "upstream", saved.PrivateData.UpstreamTaskID)
	assert.JSONEq(t, string(task.Data), string(saved.Data))
}

// The gateway boundary must retain every public and private Task value, including presence evidence.
func TestTaskDomainRoundTrip(t *testing.T) {
	assert.Nil(t, record.ToDomain(nil))
	assert.Nil(t, record.FromDomain(nil))
	var private record.TaskPrivateData
	require.NoError(t, common.Unmarshal([]byte(`{
 "key":"secret", "upstream_task_id":"upstream", "result_url":"https://example.com/result",
 "execution":{"request_id":"request", "request_path":"/v1/responses", "task_plugin":{"key":"plugin", "name":"Plugin", "version":"1", "author":{"name":"Author", "url":"https://example.com"}, "api_version":1, "generation":7}},
 "billing_source":"subscription", "subscription_id":2, "token_id":3, "node_name":"node", "responses_background":true, "plugin_state":{}, "poll_failures":4,
 "billing_context":{"settlement_mode":"openrouter_cost_v2", "openrouter_cost":{"version":"v2","effective_group_ratio":1.25,"quota_per_unit":500000,"estimated_usd":0.125}, "openrouter_order_facts":{"zero":0,"flag":false,"empty":{}}, "model_price":1.5,"group_ratio":2,"model_ratio":3,"other_ratios":{"duration":4},"origin_model_name":"original","per_call_billing":true,
 "tiered_snapshot":{"billing_mode":"expression","model_name":"model","expr_string":"1","expr_hash":"hash","group_ratio":1,"estimated_prompt_tokens":2,"estimated_completion_tokens":3,"estimated_quota_before_group":4,"estimated_quota_after_group":5,"estimated_tier":"tier","estimated_billing_unit":"call","estimated_fixed_price":1,"quota_per_unit":2,"expr_version":1,"task_usage_billing":true,"usage_facts":{"zero":0}},
 "billing_fx":{"schema_version":1,"source":"publication","rate":100,"publication_version":2,"effective_at":3,"fetched_at":4},"submit_group":{"pure_ratio":1,"has_special_ratio":true,"special_ratio":0}},
 "reconciliation":{"reserved_quota":1,"reservation_pending":true,"submission_pending":true,"refund_pending":true,"required":true,"settlement_pending":true,"main_applied":true,"target_quota":2,"finalized":true,"has_upstream_cost":true,"upstream_cost_confirmed":true,"upstream_cost_usd":0.1,"upstream_is_byok":false,"upstream_cost_details":"{}","quota_saturation":"marker"},
 "openrouter_sync":{"credential_hash":"credential","config_hash":"config","routing_hash":"routing","credential_index":1,"identity_conflict":true,"upstream_request_id":"upstream","dispatch_prepared":true,"provider_tag":"tag","reservation_source":"upstream","reserve_usd":0.2,"pricing_unknown":true,"revision":2,"request_id":"request","billing_preference":"wallet","planned_quota":3,"lease_until":4,"deadline":5,"metadata_url":"https://example.com/meta","headers":{"Header":["value"]},
 "transport":{"task_plugin_key":"plugin","force_format":true,"thinking_to_content":true,"proxy":"https://example.com","pass_through_body_enabled":true,"system_prompt":"prompt","system_prompt_override":true,"openrouter_model_reserve_usd":{"model":0},"openrouter_reservation_source":{"model":"upstream"},"openrouter_sonar_research_reserve_usd":0,"http_protocol":"http1","http2_connection_shards":1},
 "attempts":6,"next_poll":7,"last_http_status":502,"last_error":"retry","result_ready":true,"result_failed":true,"result_mime_type":"audio/wav","result_size":8,"result_sha256":"sha"}
 }`), &private))
	private.OpenRouterAudio = private.OpenRouterSync
	full := record.Task{ID: 1, CreatedAt: 2, UpdatedAt: 3, TaskID: "task_public", Platform: "plugin", UserId: 4, Group: "group", ChannelId: 5, Quota: 6, Action: "action", Status: record.TaskStatusInProgress, FailReason: "reason", SubmitTime: 7, StartTime: 8, FinishTime: 9, Progress: "50%", Properties: record.Properties{Input: "input", UpstreamModelName: "upstream", OriginModelName: "original"}, Username: "user", PrivateData: private, Data: json.RawMessage(`{"zero":0,"false":false,"empty":{}}`)}
	for _, tc := range []struct {
		name string
		task record.Task
	}{
		{"zero", record.Task{}}, {"populated", full},
		{"empty", record.Task{Data: json.RawMessage{}, PrivateData: record.TaskPrivateData{PluginState: json.RawMessage{}, OpenRouterSync: &record.OpenRouterSyncTask{Headers: map[string][]string{}, Transport: record.FromDomainTransport(domain.TransportSettings{OpenRouterModelReserveUSD: map[string]float64{}, OpenRouterReservationSource: map[string]string{}})}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			converted := record.ToDomain(&tc.task)
			restored := record.FromDomain(converted)
			assert.Equal(t, &tc.task, restored)
			assert.Equal(t, converted, record.ToDomain(restored))
		})
	}
}
