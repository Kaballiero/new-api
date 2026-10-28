package settings

import billinggateway "github.com/QuantumNous/new-api/relay/billing"

func BillingFX() any { return billinggateway.CurrentBillingFXStatus() }
