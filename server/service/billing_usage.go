package service

import "github.com/QuantumNous/new-api/relaykit/dto"

// EffectiveBillingUsage selects canonical provider billing facts for settlement and logs.
func EffectiveBillingUsage(usage *dto.Usage) *dto.Usage {
	if billingUsage, ok := usageFromBillingUsage(usage); ok {
		return billingUsage
	}
	return usage
}

func usageFromBillingUsage(usage *dto.Usage) (*dto.Usage, bool) {
	if usage == nil || usage.BillingUsage == nil {
		return nil, false
	}
	return usage.BillingUsage.CanonicalUsage()
}
