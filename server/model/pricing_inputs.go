package model

import (
	"context"
	catalogrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/observe_catalogue_inputs"
	"sync/atomic"
)

var pricingObservedSource atomic.Pointer[string]
var pricingObservedChannels atomic.Pointer[string]

func pricingPersistedInputFingerprint(metadata []Model, vendors []Vendor, channels []*Channel, abilities []AbilityWithChannel) (string, error) {
	m := make([]domain.BuildModel, len(metadata))
	for i := range metadata {
		m[i] = *catalogrecord.ToModel((*catalogrecord.Model)(&metadata[i]))
	}
	v := make([]domain.BuildVendor, len(vendors))
	for i := range vendors {
		v[i] = *catalogrecord.ToVendor((*catalogrecord.Vendor)(&vendors[i]))
	}
	c := make([]*domain.BuildChannel, len(channels))
	for i, row := range channels {
		c[i] = pricingBuildChannel(row)
	}
	revision, err := persistedCatalogRevision(context.Background())
	if err != nil {
		return "", err
	}
	return domain.PersistedInputFingerprint(m, v, c, pricingBuildAbilities(abilities), revision)
}
func syncPricingCatalogueInputs() {
	observe_catalogue_inputs.NewUsecase(store.NewObservations(newPricingCatalogueStore()), pricingObservationState{}).Execute()
}
func pricingChannelInputIdentity(channel *Channel) (string, error) {
	return domain.ChannelInputIdentity(pricingBuildChannel(channel))
}

type pricingObservationState struct{ pricingBuildState }

func (pricingObservationState) Observed() *string     { return pricingObservedSource.Load() }
func (pricingObservationState) Observe(value *string) { pricingObservedSource.Store(value) }
func (pricingObservationState) Invalidate()           { pricingInputsGeneration.Add(1) }
