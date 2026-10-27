package channels

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	legacy "github.com/QuantumNous/new-api/model"
)

func openChannelStateSnapshotTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&legacy.Channel{}))

	previousDB := legacy.DB
	previousLogDB := legacy.LOG_DB
	legacy.DB = db
	legacy.LOG_DB = db
	t.Cleanup(func() {
		legacy.DB = previousDB
		legacy.LOG_DB = previousLogDB
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func TestListChannelStateSnapshots(t *testing.T) {
	db := openChannelStateSnapshotTestDB(t)
	channels := []legacy.Channel{
		{Name: "second", Key: "secret-two", Type: 14, Status: common.ChannelStatusAutoDisabled},
		{Name: "first", Key: "secret-one", Type: 1, Status: common.ChannelStatusEnabled},
	}
	for index := range channels {
		require.NoError(t, db.Create(&channels[index]).Error)
	}

	got, err := ListChannelStateSnapshots()

	require.NoError(t, err)
	assert.Equal(t, []ChannelStateSnapshot{
		{ID: channels[0].Id, Type: 14, Status: common.ChannelStatusAutoDisabled},
		{ID: channels[1].Id, Type: 1, Status: common.ChannelStatusEnabled},
	}, got)
}
