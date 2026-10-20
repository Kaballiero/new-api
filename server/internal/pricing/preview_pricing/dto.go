package preview_pricing

import (
	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Input struct {
	UserID     int
	UserGroups []string
	Supplied   bool
}
type Output struct {
	domain.EffectivePricingResponse
	AvailableUserGroups []string `json:"available_user_groups"`
}
