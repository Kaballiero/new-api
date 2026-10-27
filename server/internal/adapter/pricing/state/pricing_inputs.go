package pricingstate

import (
	"context"
	"sync/atomic"

	catalogrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	legacy "github.com/QuantumNous/new-api/model"
)

var pricingObservedSource atomic.Pointer[string]

var PricingObservedChannels atomic.Pointer[string]

func PricingPersistedInputFingerprint(
	metadata []legacy.Model,
	vendors []legacy.Vendor,
	channels []*legacy.Channel,
	abilities []channelrecord.AbilityWithChannel,
) (string, error) {
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
		c[i] = PricingBuildChannel(row)
	}
	revision, err := persistedCatalogRevision(context.Background())
	if err != nil {
		return "", err
	}
	return domain.PersistedInputFingerprint(pricingCapabilities(), m, v, c, PricingBuildAbilities(abilities), revision)
}

func PricingChannelInputIdentity(channel *legacy.Channel) (string, error) {
	return domain.ChannelInputIdentity(pricingCapabilities(), PricingBuildChannel(channel))
}

type PricingObservationState struct{ PricingBuildState }

func (PricingObservationState) Observed() *string { return pricingObservedSource.Load() }

func (PricingObservationState) Observe(value *string) { pricingObservedSource.Store(value) }

func (PricingObservationState) Invalidate() { pricingInputsGeneration.Add(1) }
