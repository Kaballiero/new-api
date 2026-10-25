package service_test

import (
	fixture "github.com/QuantumNous/new-api/internal/contracttest/batchtickfixture"
	"github.com/QuantumNous/new-api/service"
	"testing"
)

func TestBatchTickContract(t *testing.T) {
	fixture.Run(t, "scheduler", "testdata/batch", fixture.Hooks{
		Handlers: service.BatchContractHandlers, Registry: service.BatchContractRegistry, Schedule: service.BatchContractSchedule,
	})
}
