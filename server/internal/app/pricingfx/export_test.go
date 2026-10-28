package pricingfx

import (
	"testing"

	pricingdomainfx "github.com/QuantumNous/new-api/internal/pricing/domain"
)

func SystemContractFXEndpoint(t *testing.T, endpoint string) {
	old := modelCostFXEndpoint
	modelCostFXEndpoint = endpoint
	t.Cleanup(func() { modelCostFXEndpoint = old })
}
func ModelCostFXTestDialect(t *testing.T)  { withModelCostFXServiceDialect(t) }
func ModelCostFXTestTruncate(t *testing.T) { truncateModelCostFXService(t) }
func ModelCostFXTestSeed(t *testing.T) pricingdomainfx.ModelCostFXSnapshot {
	return seedModelCostFXServiceTest(t)
}
