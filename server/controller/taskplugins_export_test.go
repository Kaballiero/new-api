package controller

import (
	"context"
	"fmt"
	"testing"

	pluginsapp "github.com/QuantumNous/new-api/internal/app/taskplugins"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/stretchr/testify/require"
)

var taskPluginSyncState = pluginsapp.Default().State

func syncTaskPluginsOnceContext(ctx context.Context) error {
	return pluginsapp.Default().Sync.Execute(ctx)
}
func syncTaskPluginsOnce() error { return syncTaskPluginsOnceContext(context.Background()) }

// observationCount probes the global cap with distinct identities, then returns every probe slot.
func observationCount(t *testing.T, l *domain.ObservationLimiter, globalLimit int) int {
	t.Helper()
	releases := []func(){}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	for i := range globalLimit + 1 {
		release, err := l.Acquire(fmt.Sprintf("contract-capacity-%d", i), 10000+i, 10000+i)
		if err != nil {
			var limit *domain.ObservationLimitError
			require.ErrorAs(t, err, &limit)
			require.Equal(t, "global", limit.Scope)
			return globalLimit - len(releases)
		}
		releases = append(releases, release)
	}
	t.Fatal("global observation cap was not enforced")
	return 0
}

func SyncTaskPluginsOnce() { pluginsapp.SyncOnce() }
