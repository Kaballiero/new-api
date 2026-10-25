package sync_task_plugins

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Store interface {
	GetTaskPluginSyncSnapshot() (domain.SyncSnapshot, error)
}
type Runtime interface {
	ActiveCount() int
	CompilePlugin(string, domain.Options) (domain.Compiled, error)
	Generation() domain.Generation
	OverridePlugins() map[string]domain.Compiled
	ReplaceOverrides([]domain.Compiled) error
	RoutingErrors() map[string]string
}
type Settings interface{ InvalidatePricing() }
type Diagnostics interface {
	Debug(context.Context, string, ...any)
	Error(string)
	Now() time.Time
}
type Usecase struct {
	store       Store
	runtime     Runtime
	settings    Settings
	state       *domain.SyncState
	diagnostics Diagnostics
}

func NewUsecase(store Store, runtime Runtime, settings Settings, state *domain.SyncState, diagnostics Diagnostics) Usecase {
	return Usecase{store: store, runtime: runtime, settings: settings, state: state, diagnostics: diagnostics}
}
func (u Usecase) Execute(ctx context.Context) error {
	started := u.diagnostics.Now()
	u.state.Lock()
	defer u.state.Unlock()
	databaseSnapshot, err := u.store.GetTaskPluginSyncSnapshot()
	if err != nil {
		syncErr := fmt.Errorf("sync task plugins: %w", err)
		u.state.LastRebuild = domain.RebuildOutcome{
			Status:           "failed",
			AttemptedAt:      u.diagnostics.Now(),
			Generation:       u.runtime.Generation().Number,
			DatabaseRevision: u.state.LastRebuild.DatabaseRevision,
			Error:            syncErr.Error(),
		}
		u.diagnostics.Debug(
			ctx,
			"task_plugin subsystem=sync event=failed stage=database_snapshot retained_generation=%d elapsed_ms=%d",
			u.runtime.Generation().Number,
			u.diagnostics.Now().Sub(started).Milliseconds(),
		)
		return syncErr
	}
	databasePlugins := databaseSnapshot.Plugins
	sort.Slice(databasePlugins, func(i, j int) bool { return databasePlugins[i].Key < databasePlugins[j].Key })
	currentOverrides := u.runtime.OverridePlugins()
	generationBefore := u.runtime.Generation().Number
	u.diagnostics.Debug(
		ctx,
		"task_plugin subsystem=sync event=start database_revision=%q generation=%d desired_plugins=%d current_overrides=%d",
		databaseSnapshot.Revision,
		generationBefore,
		len(databasePlugins),
		len(currentOverrides),
	)
	nextOverrides := make([]domain.Compiled, 0, len(databasePlugins))
	nextHashes := make(map[string]string, len(databasePlugins))
	seen := make(map[string]bool, len(databasePlugins))
	for _, plugin := range databasePlugins {
		seen[plugin.Key] = true
		if current := currentOverrides[plugin.Key]; current != nil && u.state.Hashes[plugin.Key] == plugin.SourceHash {
			nextOverrides = append(nextOverrides, current)
			nextHashes[plugin.Key] = plugin.SourceHash
			u.diagnostics.Debug(
				ctx,
				"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=reuse",
				plugin.Key,
				plugin.Version,
			)
			continue
		}
		u.diagnostics.Debug(
			ctx,
			"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=compile_start",
			plugin.Key,
			plugin.Version,
		)
		compiled, compileErr := u.runtime.CompilePlugin(plugin.Source, domain.Options{Key: plugin.Key, Version: plugin.Version})
		if compileErr != nil {
			retainedIncumbent := false
			if current := currentOverrides[plugin.Key]; current != nil {
				nextOverrides = append(nextOverrides, current)
				retainedIncumbent = true
				if currentHash := u.state.Hashes[plugin.Key]; currentHash != "" {
					nextHashes[plugin.Key] = currentHash
				}
			}
			u.state.Errors[plugin.Key] = compileErr.Error()
			u.diagnostics.Error(fmt.Sprintf("compile task plugin %s@%s: %v", plugin.Key, plugin.Version, compileErr))
			u.diagnostics.Debug(
				ctx,
				"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=compile_failed retained_incumbent=%t",
				plugin.Key,
				plugin.Version,
				retainedIncumbent,
			)
			continue
		}
		nextOverrides = append(nextOverrides, compiled)
		nextHashes[plugin.Key] = plugin.SourceHash
		delete(u.state.Errors, plugin.Key)
		u.diagnostics.Debug(
			ctx,
			"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=compile_success",
			plugin.Key,
			plugin.Version,
		)
	}
	previousGeneration := u.runtime.Generation().Number
	if err = u.runtime.ReplaceOverrides(nextOverrides); err != nil {
		syncErr := fmt.Errorf("publish task plugin generation: %w", err)
		u.state.LastRebuild = domain.RebuildOutcome{
			Status:           "failed",
			AttemptedAt:      u.diagnostics.Now(),
			Generation:       u.runtime.Generation().Number,
			DatabaseRevision: databaseSnapshot.Revision,
			Error:            syncErr.Error(),
		}
		u.diagnostics.Debug(
			ctx,
			"task_plugin subsystem=sync event=failed stage=publish retained_generation=%d retained_generation_active=true database_revision=%q elapsed_ms=%d",
			u.runtime.Generation().Number,
			databaseSnapshot.Revision,
			u.diagnostics.Now().Sub(started).Milliseconds(),
		)
		return syncErr
	}
	if previousGeneration != u.runtime.Generation().Number {
		u.settings.InvalidatePricing()
	}
	u.state.Hashes = nextHashes
	for key := range u.state.Errors {
		if !seen[key] {
			delete(u.state.Errors, key)
		}
	}
	pluginErrors := u.runtime.RoutingErrors()
	maps.Copy(pluginErrors, u.state.Errors)
	pluginErrorCount := len(pluginErrors)
	status := "success"
	if pluginErrorCount > 0 {
		status = "partial"
	}
	u.state.LastRebuild = domain.RebuildOutcome{
		Status:           status,
		AttemptedAt:      u.diagnostics.Now(),
		Generation:       u.runtime.Generation().Number,
		DatabaseRevision: databaseSnapshot.Revision,
		PluginErrorCount: pluginErrorCount,
	}
	u.diagnostics.Debug(
		ctx,
		"task_plugin subsystem=sync event=complete database_revision=%q previous_generation=%d "+
			"generation=%d status=%q active_overrides=%d plugin_errors=%d elapsed_ms=%d",
		databaseSnapshot.Revision,
		generationBefore,
		u.runtime.Generation().Number,
		status,
		u.runtime.ActiveCount(),
		pluginErrorCount,
		u.diagnostics.Now().Sub(started).Milliseconds(),
	)
	return nil
}
