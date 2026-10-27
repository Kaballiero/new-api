package channels

import (
	"errors"
	"math/rand"
	"sync"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/logger"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func NewChannelSortOptions(by, order string, id bool) record.ChannelSortOptions {
	return record.NewChannelSortOptions(by, order, id)
}

func ApplyChannelGroupFilter(query *gorm.DB, group string) *gorm.DB {
	return NewChannelPersistence().ApplyChannelGroupFilter(query, group)
}

func GetNextEnabledKey(channel *legacy.Channel) (string, int, *types.NewAPIError) {
	// If not in multi-key mode, return the original key string directly.
	if !channel.ChannelInfo.IsMultiKey {
		return channel.Key, 0, nil
	}

	// Obtain all keys (split by \n)
	keys := channel.GetKeys()
	if len(keys) == 0 {
		// No keys available, return error, should disable the channel
		return "", 0, types.NewError(errors.New("no keys available"), types.ErrorCodeChannelNoAvailableKey)
	}

	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	statusList := channel.ChannelInfo.MultiKeyStatusList
	// helper to get key status, default to enabled when missing
	getStatus := func(idx int) int {
		if statusList == nil {
			return common.ChannelStatusEnabled
		}
		if status, ok := statusList[idx]; ok {
			return status
		}
		return common.ChannelStatusEnabled
	}

	// Collect indexes of enabled keys
	enabledIdx := make([]int, 0, len(keys))
	for i := range keys {
		if getStatus(i) == common.ChannelStatusEnabled {
			enabledIdx = append(enabledIdx, i)
		}
	}
	// If no specific status list or none enabled, return an explicit error so caller can
	// properly handle a channel with no available keys (e.g. mark channel disabled).
	// Returning the first key here caused requests to keep using an already-disabled key.
	if len(enabledIdx) == 0 {
		return "", 0, types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)
	}

	switch channel.ChannelInfo.MultiKeyMode {
	case constant.MultiKeyModeRandom:
		// Randomly pick one enabled key
		selectedIdx := enabledIdx[rand.Intn(len(enabledIdx))]
		return keys[selectedIdx], selectedIdx, nil
	case constant.MultiKeyModePolling:
		// Use channel-specific lock to ensure thread-safe polling

		channelInfo, err := CacheGetChannelInfo(channel.Id)
		if err != nil {
			return "", 0, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		defer func() {
			if common.DebugEnabled {
				logger.LogDebug(nil, "channel %d polling index: %d", channel.Id, channel.ChannelInfo.MultiKeyPollingIndex)
			}
			if !common.MemoryCacheEnabled {
				_ = NewChannelPersistence().SaveChannelInfo(channel)
			} else {
				// CacheUpdateChannel(channel)
			}
		}()
		// Start from the saved polling index and look for the next enabled key
		start := channelInfo.MultiKeyPollingIndex
		if start < 0 || start >= len(keys) {
			start = 0
		}
		for i := range keys {
			idx := (start + i) % len(keys)
			if getStatus(idx) == common.ChannelStatusEnabled {
				// update polling index for next call (point to the next position)
				channel.ChannelInfo.MultiKeyPollingIndex = (idx + 1) % len(keys)
				return keys[idx], idx, nil
			}
		}
		// Fallback – should not happen, but return first enabled key
		return keys[enabledIdx[0]], enabledIdx[0], nil
	default:
		// Unknown mode, default to first enabled key (or original key string)
		return keys[enabledIdx[0]], enabledIdx[0], nil
	}
}

func GetAllChannels(startIdx int, num int, selectAll bool, idSort bool, sortOptions ...record.ChannelSortOptions) ([]*legacy.Channel, error) {
	values, err := NewChannelPersistence().GetAllChannels(startIdx, num, selectAll, idSort, sortOptions...)
	return channelRecords(values), err
}

func GetChannelsByTag(tag string, idSort bool, selectAll bool, sortOptions ...record.ChannelSortOptions) ([]*legacy.Channel, error) {
	values, err := NewChannelPersistence().GetChannelsByTag(tag, idSort, selectAll, sortOptions...)
	return channelRecords(values), err
}

func SearchChannels(keyword string, group string, model string, idSort bool, sortOptions ...record.ChannelSortOptions) ([]*legacy.Channel, error) {
	values, err := NewChannelPersistence().SearchChannels(keyword, group, model, idSort, sortOptions...)
	return channelRecords(values), err
}

// GetChannelById loads a channel directly from the database, bypassing the
// in-memory channel cache.
//
// WARNING: do NOT call this on request hot paths (middleware, distribution,
// relay submit/retry, polling). Every call is a synchronous DB query and will
// not see cache-only state. Use CacheGetChannel instead: it serves from the
// in-memory cache and falls back to this function automatically when
// MemoryCacheEnabled is false. Direct use is appropriate only where fresh DB
// state is required, e.g. admin CRUD, channel testing, or cache (re)building.
func GetChannelById(id int, selectAll bool) (*legacy.Channel, error) {
	value, err := NewChannelPersistence().GetChannelById(id, selectAll)
	return (*legacy.Channel)(value), err
}

