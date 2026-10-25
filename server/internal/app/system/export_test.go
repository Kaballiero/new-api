package system

import (
	"context"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/internal/system/purge_old_logs"

	"github.com/QuantumNous/new-api/internal/system/domain"
)

// These private seams are compiled only into the owning contract test binary.

func SystemContractRegistry(t *testing.T, handlers ...domain.SystemTaskHandler) {
	withSystemTaskRegistry(t, handlers...)
}
func SystemContractSchedule()           { _ = Default().Schedule.Execute() }
func SystemContractClaim(runner string) { _ = Default().Claim.Execute(runner) }
func SystemContractHeartbeat(task *domain.SystemTask, runner string, run func(context.Context)) {
	runWithLeaseHeartbeat(task, runner, run)
}
func SystemContractCleanup(ctx context.Context, task *domain.SystemTask, runner string) {
	_ = Default().Cleanup.Execute(ctx, purge_old_logs.Input{Task: task, RunnerID: runner})
}
func SystemContractResetRunner() {
	systemTaskRunnerOnce = sync.Once{}
	systemTaskWakeup = make(chan struct{}, 1)
}

func BatchContractRegistry(t *testing.T, handler domain.SystemTaskHandler) {
	withSystemTaskRegistry(t, handler)
}

func Handlers() []domain.SystemTaskHandler { return handlers() }
