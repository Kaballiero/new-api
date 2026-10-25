package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var PricingCatalogState = &pricingCatalogState
var PricingCatalogFingerprint = pricingCatalogFingerprint
var GroupRatioOptionMutex = &groupRatioOptionMutex

const PricingCatalogMaxBytes = pricingCatalogMaxBytes

var ImagePricingWithTokenInputs = imagePricingWithTokenInputs
var DecodePricingCatalogForTest = decodePricingCatalog
var RequireOptionValue = requireOptionValue

func pricingCatalogOptionFixture(t *testing.T) (*gorm.DB, *Channel, PricingCatalogEntry) {
	t.Helper()
	db := useGroupRatioOptionDB(t)
	require.NoError(t, db.AutoMigrate(&Channel{}))
	require.NoError(t, db.Where("1 = 1").Delete(&Channel{}).Error)
	previous := pricingCatalogState.Load()
	pricingCatalogState.Store(nil)
	t.Cleanup(func() { pricingCatalogState.Store(previous); InvalidatePricingCache() })
	channel := &Channel{Id: 970, Type: constant.ChannelTypeOpenRouter, Status: common.ChannelStatusEnabled, Key: "private-fixture", Models: "upstream/model", Group: "default"}
	require.NoError(t, db.Create(channel).Error)
	amount := .03
	entry := PricingCatalogEntry{ChannelID: channel.Id, UpstreamModel: "upstream/model", Endpoint: constant.EndpointTypeImageGeneration, Source: "https://openrouter.ai/api/v1/images/models/upstream/model/endpoints", FetchedAt: "2026-10-13T13:41:19Z", Fingerprint: PricingCatalogSourceFingerprint(channel, "upstream/model", constant.EndpointTypeImageGeneration), Offers: []CatalogOffer{{ID: "native", Operation: "image_generation", Mode: "sync", MainComponent: "image_output", Prices: []CatalogPrice{{Component: "image_output", Unit: "megapixel", AmountUSD: &amount}}}}}
	return db, channel, entry
}

var PricingCatalogOptionFixture = pricingCatalogOptionFixture
