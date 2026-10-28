package httpapi

import (
	"github.com/QuantumNous/new-api/internal/app"
	"sync"
)

// Pricing builds the pricing handlers once; settings, the catalog snapshot
// and the database are read per request by the injected functions.
var Pricing = sync.OnceValue(func() app.PricingHTTP {
	return app.NewPricingHTTP(app.PricingEffects{Audit: recordManageAudit})
})
