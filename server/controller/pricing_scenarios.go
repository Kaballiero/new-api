package controller

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/app"
)

// pricingHTTP builds the pricing handlers once; settings, the catalog snapshot
// and the database are read per request by the injected functions.
var pricingHTTP = sync.OnceValue(func() app.PricingHTTP {
	return app.NewPricingHTTP(app.PricingEffects{Audit: recordManageAudit})
})
