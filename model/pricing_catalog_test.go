package model

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
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

func TestPricingCatalogSourceSurvivesUnrelatedRoutes(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	require.NoError(t, PublishPricingCatalogEntry(entry))
	mapping := `{"neighbor":"other/upstream","alias":"upstream/model"}`
	changed := *channel
	changed.Models = "neighbor,alias"
	changed.ModelMapping = &mapping
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"models": changed.Models, "model_mapping": mapping,
	}).Error)
	_, known := GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.True(t, known, "another route must not invalidate this upstream price")
	require.NoError(t, PublishPricingCatalogEntry(entry), "unrelated route changes must not reject in-flight publication")

	remapped := `{"alias":"different/upstream"}`
	for _, change := range []struct {
		name    string
		channel Channel
	}{
		{"model removed", Channel{Models: "neighbor", ModelMapping: &mapping}},
		{"alias remapped", Channel{Models: "alias", ModelMapping: &remapped}},
	} {
		t.Run(change.name, func(t *testing.T) {
			replaced := changed
			replaced.Models = change.channel.Models
			replaced.ModelMapping = change.channel.ModelMapping
			_, known := GetPricingCatalogEntry(&replaced, entry.UpstreamModel, entry.Endpoint)
			assert.False(t, known)
		})
	}
	changed.Key = "rotated-fixture"
	_, known = GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known, "credential changes must invalidate source prices")
}

func TestPricingCatalogLegacyFingerprintCompatibility(t *testing.T) {
	_, channel, entry := pricingCatalogOptionFixture(t)
	entry.Fingerprint = pricingCatalogFingerprint(channel, entry.UpstreamModel, entry.Endpoint, []any{channel.Models, channel.GetModelMapping()})
	require.NoError(t, PublishPricingCatalogEntry(entry))
	_, known := GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	assert.True(t, known)
	changed := *channel
	changed.Models += ",neighbor"
	_, known = GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known, "stale legacy fingerprints require fresh metadata")
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

func TestImagePricingTokenInputsStayWithTheirProvider(t *testing.T) {
	amount := func(v float64) *float64 { return &v }
	image := CatalogOffer{ID: "43:google/model:studio:0:default", Operation: "image_generation", Mode: "sync", MainComponent: "image_output", Prices: []CatalogPrice{{Component: "image_output", Unit: "million_tokens", AmountUSD: amount(60)}}}
	tokens := CatalogOffer{ID: "43:google/model:studio:0:0", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: amount(.5)}, {Component: "output", Unit: "million_tokens", AmountUSD: amount(3)}, {Component: "cache_read", Unit: "million_tokens", AmountUSD: amount(0)}}}
	for _, tc := range []struct {
		name, id  string
		wantInput bool
	}{
		{"same provider", tokens.ID, true},
		{"different provider", "43:google/model:vertex:0:0", false},
		{"different channel", "44:google/model:studio:0:0", false},
		{"missing provider identity", "unknown", false},
		{"empty provider identity", "43:google/model::0:0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := tokens
			source.ID = tc.id
			offers := []CatalogOffer{image, source}
			before, err := common.Marshal(offers)
			require.NoError(t, err)
			result := imagePricingWithTokenInputs(offers)
			selected := SelectCatalogOffer(result[:1])
			require.NotNil(t, selected)
			prices := map[string]CatalogPrice{}
			for _, p := range selected.Prices {
				prices[p.Component] = p
			}
			assert.Equal(t, 60., *prices["image_output"].AmountUSD)
			assert.NotContains(t, prices, "output")
			if tc.wantInput {
				require.Contains(t, prices, "input")
				assert.Equal(t, .5, *prices["input"].AmountUSD)
				require.Contains(t, prices, "cache_read")
				assert.Equal(t, 0., *prices["cache_read"].AmountUSD)
			} else {
				assert.NotContains(t, prices, "input")
			}
			after, err := common.Marshal(offers)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after))
		})
	}
	t.Run("both provider tags absent", func(t *testing.T) {
		native := image
		native.ID = "43:google/model::0:"
		source := tokens
		source.ID = "43:google/model::0:0"
		result := imagePricingWithTokenInputs([]CatalogOffer{native, source})
		assert.Len(t, result[0].Prices, 1)
	})

	t.Run("native input wins", func(t *testing.T) {
		native := image
		native.Prices = append(slices.Clone(image.Prices), CatalogPrice{Component: "input", Unit: "million_tokens", AmountUSD: amount(.7)})
		result := imagePricingWithTokenInputs([]CatalogOffer{native, tokens})
		for _, p := range result[0].Prices {
			if p.Component == "input" {
				assert.Equal(t, .7, *p.AmountUSD)
			}
		}
	})
	t.Run("conditional alternatives retained", func(t *testing.T) {
		conditional := tokens
		conditional.ID = "43:google/model:studio:0:1"
		conditional.PublicConditional = true
		conditional.PublicConditions = []billingexpr.TierCondition{{Var: "len", Op: ">", Value: 100}}
		conditional.Prices = []CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: amount(1)}}
		source := tokens
		source.PublicAlternatives = []CatalogOffer{tokens, conditional}
		result := imagePricingWithTokenInputs([]CatalogOffer{image, source})
		require.Len(t, result[0].PublicAlternatives, 2)
		assert.Equal(t, conditional.PublicConditions, result[0].PublicAlternatives[1].PublicConditions)
		assert.True(t, result[0].PublicAlternatives[1].PublicConditional)
	})
}
