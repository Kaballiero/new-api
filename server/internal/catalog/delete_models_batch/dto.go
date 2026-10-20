package delete_models_batch

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Input struct {
	ModelIDs           []int `json:"model_ids"`
	RemoveFromChannels bool  `json:"remove_from_channels"`
	RemovePricing      bool  `json:"remove_pricing"`
	AllowRemovePricing bool  `json:"-"`
}
type Output = domain.ModelDeleteResult
