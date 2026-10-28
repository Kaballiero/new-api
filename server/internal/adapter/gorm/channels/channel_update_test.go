package channels

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	legacy "github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openChannelUpdateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousGroup, previousKey := legacy.GroupColumn, legacy.KeyColumn
	legacy.GroupColumn, legacy.KeyColumn = "`group`", "`key`"
	t.Cleanup(func() { legacy.GroupColumn, legacy.KeyColumn = previousGroup, previousKey })
	previousDB := legacy.DB
	previousLogDB := legacy.LOG_DB
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&legacy.Channel{}, &channelrecord.Ability{}))
	legacy.DB = db
	legacy.LOG_DB = db
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
		legacy.DB = previousDB
		legacy.LOG_DB = previousLogDB
		common.SetMainDatabaseType(previousMainDatabaseType)
		common.SetLogDatabaseType(previousLogDatabaseType)
	})
	return db
}

func TestChannelUpdate_RollsBackWhenAbilityInsertFails(t *testing.T) {
	db := openChannelUpdateTestDB(t)
	channel := legacy.Channel{
		Name:   "rollback",
		Key:    "sk-test",
		Models: "old-model",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, AddAbilities(&(channel), db))
	require.NoError(t, db.Exec(`
		CREATE TRIGGER fail_ability_insert
		BEFORE INSERT ON abilities
		BEGIN
			SELECT RAISE(FAIL, 'forced ability insert failure');
		END;
	`).Error)

	channel.Models = "new-model"
	err := NewChannelPersistence().Update(&(channel))

	require.ErrorContains(t, err, "forced ability insert failure")
	var stored legacy.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, "old-model", stored.Models)
	var abilities []channelrecord.Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	assert.Equal(t, "old-model", abilities[0].Model)
}

func TestBatchInsertChannelsPreservesInputAndRollsBackAbilities(t *testing.T) {
	db := openChannelUpdateTestDB(t)
	channels := []legacy.Channel{{Name: "batch", Key: "key", Models: "model", Group: "default"}}
	require.NoError(t, BatchInsertChannels(channels))
	assert.Zero(t, channels[0].Id)
	var stored legacy.Channel
	require.NoError(t, db.Where("name = ?", "batch").First(&stored).Error)
	assert.NotZero(t, stored.Id)
	assert.Zero(t, channels[0].Status)
	require.NoError(
		t,
		db.Exec(`CREATE TRIGGER reject_batch_ability BEFORE INSERT ON abilities BEGIN SELECT RAISE(FAIL, 'batch ability failure'); END;`).Error,
	)
	failed := []legacy.Channel{{Name: "rolled-back", Key: "key", Models: "model", Group: "default"}}
	require.ErrorContains(t, BatchInsertChannels(failed), "batch ability failure")
	assert.Zero(t, failed[0].Id)
	var count int64
	require.NoError(t, db.Model(&legacy.Channel{}).Where("name = ?", "rolled-back").Count(&count).Error)
	assert.Zero(t, count)
}
