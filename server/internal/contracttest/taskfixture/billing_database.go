package taskfixture

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	tasksrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var Dialect string

var MainDSN string

var TempDir string

var taskBillingPostgresMainConfig *pgx.ConnConfig
var taskBillingPostgresCleanup []func()

func registerTaskBillingPostgresCleanup(cleanup func()) {
	taskBillingPostgresCleanup = append(taskBillingPostgresCleanup, cleanup)
}

func cleanupTaskBillingPostgresSchemas() {
	for i := len(taskBillingPostgresCleanup) - 1; i >= 0; i-- {
		taskBillingPostgresCleanup[i]()
	}
	taskBillingPostgresCleanup = nil
}

func Main(m *testing.M) {
	if err := i18n.Init(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// A setup panic must not leave private PostgreSQL schemas behind.
	defer cleanupTaskBillingPostgresSchemas()
	previousCustomRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	if os.Getenv("FX_TASK_DB_DIALECT") == "" || os.Getenv("FX_TASK_DB_DIALECT") == "sqlite" {
		var err error
		TempDir, err = os.MkdirTemp("", "new-api-fx-task-")
		if err != nil {
			panic(err)
		}
	}
	mainDB, logDB, databaseType, err := openTaskBillingTestDatabases()
	if err != nil {
		panic("failed to open task billing test databases: " + err.Error())
	}
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	previousBatchUpdateEnabled := common.BatchUpdateEnabled
	previousLogConsumeEnabled := common.LogConsumeEnabled
	model.DB, model.LOG_DB = mainDB, logDB
	common.SetDatabaseTypes(databaseType, databaseType)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true

	if err := migrateTaskBillingTestDatabases(mainDB, logDB); err != nil {
		panic(err)
	}

	code := m.Run()
	model.DB, model.LOG_DB = previousDB, previousLogDB
	common.SetDatabaseTypes(previousMainType, previousLogType)
	common.RedisEnabled = previousRedisEnabled
	common.BatchUpdateEnabled = previousBatchUpdateEnabled
	common.LogConsumeEnabled = previousLogConsumeEnabled
	closeTaskBillingTestDatabase(mainDB)
	closeTaskBillingTestDatabase(logDB)
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousCustomRate
	if TempDir != "" {
		_ = os.RemoveAll(TempDir)
	}
	cleanupTaskBillingPostgresSchemas()
	os.Exit(code)
}

func migrateTaskBillingTestDatabases(mainDB, logDB *gorm.DB) error {
	if err := mainDB.AutoMigrate(
		&model.Task{}, &model.User{}, &model.Token{}, &model.Channel{}, &model.Midjourney{},
		&model.TopUp{}, &model.UserSubscription{}, &model.SystemTask{}, &model.SystemTaskLock{}, &model.ModelCostFXRow{},
	); err != nil {
		return fmt.Errorf("failed to migrate: %w", err)
	}
	if err := logDB.AutoMigrate(&model.Log{}); err != nil {
		return fmt.Errorf("failed to migrate task billing test log database: %w", err)
	}
	return nil
}

// WithSQLite isolates tests whose fault-injection triggers use SQLite syntax.
// It does not change the process dialect used by the real-database matrix.
func WithSQLite(t *testing.T) {
	t.Helper()
	mainDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "main.sqlite")), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { closeTaskBillingTestDatabase(mainDB) })
	logDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "log.sqlite")), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { closeTaskBillingTestDatabase(logDB) })
	require.NoError(t, migrateTaskBillingTestDatabases(mainDB, logDB))
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB = mainDB, logDB
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
	})
}

