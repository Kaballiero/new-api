package pricingfx_test

import (
	"strings"
	"testing"

	pricingfx "github.com/QuantumNous/new-api/internal/app/pricingfx"
	f "github.com/QuantumNous/new-api/internal/contracttest/systemruntimefixture"
)

func TestSystemRuntimeContract(t *testing.T) {
	names := []string{}
	for _, n := range f.Cases() {
		if strings.HasPrefix(n, "handler_model_cost_fx_") {
			names = append(names, n)
		}
	}
	f.Run(t, "../../../service/testdata/system", names, f.Hooks{Handlers: scheduledTaskHandlers, FXEndpoint: pricingfx.SystemContractFXEndpoint})
}
