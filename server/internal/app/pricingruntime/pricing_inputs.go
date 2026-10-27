package pricingruntime

import (
	"sync"

	store "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	"github.com/QuantumNous/new-api/internal/pricing/observe_catalogue_inputs"
)

func SyncPricingCatalogueInputs() {
	cachedObserveCatalogueInputs().Execute()
}

var cachedObserveCatalogueInputs = sync.OnceValue(func() observe_catalogue_inputs.Usecase {
	return observe_catalogue_inputs.NewUsecase(
		store.NewObservations(pricingstate.NewPricingCatalogueStore(LoadConfiguredCatalog)),
		pricingstate.PricingObservationState{},
	)
})
