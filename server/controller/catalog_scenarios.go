package controller

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/app"
)

// catalogHTTP builds the catalog handlers once; settings and the database are
// read per request by the injected functions.
var catalogHTTP = sync.OnceValue(func() app.CatalogHTTP {
	return app.NewCatalogHTTP(app.CatalogEffects{Audit: recordManageAudit})
})
