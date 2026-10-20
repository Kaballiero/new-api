package controller

import (
	"encoding/json"
	upstream "github.com/QuantumNous/new-api/internal/adapter/pricing/upstream"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
)

const pricingCatalogMaxContexts = 4096

func nativePricingClock(raw json.RawMessage) (int, error) { return domain.NativePricingClock(raw) }
func nativePricingVectors(endpoint openrouter.PricedEndpoint, conditions ...*[][]billingexpr.TierCondition) ([]map[string]json.RawMessage, error) {
	return domain.NativePricingVectors(upstream.NativePricedEndpoint(endpoint), conditions...)
}
