package channels

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func ToDomain(channel *model.Channel) *domain.Channel {
	return record.ToDomain((*record.Channel)(channel))
}
func FromDomain(channel *domain.Channel) *model.Channel {
	return (*model.Channel)(record.FromDomain(channel))
}

type Store struct {
	record.Codec
	records map[*domain.Channel]*model.Channel
}

func NewStore() Store {
	return Store{Codec: record.Codec{Log: common.SysLog}, records: make(map[*domain.Channel]*model.Channel)}
}
func (store Store) Get(id int, withKey bool) (*domain.Channel, error) {
	ch, err := model.GetChannelById(id, withKey)
	return store.track(ch), err
}
func (Store) IsMissing(err error) bool             { return errors.Is(err, gorm.ErrRecordNotFound) }
func (Store) Delete(id int) error                  { return (&model.Channel{Id: id}).Delete() }
func (Store) DeleteDisabled() (int64, error)       { return model.DeleteDisabledChannel() }
func (Store) DeleteBatch(ids []int) (int64, error) { return model.BatchDeleteChannels(ids) }
func (Store) ChangeStatus(id int, status int, reason string) bool {
	return model.UpdateChannelStatus(id, "", status, reason)
}
func (Store) DisableTag(tag string) error         { return model.DisableChannelByTag(tag) }
func (Store) EnableTag(tag string) error          { return model.EnableChannelByTag(tag) }
func (Store) SetTag(ids []int, tag *string) error { return model.BatchSetChannelTag(ids, tag) }
func (Store) ByTag(tag string) ([]*domain.Channel, error) {
	channels, err := model.GetChannelsByTag(tag, false, false)
	var result []*domain.Channel
	if channels != nil {
		result = make([]*domain.Channel, len(channels))
		for i, ch := range channels {
			result[i] = ToDomain(ch)
		}
	}
	return result, err
}
func (Store) RepairAbilities() (int, int, error) { return model.FixAbility() }
func (store Store) Proxy(channel *domain.Channel) string {
	record := store.MutableRecord(channel)
	proxy := record.GetSetting().Proxy
	*channel = *ToDomain(record)
	return proxy
}

func (Store) LockPolling(id int) func() {
	lock := model.GetChannelPollingLock(id)
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
	err := record.Update()
	*channel = *ToDomain(record)
	return err
}

func (store Store) track(record *model.Channel) *domain.Channel {
	channel := ToDomain(record)
	if channel != nil {
		store.records[channel] = record
	}
	return channel
}

// Record preserves the identity of a cache-owned record for request-scoped operations.
func (store Store) Record(channel *domain.Channel) *model.Channel {
	if record, ok := store.records[channel]; ok {
		return record
	}
	return FromDomain(channel)
}
func (store Store) Cached(id int) (*domain.Channel, error) {
	record, err := model.CacheGetChannel(id)
	return store.track(record), err
}

func (store Store) All() ([]*domain.Channel, error) {
	records, err := model.GetAllChannels(0, 0, true, false)
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
	record.UpdateBalance(balance)
	*channel = *ToDomain(record)
}

func (store Store) SaveResponseTime(channel *domain.Channel, duration int64) {
	record := store.Record(channel)
	record.UpdateResponseTime(duration)
}

func (store Store) Domain(record *model.Channel) *domain.Channel { return store.track(record) }

// MutableRecord applies an intentional mutation before a persistence operation.
func (store Store) MutableRecord(channel *domain.Channel) *model.Channel {
	record := store.Record(channel)
	*record = *FromDomain(channel)
	return record
}

func (Store) RootProbeUserID() (int, error) {
	var user model.User
	if err := model.DB.Select("id").Where("role = ?", common.RoleRootUser).First(&user).Error; err != nil {
		return 0, fmt.Errorf("failed to resolve channel test user: %w", err)
	}
	if user.Id == 0 {
		return 0, errors.New("failed to resolve channel test user")
	}
	return user.Id, nil
}