func GetChannelByIdForUpdate(tx *gorm.DB, id int) (*legacy.Channel, error) {
	value, err := NewChannelPersistence().GetChannelByIdForUpdate(tx, id)
	return (*legacy.Channel)(value), err
}

func BatchInsertChannels(channels []legacy.Channel) error {
	var values []record.Channel
	if channels != nil {
		values = make([]record.Channel, len(channels))
		for i := range channels {
			values[i] = record.Channel(channels[i])
		}
	}
	return NewChannelPersistence().BatchInsertChannels(values)
}

func BatchDeleteChannels(ids []int) (int64, error) {
	return NewChannelPersistence().BatchDeleteChannels(ids)
}

var channelStatusLock sync.Mutex

// channelPollingLocks stores locks for each channel.id to ensure thread-safe polling
var channelPollingLocks sync.Map

// GetChannelPollingLock returns or creates a mutex for the given channel ID
func GetChannelPollingLock(channelId int) *sync.Mutex {
	if lock, exists := channelPollingLocks.Load(channelId); exists {
		return lock.(*sync.Mutex)
	}
	// Create new lock for this channel
	newLock := &sync.Mutex{}
	actual, _ := channelPollingLocks.LoadOrStore(channelId, newLock)
	return actual.(*sync.Mutex)
}

// CleanupChannelPollingLocks removes locks for channels that no longer exist
// This is optional and can be called periodically to prevent memory leaks
func EnableChannelByTag(tag string) error { return NewChannelPersistence().EnableChannelByTag(tag) }

func DisableChannelByTag(tag string) error { return NewChannelPersistence().DisableChannelByTag(tag) }

func EditChannelByTag(
	tag string,
	newTag *string,
	modelMapping *string,
	models *string,
	group *string,
	priority *int64,
	weight *uint,
	paramOverride *string,
	headerOverride *string,
) error {
	return NewChannelPersistence().EditChannelByTag(tag, newTag, modelMapping, models, group, priority, weight, paramOverride, headerOverride)
}

func DeleteDisabledChannel() (int64, error) { return NewChannelPersistence().DeleteDisabledChannel() }

func GetPaginatedChannelTags(query *gorm.DB, offset int, limit int) ([]*string, error) {
	return NewChannelPersistence().GetPaginatedChannelTags(query, offset, limit)
}

func SearchTags(keyword string, group string, model string, idSort bool) ([]*string, error) {
	return NewChannelPersistence().SearchTags(keyword, group, model, idSort)
}

func GetChannelsByIds(ids []int) ([]*legacy.Channel, error) {
	values, err := NewChannelPersistence().GetChannelsByIds(ids)
	return channelRecords(values), err
}

func BatchSetChannelTag(ids []int, tag *string) error {
	return NewChannelPersistence().BatchSetChannelTag(ids, tag)
}

// CountAllChannels returns total channels in DB
func CountChannelTags(query *gorm.DB) (int64, error) {
	return NewChannelPersistence().CountChannelTags(query)
}

// Get channels of specified type with pagination
func NewChannelPersistence() record.Store {
	return record.NewStore(record.Environment{

		CachedRead: func(id int) (*record.Channel, error) {
			channel, err := CacheGetChannel(id)
			return (*record.Channel)(channel), err
		},
		CacheStatus:   CacheUpdateChannelStatus,
		StatusLock:    func() func() { channelStatusLock.Lock(); return channelStatusLock.Unlock },
		PollingLock:   func(id int) func() { lock := GetChannelPollingLock(id); lock.Lock(); return lock.Unlock },
		AbilityStatus: UpdateAbilityStatus,

		DB:            func() *gorm.DB { return legacy.DB },
		LockForUpdate: legacy.LockForUpdate,
		GroupColumn:   func() string { return legacy.GroupColumn },
		KeyColumn:     func() string { return legacy.KeyColumn },

		AddAbilities: func(
			channel *record.Channel,
			tx *gorm.DB,
		) error {
			return AddAbilities(
				(*legacy.Channel)(channel),
				tx,
			)
		},
		UpdateAbilities: func(
			channel *record.Channel,
			tx *gorm.DB,
		) error {
			return UpdateAbilities((*legacy.Channel)(channel), tx)
		},
		DeleteAbilities: func(channel *record.Channel) error { return DeleteAbilities((*legacy.Channel)(channel)) },
		DeleteAbilityIDs: func(
			tx *gorm.DB,
			ids []int,
		) error {
			return tx.Where("channel_id in (?)", ids).Delete(&record.Ability{}).Error
		},
		UpdateAbilityStatusByTag: UpdateAbilityStatusByTag,
		UpdateAbilityByTag:       UpdateAbilityByTag,
	})
}

func channelRecords(values []*record.Channel) []*legacy.Channel {
	if values == nil {
		return nil
	}
	channels := make([]*legacy.Channel, len(values))
	for i, value := range values {
		channels[i] = (*legacy.Channel)(value)
	}
	return channels
}
