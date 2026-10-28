package update_pricing_config

import "github.com/QuantumNous/new-api/internal/pricing/domain"

type Request struct {
	Changes []domain.ModelPricingChange `json:"changes"`
}
type Output struct {
	UpdatedModels []string `json:"updated_models"`
}
