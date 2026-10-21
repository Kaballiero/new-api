package controller

import (
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
)

type effectivePricingTier = pricingdomain.EffectivePricingTier

// effectivePricingTierCondition describes what selects a tier without exposing
// expression text. Numeric kinds carry an exclusive MinValue and an inclusive
// MaxValue in tokens; time_of_day carries half-open [start, end) hour windows.
type effectivePricingTierCondition = pricingdomain.EffectivePricingTierCondition

type effectivePricingTimeWindow = pricingdomain.EffectivePricingTimeWindow
