package get_personal_pricing

import "github.com/QuantumNous/new-api/internal/pricing/domain"

type Input struct {
	UserID            int
	Group, UsingGroup string
	Page              bool
	Query             domain.EffectivePricingPageQuery
}
type Output struct {
	Full *domain.ConsumerPricingResponse
	Page *domain.EffectivePricingPageData
}
