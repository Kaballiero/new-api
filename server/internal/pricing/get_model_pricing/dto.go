package get_model_pricing

import (
	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Input struct {
	UserID                 int
	Name, ExpectedRevision string
}
type Output = domain.ConsumerPricingModelDetail
