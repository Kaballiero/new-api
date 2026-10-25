package service

import (
	"context"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/model"
)

// These private seams are compiled only into the owning contract test binary.
func SystemContractHandlers() []SystemTaskHandler { return registeredSystemTaskHandlers() }
func SystemContractRegistry(t *testing.T, handlers ...SystemTaskHandler) {
	withSystemTaskRegistry(t, handlers...)
}
func SystemContractSchedule()           { runSystemTaskScheduler() }
func SystemContractClaim(runner string) { runSystemTaskClaimPass(runner) }
func SystemContractHeartbeat(task *model.SystemTask, runner string, run func(context.Context)) {
	runWithLeaseHeartbeat(task, runner, run)
}
func SystemContractCleanup(ctx context.Context, task *model.SystemTask, runner string) {
	runLogCleanupTask(ctx, task, runner)
}
func SystemContractResetRunner() {
	systemTaskRunnerOnce = sync.Once{}
	systemTaskWakeup = make(chan struct{}, 1)
}
func SystemContractFXEndpoint(t *testing.T, endpoint string) {
	old := modelCostFXEndpoint
	modelCostFXEndpoint = endpoint
	t.Cleanup(func() { modelCostFXEndpoint = old })
}
