package channels_test

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	channelcore "github.com/QuantumNous/new-api/internal/app/channelcore"
	legacy "github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelStatusTest(t *testing.T) {
	t.Helper()
	channelstore.TestOpenChannelUpdateDB(t)
	require.NoError(t, legacy.DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, legacy.DB.Exec("DELETE FROM channels").Error)

	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = memoryCacheEnabled
	})
}

func TestUpdateChannelStatusPersistsMultiKeyState(t *testing.T) {
	setupChannelStatusTest(t)

	channel := legacy.Channel{
		Name:   "multi-key-status",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: record.ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, legacy.DB.Create(&channel).Error)

	changed := channelcore.UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key")
	require.True(t, changed)

	var stored legacy.Channel
	require.NoError(t, legacy.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])
	assert.NotZero(t, stored.ChannelInfo.MultiKeyDisabledTime[0])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestSaveStatusStateFromSingleKeySnapshotPreservesUnownedColumns(t *testing.T) {
	setupChannelStatusTest(t)

	channel := legacy.Channel{
		Name:        "single-key-status",
		Key:         "original-key",
		Status:      common.ChannelStatusEnabled,
		Models:      "original-model",
		Group:       "default",
		UsedQuota:   100,
		ChannelInfo: record.ChannelInfo{},
	}
	require.NoError(t, legacy.DB.Create(&channel).Error)

	stale, err := channelstore.GetChannelById(channel.Id, true)
	require.NoError(t, err)

	concurrentChannelInfo := record.ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyMode:         constant.MultiKeyModePolling,
		MultiKeyPollingIndex: 1,
	}
	require.NoError(t, legacy.DB.Model(&legacy.Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"key":          "rotated-key",
		"used_quota":   gorm.Expr("used_quota + ?", 250),
		"models":       "concurrent-model",
		"channel_info": concurrentChannelInfo,
	}).Error)

	stale.Status = common.ChannelStatusManuallyDisabled
	staleState := record.ToDomain(stale)
	(record.Codec{Log: common.SysLog}).WriteOtherInfo(staleState, map[string]any{
		"status_reason": "manual operation",
		"status_time":   int64(1234),
	})
	stale.OtherInfo = staleState.OtherInfo
	require.NoError(t, channelstore.NewChannelPersistence().SaveStatusState(stale))

	var stored legacy.Channel
	require.NoError(t, legacy.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	assert.Equal(t, "rotated-key", stored.Key)
	assert.Equal(t, int64(350), stored.UsedQuota)
	assert.Equal(t, "concurrent-model", stored.Models)
	assert.Equal(t, concurrentChannelInfo, stored.ChannelInfo)

	otherInfo := (record.Codec{Log: common.SysLog}).ReadOtherInfo(record.ToDomain(&stored))
	assert.Equal(t, "manual operation", otherInfo["status_reason"])
	assert.Equal(t, float64(1234), otherInfo["status_time"])
}

func TestChannelStateCommitPreservesConcurrentCacheFields(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(fmt.Sprint(multi), func(t *testing.T) {
			native := &record.Channel{Id: 42, Key: "original", ChannelInfo: record.ChannelInfo{IsMultiKey: multi}}
			store := record.NewStore(record.Environment{CachedRead: func(int) (*record.Channel, error) { return native, nil }})
			state, err := store.CachedState(42)
			require.NoError(t, err)
			native.Key = "rotated"
			native.ResponseTime = 123
			native.ChannelInfo.MultiKeyPollingIndex = 7
			state.Status = common.ChannelStatusManuallyDisabled
			state.OtherInfo = "status metadata"
			state.ChannelInfo.MultiKeyPollingIndex = 3
			store.CommitState(state)
			assert.Equal(t, "rotated", native.Key)
			assert.Equal(t, 123, native.ResponseTime)
			assert.Equal(t, state.Status, native.Status)
			assert.Equal(t, state.OtherInfo, native.OtherInfo)
			expectedIndex := 7
			if multi {
				expectedIndex = 3
			}
			assert.Equal(t, expectedIndex, native.ChannelInfo.MultiKeyPollingIndex)
		})
	}
}
