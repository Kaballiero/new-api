package model

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

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

func TestPricingCatalogOptionsPersistReloadAndRevision(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	before := GetPricingRevision()
	require.NoError(t, PublishPricingCatalogEntry(entry))
	require.Greater(t, GetPricingRevision(), before)
	saved := requireOptionValue(t, db, PricingCatalogOptionKey)
	assert.NotContains(t, saved, channel.Key)
	assert.False(t, IsModelPricingOption(PricingCatalogOptionKey))
	published, known := GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	assert.Equal(t, entry, published)
	revision := GetPricingRevision()
	loadOptionsFromDatabase()
	assert.Equal(t, revision, GetPricingRevision())
	pricingCatalogState.Store(nil)
	loadOptionsFromDatabase()
	reloaded, known := GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	assert.Equal(t, entry, reloaded)
	entry.FetchedAt = "2026-10-13T14:41:19Z"
	require.NoError(t, PublishPricingCatalogEntry(entry))
	assert.Greater(t, GetPricingRevision(), revision)
}

func TestPricingCatalogOptionsRejectInvalidAndRetainSiblings(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	require.NoError(t, PublishPricingCatalogEntry(entry))
	sibling := entry
	sibling.UpstreamModel = "upstream/other"
	sibling.Fingerprint = PricingCatalogSourceFingerprint(channel, sibling.UpstreamModel, sibling.Endpoint)
	require.NoError(t, PublishPricingCatalogEntry(sibling))
	saved := requireOptionValue(t, db, PricingCatalogOptionKey)
	revision := GetPricingRevision()
	invalid := strings.Replace(saved, `"amount_usd":0.03`, `"amount_usd":-1`, 1)
	require.Error(t, UpdateOption(PricingCatalogOptionKey, invalid))
	assert.Equal(t, saved, requireOptionValue(t, db, PricingCatalogOptionKey))
	assert.Equal(t, revision, GetPricingRevision())
	_, known := GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	assert.True(t, known)
	_, known = GetPricingCatalogEntry(channel, sibling.UpstreamModel, sibling.Endpoint)
	assert.True(t, known)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("models", "changed-model").Error)
	require.ErrorContains(t, PublishPricingCatalogEntry(entry), "source changed")
	assert.Equal(t, saved, requireOptionValue(t, db, PricingCatalogOptionKey))
	changed := *channel
	changed.Models = "changed-model"
	_, known = GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known)
}

func TestPricingCatalogOptionsTransactionFailureDoesNotPublish(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	require.NoError(t, PublishPricingCatalogEntry(entry))
	saved := requireOptionValue(t, db, PricingCatalogOptionKey)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:pricing_catalog_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "options" {
			tx.AddError(errors.New("pricing catalog transaction failure"))
		}
	}))
	t.Cleanup(func() { db.Callback().Update().Remove("test:pricing_catalog_fail") })
	revision := GetPricingRevision()
	entry.FetchedAt = "2026-10-13T15:41:19Z"
	require.Error(t, PublishPricingCatalogEntry(entry))
	assert.Equal(t, saved, requireOptionValue(t, db, PricingCatalogOptionKey))
	assert.Equal(t, revision, GetPricingRevision())
	retained, known := GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	assert.Equal(t, "2026-10-13T13:41:19Z", retained.FetchedAt)
}

func TestPricingCatalogOptionsCancelledPublicationRetainsLastValid(t *testing.T) {
	db, _, entry := pricingCatalogOptionFixture(t)
	require.NoError(t, PublishPricingCatalogEntry(entry))
	previous := requireOptionValue(t, db, PricingCatalogOptionKey)
	revision := GetPricingRevision()
	ctx, cancel := context.WithCancel(context.Background())
	groupRatioOptionMutex.Lock()
	done := make(chan error, 1)
	go func() { done <- PublishPricingCatalogEntryContext(ctx, entry) }()
	cancel()
	groupRatioOptionMutex.Unlock()
	require.ErrorIs(t, <-done, context.Canceled)
	assert.Equal(t, previous, requireOptionValue(t, db, PricingCatalogOptionKey))
	assert.Equal(t, revision, GetPricingRevision())
}

func TestPricingCatalogOptionsInvalidPersistedStateRetainsLastValid(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	require.NoError(t, PublishPricingCatalogEntry(entry))
	valid := requireOptionValue(t, db, PricingCatalogOptionKey)
	revision := GetPricingRevision()
	for _, invalid := range []string{`{`, `{"version":0,"entries":{}}`, strings.Repeat("x", pricingCatalogMaxBytes+1), strings.Replace(valid, entry.FetchedAt, "invalid", 1)} {
		require.NoError(t, db.Model(&Option{}).Where(map[string]any{"key": PricingCatalogOptionKey}).Update("value", invalid).Error)
		loadOptionsFromDatabase()
		retained, known := GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
		require.True(t, known)
		assert.Equal(t, entry, retained)
		assert.Equal(t, revision, GetPricingRevision())
		assert.Equal(t, valid, common.OptionMap[PricingCatalogOptionKey])
	}
}

func TestPricingCatalogOptionsConcurrentMergeSyncPreservesBillingOptions(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	sentinel := Option{Key: "ModelRatio", Value: `{"financial-sentinel":7.125}`}
	require.NoError(t, db.Create(&sentinel).Error)
	previousRatio := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatio)) })
	second := entry
	second.UpstreamModel = "upstream/sibling"
	second.Fingerprint = PricingCatalogSourceFingerprint(channel, second.UpstreamModel, second.Endpoint)
	var workers sync.WaitGroup
	errors := make(chan error, 2)
	groupRatioOptionMutex.Lock()
	workers.Go(func() { errors <- PublishPricingCatalogEntry(entry) })
	workers.Go(func() { errors <- PublishPricingCatalogEntry(second) })
	workers.Go(loadOptionsFromDatabase)
	groupRatioOptionMutex.Unlock()
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	_, known := GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	_, known = GetPricingCatalogEntry(channel, second.UpstreamModel, second.Endpoint)
	require.True(t, known)
	assert.Equal(t, sentinel.Value, requireOptionValue(t, db, sentinel.Key))
}
