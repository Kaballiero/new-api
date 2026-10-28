package get_pricing_overview

import "github.com/QuantumNous/new-api/internal/pricing/domain"

type Input struct {
	Group, UsingGroup string
	Query             domain.EffectivePricingPageQuery
}
type Output = domain.EffectivePricingOverviewData
