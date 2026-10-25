package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func SystemContractFXEndpoint(t *testing.T, endpoint string) {
	old := modelCostFXEndpoint
	modelCostFXEndpoint = endpoint
	t.Cleanup(func() { modelCostFXEndpoint = old })
}
func ModelCostFXTestDialect(t *testing.T)  { withModelCostFXServiceDialect(t) }
func ModelCostFXTestTruncate(t *testing.T) { truncateModelCostFXService(t) }
func ModelCostFXTestSeed(t *testing.T) model.ModelCostFXSnapshot {
	return seedModelCostFXServiceTest(t)
}
