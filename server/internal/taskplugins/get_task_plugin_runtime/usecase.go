package get_task_plugin_runtime

import (
	"context"
	"maps"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Store interface {
	GetTaskPluginSyncSnapshot() (domain.SyncSnapshot, error)
}
type Runtime interface{ RoutingStatus() domain.RoutingStatus }
type Usecase struct {
	store   Store
	runtime Runtime
	state   *domain.SyncState
}

func NewUsecase(store Store, runtime Runtime, state *domain.SyncState) Usecase {
	return Usecase{store: store, runtime: runtime, state: state}
}
func (u Usecase) Execute(ctx context.Context) (any, error) {
	routingStatus := u.runtime.RoutingStatus()
	pluginErrors := routingStatus.Errors

	u.state.Lock()
	maps.Copy(pluginErrors, u.state.Errors)
	lastRebuild := u.state.LastRebuild
	lastDatabaseRevision := lastRebuild.DatabaseRevision
	u.state.Unlock()

	registryRebuild := routingStatus.LastRebuild
	if lastRebuild.AttemptedAt.Before(registryRebuild.AttemptedAt) {
		lastRebuild = domain.RebuildOutcome{
			Status:      registryRebuild.Status,
			AttemptedAt: registryRebuild.AttemptedAt,
			Generation:  registryRebuild.Generation,
			Error:       registryRebuild.Error,
		}
	}
	if lastRebuild.Status == "" {
		lastRebuild.Status = "never"
	}
	lastRebuild.PluginErrorCount = len(pluginErrors)
	if lastRebuild.Status == "success" && len(pluginErrors) > 0 {
		lastRebuild.Status = "partial"
	}

	status := domain.RuntimeStatus{
		DatabaseRevision: lastDatabaseRevision,
		LastRebuild:      lastRebuild,
		PluginErrors:     pluginErrors,
	}
	databaseSnapshot, err := u.store.GetTaskPluginSyncSnapshot()
	if err != nil {
		status.DatabaseError = "database snapshot unavailable"
	} else {
		status.DatabaseRevision = databaseSnapshot.Revision
	}
	if routingStatus.Generation != nil {
		status.CurrentGeneration = routingStatus.Generation.Number
		status.GenerationPublishedAt = routingStatus.Generation.PublishedAt
	}
	return status, nil
}
