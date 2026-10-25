package system_test

import (
	"strings"
	"testing"

	core "github.com/QuantumNous/new-api/internal/app/system"
	f "github.com/QuantumNous/new-api/internal/contracttest/systemruntimefixture"
)

func TestSystemRuntimeContract(t *testing.T) {
	names := []string{}
	for _, n := range f.Cases() {
		if !strings.HasPrefix(n, "handler_model_cost_fx_") {
			names = append(names, n)
		}
	}
	f.Run(t, "../../../service/testdata/system", names, f.Hooks{Handlers: core.Handlers, Registry: core.SystemContractRegistry, Schedule: core.SystemContractSchedule, Claim: core.SystemContractClaim, Heartbeat: core.SystemContractHeartbeat, Cleanup: core.SystemContractCleanup, ResetRunner: core.SystemContractResetRunner})
}
