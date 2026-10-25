package system_test

import (
	"testing"

	core "github.com/QuantumNous/new-api/internal/app/system"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/batchtickfixture"
)

func TestBatchTickContract(t *testing.T) {
	fixture.Run(t, "scheduler", "../../../service/testdata/batch", fixture.Hooks{
		Handlers: core.Handlers, Registry: core.BatchContractRegistry, Schedule: core.SystemContractSchedule,
	})
}