func openTaskBillingTestDatabases() (mainDB, logDB *gorm.DB, databaseType common.DatabaseType, err error) {
	Dialect = os.Getenv("FX_TASK_DB_DIALECT")
	if Dialect == "" {
		Dialect = "sqlite"
	}
	open := func(dsn string) (*gorm.DB, error) {
		switch Dialect {
		case "sqlite":
			return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		case "mysql":
			return gorm.Open(mysql.Open(dsn), &gorm.Config{})
		default:
			return nil, fmt.Errorf("unsupported FX_TASK_DB_DIALECT %q", Dialect)
		}
	}

	switch Dialect {
	case "sqlite":
		MainDSN = "file:" + filepath.Join(TempDir, "main.sqlite") + "?cache=shared"
		mainDB, err = open(MainDSN)
		if err != nil {
			return nil, nil, "", err
		}
		logDB, err = open("file:" + filepath.Join(TempDir, "log.sqlite") + "?cache=shared")
		databaseType = common.DatabaseTypeSQLite
	case "mysql":
		MainDSN = os.Getenv("FX_TASK_MAIN_DSN")
		logDSN := os.Getenv("FX_TASK_LOG_DSN")
		if MainDSN == "" || logDSN == "" {
			return nil, nil, "", fmt.Errorf("FX_TASK_MAIN_DSN and FX_TASK_LOG_DSN are required for FX_TASK_DB_DIALECT=%s", Dialect)
		}
		mainDB, err = open(MainDSN)
		if err != nil {
			return nil, nil, "", err
		}
		logDB, err = open(logDSN)
		databaseType = common.DatabaseTypeMySQL
	case "postgres":
		mainDSN, logDSN := os.Getenv("FX_TASK_MAIN_DSN"), os.Getenv("FX_TASK_LOG_DSN")
		if mainDSN == "" {
			mainDSN = os.Getenv("TEST_POSTGRES_DSN")
		}
		if logDSN == "" {
			logDSN = os.Getenv("TEST_POSTGRES_DSN")
		}
		if mainDSN == "" || logDSN == "" {
			return nil, nil, "", fmt.Errorf("PostgreSQL fixture DSN is not configured")
		}
		schema := "wave5a_runtime_" + strings.ReplaceAll(common.GetUUID(), "-", "")
		mainDB, taskBillingPostgresMainConfig, err = openTaskBillingPostgresDatabase(mainDSN, schema, registerTaskBillingPostgresCleanup)
		if err != nil {
			return nil, nil, "", err
		}
		logDB, _, err = openTaskBillingPostgresDatabase(logDSN, schema+"_log", registerTaskBillingPostgresCleanup)
		databaseType = common.DatabaseTypePostgreSQL
	}
	if err != nil {
		closeTaskBillingTestDatabase(mainDB)
		return nil, nil, "", err
	}
	return mainDB, logDB, databaseType, nil
}

// openTaskBillingPostgresDatabase creates an isolated schema and installs its
// search_path on every pooled connection, including the other-node fixture.
func openTaskBillingPostgresDatabase(dsn, schema string, cleanup func(func())) (*gorm.DB, *pgx.ConnConfig, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, nil, err
	}
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	admin := stdlib.OpenDB(*config.Copy())
	quoted := pgx.Identifier{schema}.Sanitize()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+quoted)
	cancel()
	if err != nil {
		_ = admin.Close()
		return nil, nil, err
	}
	cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			panic(err)
		}
		if err := admin.Close(); err != nil {
			panic(err)
		}
	})
	if config.RuntimeParams == nil {
		config.RuntimeParams = make(map[string]string)
	}
	config.RuntimeParams["search_path"] = schema
	pool := stdlib.OpenDB(*config.Copy())
	// Pool closes before the schema is removed, including setup-failure paths.
	cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, err := pool.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer first.Close()
	second, err := pool.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer second.Close()
	for _, conn := range []*sql.Conn{first, second} {
		var actual string
		if err := conn.QueryRowContext(ctx, "SELECT current_schema()").Scan(&actual); err != nil {
			return nil, nil, err
		}
		if actual != schema {
			return nil, nil, fmt.Errorf("task fixture schema mismatch")
		}
	}
	var version string
	if err := db.Raw("SELECT version()").Scan(&version).Error; err != nil {
		return nil, nil, err
	}
	fmt.Printf("database version: %s; isolated task runtime schema %s\n", version, schema)
	return db, config, nil
}

