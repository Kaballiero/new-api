package channels

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	legacy "github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openChannelStateSnapshotTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	var db *gorm.DB
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		db = pgfixture.Open(t, dsn)
		var version string
		require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
		t.Logf("database version: %s", version)
	} else {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		t.Cleanup(func() {
			sqlDB, err := db.DB()
			require.NoError(t, err)
			require.NoError(t, sqlDB.Close())
		})
	}
	require.NoError(t, db.AutoMigrate(&legacy.Channel{}))

	previousDB := legacy.DB
	previousLogDB := legacy.LOG_DB
	legacy.DB = db
	legacy.LOG_DB = db
	t.Cleanup(func() {
		legacy.DB = previousDB
		legacy.LOG_DB = previousLogDB
	})

	return db
}

func TestListChannelStateSnapshots(t *testing.T) {
	db := openChannelStateSnapshotTestDB(t)
	channels := []legacy.Channel{
		{Name: "second", Key: "secret-two", Type: 14, Status: common.ChannelStatusAutoDisabled, OtherInfo: `{"status_reason":"status_code=402, insufficient credits","status_time":1700000000}`},
		{Name: "first", Key: "secret-one", Type: 1, Status: common.ChannelStatusEnabled},
	}
	for index := range channels {
		require.NoError(t, db.Create(&channels[index]).Error)
	}

	got, err := ListChannelStateSnapshots()

	require.NoError(t, err)
	assert.Equal(t, []ChannelStateSnapshot{
		{ID: channels[0].Id, Type: 14, Status: common.ChannelStatusAutoDisabled, Name: "second", OtherInfo: channels[0].OtherInfo},
		{ID: channels[1].Id, Type: 1, Status: common.ChannelStatusEnabled, Name: "first"},
	}, got)
	var columns []string
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("snapshot:selected-columns", func(tx *gorm.DB) {
		columns = append(columns, tx.Statement.Selects...)
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove("snapshot:selected-columns")) })
	_, err = ListChannelStateSnapshots()
	require.NoError(t, err)
	assert.NotEmpty(t, columns, "an unrestricted SELECT would read channel credentials")
	assert.NotContains(t, columns, "key")
	assert.NotContains(t, columns, "*")
}
