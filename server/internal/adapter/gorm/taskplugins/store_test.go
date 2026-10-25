package taskplugins

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func setupTaskPluginModelTest(t *testing.T) {
	t.Helper()
	originalDB := DB
	t.Cleanup(func() { DB = originalDB })
	var err error
	DB, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, DB.AutoMigrate(&TaskPlugin{}))
}

func TestTaskPluginVersionActivationAndSourceImmutability(t *testing.T) {
	setupTaskPluginModelTest(t)

	v1 := TaskPlugin{Key: "mock", APIVersion: 1, Version: "1.0.0", Source: "v1", SourceHash: "hash-v1", Enabled: true}
	require.NoError(t, savePlugin(&v1))
	assert.True(t, v1.Active)

	v2 := TaskPlugin{Key: "mock", APIVersion: 1, Version: "2.0.0", Source: "v2", SourceHash: "hash-v2", Enabled: true}
	require.NoError(t, savePlugin(&v2))
	assert.False(t, v2.Active)
	require.NoError(t, testStore().ActivateTaskPlugin("mock", "2.0.0"))

	active, err := activePlugins()
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "2.0.0", active[0].Version)

	conflict := TaskPlugin{Key: "mock", APIVersion: 1, Version: "2.0.0", Source: "changed", SourceHash: "different", Enabled: true}
	err = savePlugin(&conflict)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different source")

	require.NoError(t, testStore().SetTaskPluginEnabled("mock", false))
	active, err = activePlugins()
	require.NoError(t, err)
	assert.Empty(t, active)

	all, err := testStore().ListTaskPlugins()
	require.NoError(t, err)
	assert.Len(t, all, 2)
	deleteResult, err := testStore().DeleteTaskPluginVersion("mock", "2.0.0")
	require.NoError(t, err)
	assert.True(t, deleteResult.DeletedActive)
	require.NotNil(t, deleteResult.Promoted)
	assert.Equal(t, "1.0.0", deleteResult.Promoted.Version)
	versions, err := testStore().ListTaskPluginVersions("mock")
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.Equal(t, "1.0.0", versions[0].Version)
	assert.True(t, versions[0].Active)
}

func TestDeleteActiveTaskPluginPromotesNewestRemainingVersion(t *testing.T) {
	setupTaskPluginModelTest(t)

	plugins := []*TaskPlugin{
		{Key: "promote", APIVersion: 1, Version: "1.0.0", Source: "v1", SourceHash: "hash-v1", Enabled: true},
		{Key: "promote", APIVersion: 1, Version: "2.0.0", Source: "v2", SourceHash: "hash-v2", Enabled: false},
		{Key: "promote", APIVersion: 1, Version: "3.0.0", Source: "v3", SourceHash: "hash-v3", Enabled: true},
		{Key: "promote", APIVersion: 1, Version: "4.0.0", Source: "v4", SourceHash: "hash-v4", Enabled: true},
	}
	for _, plugin := range plugins {
		require.NoError(t, savePlugin(plugin))
	}
	require.NoError(t, DB.Model(plugins[1]).Update("created_at", 200).Error)
	require.NoError(t, DB.Model(plugins[2]).Update("created_at", 100).Error)
	require.NoError(t, DB.Model(plugins[3]).Update("created_at", 100).Error)

	deleteResult, err := testStore().DeleteTaskPluginVersion("promote", "1.0.0")
	require.NoError(t, err)
	assert.True(t, deleteResult.DeletedActive)
	require.NotNil(t, deleteResult.Promoted)
	assert.Equal(t, "2.0.0", deleteResult.Promoted.Version)
	assert.False(t, deleteResult.Promoted.Enabled)

	deleteResult, err = testStore().DeleteTaskPluginVersion("promote", "2.0.0")
	require.NoError(t, err)
	assert.True(t, deleteResult.DeletedActive)
	require.NotNil(t, deleteResult.Promoted)
	assert.Equal(t, "4.0.0", deleteResult.Promoted.Version)

	active, err := testStore().GetTaskPluginVersion("promote", "")
	require.NoError(t, err)
	assert.Equal(t, "4.0.0", active.Version)
}

func TestTaskPluginSyncSnapshotRevisionTracksDesiredRuntimeState(t *testing.T) {
	setupTaskPluginModelTest(t)

	empty, err := testStore().GetTaskPluginSyncSnapshot()
	require.NoError(t, err)
	assert.Empty(t, empty.Plugins)
	require.NotEmpty(t, empty.Revision)

	v1 := TaskPlugin{
		Key: "revision-probe", APIVersion: 1, Version: "1.0.0",
		Source: "v1", SourceHash: "hash-v1", Enabled: true,
	}
	require.NoError(t, savePlugin(&v1))
	v1Snapshot, err := testStore().GetTaskPluginSyncSnapshot()
	require.NoError(t, err)
	require.Len(t, v1Snapshot.Plugins, 1)
	assert.NotEqual(t, empty.Revision, v1Snapshot.Revision)

	v2 := TaskPlugin{
		Key: "revision-probe", APIVersion: 1, Version: "2.0.0",
		Source: "v2", SourceHash: "hash-v2", Enabled: true,
	}
	require.NoError(t, savePlugin(&v2))
	inactiveAdded, err := testStore().GetTaskPluginSyncSnapshot()
	require.NoError(t, err)
	assert.Equal(t, v1Snapshot.Revision, inactiveAdded.Revision)

	require.NoError(t, DB.Model(&v1).Update("remark", "operator note").Error)
	remarkChanged, err := testStore().GetTaskPluginSyncSnapshot()
	require.NoError(t, err)
	assert.Equal(t, v1Snapshot.Revision, remarkChanged.Revision)

	require.NoError(t, testStore().ActivateTaskPlugin("revision-probe", "2.0.0"))
	v2Snapshot, err := testStore().GetTaskPluginSyncSnapshot()
	require.NoError(t, err)
	require.Len(t, v2Snapshot.Plugins, 1)
	assert.Equal(t, "2.0.0", v2Snapshot.Plugins[0].Version)
	assert.NotEqual(t, v1Snapshot.Revision, v2Snapshot.Revision)

	require.NoError(t, testStore().SetTaskPluginEnabled("revision-probe", false))
	disabled, err := testStore().GetTaskPluginSyncSnapshot()
	require.NoError(t, err)
	assert.Empty(t, disabled.Plugins)
	assert.NotEqual(t, v2Snapshot.Revision, disabled.Revision)
}

