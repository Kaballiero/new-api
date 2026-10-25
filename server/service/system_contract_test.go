package service_test

import (
	"strings"
	"testing"

	f "github.com/QuantumNous/new-api/internal/contracttest/systemruntimefixture"
	"github.com/QuantumNous/new-api/service"
)

func TestSystemRuntimeContract(t *testing.T) {
	names := []string{}
	for _, n := range f.Cases() {
		if strings.HasPrefix(n, "handler_model_cost_fx_") {
			names = append(names, n)
		}
	}
	f.Run(t, "testdata/system", names, f.Hooks{Handlers: scheduledTaskHandlers, FXEndpoint: service.SystemContractFXEndpoint})
}
