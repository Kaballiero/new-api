package delete_task_plugin_version

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Store interface {
	DeleteTaskPluginVersion(string, string) (domain.DeleteResult, error)
	GetTaskPluginUsage(string) ([]domain.ChannelRef, int64, error)
	GetTaskPluginVersion(string, string) (*domain.Plugin, error)
}
type Runtime interface {
	Snapshot() domain.RegistrySnapshot
}
type Syncer interface{ Execute(context.Context) error }
type Usecase struct {
	store   Store
	runtime Runtime
	syncer  Syncer
}

func NewUsecase(store Store, runtime Runtime, syncer Syncer) Usecase {
	return Usecase{store: store, runtime: runtime, syncer: syncer}
}

type Input struct {
	Key, VersionQuery string
	Force             bool
}

func (u Usecase) Execute(ctx context.Context, input Input) (any, error) {
	key := input.Key
	version := input.VersionQuery
	plugin, lookupErr := u.store.GetTaskPluginVersion(key, version)
	if lookupErr != nil {
		if errors.Is(lookupErr, domain.ErrNotFound) {
			return nil, domain.ErrDeleteFactory
		}
		return nil, lookupErr
	}
	if plugin.Active && !domain.HasFactory(u.runtime.Snapshot(), key) {
		channels, inFlight, usageErr := u.store.GetTaskPluginUsage(key)
		if usageErr != nil {
			return nil, usageErr
		}
		if (len(channels) > 0 || inFlight > 0) && !input.Force {
			return nil, &domain.InUseError{Channels: channels, InFlight: inFlight}
		}
	}
	_, err := u.store.DeleteTaskPluginVersion(key, version)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrDeleteFactory
		}
		return nil, err
	}
	if err = u.syncer.Execute(ctx); err != nil {
		return nil, err
	}
	return nil, nil
}
