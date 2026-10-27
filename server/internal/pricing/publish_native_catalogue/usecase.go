package publish_native_catalogue

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"maps"
	"slices"
)

type Transaction interface {
	ReadLockedOption() (string, error)
	LockChannel(int) (*domain.BuildChannel, error)
	SaveOption(string) error
}
type Store interface {
	Lock() func()
	Snapshot() *domain.PricingCatalogSnapshot
	Transaction(context.Context, func(Transaction) error) error
	PublishOption(string) error
}
type Usecase struct {
	deps  domain.Capabilities
	store Store
}

func NewUsecase(deps domain.Capabilities, store Store) Usecase {
	return Usecase{deps: deps, store: store}
}
func (u Usecase) Execute(ctx context.Context, entries []domain.PricingCatalogEntry, allowChangedSources bool) (int, error) {
	unlock := u.store.Lock()
	defer unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	snapshot := domain.PricingCatalogSnapshot{Version: 1, Entries: make(map[string]domain.PricingCatalogEntry)}
	if current := u.store.Snapshot(); current != nil {
		snapshot.Entries = maps.Clone(current.Entries)
	}
	var data []byte
	published := 0
	err := u.store.Transaction(ctx, func(tx Transaction) error {
		value, err := tx.ReadLockedOption()
		if err != nil {
			return err
		}

		if current, err := domain.DecodePricingCatalog(u.deps, value); err == nil {
			maps.Copy(snapshot.Entries, current.Entries)
		}
		ids := map[int]bool{}
		for _, entry := range snapshot.Entries {
			ids[entry.ChannelID] = true
		}
		for _, entry := range entries {
			ids[entry.ChannelID] = true
		}
		channels := make(map[int]*domain.BuildChannel)
		for _, id := range slices.Sorted(maps.Keys(ids)) {
			channel, err := tx.LockChannel(id)
			if err != nil {
				return err
			}
			if channel == nil {
				continue
			}
			channels[id] = channel
		}
		for key, entry := range snapshot.Entries {
			channel := channels[entry.ChannelID]
			if channel == nil || channel.Status != constant.ChannelStatusEnabled || !domain.PricingCatalogSourceMatches(u.deps, entry, channel) {
				delete(snapshot.Entries, key)
			}
		}
		for _, entry := range entries {
			channel := channels[entry.ChannelID]
			if channel != nil && channel.Status == constant.ChannelStatusEnabled && domain.PricingCatalogSourceMatches(u.deps, entry, channel) {
				snapshot.Entries[domain.PricingCatalogEntryKey(entry)] = entry
				published++
			} else if !allowChangedSources {
				return fmt.Errorf("pricing catalog source changed before publication")
			}
		}

		data, err = u.deps.Codec.Marshal(snapshot)
		if err != nil {
			return err
		}
		if _, err = domain.DecodePricingCatalog(u.deps, string(data)); err != nil {
			return err
		}
		return tx.SaveOption(string(data))
	})
	if err != nil {
		return 0, err
	}
	return published, u.store.PublishOption(string(data))
}
