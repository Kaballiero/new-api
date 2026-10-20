package model

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/internal/channels/change_channel_state"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"

	"errors"
	"fmt"
	"math/rand"

	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"gorm.io/gorm"
)

type Channel record.Channel
type ChannelInfo = record.ChannelInfo
type ChannelSortOptions = record.ChannelSortOptions

func NewChannelSortOptions(by, order string, id bool) ChannelSortOptions {
	return record.NewChannelSortOptions(by, order, id)
}
func resolveChannelSortOptions(id bool, options []ChannelSortOptions) ChannelSortOptions {
	if len(options) > 0 {
		return options[0]
	}
	return NewChannelSortOptions("", "", id)
}
func NormalizeChannelGroupFilter(group string) string {
	return record.NormalizeChannelGroupFilter(group)
}
func channelGroupFilterCondition() string           { return NewChannelPersistence().GroupFilterCondition() }
func channelGroupFilterPattern(group string) string { return record.GroupFilterPattern(group) }
func ApplyChannelGroupFilter(query *gorm.DB, group string) *gorm.DB {
	return NewChannelPersistence().ApplyChannelGroupFilter(query, group)
}

func (channel *Channel) GetKeys() []string { return ((*record.Channel)(channel)).GetKeys() }

func (channel *Channel) GetNextEnabledKey() (string, int, *types.NewAPIError) {
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
				_ = channel.SaveChannelInfo()
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

func (channel *Channel) SaveChannelInfo() error {
	return NewChannelPersistence().SaveChannelInfo((*record.Channel)(channel))
}

func (channel *Channel) GetModels() []string { return ((*record.Channel)(channel)).GetModels() }

func (channel *Channel) GetGroups() []string { return ((*record.Channel)(channel)).GetGroups() }

func (channel *Channel) GetOtherInfo() map[string]any {
	return record.ToDomain((*record.Channel)(channel)).GetOtherInfo()
}

func (channel *Channel) SetOtherInfo(info map[string]any) {
	value := record.ToDomain((*record.Channel)(channel))
	value.SetOtherInfo(info)
	channel.OtherInfo = value.OtherInfo
}

func (channel *Channel) GetTag() string { return ((*record.Channel)(channel)).GetTag() }

func (channel *Channel) SetTag(tag string) {
	channel.Tag = &tag
}

func (channel *Channel) GetAutoBan() bool {
	if channel.AutoBan == nil {
		return false
	}
	return *channel.AutoBan == 1
}

func (channel *Channel) Save() error { return NewChannelPersistence().Save((*record.Channel)(channel)) }

// saveStatusState persists only the fields owned by the channel status flow.
// Keeping this allowlist here prevents a stale channel snapshot from
// overwriting credentials, accounting counters, or channel configuration.
func (channel *Channel) saveStatusState() error {
	return NewChannelPersistence().SaveStatusState((*record.Channel)(channel))
}

func GetAllChannels(startIdx int, num int, selectAll bool, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	values, err := NewChannelPersistence().GetAllChannels(startIdx, num, selectAll, idSort, sortOptions...)
	return channelRecords(values), err
}

func GetChannelsByTag(tag string, idSort bool, selectAll bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	values, err := NewChannelPersistence().GetChannelsByTag(tag, idSort, selectAll, sortOptions...)
	return channelRecords(values), err
}

func SearchChannels(keyword string, group string, model string, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
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
func GetChannelById(id int, selectAll bool) (*Channel, error) {
	value, err := NewChannelPersistence().GetChannelById(id, selectAll)
	return (*Channel)(value), err
}

func GetChannelByIdForUpdate(tx *gorm.DB, id int) (*Channel, error) {
	value, err := NewChannelPersistence().GetChannelByIdForUpdate(tx, id)
	return (*Channel)(value), err
}

func BatchInsertChannels(channels []Channel) error {
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

func (channel *Channel) GetPriority() int64 { return ((*record.Channel)(channel)).GetPriority() }

func (channel *Channel) GetWeight() int { return ((*record.Channel)(channel)).GetWeight() }

func (channel *Channel) GetBaseURL() string { return ((*record.Channel)(channel)).GetBaseURL() }

func (channel *Channel) GetModelMapping() string {
	return ((*record.Channel)(channel)).GetModelMapping()
}

func (channel *Channel) GetStatusCodeMapping() string {
	return ((*record.Channel)(channel)).GetStatusCodeMapping()
}

func (channel *Channel) Insert() error {
	return NewChannelPersistence().Insert((*record.Channel)(channel))
}

func (channel *Channel) Update() error {
	return NewChannelPersistence().Update((*record.Channel)(channel))
}

func (channel *Channel) UpdateWithTx(tx *gorm.DB) error {
	return NewChannelPersistence().UpdateWithTx((*record.Channel)(channel), tx)
}

func (channel *Channel) UpdateResponseTime(responseTime int64) {
	NewChannelPersistence().UpdateResponseTime((*record.Channel)(channel), responseTime)
}

func (channel *Channel) UpdateBalance(balance float64) {
	NewChannelPersistence().UpdateBalance((*record.Channel)(channel), balance)
}

func (channel *Channel) Delete() error {
	return NewChannelPersistence().Delete((*record.Channel)(channel))
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
func CleanupChannelPollingLocks() {
	var activeChannelIds []int
	DB.Model(&Channel{}).Pluck("id", &activeChannelIds)

	activeChannelSet := make(map[int]bool)
	for _, id := range activeChannelIds {
		activeChannelSet[id] = true
	}

	channelPollingLocks.Range(func(key, value any) bool {
		channelId := key.(int)
		if !activeChannelSet[channelId] {
			channelPollingLocks.Delete(channelId)
		}
		return true
	})
}

func handlerMultiKeyUpdate(channel *Channel, key string, status int, reason string) {
	value := record.ToDomain((*record.Channel)(channel))
	channeldomain.ApplyMultiKeyState(value, channel.GetKeys(), key, status, reason, common.GetTimestamp, common.SysLog)
	*channel = Channel(*record.FromDomain(value))
}

func hasEnabledMultiKey(keys []string, status map[int]int) bool {
	return channeldomain.HasEnabledMultiKey(keys, status)
}

func UpdateChannelStatus(id int, key string, status int, reason string) bool {
	return change_channel_state.NewUsecase(NewChannelPersistence(), func() bool { return common.MemoryCacheEnabled }, common.GetTimestamp, common.SysLog).Execute(id, key, status, reason)
}

func EnableChannelByTag(tag string) error { return NewChannelPersistence().EnableChannelByTag(tag) }

func DisableChannelByTag(tag string) error { return NewChannelPersistence().DisableChannelByTag(tag) }

func EditChannelByTag(tag string, newTag *string, modelMapping *string, models *string, group *string, priority *int64, weight *uint, paramOverride *string, headerOverride *string) error {
	return NewChannelPersistence().EditChannelByTag(tag, newTag, modelMapping, models, group, priority, weight, paramOverride, headerOverride)
}

func UpdateChannelUsedQuota(id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeChannelUsedQuota, id, quota)
		return
	}
	updateChannelUsedQuota(id, quota)
}

func updateChannelUsedQuota(id int, quota int) {
	err := DB.Model(&Channel{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update channel used quota: channel_id=%d, delta_quota=%d, error=%v", id, quota, err))
	}
}

func DeleteChannelByStatus(status int64) (int64, error) {
	return NewChannelPersistence().DeleteChannelByStatus(status)
}

func DeleteDisabledChannel() (int64, error) { return NewChannelPersistence().DeleteDisabledChannel() }

func GetPaginatedTags(offset int, limit int) ([]*string, error) {
	return NewChannelPersistence().GetPaginatedTags(offset, limit)
}

func GetPaginatedChannelTags(query *gorm.DB, offset int, limit int) ([]*string, error) {
	return NewChannelPersistence().GetPaginatedChannelTags(query, offset, limit)
}

func SearchTags(keyword string, group string, model string, idSort bool) ([]*string, error) {
	return NewChannelPersistence().SearchTags(keyword, group, model, idSort)
}

func (channel *Channel) ValidateSettings() error {
	return record.ToDomain((*record.Channel)(channel)).ValidateSettings()
}

func (channel *Channel) GetSetting() dto.ChannelSettings {
	return NewChannelPersistence().GetSetting((*record.Channel)(channel))
}

func (channel *Channel) SetSetting(setting dto.ChannelSettings) {
	((*record.Channel)(channel)).SetSetting(setting)
}

func (channel *Channel) GetOtherSettings() dto.ChannelOtherSettings {
	return NewChannelPersistence().GetOtherSettings((*record.Channel)(channel))
}

func (channel *Channel) SetOtherSettings(setting dto.ChannelOtherSettings) {
	((*record.Channel)(channel)).SetOtherSettings(setting)
}

func (channel *Channel) GetParamOverride() map[string]any {
	return ((*record.Channel)(channel)).GetParamOverride()
}

func (channel *Channel) GetHeaderOverride() map[string]any {
	return ((*record.Channel)(channel)).GetHeaderOverride()
}

func GetChannelsByIds(ids []int) ([]*Channel, error) {
	values, err := NewChannelPersistence().GetChannelsByIds(ids)
	return channelRecords(values), err
}

func BatchSetChannelTag(ids []int, tag *string) error {
	return NewChannelPersistence().BatchSetChannelTag(ids, tag)
}

// CountAllChannels returns total channels in DB
func CountAllChannels() (int64, error) { return NewChannelPersistence().CountAllChannels() }

// CountAllTags returns number of non-empty distinct tags
func CountAllTags() (int64, error) { return NewChannelPersistence().CountAllTags() }

func CountChannelTags(query *gorm.DB) (int64, error) {
	return NewChannelPersistence().CountChannelTags(query)
}

// Get channels of specified type with pagination
func GetChannelsByType(startIdx int, num int, idSort bool, channelType int) ([]*Channel, error) {
	values, err := NewChannelPersistence().GetChannelsByType(startIdx, num, idSort, channelType)
	return channelRecords(values), err
}

// Count channels of specific type
func CountChannelsByType(channelType int) (int64, error) {
	return NewChannelPersistence().CountChannelsByType(channelType)
}

// Return map[type]count for all channels
func CountChannelsGroupByType() (map[int64]int64, error) {
	return NewChannelPersistence().CountChannelsGroupByType()
}

// NewChannelPersistence binds legacy database/cache owners without hidden initialization.
func NewChannelPersistence() record.Store {
	return record.NewStore(record.Environment{
		CachedRead: func(id int) (*record.Channel, error) {
			channel, err := CacheGetChannel(id)
			return (*record.Channel)(channel), err
		}, CacheStatus: CacheUpdateChannelStatus, StatusLock: func() func() { channelStatusLock.Lock(); return channelStatusLock.Unlock }, PollingLock: func(id int) func() { lock := GetChannelPollingLock(id); lock.Lock(); return lock.Unlock }, AbilityStatus: UpdateAbilityStatus,
		DB: func() *gorm.DB { return DB }, LockForUpdate: lockForUpdate, GroupColumn: func() string { return commonGroupCol }, KeyColumn: func() string { return commonKeyCol },
		AddAbilities: func(channel *record.Channel, tx *gorm.DB) error { return (*Channel)(channel).AddAbilities(tx) }, UpdateAbilities: func(channel *record.Channel, tx *gorm.DB) error { return (*Channel)(channel).UpdateAbilities(tx) }, DeleteAbilities: func(channel *record.Channel) error { return (*Channel)(channel).DeleteAbilities() }, DeleteAbilityIDs: func(tx *gorm.DB, ids []int) error { return tx.Where("channel_id in (?)", ids).Delete(&Ability{}).Error }, UpdateAbilityStatusByTag: UpdateAbilityStatusByTag, UpdateAbilityByTag: UpdateAbilityByTag,
	})
}
func channelRecords(values []*record.Channel) []*Channel {
	if values == nil {
		return nil
	}
	channels := make([]*Channel, len(values))
	for i, value := range values {
		channels[i] = (*Channel)(value)
	}
	return channels
}