func closeTaskBillingTestDatabase(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

func WithOtherNode(t *testing.T, run func()) {
	t.Helper()
	var db *gorm.DB
	var err error
	switch Dialect {
	case "sqlite":
		db, err = gorm.Open(sqlite.Open(MainDSN), &gorm.Config{})
	case "mysql":
		db, err = gorm.Open(mysql.Open(MainDSN), &gorm.Config{})
	case "postgres":
		require.NotNil(t, taskBillingPostgresMainConfig)
		pool := stdlib.OpenDB(*taskBillingPostgresMainConfig.Copy())
		db, err = gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
	default:
		t.Fatalf("unsupported task billing test dialect %q", Dialect)
	}
	require.NoError(t, err)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		closeTaskBillingTestDatabase(db)
	})
	run()
}

// ---------------------------------------------------------------------------
// Seed helpers
// ---------------------------------------------------------------------------

func BillingFX(rate float64) *tasksrecord.TaskBillingFX {
	return &tasksrecord.TaskBillingFX{SchemaVersion: 1, Source: "cbr-xml-daily.ru", Rate: rate, PublicationVersion: 1, EffectiveAt: 1, FetchedAt: 1}
}

func Truncate(t *testing.T) {
	t.Helper()
	db, logDB := model.DB, model.LOG_DB
	t.Cleanup(func() {
		for _, table := range []string{"tasks", "users", "tokens", "channels", "midjourneys", "top_ups", "user_subscriptions", "system_task_locks", "system_tasks", "model_cost_fx"} {
			require.NoError(t, db.Exec("DELETE FROM "+table).Error)
		}
		require.NoError(t, logDB.Exec("DELETE FROM logs").Error)
	})
}

func SeedUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &model.User{
		Id:       id,
		Username: fmt.Sprintf("test_user_%d", id),
		AffCode:  fmt.Sprintf("test_aff_%d", id),
		Quota:    quota,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
}

func SeedToken(t *testing.T, id int, userId int, key string, remainQuota int) {
	t.Helper()
	token := &model.Token{
		Id:          id,
		UserId:      userId,
		Key:         key,
		Name:        "test_token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: remainQuota,
		UsedQuota:   0,
	}
	require.NoError(t, model.DB.Create(token).Error)
}

func SeedSubscription(t *testing.T, id int, userId int, amountTotal int64, amountUsed int64) {
	t.Helper()
	sub := &model.UserSubscription{
		Id:          id,
		UserId:      userId,
		AmountTotal: amountTotal,
		AmountUsed:  amountUsed,
		Status:      "active",
		StartTime:   time.Now().Unix(),
		EndTime:     time.Now().Add(30 * 24 * time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func SeedChannel(t *testing.T, id int) {
	t.Helper()
	ch := &model.Channel{Id: id, Name: "test_channel", Key: "sk-test", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(ch).Error)
}

func SeedChargedAccounting(t *testing.T, userID, channelID, tokenID, quota, requestCount int) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"used_quota":    quota,
		"request_count": requestCount,
	}).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channelID).
		Update("used_quota", quota).Error)
	if tokenID > 0 {
		require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).
			Update("used_quota", quota).Error)
	}
}

func MakeTask(userId, channelId, quota, tokenId int, billingSource string, subscriptionId int) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     quota,
		Status:    tasksrecord.TaskStatus(tasksrecord.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: tasksrecord.Properties{
			OriginModelName: "test-model",
		},
		PrivateData: tasksrecord.TaskPrivateData{
			BillingSource:  billingSource,
			SubscriptionId: subscriptionId,
			TokenId:        tokenId,
			BillingContext: &tasksrecord.TaskBillingContext{
				ModelPrice:      0.02,
				GroupRatio:      1.0,
				OriginModelName: "test-model",
			},
		},
	}
}
