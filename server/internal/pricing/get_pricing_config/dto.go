package get_pricing_config

import (
	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Input struct{ Models []string }
type Output = domain.ModelPricingSnapshot
