package list_task_plugins

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"sort"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Store interface {
	GetTaskPluginUsage(string) ([]domain.ChannelRef, int64, error)
	ListTaskPlugins() ([]domain.Plugin, error)
}
type Runtime interface {
	Register(string, domain.Options) (domain.Compiled, error)
	RoutingErrors() map[string]string
	Snapshot() domain.RegistrySnapshot
}
type Settings interface {
	FactoryDisabled(string) bool
	FactoryIcon(string) (string, []byte, bool)
	FactorySource(string) (string, error)
}
type Usecase struct {
	store    Store
	runtime  Runtime
	settings Settings
	state    *domain.SyncState
}

func NewUsecase(store Store, runtime Runtime, settings Settings, state *domain.SyncState) Usecase {
	return Usecase{store: store, runtime: runtime, settings: settings, state: state}
}
func (u Usecase) Execute(ctx context.Context) (any, error) {
	databasePlugins, err := u.store.ListTaskPlugins()
	if err != nil {
		return nil, err
	}
	snapshot := u.runtime.Snapshot()
	factory := make(map[string]domain.Meta, len(snapshot.Factory))
	override := make(map[string]domain.Meta, len(snapshot.Override))
	for _, meta := range snapshot.Factory {
		factory[meta.Key] = meta
	}
	for _, meta := range snapshot.Override {
		override[meta.Key] = meta
	}
	activeRows := make(map[string]domain.Plugin)
	keys := make(map[string]struct{}, len(factory)+len(databasePlugins))
	for key := range factory {
		keys[key] = struct{}{}
	}
	for _, plugin := range databasePlugins {
		keys[plugin.Key] = struct{}{}
		if plugin.Active {
			activeRows[plugin.Key] = plugin
		}
	}

	runtimeErrors := u.runtime.RoutingErrors()
	u.state.Lock()
	maps.Copy(runtimeErrors, u.state.Errors)
	u.state.Unlock()

	items := make([]domain.ListItem, 0, len(keys))
	for key := range keys {
		factoryMeta, hasFactory := factory[key]
		row, hasOverride := activeRows[key]
		item := domain.ListItem{Enabled: true, Active: true, RuntimeStatus: "registered"}
		if hasOverride {
			item.Source = "override"
			if hasFactory {
				item.Source = "override_over_factory"
				factoryCopy := factoryMeta
				item.FactoryMeta = &factoryCopy
			}
			item.Meta = domain.Meta{Key: row.Key, Version: row.Version, APIVersion: row.APIVersion}
			if compiled, compileErr := u.runtime.Register(row.Source, domain.Options{Key: row.Key, Version: row.Version}); compileErr == nil {
				item.Meta = compiled.Metadata()
			}
			item.Enabled = row.Enabled
			item.Active = row.Active
			item.SourceHash = row.SourceHash
			item.HasIcon = row.Icon != ""
			item.Remark = row.Remark
			if message := runtimeErrors[key]; message != "" {
				item.RuntimeStatus = "compile_failed"
				item.RuntimeError = message
			} else if runtimeMeta, ok := override[key]; ok {
				item.Meta = runtimeMeta
			} else if !row.Enabled {
				item.RuntimeStatus = "disabled_fallback"
			} else {
				item.RuntimeStatus = "not_registered"
			}
			// "disabled_fallback" promises that the built-in still serves. When
			// the factory layer is suppressed as well, nothing serves this key.
			if item.RuntimeStatus == "disabled_fallback" && hasFactory && u.settings.FactoryDisabled(key) {
				item.RuntimeStatus = "disabled"
			}
		} else {
			item.Source = "factory"
			item.Meta = factoryMeta
			item.Enabled = !u.settings.FactoryDisabled(key)
			_, _, item.HasIcon = u.settings.FactoryIcon(key)
			source, sourceErr := u.settings.FactorySource(key)
			if sourceErr == nil {
				item.SourceHash = fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
			}
			if !item.Enabled {
				item.RuntimeStatus = "disabled"
			} else if message := runtimeErrors[key]; message != "" {
				item.RuntimeStatus = "compile_failed"
				item.RuntimeError = message
			}
		}
		if !hasFactory {
			channels, inFlight, usageErr := u.store.GetTaskPluginUsage(key)
			if usageErr != nil {
				return nil, usageErr
			}
			item.ChannelCount = len(channels)
			item.InFlightCount = inFlight
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Meta.SortPriority != items[j].Meta.SortPriority {
			return items[i].Meta.SortPriority > items[j].Meta.SortPriority
		}
		return items[i].Meta.Key < items[j].Meta.Key
	})
	return items, nil
}
