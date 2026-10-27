package channels

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	legacy "github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func ToDomain(channel *legacy.Channel) *domain.Channel {
	return record.ToDomain((*record.Channel)(channel))
}
func FromDomain(channel *domain.Channel) *legacy.Channel {
	return (*legacy.Channel)(record.FromDomain(channel))
}

type Store struct {
	record.Codec
	records map[*domain.Channel]*legacy.Channel
}

func NewStore() Store {
	return Store{Codec: record.Codec{Log: common.SysLog}, records: make(map[*domain.Channel]*legacy.Channel)}
}
func (store Store) Get(id int, withKey bool) (*domain.Channel, error) {
	ch, err := GetChannelById(id, withKey)
	return store.track(ch), err
}
func (Store) IsMissing(err error) bool             { return errors.Is(err, gorm.ErrRecordNotFound) }
func (Store) Delete(id int) error                  { return NewChannelPersistence().Delete((&legacy.Channel{Id: id})) }
func (Store) DeleteDisabled() (int64, error)       { return DeleteDisabledChannel() }
func (Store) DeleteBatch(ids []int) (int64, error) { return BatchDeleteChannels(ids) }
func (Store) DisableTag(tag string) error          { return DisableChannelByTag(tag) }
func (Store) EnableTag(tag string) error           { return EnableChannelByTag(tag) }
func (Store) SetTag(ids []int, tag *string) error  { return BatchSetChannelTag(ids, tag) }
func (Store) ByTag(tag string) ([]*domain.Channel, error) {
	channels, err := GetChannelsByTag(tag, false, false)
	var result []*domain.Channel
	if channels != nil {
		result = make([]*domain.Channel, len(channels))
		for i, ch := range channels {
			result[i] = ToDomain(ch)
		}
	}
	return result, err
}
func (Store) RepairAbilities() (int, int, error) { return FixAbility() }
func (store Store) Proxy(channel *domain.Channel) string {
	record := store.MutableRecord(channel)
	proxy := NewChannelPersistence().GetSetting(record).Proxy
	*channel = *ToDomain(record)
	return proxy
}

func (Store) LockPolling(id int) func() {
	lock := GetChannelPollingLock(id)
	lock.Lock()
	return lock.Unlock
}
func (store Store) Keys(channel *domain.Channel) []string {
	record := store.MutableRecord(channel)
	keys := record.GetKeys()
	*channel = *ToDomain(record)
	return keys
}
func (store Store) Update(channel *domain.Channel) error {
	record := store.MutableRecord(channel)
	err := NewChannelPersistence().Update(record)
	*channel = *ToDomain(record)
	return err
}

func (store Store) track(record *legacy.Channel) *domain.Channel {
	channel := ToDomain(record)
	if channel != nil {
		store.records[channel] = record
	}
	return channel
}

// Record preserves the identity of a cache-owned record for request-scoped operations.
func (store Store) Record(channel *domain.Channel) *legacy.Channel {
	if record, ok := store.records[channel]; ok {
		return record
	}
	return FromDomain(channel)
}
func (store Store) Cached(id int) (*domain.Channel, error) {
	record, err := CacheGetChannel(id)
	return store.track(record), err
}

func (store Store) All() ([]*domain.Channel, error) {
	records, err := GetAllChannels(0, 0, true, false)
	if records == nil {
		return nil, err
	}
	result := make([]*domain.Channel, len(records))
	for i, record := range records {
		result[i] = store.track(record)
	}
	return result, err
}
func (store Store) SaveBalance(channel *domain.Channel, balance float64) {
	record := store.Record(channel)
	NewChannelPersistence().UpdateBalance(record, balance)
	*channel = *ToDomain(record)
}

func (store Store) SaveResponseTime(channel *domain.Channel, duration int64) {
	record := store.Record(channel)
	NewChannelPersistence().UpdateResponseTime(record, duration)
}

func (store Store) Domain(record *legacy.Channel) *domain.Channel { return store.track(record) }

// MutableRecord applies an intentional mutation before a persistence operation.
func (store Store) MutableRecord(channel *domain.Channel) *legacy.Channel {
	record := store.Record(channel)
	*record = *FromDomain(channel)
	return record
}

func (Store) RootProbeUserID() (int, error) {
	var user legacy.User
	if err := legacy.DB.Select("id").Where("role = ?", common.RoleRootUser).First(&user).Error; err != nil {
		return 0, fmt.Errorf("failed to resolve channel test user: %w", err)
	}
	if user.Id == 0 {
		return 0, errors.New("failed to resolve channel test user")
	}
	return user.Id, nil
}