func TestTaskPluginOrderSQLQuotesMySQLKeyColumn(t *testing.T) {
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)

	var sqls []string
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:capture_task_plugin_sql", func(tx *gorm.DB) {
		sqls = append(sqls, tx.Statement.SQL.String())
	}))

	originalDB := DB
	t.Cleanup(func() { DB = originalDB })
	DB = db

	_, err = testStore().ListTaskPlugins()
	require.NoError(t, err)
	_, err = testStore().GetTaskPluginSyncSnapshot()
	require.NoError(t, err)

	require.Len(t, sqls, 2)
	for _, sql := range sqls {
		assert.Contains(t, sql, "`key`")
		assert.NotRegexp(t, `(?i)ORDER BY[[:space:]]+key([[:space:],]|$)`, sql)
	}
}

var DB *gorm.DB

func testStore() Store { return NewStore(func() *gorm.DB { return DB }) }
func activePlugins() ([]domain.Plugin, error) {
	v, e := testStore().GetTaskPluginSyncSnapshot()
	return v.Plugins, e
}

func savePlugin(p *TaskPlugin) error { return testStore().SaveTaskPlugin((*domain.Plugin)(p)) }

// PostgreSQL uses its own schema and startup search_path on every pooled connection.
// The original native regression tests above retain their private SQLite databases.
func TestTaskPluginPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	scoped := pgfixture.ScopedDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(scoped), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	pgfixture.ProbePool(t, db)
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	t.Cleanup(func() { common.SetDatabaseTypes(oldMain, oldLog) })
	var version string
	require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	t.Log("database version:", version)
	require.NoError(t, db.AutoMigrate(&TaskPlugin{}))
	store := NewStore(func() *gorm.DB { return db })
	first := domain.Plugin{Key: "versions", APIVersion: 1, Version: "1.0.0", Source: "first", SourceHash: "first", Enabled: true}
	require.NoError(t, store.SaveTaskPlugin(&first))
	assert.True(t, first.Active)
	repeat := first
	repeat.Remark = "changed"
	require.NoError(t, store.SaveTaskPlugin(&repeat))
	assert.Equal(t, first.Id, repeat.Id)
	conflict := first
	conflict.SourceHash = "conflict"
	require.Error(t, store.SaveTaskPlugin(&conflict))
	second := domain.Plugin{Key: first.Key, APIVersion: 1, Version: "2.0.0", Source: "second", SourceHash: "second", Enabled: true}
	require.NoError(t, store.SaveTaskPlugin(&second))
	assert.False(t, second.Active)
	before, err := store.GetTaskPluginSyncSnapshot()
	require.NoError(t, err)
	require.NoError(t, store.ActivateTaskPlugin(first.Key, second.Version))
	after, err := store.GetTaskPluginSyncSnapshot()
	require.NoError(t, err)
	assert.NotEqual(t, before.Revision, after.Revision)
	require.ErrorIs(t, store.ActivateTaskPlugin(first.Key, "missing"), domain.ErrNotFound)
	active, err := store.GetTaskPluginVersion(first.Key, "")
	require.NoError(t, err)
	assert.Equal(t, second.Version, active.Version)
	deleted, err := store.DeleteTaskPluginVersion(first.Key, second.Version)
	require.NoError(t, err)
	require.NotNil(t, deleted.Promoted)
	assert.Equal(t, first.Version, deleted.Promoted.Version)
	require.NoError(t, db.AutoMigrate(&TaskPlugin{}))
	require.NoError(t, db.AutoMigrate(&TaskPlugin{}))
	reopened, err := gorm.Open(postgres.Open(scoped), &gorm.Config{})
	require.NoError(t, err)
	reopenedPool, err := reopened.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopenedPool.Close()) })
	pgfixture.ProbePool(t, reopened)
	rows, err := NewStore(func() *gorm.DB { return reopened }).ListTaskPlugins()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "changed", rows[0].Remark)
	assert.True(t, rows[0].Active)
	duplicate := TaskPlugin{Key: first.Key, Version: first.Version, Source: "duplicate", SourceHash: "dup", APIVersion: 1}
	require.Error(t, reopened.Create(&duplicate).Error, "unique key/version index survives repeated migration")
}
