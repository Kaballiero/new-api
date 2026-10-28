package get_task_plugin

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Store interface {
	GetTaskPluginVersion(string, string) (*domain.Plugin, error)
}
type Runtime interface {
	Register(string, domain.Options) (domain.Compiled, error)
	RegisterFactory(string, domain.Options) (domain.Compiled, error)
}
type Settings interface {
	FactoryIcon(string) (string, []byte, bool)
	FactorySource(string) (string, error)
}
type Usecase struct {
	store    Store
	runtime  Runtime
	settings Settings
}

func NewUsecase(store Store, runtime Runtime, settings Settings) Usecase {
	return Usecase{store: store, runtime: runtime, settings: settings}
}

type Input struct {
	Key, VersionQuery string
}

func (u Usecase) Execute(ctx context.Context, input Input) (any, error) {
	key := input.Key
	version := input.VersionQuery
	plugin, err := u.store.GetTaskPluginVersion(key, version)
	if err == nil {
		loaded, compileErr := u.runtime.Register(plugin.Source, domain.Options{Key: plugin.Key, Version: plugin.Version})
		if compileErr != nil {
			return nil, compileErr
		}
		return domain.Detail{Plugin: plugin, Meta: loaded.Metadata(), Source: plugin.Source, Layer: "override", HasIcon: plugin.Icon != ""}, nil
	}
	if !errors.Is(err, domain.ErrNotFound) || version != "" {
		return nil, err
	}
	source, err := u.settings.FactorySource(key)
	if err != nil {
		return nil, domain.ErrPluginNotFound
	}
	loaded, err := u.runtime.RegisterFactory(source, domain.Options{Key: key})
	if err != nil {
		return nil, err
	}
	_, _, hasIcon := u.settings.FactoryIcon(key)
	return domain.Detail{Meta: loaded.Metadata(), Source: source, Layer: "factory", HasIcon: hasIcon}, nil
}
