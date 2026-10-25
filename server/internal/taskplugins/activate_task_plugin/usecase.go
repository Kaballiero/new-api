package activate_task_plugin

import (
	"context"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Store interface {
	ActivateTaskPlugin(string, string) error
	ListTaskPluginVersions(string) ([]domain.Plugin, error)
}
type Runtime interface {
	Register(string, domain.Options) (domain.Compiled, error)
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
	Version string
	Key     string
}

func (u Usecase) Execute(ctx context.Context, input Input) (any, error) {

	versions, err := u.store.ListTaskPluginVersions(input.Key)
	if err != nil {
		return nil, err
	}
	var target *domain.Plugin
	for i := range versions {
		if versions[i].Version == input.Version {
			target = &versions[i]
			break
		}
	}
	if target == nil {
		return nil, domain.ErrVersionNotFound
	}
	if _, err = u.runtime.Register(target.Source, domain.Options{Key: target.Key, Version: target.Version}); err != nil {
		return nil, err
	}
	if err = u.store.ActivateTaskPlugin(target.Key, target.Version); err != nil {
		return nil, err
	}
	if err = u.syncer.Execute(ctx); err != nil {
		return nil, err
	}
	return nil, nil
}
