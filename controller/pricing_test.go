package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDecisionCatalogConsumerPrices(t *testing.T) {
	previousDB, previousLogDB, previousMemory := model.DB, model.LOG_DB, common.MemoryCacheEnabled
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.MemoryCacheEnabled = previousDB, previousLogDB, previousMemory
		model.InvalidatePricingCache()
	})
	db := setupModelListControllerTestDB(t)
	common.MemoryCacheEnabled = false
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.CatalogSource{}, &model.CatalogRevision{}, &model.CatalogDocument{}))
	t.Setenv(model.CatalogAPIURLEnv, "https://catalog.fixture")
	source, enabled, err := model.ConfiguredCatalogSource()
	require.NoError(t, err)
	require.True(t, enabled)
	head, err := model.EnsureCatalogSource(context.Background(), source)
	require.NoError(t, err)
	selection := model.CatalogSelection{SchemaVersion: 1, NormalizationVersion: "2"}
	documents := []model.CatalogDocument{}
	mapping := map[string]string{}
	for _, fixture := range []struct{ name, id, hash string }{
		{"clef", "cloudflare/clef", "59703a8512b71bafe302dc9dd0f0cddd6a32061d5d73176a9aae617bd0fca8f3"},
		{"clef-flash", "cloudflare/clef-flash", "64073044bc2dd8f7864199195cce730584d60722460672c228c16e8c6e49713b"},
		{"clef-omni", "cloudflare/clef-omni", "6cde2d5139462df26ef8cdd778bd12458952210c7fe0e6eda6c947d7a057e61f"},
		{"classify-1", "scaledown/classify-1", "97debcdf89d11900bf0384df8d9c6291f8b39b881ee83d27a9322e3ff15bd358"},
	} {
		raw, err := os.ReadFile("../model/testdata/upstream_catalog/" + strings.ReplaceAll(fixture.id, "/", "__") + ".json")
		require.NoError(t, err)
		require.Equal(t, fixture.hash, catalogsync.Hash(raw))
		_, err = catalogsync.DecodeDocument(raw, catalogsync.Reference{ID: fixture.id, Hash: fixture.hash})
		require.NoError(t, err)
		selection.Models = append(selection.Models, model.CatalogModelReference{ID: fixture.id, Hash: fixture.hash})
		documents = append(documents, model.CatalogDocument{SourceID: source.ID, ModelID: fixture.id, Hash: fixture.hash, Payload: string(raw)})
		mapping[fixture.name] = fixture.id
		require.NoError(t, db.Create(&model.Ability{Model: fixture.name, Group: "default", ChannelId: 43, Enabled: true}).Error)
	}
	_, err = model.PublishCatalogSelection(context.Background(), head, selection, catalogsync.Hash([]byte("retained-decision-fixtures")), "2026-10-20T00:00:00Z", documents)
	require.NoError(t, err)
	mappingJSON, err := common.Marshal(mapping)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Channel{Id: 43, Type: constant.ChannelTypeOpenRouter, Key: "unused-fixture", Status: common.ChannelStatusEnabled, ModelMapping: common.GetPointer(string(mappingJSON))}).Error)
	model.RefreshPricing()
	items, _, revision, err := model.GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	require.NotEmpty(t, revision)
	require.Len(t, items, 4)
	for _, item := range items {
		t.Run(item.ModelName, func(t *testing.T) {
			assert.Equal(t, "decision", item.CatalogType)
			assert.Equal(t, "TEXT", item.CatalogModality)
			assert.NotContains(t, item.SupportedEndpointTypes, model.PricingEndpointSystemOne)
			routes := item.UpstreamCatalogRoutesForGroup("default")
			require.Len(t, routes, 1)
			assert.Equal(t, "decisions", routes[0].Operation)
			assert.Equal(t, model.PricingEndpointSystemOne, routes[0].Endpoint)
			context := effectivePricingPageContext{pricing: []model.Pricing{item}, usableGroups: map[string]string{"default": "Default"}, groupRatios: map[string]float64{"default": 1.4}, fxFactor: .8}
			public := buildPublicModelTariff(context, item)
			personal := buildConsumerPricingModel(context.usableGroups, context.groupRatios, context.fxFactor, item, true)
			require.Len(t, public.Operations, 1)
			require.Len(t, personal.GroupPrices, 1)
			group := personal.GroupPrices[0]
			require.Len(t, group.Operations, 1)
			assert.Equal(t, "decisions", public.Operations[0].Operation)
			assert.Equal(t, "decisions", group.Operations[0].Operation)
			assert.Equal(t, "sync", group.Operations[0].Mode)
			expectedInput := map[string]float64{"clef": .042, "clef-flash": .021, "clef-omni": .15}
			if item.ModelName == "classify-1" {
				assert.Empty(t, public.Operations[0].Prices)
				for _, tier := range public.Operations[0].Tiers {
					assert.Empty(t, tier.Prices)
				}
				assert.Empty(t, group.Operations[0].Prices, "discount relationship remains unknown")
			} else {
				require.Len(t, group.Operations[0].Prices, 2)
				amounts := map[string]float64{}
				for _, price := range group.Operations[0].Prices {
					require.NotNil(t, price.AmountRub)
					amounts[price.Component] = *price.AmountRub
				}
				assert.InDelta(t, expectedInput[item.ModelName]*100*1.4*.8, amounts["input"], 1e-9)
				assert.Equal(t, float64(0), amounts["output"], "explicit zero is a valid rate")
				assert.False(t, public.Operations[0].IsFree, "positive input is not free")
				require.NotEmpty(t, public.Operations[0].Prices)
				assert.Equal(t, group.Operations[0].Prices, public.Operations[0].Prices)
			}
			require.NotEmpty(t, group.ProviderOffers)
			ids := map[string]bool{}
			for _, offer := range group.ProviderOffers {
				assert.False(t, ids[offer.ID])
				ids[offer.ID] = true
				assert.Equal(t, "decisions", offer.Operation)
				assert.Equal(t, "sync", offer.Mode)
				if item.ModelName == "classify-1" {
					for _, price := range offer.Prices {
						assert.Nil(t, price.AmountRub)
					}
				}
			}
			if item.ModelName == "clef" {
				// A Chat path would leave PrimeIntellect's cheaper offer in the
				// minimum; the native path must apply the Cloudflare constraint.
				policy := `{"operations":[{"path":"provider.only","mode":"set","value":["cloudflare"],"conditions":[{"path":"request_path","mode":"full","value":"/v1/systemone"}]}]}`
				routes[0].ParamOverride = &policy
				item.UpstreamCatalogRoutesByGroup = map[string][]model.UpstreamCatalogRoute{"default": routes}
				offers := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{})
				require.Len(t, offers, 1)
				assert.Equal(t, "cloudflare", *offers[0].Provider.Tag)
				minimum := catalogMinima(offers)
				require.Len(t, minimum, 1)
				for _, price := range minimum[0].Prices {
					if price.Component == "input" {
						require.NotNil(t, price.AmountRub)
						assert.Equal(t, float64(24), *price.AmountRub)
					}
				}
			}
		})
	}
	t.Run("native endpoint binding fails closed", func(t *testing.T) {
		for _, missing := range []string{"decisions output", "eligible endpoint", "endpoint scope", "matching operation", "sync mode"} {
			t.Run(missing, func(t *testing.T) {
				var doc catalogsync.Document
				require.NoError(t, common.UnmarshalJsonStr(documents[0].Payload, &doc))
				switch missing {
				case "decisions output":
					doc.Modalities.Output = []string{"text"}
				case "eligible endpoint":
					for i := range doc.Offers {
						doc.Offers[i].Eligibility = &catalogsync.Eligibility{Eligible: false}
					}
				case "endpoint scope":
					for i := range doc.Offers {
						doc.Offers[i].Scope = "model"
					}
				case "matching operation":
					for i := range doc.Offers {
						doc.Offers[i].Operation = common.GetPointer("text")
					}
				case "sync mode":
					for i := range doc.Offers {
						doc.Offers[i].Mode = common.GetPointer("batch")
					}
				}
				raw, err := common.Marshal(doc)
				require.NoError(t, err)
				head, enabled, err := model.ConfiguredCatalogSource()
				require.NoError(t, err)
				require.True(t, enabled)
				head, err = model.EnsureCatalogSource(context.Background(), head)
				require.NoError(t, err)
				selection := model.CatalogSelection{SchemaVersion: 1, NormalizationVersion: "2", Models: []model.CatalogModelReference{{ID: doc.ID, Hash: catalogsync.Hash(raw)}}}
				_, err = model.PublishCatalogSelection(context.Background(), head, selection, catalogsync.Hash([]byte(missing)), "2026-10-20T00:00:00Z", []model.CatalogDocument{{SourceID: source.ID, ModelID: doc.ID, Hash: catalogsync.Hash(raw), Payload: string(raw)}})
				require.NoError(t, err)
				model.RefreshPricing()
				items, _, _, err := model.GetPricingCatalogueSnapshot()
				require.NoError(t, err)
				found := false
				for _, item := range items {
					if item.ModelName != "clef" {
						continue
					}
					found = true
					assert.Empty(t, item.UpstreamCatalogRoutesForGroup("default"))
					consumer := buildConsumerPricingModel(map[string]string{"default": "Default"}, map[string]float64{"default": 1}, 1, item, true)
					require.Len(t, consumer.GroupPrices, 1)
					assert.Empty(t, consumer.GroupPrices[0].ProviderOffers)
					for _, operation := range consumer.GroupPrices[0].Operations {
						assert.Empty(t, operation.Prices)
					}
				}
				require.True(t, found)
			})
		}
	})
}

func TestConsumerCatalogBatchPricesStaySeparate(t *testing.T) {
	raw, err := os.ReadFile("../model/testdata/upstream_catalog/aggregator.json")
	require.NoError(t, err)
	doc, err := catalogsync.DecodeDocument(raw, catalogsync.Reference{ID: "router/aggregated", Hash: catalogsync.Hash(raw)})
	require.NoError(t, err)
	doc.ID, doc.Type = "vendor/model", "text"
	doc.Modalities.Output = []string{"text"}
	syncOffer := doc.Offers[0]
	syncOffer.Operation, syncOffer.Mode = nil, nil
	syncOffer.Identity = []byte(`{"modelId":"vendor/model"}`)
	syncOffer.Variant = []byte(`{"variant":"standard","model_variant_slug":"vendor/model"}`)
	syncOffer.Components = append([]catalogsync.Component{}, syncOffer.Components[:2]...)
	syncOffer.Components[0].AmountUSD = common.GetPointer("10")
	syncOffer.Components[1].AmountUSD = common.GetPointer("50")
	syncOffer.Components[1].Billable = common.GetPointer("output_tokens")
	syncOffer.Components[1].Direction = common.GetPointer("output")
	syncOffer.Components[1].Modality = common.GetPointer("text")
	syncOffer.Components[1].Normalization.Comparable = true
	syncOffer.Components[1].Normalization.Effective = true
	batchOffer := syncOffer
	batchOffer.ID = "batch-provider"
	batchOffer.Identity = []byte(`{"modelId":"vendor/model:batch"}`)
	batchOffer.Variant = []byte(`{"variant":"batch","model_variant_slug":"vendor/model:batch"}`)
	batchOffer.Components = append([]catalogsync.Component{}, syncOffer.Components...)
	batchOffer.Components[0].AmountUSD = common.GetPointer("5")
	batchOffer.Components[1].AmountUSD = common.GetPointer("25")
	doc.Offers = []catalogsync.Offer{syncOffer, batchOffer}
	item := model.Pricing{ModelName: "alias", CatalogModality: "TEXT", EnableGroup: []string{"default"}, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}, UpstreamCatalogRoutesByGroup: map[string][]model.UpstreamCatalogRoute{"default": {
		{Operation: "text", Mode: "sync", Endpoint: constant.EndpointTypeOpenAI, ModelID: doc.ID, Document: doc},
		{Operation: "text", Mode: "batch", Endpoint: model.PricingEndpointBatch, ModelID: doc.ID + ":batch", Document: doc},
	}}}
	context := effectivePricingPageContext{pricing: []model.Pricing{item}, usableGroups: map[string]string{"default": "Default"}, groupRatios: map[string]float64{"default": 1.4}, fxFactor: .8}
	for _, details := range []bool{false, true} {
		consumer := buildConsumerPricingModel(context.usableGroups, context.groupRatios, context.fxFactor, item, details)
		require.Len(t, consumer.GroupPrices, 1)
		operations := consumer.GroupPrices[0].Operations
		require.Len(t, operations, 2)
		for _, operation := range operations {
			expected := 50.0
			if operation.Mode == "batch" {
				expected = 25
			}
			for _, price := range operation.Prices {
				if price.Component == "output" {
					require.NotNil(t, price.AmountRub)
					assert.InDelta(t, expected*100*1.4*.8, *price.AmountRub, 1e-9)
				}
			}
		}
		if details {
			require.Len(t, consumer.GroupPrices[0].ProviderOffers, 2)
			modes, ids := []string{}, map[string]bool{}
			for _, offer := range consumer.GroupPrices[0].ProviderOffers {
				modes = append(modes, offer.Mode)
				assert.False(t, ids[offer.ID], "mode-separated offers need unique IDs")
				ids[offer.ID] = true
				assert.NotEmpty(t, offer.Variant)
			}
			assert.ElementsMatch(t, []string{"sync", "batch"}, modes)
		}
	}
	public := buildPublicModelTariff(context, item)
	require.Len(t, public.Operations, 2)
	for _, operation := range public.Operations {
		expected := 50.0
		if operation.Mode == "batch" {
			expected = 25
		}
		for _, price := range operation.Prices {
			if price.Component == "output" {
				require.NotNil(t, price.AmountRub)
				assert.InDelta(t, expected*100*1.4*.8, *price.AmountRub, 1e-9)
			}
		}
	}
	// Explicit Mode is authoritative over the legacy tuple.
	batchOffer.Mode = common.GetPointer("sync")
	doc.Offers = []catalogsync.Offer{batchOffer}
	assert.Empty(t, upstreamProviderOffers(model.Pricing{UpstreamCatalogRoutesByGroup: map[string][]model.UpstreamCatalogRoute{"default": {item.UpstreamCatalogRoutesByGroup["default"][1]}}}, "default", 1, true, pricingUserContext{}))
	batchOffer.Mode = nil
	batchOffer.Variant = []byte(`{"variant":"batch","model_variant_slug":"other/model:batch"}`)
	doc.Offers = []catalogsync.Offer{batchOffer}
	assert.Empty(t, upstreamProviderOffers(item, "default", 1, true, pricingUserContext{}), "unbound Batch identity must not become sync or a guessed Batch price")
}

func TestConsumerBatchEmptyZeroAndTierPrices(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prices []model.CatalogPrice
		want   bool
	}{
		{"empty", nil, false},
		{"unknown", []model.CatalogPrice{{Component: "output", Unit: "million_tokens"}}, false},
		{"zero", []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(0)}}, true},
		{"known", []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(5)}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := model.Pricing{ModelName: "alias", CatalogModality: "TEXT", CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": {{ID: "batch", Operation: "text", Mode: "batch", MainComponent: "output", Prices: tc.prices, PublicConditional: true}}}}
			context := effectivePricingPageContext{groupRatios: map[string]float64{"default": 1}, fxFactor: 1}
			tariff := buildPublicModelTariff(context, item)
			assert.Equal(t, tc.want, len(tariff.Operations) == 1)
			item.EnableGroup = []string{"default"}
			private := buildConsumerPricingModel(map[string]string{"default": "Default"}, context.groupRatios, 1, item, true)
			assert.Equal(t, tc.want, len(private.GroupPrices[0].Operations) == 1)
			item.CatalogModality = "" // An unrelated non-catalog channel keeps its prior contract.
			assert.Len(t, buildPublicModelTariff(context, item).Operations, 1)

		})
	}
	assert.True(t, confirmedConsumerPrices(nil, []publicPricingTier{{Prices: []consumerUnitPrice{{Component: "output", Unit: "million_tokens", AmountRub: float(0)}}}}), "tier-only zero is a real tariff")
	assert.Equal(t, "OTHER", effectivePricingModality(model.Pricing{CatalogModality: "OTHER", ModelName: "gpt-image-misleading", CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": {{Operation: "text"}, {Operation: "image_generation"}}}}))
}

const prodFXFactor = 0.854594

func tieredPricing(expr string) model.Pricing {
	return model.Pricing{BillingMode: billing_setting.BillingModeTieredExpr, BillingExpr: expr, ConfiguredPrices: map[string][][]billingexpr.DisplayPrice{expr: billingexpr.CatalogPrices(expr)}}
}

func float(v float64) *float64 { return &v }

func TestEffectiveGroupPricingTieredExpressionEmitsRoubleTiers(t *testing.T) {
	groupPrice := buildEffectiveGroupPricing(tieredPricing(`tier("base", p * 2 + c * 10 + cr * 0.2 + cc * 2.5 + cc1h * 4)`), "default", 1, prodFXFactor)

	assert.Equal(t, "formula", groupPrice.Status)
	assert.Empty(t, groupPrice.Limitations)
	assert.False(t, groupPrice.IsFree)
	require.Len(t, groupPrice.Tiers, 1)
	tier := groupPrice.Tiers[0]
	assert.Equal(t, "base", tier.Label)
	assert.Nil(t, tier.Condition)
	require.Len(t, tier.UnitPrices, 5)
	for i, expected := range []effectiveUnitPrice{
		{Component: "input", Unit: "million_tokens", AmountRub: 170.9188},
		{Component: "output", Unit: "million_tokens", AmountRub: 854.594},
		{Component: "cache_read", Unit: "million_tokens", AmountRub: 17.09188},
		{Component: "cache_write", Unit: "million_tokens", AmountRub: 213.6485},
		{Component: "cache_write_1h", Unit: "million_tokens", AmountRub: 341.8376},
	} {
		assert.Equal(t, expected.Component, tier.UnitPrices[i].Component)
		assert.Equal(t, expected.Unit, tier.UnitPrices[i].Unit)
		assert.InDelta(t, expected.AmountRub, tier.UnitPrices[i].AmountRub, 1e-9, expected.Component)
	}
}

func TestEffectiveGroupPricingRatioAndPerCallMirrorUnitPrices(t *testing.T) {
	ratio := buildEffectiveGroupPricing(model.Pricing{ModelRatio: 5, CompletionRatio: 5, CacheRatio: float(0.1)}, "default", 1, prodFXFactor)
	require.Len(t, ratio.UnitPrices, 3)
	assert.InDelta(t, 854.594, ratio.UnitPrices[0].AmountRub, 1e-9)
	require.Len(t, ratio.Tiers, 1)
	assert.Equal(t, effectivePricingTier{UnitPrices: ratio.UnitPrices}, ratio.Tiers[0])
	assert.False(t, ratio.IsFree)

	perCall := buildEffectiveGroupPricing(model.Pricing{QuotaType: 1, ModelPrice: 0.5}, "default", 1, prodFXFactor)
	require.Len(t, perCall.Tiers, 1)
	assert.Equal(t, effectivePricingTier{UnitPrices: perCall.UnitPrices}, perCall.Tiers[0])
	assert.Equal(t, []string{"provider-specific multipliers may apply for some request parameters"}, perCall.Limitations)
}

func TestEffectiveGroupPricingConditions(t *testing.T) {
	t.Run("context length bounds are exclusive below and inclusive above", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`len <= 32000 ? tier("0_32k", p * 1) : len <= 256000 ? tier("32k_256k", p * 2) : tier("256k_plus", p * 3)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 3)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "context_length", Unit: "token", MaxValue: float(32000)}, groupPrice.Tiers[0].Condition)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "context_length", Unit: "token", MinValue: float(32000), MaxValue: float(256000)}, groupPrice.Tiers[1].Condition)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "context_length", Unit: "token", MinValue: float(256000)}, groupPrice.Tiers[2].Condition)
	})
	t.Run("strict comparisons shift by one token", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`p < 1000 ? tier("small", p * 1) : tier("large", p * 2)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 2)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "input_tokens", Unit: "token", MaxValue: float(999)}, groupPrice.Tiers[0].Condition)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "input_tokens", Unit: "token", MinValue: float(999)}, groupPrice.Tiers[1].Condition)
	})
	t.Run("hour guards become time windows and their complement", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`(hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10) ? tier("peak", p * 1.32 + c * 3.96 + cr * 0.044) : tier("off_peak", p * 0.66 + c * 1.98 + cr * 0.022)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 2)
		assert.Equal(t, "peak", groupPrice.Tiers[0].Label)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "time_of_day", TimeZone: "UTC", TimeWindows: []effectivePricingTimeWindow{{1, 4}, {6, 10}}}, groupPrice.Tiers[0].Condition)
		assert.Equal(t, "off_peak", groupPrice.Tiers[1].Label)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "time_of_day", TimeZone: "UTC", TimeWindows: []effectivePricingTimeWindow{{0, 1}, {4, 6}, {10, 24}}}, groupPrice.Tiers[1].Condition)
		assert.InDelta(t, 112.806408, groupPrice.Tiers[0].UnitPrices[0].AmountRub, 1e-9)
		assert.InDelta(t, 56.403204, groupPrice.Tiers[1].UnitPrices[0].AmountRub, 1e-9)
	})
	t.Run("unclassifiable guard is reported as other", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`len == 4096 ? tier("exact", p * 1) : tier("rest", p * 2)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 2)
		assert.Equal(t, &effectivePricingTierCondition{Kind: "other"}, groupPrice.Tiers[0].Condition)
	})
}

func TestEffectiveGroupPricingComponentPresence(t *testing.T) {
	t.Run("referenced zero is free, unreferenced is absent", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`p * 0 + c * 0`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 1)
		assert.Empty(t, groupPrice.Tiers[0].Label)
		assert.Equal(t, []effectiveUnitPrice{{Component: "input", Unit: "million_tokens"}, {Component: "output", Unit: "million_tokens"}}, groupPrice.Tiers[0].UnitPrices)
		assert.True(t, groupPrice.IsFree)
	})
	t.Run("component referenced by one tier is listed at zero in the others", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`len <= 1000 ? tier("a", p * 1 + cr * 0.5) : tier("b", p * 2)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 2)
		require.Len(t, groupPrice.Tiers[1].UnitPrices, 2)
		assert.Equal(t, "cache_read", groupPrice.Tiers[1].UnitPrices[1].Component)
		assert.Zero(t, groupPrice.Tiers[1].UnitPrices[1].AmountRub)
	})
	t.Run("variable outside the vocabulary is passed through verbatim", func(t *testing.T) {
		groupPrice := buildEffectiveGroupPricing(tieredPricing(`tier("base", p * 1 + len * 0.1)`), "default", 1, prodFXFactor)
		require.Len(t, groupPrice.Tiers, 1)
		assert.Equal(t, []string{"input", "len"}, []string{groupPrice.Tiers[0].UnitPrices[0].Component, groupPrice.Tiers[0].UnitPrices[1].Component})
	})
}

func TestEffectiveGroupPricingRefusesUnpriceableExpressions(t *testing.T) {
	for name, expr := range map[string]string{
		"non-linear body":     `tier("base", max(p, c) * 2)`,
		"summed tiers":        `tier("a", p * 2) + tier("b", c * 3)`,
		"one non-linear tier": `len <= 1000 ? tier("a", p * 1) : tier("b", ceil(p) * 2)`,
	} {
		t.Run(name, func(t *testing.T) {
			groupPrice := buildEffectiveGroupPricing(tieredPricing(expr), "default", 1, prodFXFactor)
			assert.Equal(t, "formula", groupPrice.Status)
			assert.NotNil(t, groupPrice.Formula)
			assert.Equal(t, []effectivePricingTier{}, groupPrice.Tiers)
			assert.False(t, groupPrice.IsFree)
		})
	}
}

func TestEffectiveGroupPricingLegacyAudioOutputUsesAudioInputBasis(t *testing.T) {
	for _, tc := range []struct {
		name       string
		audioRatio *float64
		want       float64
	}{
		{name: "configured audio input ratio", audioRatio: float(4), want: 9600},
		{name: "missing audio input ratio defaults to one", want: 2400},
		{name: "explicit zero remains free", audioRatio: float(0), want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			price := buildEffectiveGroupPricing(model.Pricing{
				ModelRatio: 2, CompletionRatio: 7,
				AudioRatio: tc.audioRatio, AudioCompletionRatio: float(3),
			}, "paid", 4, 2)
			require.NotEmpty(t, price.UnitPrices)
			output := price.UnitPrices[len(price.UnitPrices)-1]
			assert.Equal(t, "audio_output", output.Component)
			assert.Equal(t, "million_tokens", output.Unit)
			assert.Equal(t, tc.want, output.AmountRub)
			require.Len(t, price.Tiers, 1)
			assert.Equal(t, price.UnitPrices, price.Tiers[0].UnitPrices)
		})
	}
}

func TestEffectiveGroupPricingExpressionAudioPricesIgnoreLegacyRatios(t *testing.T) {
	item := tieredPricing(`tier("audio", p * 2 + c * 7 + ai * 11 + ao * 13)`)
	item.ModelRatio = 50
	item.CompletionRatio = 60
	item.AudioRatio = float(70)
	item.AudioCompletionRatio = float(80)
	price := buildEffectiveGroupPricing(item, "paid", 4, 2)
	require.NotNil(t, price.Formula)
	assert.Equal(t, item.BillingExpr, price.Formula.Expression)
	assert.Empty(t, price.UnitPrices)
	require.Len(t, price.Tiers, 1)
	require.Len(t, price.Tiers[0].UnitPrices, 4)
	for i, expected := range []effectiveUnitPrice{
		{Component: "input", Unit: "million_tokens", AmountRub: 400},
		{Component: "output", Unit: "million_tokens", AmountRub: 1400},
		{Component: "audio_input", Unit: "million_tokens", AmountRub: 2200},
		{Component: "audio_output", Unit: "million_tokens", AmountRub: 2600},
	} {
		actual := price.Tiers[0].UnitPrices[i]
		assert.Equal(t, expected.Component, actual.Component)
		assert.Equal(t, expected.Unit, actual.Unit)
		assert.InDelta(t, expected.AmountRub, actual.AmountRub, 1e-9)
	}
}

func pricingPageFixture() effectivePricingPageContext {
	context := effectivePricingPageContext{
		metadata: consumerPricingResponse{SchemaVersion: "v2", UserGroup: "default", Currency: "RUB", AutoGroups: []string{}}, fxFactor: 1,
		usableGroups: map[string]string{"default": "Default", "premium": "Premium"},
		groupRatios:  map[string]float64{"default": 1, "premium": 2},
		vendors:      []model.PricingVendor{{ID: 1, Name: " OpenAI "}, {ID: 2, Name: "Anthropic"}, {ID: 3, Name: " "}, {ID: 0, Name: "omitted"}},
		pricing: []model.Pricing{
			{ModelName: "z-text", VendorID: 2, EnableGroup: []string{"default", "premium"}, ModelRatio: 2, CompletionRatio: 2, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}},
			{ModelName: "vendor/gpt-image-1", VendorID: 1, EnableGroup: []string{"default"}, QuotaType: 1, ModelPrice: 1, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}},
			{ModelName: "a-text", VendorID: 3, EnableGroup: []string{"default"}, ModelRatio: 1, CompletionRatio: 3, SupportedEndpointTypes: []constant.EndpointType{}},
			{ModelName: "private", VendorID: 2, EnableGroup: []string{"unusable"}, SupportedEndpointTypes: []constant.EndpointType{}},
			{ModelName: "premium-only", VendorID: 1, EnableGroup: []string{"premium"}, ModelRatio: 0.5, SupportedEndpointTypes: []constant.EndpointType{}},
		},
	}
	for index := range context.pricing {
		item := &context.pricing[index]
		item.PriceConfigured = true
		endpoint := constant.EndpointTypeOpenAI
		if item.QuotaType == 1 {
			endpoint = constant.EndpointTypeImageGeneration
		}
		item.BillingSourcesByGroup = map[string]map[constant.EndpointType][]string{}
		item.CatalogOffersByGroup = map[string][]model.CatalogOffer{}
		for _, group := range item.EnableGroup {
			item.BillingSourcesByGroup[group] = map[constant.EndpointType][]string{endpoint: {model.BillingSourceConfiguredTariff}}
			offer := model.CatalogOffer{ID: "fixture", Operation: "text", Mode: "sync", MainComponent: "output"}
			if item.QuotaType == 1 {
				offer.Operation, offer.MainComponent = "image_generation", "image_output"
			} else {
				offer.Prices = []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: float(item.ModelRatio * 2)}, {Component: "output", Unit: "million_tokens", AmountUSD: float(item.ModelRatio * item.CompletionRatio * 2)}}
			}
			item.CatalogOffersByGroup[group] = []model.CatalogOffer{offer}
		}
	}
	return context
}

func TestEffectivePricingPageSelectionAndProjectionBound(t *testing.T) {
	context := pricingPageFixture()
	query, err := parseEffectivePricingPageQuery(url.Values{"limit": {"1"}, "offset": {"1"}}, true)
	require.NoError(t, err)
	for range 2 {
		calls := 0
		page := buildEffectivePricingPage(context, query, func(item model.Pricing) consumerPricingModel { calls++; return context.project(item) })
		assert.Equal(t, 4, page.TotalCount)
		require.Len(t, page.Data, 1)
		assert.Equal(t, "premium-only", page.Data[0].ModelName)
		assert.Equal(t, 1, calls)
		assert.Equal(t, []effectivePricingCreator{{ID: 2, Name: "Anthropic"}, {ID: 1, Name: "OpenAI"}}, page.Creators)
	}
	for _, values := range []url.Values{{"limit": {"0"}}, {"offset": {"9007199254740991"}}} {
		query, err = parseEffectivePricingPageQuery(values, true)
		require.NoError(t, err)
		page := buildEffectivePricingPage(context, query, func(model.Pricing) consumerPricingModel {
			t.Fatal("empty page projected tariffs")
			return consumerPricingModel{}
		})
		assert.Equal(t, 4, page.TotalCount)
		assert.Empty(t, page.Data)
		assert.NotNil(t, page.Data)
	}
	query, err = parseEffectivePricingPageQuery(url.Values{"search": {"  GPT-IMAGE  "}, "modality": {"IMAGE"}, "creator_id": {"1"}}, true)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "vendor/gpt-image-1", page.Data[0].ModelName)
	assert.Len(t, page.Creators, 2)
	query.creatorID = new(int)
	assert.Empty(t, buildEffectivePricingPage(context, query, context.project).Data)
}

func TestEffectivePricingPageSearchIgnoresModelFormatting(t *testing.T) {
	for _, test := range []struct {
		name, modelName, search string
		matches                 bool
	}{
		{"spaces", "openai/gpt-4.1-mini", "  GPT 4.1 MINI  ", true},
		{"compact", "openai/gpt-4.1-mini", "gpt41mini", true},
		{"separators", "openai/gpt-4.1-mini", "OpenAI_GPT_4_1_MINI", true},
		{"display name", "Claude Sonnet 4.5", "claude-sonnet-4-5", true},
		{"unicode dash", "Claude\u2011Sonnet\u20144.5", "claude sonnet 4.5", true},
		{"unicode spaces", "DeepSeek\u00a0R1", "deep seek r1", true},
		{"unicode width", "openai/gpt-4.1-mini", "ＧＰＴ４．１", true},
		{"canonical unicode", "caf\u00e9-model", "cafe\u0301 model", true},
		{"partial name", "google/gemini-2.5-pro-preview", "gemini 2.5 pro", true},
		{"different version", "gpt-4.1-mini", "gpt4.2", false},
		{"different family", "gpt-4.1-mini", "claude", false},
		{"punctuation only", "gpt-4.1-mini", "???", false},
		{"empty", "gpt-4.1-mini", " \t ", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, personal := range []bool{false, true} {
				context := pricingPageFixture()
				context.pricing = context.pricing[:1]
				context.pricing[0].ModelName = test.modelName
				query, err := parseEffectivePricingPageQuery(url.Values{"search": {test.search}}, personal)
				require.NoError(t, err)
				page := buildEffectivePricingPage(context, query, context.project)
				if test.matches {
					assert.Equal(t, 1, page.TotalCount)
					require.Len(t, page.Data, 1)
					assert.Equal(t, test.modelName, page.Data[0].ModelName)
				} else {
					assert.Zero(t, page.TotalCount)
					assert.Empty(t, page.Data)
				}
			}
		})
	}
}

func TestEffectivePricingSmartSearchFiltersBeforePagination(t *testing.T) {
	context := pricingPageFixture()
	context.pricing[0].ModelName = "openai/gpt-4.1-mini"
	context.pricing[2].ModelName = "openai/gpt-4.1"
	context.pricing[2].VendorID = 2
	for _, personal := range []bool{false, true} {
		query, err := parseEffectivePricingPageQuery(url.Values{
			"search": {"GPT 4.1"}, "limit": {"1"}, "offset": {"1"}, "modality": {"TEXT"}, "creator_id": {"2"},
		}, personal)
		require.NoError(t, err)
		page := buildEffectivePricingPage(context, query, context.project)
		assert.Equal(t, 2, page.TotalCount)
		require.Len(t, page.Data, 1)
		assert.Equal(t, "openai/gpt-4.1-mini", page.Data[0].ModelName)
		query.offset = 0
		query.modality = "IMAGE"
		assert.Zero(t, buildEffectivePricingPage(context, query, context.project).TotalCount)
		query.modality = "TEXT"
		creatorID := 1
		query.creatorID = &creatorID
		assert.Zero(t, buildEffectivePricingPage(context, query, context.project).TotalCount)
	}
}

func TestEffectivePricingPagePublicVisibilityAndSimpleWire(t *testing.T) {
	context := pricingPageFixture()
	context.usingGroup = "default"
	context.usableGroups = map[string]string{"default": "Default"}
	query, err := parseEffectivePricingPageQuery(url.Values{}, false)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	require.Len(t, page.Data, 3)
	for _, row := range page.Data {
		require.Len(t, row.GroupPrices, 1)
		assert.Equal(t, "default", row.GroupPrices[0].UsingGroup)
	}
	encoded, err := common.Marshal(page)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"operations"`)
	for _, removed := range []string{"formula", "tiers", "operation_tariffs", "accounting", "billing_sources", "effective_billing_ratio"} {
		assert.NotContains(t, string(encoded), removed)
	}
}

func TestEffectivePricingPagePersonalPricesAndUnit(t *testing.T) {
	context := pricingPageFixture()
	query, err := parseEffectivePricingPageQuery(url.Values{"order_by": {"INPUT_PRICE"}, "direction": {"desc"}}, true)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	names := []string{}
	for _, item := range page.Data {
		names = append(names, item.ModelName)
	}
	assert.Equal(t, []string{"z-text", "a-text", "premium-only", "vendor/gpt-image-1"}, names)
	require.Len(t, page.Data[0].GroupPrices, 2)
	query.direction = "asc"
	page = buildEffectivePricingPage(context, query, context.project)
	names = nil
	for _, item := range page.Data {
		names = append(names, item.ModelName)
	}
	assert.Equal(t, []string{"a-text", "premium-only", "z-text", "vendor/gpt-image-1"}, names)
	query.pricingUnit = "TOKENS"
	query.limit = 0
	calls := 0
	page = buildEffectivePricingPage(context, query, func(item model.Pricing) consumerPricingModel { calls++; return context.project(item) })
	assert.Equal(t, 3, page.TotalCount)
	assert.Empty(t, page.Data)
	assert.Equal(t, 4, calls)
	prices := []consumerUnitPrice{{Component: "input", Unit: "million_tokens", AmountRub: float(3)}}
	unit, price := effectivePricingPageKeys(consumerPricingModel{GroupPrices: []consumerGroupPricing{{Operations: []consumerPricingOperation{{Operation: "text", Mode: "batch", Prices: []consumerUnitPrice{{Component: "base", Unit: "call", AmountRub: float(1)}}}, {Operation: "text", Mode: "sync", Prices: prices}}}}}, "INPUT_PRICE")
	assert.Equal(t, "TOKENS", unit)
	require.NotNil(t, price)
	assert.Equal(t, 3.0, *price)
	unit, price = effectivePricingPageKeys(consumerPricingModel{}, "INPUT_PRICE")
	assert.Equal(t, "UNCONFIGURED", unit)
	assert.Nil(t, price)
}

func TestEffectivePricingPageValidationAndMetadata(t *testing.T) {
	for _, raw := range []string{"limit=101", "limit=-1", "limit=", "limit=1.2", "limit=1&limit=2", "offset=9007199254740992", "offset=-1", "offset=99999999999999999999", "creator_id=2147483648", "creator_id=x", "creator_id=1&creator_id=1", "modality=audio", "modality=TEXT&modality=IMAGE", "search=a&search=b", "pricing_unit=TASK", "order_by=PRICE", "direction=up", "group=default", "using_group=default"} {
		t.Run(raw, func(t *testing.T) {
			values, err := url.ParseQuery(raw)
			require.NoError(t, err)
			_, err = parseEffectivePricingPageQuery(values, true)
			require.Error(t, err)
		})
	}
	for _, raw := range []string{"limit=0", "limit=100", "creator_id=-2", "creator_id=0", "offset=9007199254740991", "search=%EF%BB%BFABC%EF%BB%BF"} {
		values, err := url.ParseQuery(raw)
		require.NoError(t, err)
		_, err = parseEffectivePricingPageQuery(values, true)
		require.NoError(t, err)
	}
	for _, test := range []struct {
		name      string
		endpoints []constant.EndpointType
		want      string
	}{
		{"vendor/sora-2", []constant.EndpointType{"openai"}, "VIDEO"},
		{"GPT-IMAGE-1", []constant.EndpointType{"OPENAI"}, "IMAGE"},
		{"vendor/text-embedding-3", []constant.EndpointType{"openai"}, "EMBEDDINGS"},
		{"tts-1", []constant.EndpointType{"openai"}, "OTHER"},
		{"gpt-4o-transcribe", []constant.EndpointType{"openai"}, "OTHER"},
		{"mixed", []constant.EndpointType{"audio", "rerank", "embeddings", "video", "image-generation"}, "IMAGE"},
		{"plain", nil, "TEXT"},
	} {
		assert.Equal(t, test.want, effectivePricingModality(model.Pricing{ModelName: test.name, SupportedEndpointTypes: test.endpoints}))
	}
	assert.Negative(t, compareEffectivePricingNames("A", "a"))
	assert.Negative(t, compareEffectivePricingNames("𐀀", "\ue000"))
	assert.Zero(t, compareEffectivePricingNames("İ", "İ"))
	assert.Equal(t, "abc", trimPricingText("\ufeffabc\ufeff"))
	assert.Equal(t, "\u0085", trimPricingText("\u0085"))
}

func TestEffectivePricingPageModeIsExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		query    string
		personal bool
		handled  bool
		status   int
	}{
		{"", false, false, 200}, {"?group=default", false, false, 200}, {"?group=ignored", true, false, 200},
		{"?offset=0", false, true, 400}, {"?search=", true, true, 400}, {"?using_group=default", false, true, 400},
		{"?limit=", false, true, 400}, {"?limit=1&limit=2", false, true, 400}, {"?limit=1&group=default", true, true, 400},
		{"?limit=1&using_group=default", true, true, 400}, {"?limit=1&group=auto", false, true, 400},
		{"?limit=1&using_group=not-authorized", false, true, 400},
		{"?limit=%ZZ", false, true, 400}, {"?limit=1;offset=0", false, true, 400},
		{"?limit=1&limit=%ZZ", false, true, 400}, {"?limit=1&modality=%ZZ", false, true, 400},
		{"?limit=1&group=%ZZ", true, true, 400}, {"?limit=1&using_group=%ZZ", true, true, 400},
		{"?limit=1&unknown=%ZZ", false, true, 400}, {"?%ZZ=1", false, true, 400},
		{"?%6cimit=%ZZ", false, true, 400}, {"?modality=%ZZ", true, true, 400},
		{"?group=%64efault", false, false, 200}, {"?unknown=%76alid", false, false, 200},
	} {
		t.Run(test.query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/effective"+test.query, nil)
			assert.Equal(t, test.handled, routeEffectivePricingPage(context, test.personal))
			assert.Equal(t, test.status, recorder.Code)
			if test.handled {
				assert.Contains(t, recorder.Body.String(), `"success":false`)
			}
		})
	}
}

func TestEffectivePricingPageUnicodeGolden(t *testing.T) {
	context := pricingPageFixture()
	names := []string{"éclair", "Eclair", "eclair", "Å", "a", "Z", "same", "Ä", "Ö", "same"}
	context.pricing = nil
	context.vendors = nil
	for index, name := range names {
		id := index + 1
		context.vendors = append(context.vendors, model.PricingVendor{ID: id, Name: name})
		context.pricing = append(context.pricing, model.Pricing{ModelName: name, VendorID: id, EnableGroup: []string{"default"}})
	}
	query, err := parseEffectivePricingPageQuery(url.Values{}, true)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	actual := []effectivePricingCreator{}
	actual = append(actual, page.Creators...)
	assert.Equal(t, []effectivePricingCreator{{ID: 5, Name: "a"}, {ID: 4, Name: "Å"}, {ID: 8, Name: "Ä"}, {ID: 3, Name: "eclair"}, {ID: 2, Name: "Eclair"}, {ID: 1, Name: "éclair"}, {ID: 9, Name: "Ö"}, {ID: 7, Name: "same"}, {ID: 10, Name: "same"}, {ID: 6, Name: "Z"}}, actual)
	models := []string{}
	for _, item := range page.Data {
		models = append(models, item.ModelName)
	}
	assert.Equal(t, []string{"a", "Eclair", "eclair", "same", "same", "Z", "Ä", "Å", "éclair", "Ö"}, models)
	query, err = parseEffectivePricingPageQuery(url.Values{"search": {"İ"}}, true)
	require.NoError(t, err)
	assert.Equal(t, "i\u0307", query.search)
	assert.Negative(t, compareEffectivePricingNames("ΟΣ", "οσ"))
}

func TestConsumerPricingKeepsKnownPricesOfCheapestOfferAndNativeUnits(t *testing.T) {
	offers := []model.CatalogOffer{
		{ID: "cheap-input", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: float(1)}, {Component: "output", Unit: "million_tokens", AmountUSD: float(9)}}},
		{ID: "cheap-output", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: float(7)}, {Component: "output", Unit: "million_tokens", AmountUSD: float(3)}, {Component: "cache_read", Unit: "million_tokens", AmountUSD: nil}}},
	}
	item := model.Pricing{ModelName: "coherent", EnableGroup: []string{"default", "premium"}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"all": offers}}
	result := buildConsumerPricingModel(map[string]string{"default": "Default", "premium": "Premium"}, map[string]float64{"default": 1, "premium": 2}, .8, item, false)
	require.Len(t, result.GroupPrices, 2)
	for index, multiplier := range []float64{1, 2} {
		require.Len(t, result.GroupPrices[index].Operations, 1)
		prices := result.GroupPrices[index].Operations[0].Prices
		require.Len(t, prices, 2)
		assert.InDelta(t, 7*100*.8*multiplier, *prices[0].AmountRub, 1e-9)
		assert.InDelta(t, 3*100*.8*multiplier, *prices[1].AmountRub, 1e-9)
	}
	for _, unit := range []string{"image", "megapixel", "million_tokens", "second", "call"} {
		t.Run(unit, func(t *testing.T) {
			item.CatalogOffersByGroup = map[string][]model.CatalogOffer{"default": {{ID: "native", Operation: "image_generation", Mode: "sync", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: unit, AmountUSD: float(.03)}}}}}
			group := consumerOperations(item, "default", 1, true)
			require.Len(t, group, 1)
			require.Len(t, group[0].Prices, 1)
			assert.Equal(t, unit, group[0].Prices[0].Unit)
			assert.InDelta(t, 3, *group[0].Prices[0].AmountRub, 1e-9)
		})
	}
}

func TestConsumerPricingZeroUnknownAndIncompatibleUnits(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		offers []model.CatalogOffer
		want   *float64
	}{
		{"zero", []model.CatalogOffer{{ID: "free", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: float(0)}}}, {ID: "paid", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: float(1)}}}}, float(0)},
		{"unknown", []model.CatalogOffer{{ID: "unknown", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: nil}}}}, nil},
		{"different native units", []model.CatalogOffer{{ID: "image", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: float(1)}}}, {ID: "token", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "million_tokens", AmountUSD: float(.001)}}}}, nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for index := range scenario.offers {
				scenario.offers[index].Operation = "image_generation"
				scenario.offers[index].Mode = "sync"
			}
			item := model.Pricing{CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": scenario.offers}}
			rows := consumerOperations(item, "default", 1, true)
			require.Len(t, rows, 1)
			if scenario.want == nil {
				assert.Empty(t, rows[0].Prices)
			} else {
				require.Len(t, rows[0].Prices, 1)
				require.NotNil(t, rows[0].Prices[0].AmountRub)
				assert.Equal(t, *scenario.want, *rows[0].Prices[0].AmountRub)
			}
		})
	}
}

func TestConsumerPricingOmitsUnpricedComponentsWithoutChangingSource(t *testing.T) {
	for _, operation := range []string{"transcription", "speech_generation", "text", "image_generation", "text_to_video"} {
		t.Run(operation, func(t *testing.T) {
			prices := []model.CatalogPrice{
				{Component: "input", Unit: "million_tokens", AmountUSD: float(.6)},
				{Component: "audio_output", Unit: "unknown"},
				{Component: "output", Unit: "million_tokens"},
				{Component: "cache_read", Unit: "million_tokens", AmountUSD: float(0)},
				{Component: "image_input", Unit: "unknown", AmountUSD: float(1)},
				{Component: "image_output", Unit: "", AmountUSD: float(1)},
				{Component: "reference_input", Unit: "image", AmountUSD: float(math.NaN())},
				{Component: "additional_reference_input", Unit: "image", AmountUSD: float(math.Inf(1))},
				{Component: "font_input", Unit: "image", AmountUSD: float(-1)},
			}
			item := model.Pricing{CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": {{ID: "source", Operation: operation, Mode: "sync", MainComponent: "input", Prices: prices}}}}
			rows := consumerOperations(item, "default", 2, true)
			require.Len(t, rows, 1)
			assert.Equal(t, []consumerUnitPrice{{Component: "input", Unit: "million_tokens", AmountRub: float(120)}, {Component: "cache_read", Unit: "million_tokens", AmountRub: float(0)}}, rows[0].Prices)
			require.Len(t, item.CatalogOffersByGroup["default"][0].Prices, len(prices))
			assert.Nil(t, item.CatalogOffersByGroup["default"][0].Prices[1].AmountUSD)
		})
	}
}

func TestConsumerPricingPageUsesNativeChannelVideoPluginBinding(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousRegistry := jsplugin.DefaultRegistry
	previousMemoryCache := common.MemoryCacheEnabled
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		jsplugin.DefaultRegistry = previousRegistry
		common.MemoryCacheEnabled = previousMemoryCache
		model.InvalidatePricingCache()
	})
	source, err := os.ReadFile("../plugins/tasks/openrouter-video/plugin.js")
	require.NoError(t, err)
	_, err = jsplugin.DefaultRegistry.Register(string(source), jsplugin.Options{})
	require.NoError(t, err)
	mapping := `{"heygen-alias":"heygen/heygen-video-1"}`
	require.NoError(t, db.Create(&model.Channel{Id: 950, Type: constant.ChannelTypeOpenRouter, Status: 1, Key: "fixture", Models: "heygen-alias", ModelMapping: &mapping}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "heygen-alias", ChannelId: 950, Enabled: true}).Error)
	model.InitChannelCache()
	model.RefreshPricing()
	context := effectivePricingPageContext{metadata: consumerPricingResponse{SchemaVersion: "v2", UserGroup: "default", Currency: "RUB"}, fxFactor: 1, pricing: model.GetPricing(), usableGroups: map[string]string{"default": "Default"}, groupRatios: map[string]float64{"default": 1}, usingGroup: "default"}
	query, err := parseEffectivePricingPageQuery(url.Values{"limit": {"20"}, "modality": {"VIDEO"}}, false)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	require.Equal(t, 1, page.TotalCount)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "heygen-alias", page.Data[0].ModelName)
	require.Len(t, page.Data[0].GroupPrices, 1)
	operations := make(map[string]consumerPricingOperation)
	for _, operation := range page.Data[0].GroupPrices[0].Operations {
		operations[operation.Operation] = operation
	}
	// First-frame I2V uses ordinary output pricing; reference video uses
	// the refreshed reference-output tariff, independently of the image allowance.
	for operation, amountRub := range map[string]float64{"text_to_video": 2, "image_to_video": 2, "video_to_video": 7} {
		require.Contains(t, operations, operation)
		prices := operations[operation].Prices
		require.Len(t, prices, 1)
		assert.Equal(t, "second", prices[0].Unit)
		require.NotNil(t, prices[0].AmountRub)
		assert.InDelta(t, amountRub, *prices[0].AmountRub, 1e-9)
	}
}

func TestPricingCatalogNormalizeCoherentEndpointVariants(t *testing.T) {
	body := []byte(`{"data":{"id":"provider/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"tag":"a","context_length":100,"pricing":{"prompt":"0.000001","completion":"0.000009","input_cache_read":"0","overrides":[{"min_prompt_tokens":0,"prompt":"0.000007","completion":"0.000003"},{"min_prompt_tokens":200,"completion":"0.0000001"}]}},{"tag":"b","pricing":{"prompt":"0.0000001","completion":"0.000008"}}]}}`)
	offers, err := normalizeEndpointPricing("provider/model", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 3)
	reduced := selectPricingCatalogOffers(offers)
	require.Len(t, reduced, 1)
	selected := model.SelectCatalogOffer(reduced)
	require.NotNil(t, selected)
	prices := map[string]*float64{}
	for _, price := range selected.Prices {
		prices[price.Component] = price.AmountUSD
	}
	assert.InDelta(t, 7, *prices["input"], 1e-9)
	assert.InDelta(t, 3, *prices["output"], 1e-9)
	assert.Equal(t, 0.0, *prices["cache_read"])
}

func TestPricingCatalogReductionPreservesSelection(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		offers []model.CatalogOffer
	}{
		{"tie", []model.CatalogOffer{{ID: "z", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(1)}, {Component: "input", Unit: "million_tokens", AmountUSD: float(2)}}}, {ID: "a", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(1)}, {Component: "input", Unit: "million_tokens", AmountUSD: float(9)}}}}},
		{"zero_unknown", []model.CatalogOffer{{ID: "null", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens"}}}, {ID: "zero", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(0)}}}}},
		{"unlike_units", []model.CatalogOffer{{ID: "image", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: float(1)}}}, {ID: "mp", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "megapixel", AmountUSD: float(.01)}}}}},
		{"unknown_main", []model.CatalogOffer{{ID: "null", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: float(0)}}}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for index := range scenario.offers {
				scenario.offers[index].Operation = "text"
				scenario.offers[index].Mode = "sync"
			}
			before := model.SelectCatalogOffer(scenario.offers)
			after := model.SelectCatalogOffer(selectPricingCatalogOffers(scenario.offers))
			if after != nil {
				require.NotEmpty(t, after.PublicAlternatives)
				copied := *after
				copied.PublicAlternatives = nil
				after = &copied
			}
			assert.Equal(t, before, after)
		})
	}
}

func TestPricingCatalogNativeImageUnitsAndUnknownVariants(t *testing.T) {
	for _, scenario := range []struct {
		name, body, unit string
		amount           *float64
	}{
		{"first_megapixel", `{"id":"provider/image","endpoints":[{"provider_tag":"native","pricing":[{"billable":"output_image","unit":"megapixel","cost_usd":0.03}]}]}`, "megapixel", float(.03)},
		{"token", `{"id":"provider/image","endpoints":[{"pricing":[{"billable":"output_image","unit":"token","cost_usd":0.00003}]}]}`, "million_tokens", float(30)},
		{"free", `{"id":"provider/image","endpoints":[{"pricing":[{"billable":"output_image","unit":"image","cost_usd":0}]}]}`, "image", float(0)},
		{"opaque_variant", `{"id":"provider/image","endpoints":[{"pricing":[{"billable":"output_image","unit":"image","cost_usd":0.001,"variant":"opaque"}]}]}`, "", nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			offers, err := normalizeImagePricing("provider/image", []byte(scenario.body))
			require.NoError(t, err)
			selected := model.SelectCatalogOffer(selectPricingCatalogOffers(offers))
			if scenario.amount == nil {
				assert.Nil(t, selected)
				return
			}
			require.NotNil(t, selected)
			require.Len(t, selected.Prices, 1)
			assert.Equal(t, scenario.unit, selected.Prices[0].Unit)
			assert.InDelta(t, *scenario.amount, *selected.Prices[0].AmountUSD, 1e-9)
		})
	}
	_, err := normalizeImagePricing("provider/image", []byte(`{"id":"provider/image","endpoints":[{"pricing":[{"billable":"output_image","unit":"image"}]}]}`))
	require.Error(t, err)
}

func TestPricingCatalogRefreshPartialFailureAndCancellation(t *testing.T) {
	sources := make([]pricingCatalogSource, 12)
	for index := range sources {
		sources[index].upstream = string(rune('a' + index))
	}
	var calls, published, publications, active, maximum atomic.Int64
	gate := make(chan struct{})
	ready := make(chan struct{}, pricingCatalogWorkers)
	done := make(chan pricingCatalogRefreshSummary, 1)
	go func() {
		summary, err := refreshPricingCatalogSources(context.Background(), sources, func(ctx context.Context, source pricingCatalogSource) (model.PricingCatalogEntry, error) {
			count := active.Add(1)
			defer active.Add(-1)
			for previous := maximum.Load(); count > previous && !maximum.CompareAndSwap(previous, count); previous = maximum.Load() {
			}
			if calls.Add(1) <= pricingCatalogWorkers {
				ready <- struct{}{}
				<-gate
			}
			if source.upstream == "a" {
				return model.PricingCatalogEntry{}, errors.New("fixture failed metadata")
			}
			return model.PricingCatalogEntry{UpstreamModel: source.upstream}, nil
		}, func(entries []model.PricingCatalogEntry) (int, error) {
			publications.Add(1)
			published.Add(int64(len(entries)))
			return len(entries), nil
		})
		assert.ErrorContains(t, err, "failed for 1 of 12 sources")
		done <- summary
	}()
	for range pricingCatalogWorkers {
		<-ready
	}
	close(gate)
	summary := <-done
	assert.Equal(t, 11, summary.Published)
	assert.Equal(t, 1, summary.Failed)
	assert.EqualValues(t, pricingCatalogWorkers, maximum.Load())
	assert.EqualValues(t, 11, published.Load())
	assert.EqualValues(t, 1, publications.Load(), "all successful metadata must commit together")
	for _, failure := range []string{"fetch", "persist"} {
		t.Run("all_"+failure+"_fail", func(t *testing.T) {
			summary, err := refreshPricingCatalogSources(context.Background(), sources, func(context.Context, pricingCatalogSource) (model.PricingCatalogEntry, error) {
				if failure == "fetch" {
					return model.PricingCatalogEntry{}, errors.New("metadata unavailable")
				}
				return model.PricingCatalogEntry{}, nil
			}, func([]model.PricingCatalogEntry) (int, error) {
				if failure == "fetch" {
					t.Error("failed fetch was published")
				}
				return 0, errors.New("persistence unavailable")
			})
			require.ErrorContains(t, err, "failed for 12 of 12 sources")
			assert.Equal(t, 12, summary.Failed)
			assert.Zero(t, summary.Published)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := refreshPricingCatalogSources(ctx, sources, func(ctx context.Context, source pricingCatalogSource) (model.PricingCatalogEntry, error) {
		return model.PricingCatalogEntry{}, ctx.Err()
	}, func([]model.PricingCatalogEntry) (int, error) { t.Fatal("cancelled task published"); return 0, nil })
	require.ErrorIs(t, err, context.Canceled)
}

func TestConsumerPricingUsesProjectedSnapshotOnly(t *testing.T) {
	item := tieredPricing(`tier("base",p*2+c*3)`)
	item.CatalogOffersByGroup = map[string][]model.CatalogOffer{"default": {{ID: "actual", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(7)}}}}}
	rows := consumerOperations(item, "default", 1, true)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].Prices, 1)
	assert.Equal(t, 700.0, *rows[0].Prices[0].AmountRub)
	item.BillingExpr = `tier("changed",p*200+c*300)`
	item.ConfiguredPrices = nil
	assert.Equal(t, rows, consumerOperations(item, "default", 1, true))
}

type pricingCatalogTestTransport func(*http.Request) (*http.Response, error)

func (transport pricingCatalogTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestPricingCatalogFetchUsesBoundedMetadataAndConsumerDoesNoHTTP(t *testing.T) {
	client := service.GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	previousTransport := client.Transport
	t.Cleanup(func() { client.Transport = previousTransport })
	var attempts atomic.Int64
	body := `{"data":{"id":"provider/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"tag":"native","pricing":{"prompt":"0.000001","completion":"0.000002","internal_reasoning":"0.000003","audio_output":"0.0005","web_search":"0.01"}}]}}`
	client.Transport = pricingCatalogTestTransport(func(request *http.Request) (*http.Response, error) {
		attempts.Add(1)
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Equal(t, "https://openrouter.ai/api/v1/models/provider/model/endpoints", request.URL.String())
		assert.Equal(t, "Bearer secret-fixture", request.Header.Get("Authorization"))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	channel := &model.Channel{Id: 990, Type: constant.ChannelTypeOpenRouter, Status: common.ChannelStatusEnabled, Key: "secret-fixture", Models: "provider/model"}
	source := pricingCatalogSource{channel: channel, upstream: "provider/model", endpoint: constant.EndpointTypeOpenAI, fingerprint: model.PricingCatalogSourceFingerprint(channel, "provider/model", constant.EndpointTypeOpenAI)}
	entry, err := fetchPricingCatalogSource(context.Background(), source)
	require.NoError(t, err)
	assert.EqualValues(t, 1, attempts.Load())
	require.Len(t, entry.Offers, 1)
	prices := map[string]model.CatalogPrice{}
	for _, price := range entry.Offers[0].Prices {
		prices[price.Component] = price
	}
	assert.Equal(t, "million_tokens", prices["reasoning_output"].Unit)
	assert.Equal(t, 3.0, *prices["reasoning_output"].AmountUSD)
	assert.Equal(t, "search", prices["web_search"].Unit)
	assert.Equal(t, .01, *prices["web_search"].AmountUSD)
	assert.NotContains(t, prices, "audio_output")
	item := model.Pricing{ModelName: "provider/model", EnableGroup: []string{"default"}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": entry.Offers}}
	for range 3 {
		rows := buildConsumerPricingModel(map[string]string{"default": "Default"}, map[string]float64{"default": 1}, 1, item, false)
		require.Len(t, rows.GroupPrices[0].Operations, 1)
	}
	assert.EqualValues(t, 1, attempts.Load())
	body = strings.Repeat("x", pricingCatalogBodyLimit+1)
	_, err = fetchPricingCatalogSource(context.Background(), source)
	require.ErrorContains(t, err, "body limit")
	started := make(chan struct{})
	client.Transport = pricingCatalogTestTransport(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := refreshPricingCatalogSources(ctx, []pricingCatalogSource{source}, fetchPricingCatalogSource, func([]model.PricingCatalogEntry) (int, error) {
			t.Error("cancelled metadata was published")
			return 0, nil
		})
		done <- err
	}()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestPricingCatalogNativeTextRequiresArchitectureProof(t *testing.T) {
	for _, architecture := range []string{`{"input_modalities":["text"],"output_modalities":["audio"]}`, `{"input_modalities":["text"],"output_modalities":["image"]}`} {
		body := []byte(`{"data":{"id":"provider/model","architecture":` + architecture + `,"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002"}}]}}`)
		offers, err := normalizeEndpointPricing("provider/model", "text", "output", body)
		require.NoError(t, err)
		assert.Empty(t, offers)
	}
}

func TestPricingCatalogImageFontInputPreservesNativeCoherentPrice(t *testing.T) {
	body := []byte(`{"id":"sourceful/riverflow","endpoints":[{"provider_tag":"sourceful","pricing":[{"billable":"output_image","unit":"image","cost_usd":0.06},{"billable":"input_font","unit":"image","cost_usd":0.03}]}]}`)
	offers, err := normalizeImagePricing("sourceful/riverflow", body)
	require.NoError(t, err)
	selected := model.SelectCatalogOffer(selectPricingCatalogOffers(offers))
	require.NotNil(t, selected)
	require.Len(t, selected.Prices, 2)
	assert.Equal(t, "image_output", selected.Prices[0].Component)
	assert.Equal(t, 0.06, *selected.Prices[0].AmountUSD)
	assert.Equal(t, "font_input", selected.Prices[1].Component)
	assert.Equal(t, "image", selected.Prices[1].Unit)
	assert.Equal(t, 0.03, *selected.Prices[1].AmountUSD)
	item := model.Pricing{CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": {*selected}}}
	rows := consumerOperations(item, "default", 1, true)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].Prices, 2)
	for _, price := range rows[0].Prices {
		if price.Component == "font_input" {
			assert.Equal(t, "image", price.Unit)
			require.NotNil(t, price.AmountRub)
			assert.Equal(t, 3.0, *price.AmountRub)
			return
		}
	}
	t.Fatal("font_input native price lost in consumer projection")
}

func TestConsumerPricingNativeVideoOperationsMatchBoundSourceDescriptors(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
		model.InvalidatePricingCache()
	})
	prices := map[string]float64{}
	require.NoError(t, common.UnmarshalJsonStr(previousPrices, &prices))
	prices["sora-2"] = .3
	encoded, err := common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(encoded)))
	previousRegistry, previousMemoryCache := jsplugin.DefaultRegistry, common.MemoryCacheEnabled
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		jsplugin.DefaultRegistry = previousRegistry
		common.MemoryCacheEnabled = previousMemoryCache
		model.InvalidatePricingCache()
	})
	for _, path := range []string{"../plugins/tasks/openrouter-video/plugin.js", "../plugins/tasks/sora/plugin.js"} {
		source, err := os.ReadFile(path)
		require.NoError(t, err)
		_, err = jsplugin.DefaultRegistry.Register(string(source), jsplugin.Options{})
		require.NoError(t, err)
	}
	mapping := `{"aleph-alias":"runway/aleph-2","avatar-alias":"heygen/avatar-iv","edit-alias":"black-forest-labs/flux-video-edit","flux-alias":"black-forest-labs/flux-3-video","seedance-alias":"bytedance/seedance-2.5","upscale-alias":"black-forest-labs/flux-video-upscale"}`
	names := []string{"aleph-alias", "avatar-alias", "edit-alias", "flux-alias", "seedance-alias", "upscale-alias"}
	require.NoError(t, db.Create(&model.Channel{Id: 991, Type: constant.ChannelTypeOpenRouter, Status: 1, Key: "fixture", Models: strings.Join(names, ","), ModelMapping: &mapping}).Error)
	for _, name := range names {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: 991, Enabled: true}).Error)
	}
	require.NoError(t, db.Create(&model.Channel{Id: 992, Type: constant.ChannelTypeOpenAI, Status: 1, Key: "fixture", Models: "sora-2"}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "sora-2", ChannelId: 992, Enabled: true}).Error)
	model.InitChannelCache()
	model.RefreshPricing()
	items := map[string]model.Pricing{}
	for _, item := range model.GetPricing() {
		items[item.ModelName] = item
	}
	for _, name := range append(names, "sora-2") {
		item, exists := items[name]
		require.True(t, exists)
		rows := consumerOperations(item, "default", 1, true)
		operations := map[string]consumerPricingOperation{}
		for _, row := range rows {
			operations[row.Operation] = row
			assert.NotEqual(t, "text", row.Operation, name)
		}
		switch name {
		case "aleph-alias":
			require.Len(t, operations, 1)
			require.Contains(t, operations, "video_editing")
			require.Len(t, operations["video_editing"].Prices, 1)
			assert.Equal(t, "second", operations["video_editing"].Prices[0].Unit)
			assert.InDelta(t, 28.0, *operations["video_editing"].Prices[0].AmountRub, 1e-9)
		case "upscale-alias":
			require.Len(t, operations, 1)
			require.Contains(t, operations, "video_upscaling")
			require.Len(t, operations["video_upscaling"].Prices, 1)
			assert.Equal(t, "megapixel_second", operations["video_upscaling"].Prices[0].Unit)
			assert.InDelta(t, 7.5, *operations["video_upscaling"].Prices[0].AmountRub, 1e-9)
		case "avatar-alias":
			require.Len(t, operations, 1)
			require.Contains(t, operations, "image_to_video")
		case "edit-alias":
			require.Len(t, operations, 1)
			require.Contains(t, operations, "video_editing")
		case "flux-alias", "seedance-alias":
			require.Contains(t, operations, "text_to_video")
			require.Contains(t, operations, "image_to_video")
		case "sora-2":
			require.Len(t, operations, 3)
			require.Contains(t, operations, "remix")
			require.Len(t, operations["remix"].Prices, 1)
			assert.Equal(t, "call", operations["remix"].Prices[0].Unit)
			assert.Equal(t, 30.0, *operations["remix"].Prices[0].AmountRub)
		}
	}
	// A genuinely separate text route sharing an alias remains independently supported.
	require.NoError(t, db.Create(&model.Channel{Id: 993, Type: constant.ChannelTypeOpenAI, Status: 1, Key: "fixture", Models: "edit-alias"}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "edit-alias", ChannelId: 993, Enabled: true}).Error)
	model.InitChannelCache()
	model.RefreshPricing()
	for _, item := range model.GetPricing() {
		if item.ModelName == "edit-alias" {
			rows := consumerOperations(item, "default", 1, true)
			operations := []string{}
			for _, row := range rows {
				operations = append(operations, row.Operation)
			}
			assert.Contains(t, operations, "text")
			assert.Contains(t, operations, "video_editing")
		}
	}
}

func TestPricingCatalogVariantIdentityAndMixedOutputBasis(t *testing.T) {
	for _, suffix := range []string{":batch", ":free"} {
		upstream := "provider/model" + suffix
		body := []byte(`{"data":{"id":"` + upstream + `","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"model_id":"` + upstream + `","pricing":{"prompt":"0.000005","completion":"0.000025"}}]}}`)
		offers, err := normalizeEndpointPricing(upstream, "text", "output", body)
		require.NoError(t, err)
		selected := model.SelectCatalogOffer(offers)
		require.NotNil(t, selected)
		assert.Equal(t, "output", selected.Prices[0].Component)
		assert.Equal(t, 25.0, *selected.Prices[0].AmountUSD)
		mismatched := strings.Replace(string(body), `"model_id":"`+upstream+`"`, `"model_id":"provider/model"`, 1)
		_, err = normalizeEndpointPricing(upstream, "text", "output", []byte(mismatched))
		require.ErrorContains(t, err, "identity")
	}
	for _, output := range []string{"image", "audio"} {
		dimension := output + "_output"
		body := []byte(`{"data":{"id":"provider/model","architecture":{"input_modalities":["text"],"output_modalities":["text","` + output + `"]},"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002","` + dimension + `":"0.00003"}}]}}`)
		offers, err := normalizeEndpointPricing("provider/model", "text", "output", body)
		require.NoError(t, err)
		require.Len(t, offers, 1)
		assert.Equal(t, "text", offers[0].Operation)
		if output == "audio" {
			require.NotNil(t, model.SelectCatalogOffer(offers))
			assert.Contains(t, offers[0].Prices, model.CatalogPrice{Component: "audio_output", Unit: "million_tokens", AmountUSD: float(30)})
		} else {
			require.NotNil(t, model.SelectCatalogOffer(offers))
			assert.Contains(t, offers[0].Prices, model.CatalogPrice{Component: "input", Unit: "million_tokens", AmountUSD: float(1)})
			assert.Contains(t, offers[0].Prices, model.CatalogPrice{Component: "output", Unit: "million_tokens", AmountUSD: float(2)})
		}
	}
	body := []byte(`{"data":{"id":"provider/model:free","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[]}}`)
	offers, err := normalizeEndpointPricing("provider/model:free", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 1)
	assert.Nil(t, model.SelectCatalogOffer(offers))
	_, err = normalizeEndpointPricing("provider/model", "text", "output", []byte(`{"data":{"id":"provider/model","architecture":{},"endpoints":[]}}`))
	require.ErrorContains(t, err, "operation")
	for _, fee := range []string{"0", "0.0001"} {
		body := []byte(`{"data":{"id":"provider/model","architecture":{"input_modalities":["text"],"output_modalities":["text","image"]},"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002","image_output":"0.00003","unknown_fee":"` + fee + `"}}]}}`)
		offers, err := normalizeEndpointPricing("provider/model", "text", "output", body)
		require.NoError(t, err)
		if fee == "0" {
			require.NotNil(t, model.SelectCatalogOffer(offers))
		} else {
			assert.Nil(t, model.SelectCatalogOffer(offers))
		}
	}

}

func TestPricingCatalogSourceJobsUseNativeCapabilitiesAndExactBatch(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemory
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		model.InvalidatePricingCache()
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.batch_billing_expr": `{"fable:batch":"tier(\"batch\",provider_cost)"}`}))
	mapping := `{"image-alias":"sourceful/riverflow","fable":"anthropic/claude-fable-5.1","fable:batch":"anthropic/claude-fable-5.1:batch","free":"qwen/qwen3.5-9b:free","unsupported":"provider/model:nitro"}`
	require.NoError(t, db.Create(&model.Channel{Id: 991, Type: constant.ChannelTypeOpenRouter, Status: 1, Key: "fixture", Models: "image-alias,fable,fable:batch,free,unsupported", ModelMapping: &mapping}).Error)
	for _, name := range []string{"image-alias", "fable", "fable:batch", "free", "unsupported"} {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: 991, Enabled: true}).Error)
	}
	model.InitChannelCache()
	model.RefreshPricing()
	client := service.GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	previousTransport := client.Transport
	t.Cleanup(func() { client.Transport = previousTransport })
	attempts := 0
	client.Transport = pricingCatalogTestTransport(func(request *http.Request) (*http.Response, error) {
		attempts++
		require.Equal(t, "https://openrouter.ai/api/v1/images/models", request.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"sourceful/riverflow"}]}`)), Header: make(http.Header)}, nil
	})
	sources, err := pricingCatalogSources(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, attempts)
	found := map[string]map[constant.EndpointType]bool{}
	for _, source := range sources {
		if found[source.upstream] == nil {
			found[source.upstream] = map[constant.EndpointType]bool{}
		}
		found[source.upstream][source.endpoint] = true
	}
	assert.True(t, found["sourceful/riverflow"][constant.EndpointTypeImageGeneration])
	assert.Equal(t, map[constant.EndpointType]bool{model.PricingEndpointBatch: true}, found["anthropic/claude-fable-5.1:batch"])
	assert.False(t, found["anthropic/claude-fable-5.1"][model.PricingEndpointBatch])
	assert.NotEmpty(t, found["qwen/qwen3.5-9b:free"])
	assert.NotContains(t, found, "provider/model:nitro")
	client.Transport = pricingCatalogTestTransport(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "https://openrouter.ai/api/v1/models/anthropic/claude-fable-5.1:batch/endpoints", request.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":{"id":"anthropic/claude-fable-5.1:batch","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"model_id":"anthropic/claude-fable-5.1:batch","pricing":{"prompt":"0.000005","completion":"0.000025"}}]}}`)), Header: make(http.Header)}, nil
	})
	for _, source := range sources {
		if source.endpoint != model.PricingEndpointBatch {
			continue
		}
		entry, err := fetchPricingCatalogSource(context.Background(), source)
		require.NoError(t, err)
		require.Len(t, entry.Offers, 1)
		assert.Equal(t, "batch", entry.Offers[0].Mode)
		assert.Equal(t, 25.0, *entry.Offers[0].Prices[0].AmountUSD)
	}
}

func TestEffectivePricingPageFoldsBatchBeforePagingAndKeepsGroupFX(t *testing.T) {
	context := effectivePricingPageContext{
		metadata:     consumerPricingResponse{SchemaVersion: "v2", Currency: "RUB", UserGroup: "default"},
		usableGroups: map[string]string{"default": "Default", "premium": "Premium"}, groupRatios: map[string]float64{"default": 2, "premium": 3}, fxFactor: .5,
		pricing: []model.Pricing{
			{ModelName: "canonical", EnableGroup: []string{"default"}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": {{ID: "sync", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(10)}}}}}},
			{ModelName: "canonical:batch", EnableGroup: []string{"default", "premium"}, ConsumerCanonicalModelsByGroup: map[string]string{"default": "canonical"}, CatalogOffersByGroup: map[string][]model.CatalogOffer{
				"default": {{ID: "batch-default", Operation: "text", Mode: "batch", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(7)}}}},
				"premium": {{ID: "batch-premium", Operation: "text", Mode: "batch", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: float(9)}}}},
			}},
		},
	}
	query, err := parseEffectivePricingPageQuery(url.Values{"limit": {"1"}}, true)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	require.Equal(t, 2, page.TotalCount)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "canonical", page.Data[0].ModelName)
	groups := page.Data[0].GroupPrices
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Operations, 2)
	assert.Equal(t, "batch", groups[0].Operations[0].Mode)
	assert.Equal(t, 700.0, *groups[0].Operations[0].Prices[0].AmountRub)
	query.offset = 1
	page = buildEffectivePricingPage(context, query, context.project)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "canonical:batch", page.Data[0].ModelName)
	require.Len(t, page.Data[0].GroupPrices, 1)
	assert.Equal(t, "premium", page.Data[0].GroupPrices[0].UsingGroup)
	assert.Equal(t, 1350.0, *page.Data[0].GroupPrices[0].Operations[0].Prices[0].AmountRub)
	context.usableGroups = map[string]string{"default": "Default"}
	query.offset = 0
	page = buildEffectivePricingPage(context, query, context.project)
	assert.Equal(t, 1, page.TotalCount)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "canonical", page.Data[0].ModelName)
	assert.Len(t, context.pricing, 2)
	assert.Equal(t, "canonical:batch", context.pricing[1].ModelName)
}

func TestEffectivePricingPageUsesProvedAliasOperationsForModality(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemory
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		model.InvalidatePricingCache()
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": `{"semantic-index":"tiered_expr"}`, "billing_setting.billing_expr": `{"semantic-index":"tier(\"base\",p*.13)"}`}))
	mapping := `{"semantic-index":"text-embedding-3-large"}`
	require.NoError(t, db.Create(&model.Channel{Id: 993, Type: constant.ChannelTypeOpenAI, Status: 1, Key: "fixture", Models: "semantic-index", ModelMapping: &mapping}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "semantic-index", ChannelId: 993, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Vendor{Id: 11, Name: "Embedding source"}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "semantic-index", VendorID: 11}).Error)
	model.InitChannelCache()
	model.RefreshPricing()
	items := model.GetPricing()
	require.Len(t, items, 1)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, items[0].SupportedEndpointTypes)
	imageOffers, err := normalizeImagePricing("sourceful/riverflow", []byte(`{"id":"sourceful/riverflow","endpoints":[{"provider_tag":"sourceful","pricing":[{"billable":"output_image","unit":"image","cost_usd":0.06}]}]}`))
	require.NoError(t, err)
	textOffers, err := normalizeEndpointPricing("google/gemini-image", "text", "output", []byte(`{"data":{"id":"google/gemini-image","architecture":{"input_modalities":["text"],"output_modalities":["text","image"]},"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002","image_output":"0.00003"}}]}}`))
	require.NoError(t, err)
	items = append(items,
		model.Pricing{ModelName: "visual-route", VendorID: 12, EnableGroup: []string{"default"}, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": imageOffers}},
		model.Pricing{ModelName: "mixed-route", VendorID: 12, EnableGroup: []string{"default"}, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": append(append([]model.CatalogOffer{}, imageOffers...), textOffers...)}},
	)
	context := effectivePricingPageContext{metadata: consumerPricingResponse{SchemaVersion: "v2", Currency: "RUB", UserGroup: "default"}, pricing: items, usableGroups: map[string]string{"default": "Default"}, groupRatios: map[string]float64{"default": 1}, fxFactor: 1, vendors: []model.PricingVendor{{ID: 11, Name: "Embedding source"}, {ID: 12, Name: "Image source"}}}
	query, err := parseEffectivePricingPageQuery(url.Values{"modality": {"EMBEDDINGS"}, "limit": {"1"}}, true)
	require.NoError(t, err)
	page := buildEffectivePricingPage(context, query, context.project)
	require.Equal(t, 1, page.TotalCount)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "semantic-index", page.Data[0].ModelName)
	assert.Equal(t, "EMBEDDINGS", page.Data[0].Modality)
	assert.Equal(t, "embeddings", page.Data[0].GroupPrices[0].Operations[0].Operation)
	assert.Equal(t, []effectivePricingCreator{{ID: 11, Name: "Embedding source"}, {ID: 12, Name: "Image source"}}, page.Creators)
	query.modality = "IMAGE"
	query.limit = 20
	page = buildEffectivePricingPage(context, query, context.project)
	require.Equal(t, 2, page.TotalCount)
	require.Len(t, page.Data, 2)
	assert.Equal(t, "mixed-route", page.Data[0].ModelName)
	assert.Equal(t, "visual-route", page.Data[1].ModelName)
	operations := page.Data[0].GroupPrices[0].Operations
	require.Len(t, operations, 2)
	assert.Equal(t, "image_generation", operations[0].Operation)
	assert.Equal(t, "text", operations[1].Operation)
	query.creatorID = new(int)
	*query.creatorID = 11
	page = buildEffectivePricingPage(context, query, context.project)
	assert.Zero(t, page.TotalCount)
	assert.Empty(t, page.Data)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, items[0].SupportedEndpointTypes)
}

func TestPricingRevisionIncludesCapturedContextAndGuardsBeforePage(t *testing.T) {
	context := pricingPageFixture()
	context.metadata.UserGroup = "default"
	revision, err := consumerPricingRevision("source", context)
	require.NoError(t, err)
	require.NotEmpty(t, revision)
	permuted := context
	permuted.usableGroups = map[string]string{"premium": "Premium", "default": "Default"}
	permuted.groupRatios = map[string]float64{"premium": 2, "default": 1}
	same, err := consumerPricingRevision("source", permuted)
	require.NoError(t, err)
	assert.Equal(t, revision, same)
	for _, change := range []func(*effectivePricingPageContext){
		func(value *effectivePricingPageContext) { value.fxFactor = 2 },
		func(value *effectivePricingPageContext) {
			value.groupRatios = map[string]float64{"default": 3, "premium": 2}
		},
		func(value *effectivePricingPageContext) { value.usableGroups = map[string]string{"default": "Default"} },
		func(value *effectivePricingPageContext) { value.metadata.AutoGroups = []string{"premium"} },
	} {
		changed := context
		change(&changed)
		current, err := consumerPricingRevision("source", changed)
		require.NoError(t, err)
		assert.NotEqual(t, revision, current)
	}
	context.metadata.CatalogRevision = revision
	for _, limit := range []string{"0", "7"} {
		query, err := parseEffectivePricingPageQuery(url.Values{"limit": {limit}, "expected_revision": {revision}}, true)
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		ginContext, _ := gin.CreateTestContext(recorder)
		require.True(t, pricingRevisionMatches(ginContext, query.expectedRevision, context.metadata.CatalogRevision))
		page := buildEffectivePricingPage(context, query, context.project)
		assert.Equal(t, revision, page.CatalogRevision)
		assert.Equal(t, 4, page.TotalCount)
	}
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	assert.False(t, pricingRevisionMatches(ginContext, "stale", revision))
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.JSONEq(t, `{"success":false,"code":"pricing_catalog_revision_mismatch","message":"Pricing catalog revision changed"}`, recorder.Body.String())
	for _, raw := range []string{"expected_revision=", "expected_revision=a&expected_revision=b"} {
		values, err := url.ParseQuery(raw)
		require.NoError(t, err)
		_, err = expectedPricingRevision(values)
		require.Error(t, err)
	}
}

func TestPricingCatalogGeminiKeepsWholeAudioVectorsAndContextOverrides(t *testing.T) {
	body := []byte(`{"data":{"id":"google/gemini-2.5-pro:batch","architecture":{"input_modalities":["text","audio"],"output_modalities":["text"]},"endpoints":[{"model_id":"google/gemini-2.5-pro:batch","tag":"google","pricing":{"prompt":"0.000000625","completion":"0.000005","audio":"0.000000625","input_audio_cache":"0.000000125","input_cache_read":"0.000000125","overrides":[{"min_prompt_tokens":200000,"prompt":"0.00000125","completion":"0.0000075","audio":"0.00000125","input_audio_cache":"0.00000025"}]}}]}}`)
	offers, err := normalizeEndpointPricing("google/gemini-2.5-pro:batch", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 2)
	for index, offer := range offers {
		prices := map[string]float64{}
		for _, price := range offer.Prices {
			require.NotNil(t, price.AmountUSD)
			assert.Equal(t, "million_tokens", price.Unit)
			prices[price.Component] = *price.AmountUSD
		}
		factor := float64(index + 1)
		assert.Equal(t, .625*factor, prices["input"])
		assert.Equal(t, .625*factor, prices["audio_input"])
		assert.Equal(t, .125*factor, prices["audio_cache_read"])
		assert.NotContains(t, prices, "audio_output")
	}
	selected := model.SelectCatalogOffer(offers)
	require.NotNil(t, selected)
	assert.Equal(t, offers[0], *selected)
	item := model.Pricing{CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": offers}}
	rows := consumerOperations(item, "default", 1.2, true)
	require.Len(t, rows, 1)
	audioCache := []consumerUnitPrice{}
	for _, price := range rows[0].Prices {
		if price.Component == "audio_cache_read" {
			audioCache = append(audioCache, price)
		}
	}
	require.Len(t, audioCache, 1)
	assert.Equal(t, 15.0, *audioCache[0].AmountRub)
	zeroBody := strings.Replace(string(body), `"audio":"0.000000625"`, `"audio":"0"`, 1)
	zero, err := normalizeEndpointPricing("google/gemini-2.5-pro:batch", "text", "output", []byte(zeroBody))
	require.NoError(t, err)
	require.NotNil(t, model.SelectCatalogOffer(zero))
	for _, price := range zero[0].Prices {
		if price.Component == "audio_input" {
			assert.Zero(t, *price.AmountUSD)
		}
	}
}

func TestPricingCatalogCalendarOverridesAreReachableWholeVectors(t *testing.T) {
	body := []byte(`{"data":{"id":"provider/calendar","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"tag":"calendar","pricing":{"prompt":"0","completion":"0","input_cache_read":"0","overrides":[{"utc_start":0,"utc_end":1400,"prompt":"0.00000132","completion":"0.00000396","input_cache_read":"0.000000132"},{"utc_start":1400,"utc_end":0,"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000066"}]}}]}}`)
	offers, err := normalizeEndpointPricing("provider/calendar", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 2)
	selected := model.SelectCatalogOffer(offers)
	require.NotNil(t, selected)
	prices := map[string]float64{}
	for _, price := range selected.Prices {
		prices[price.Component] = *price.AmountUSD
	}
	require.Len(t, prices, 3)
	for component, expected := range map[string]float64{"input": .66, "output": 1.98, "cache_read": .066} {
		assert.InDelta(t, expected, prices[component], 1e-12)
	}
	overlap := []byte(`{"data":{"id":"provider/calendar","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"tag":"calendar","pricing":{"prompt":"0.000009","completion":"0.000004","overrides":[{"utc_days":["saturday","sunday"],"prompt":"0.000007","completion":"0.000002"},{"utc_start":2300,"utc_end":100,"completion":"0.000001"}]}}]}}`)
	offers, err = normalizeEndpointPricing("provider/calendar", "text", "output", overlap)
	require.NoError(t, err)
	pairs := [][2]float64{}
	for _, offer := range offers {
		rates := map[string]float64{}
		for _, price := range offer.Prices {
			rates[price.Component] = *price.AmountUSD
		}
		pairs = append(pairs, [2]float64{rates["input"], rates["output"]})
	}
	assert.ElementsMatch(t, [][2]float64{{9, 1}, {7, 1}, {9, 4}, {7, 2}}, pairs)
	selected = model.SelectCatalogOffer(offers)
	require.NotNil(t, selected)
	assert.Equal(t, 9.0, *selected.Prices[1].AmountUSD)
	assert.Equal(t, 1.0, *selected.Prices[0].AmountUSD)
}

func TestPricingCatalogCalendarValidatesGatesAndContextReachability(t *testing.T) {
	for _, condition := range []string{
		`"utc_start":1260,"utc_end":1400`, `"utc_start":2400,"utc_end":100`,
		`"utc_start":-1,"utc_end":100`, `"utc_start":"14:00","utc_end":100`,
		`"utc_start":0`, `"utc_start":0,"utc_end":0`,
		`"utc_start":null,"utc_end":100`, `"utc_start":0,"utc_end":null`, `"utc_start": null ,"utc_end":100`,
		`"utc_days":[]`, `"utc_days":[0]`, `"utc_days":["Sunday"]`, `"utc_days":["sunday","sunday"]`,
		`"min_prompt_tokens":-1`, `"unrecognized_gate":true`,
		`"min_prompt_tokens":null`, `"min_prompt_tokens": null `,
	} {
		body := []byte(`{"data":{"id":"provider/calendar","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{` + condition + `,"completion":"0"}]}}]}}`)
		_, err := normalizeEndpointPricing("provider/calendar", "text", "output", body)
		require.Error(t, err, condition)
	}
	body := []byte(`{"data":{"id":"provider/calendar","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"context_length":100,"max_prompt_tokens":50,"pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{"min_prompt_tokens":75,"utc_days":["saturday","sunday"],"completion":"0"}]}}]}}`)
	offers, err := normalizeEndpointPricing("provider/calendar", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 1)
	selected := model.SelectCatalogOffer(offers)
	require.NotNil(t, selected)
	assert.Equal(t, 2.0, *selected.Prices[0].AmountUSD)
}

func TestPricingCatalogFailureDiagnosticsAreBoundedAndSanitized(t *testing.T) {
	sources := make([]pricingCatalogSource, pricingCatalogFailureLimit+4)
	for index := range sources {
		sources[index] = pricingCatalogSource{channel: &model.Channel{Id: 43, Key: "super-secret"}, upstream: fmt.Sprintf("public/model-%02d", index), endpoint: constant.EndpointTypeOpenAI}
	}
	summary, err := refreshPricingCatalogSources(context.Background(), sources, func(context.Context, pricingCatalogSource) (model.PricingCatalogEntry, error) {
		return model.PricingCatalogEntry{}, errors.New("https://private.invalid super-secret native endpoint pricing response")
	}, func([]model.PricingCatalogEntry) (int, error) { t.Fatal("failed metadata published"); return 0, nil })
	require.Error(t, err)
	assert.Equal(t, len(sources), summary.Failed)
	assert.True(t, summary.FailuresTruncated)
	require.Len(t, summary.FailedSources, pricingCatalogFailureLimit)
	assert.Equal(t, pricingCatalogFailure{ChannelID: 43, UpstreamModel: "public/model-00", Endpoint: constant.EndpointTypeOpenAI, Class: "metadata_identity_or_schema_invalid"}, summary.FailedSources[0])
	encoded, err := common.Marshal(summary)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "super-secret")
	assert.NotContains(t, string(encoded), "private.invalid")
	summary, err = refreshPricingCatalogSources(context.Background(), sources[:1], func(context.Context, pricingCatalogSource) (model.PricingCatalogEntry, error) {
		return model.PricingCatalogEntry{}, nil
	}, func([]model.PricingCatalogEntry) (int, error) { return 0, errors.New("super-secret DB failure") })
	require.Error(t, err)
	require.Len(t, summary.FailedSources, 1)
	assert.Equal(t, "publication_failed", summary.FailedSources[0].Class)
}

func TestPricingCatalogPromptThresholdRequiresReachableStrictSuccessor(t *testing.T) {
	for _, scenario := range []struct {
		name                       string
		context, prompt, threshold int
		expected                   float64
	}{
		{"equal_context_limit", 100, 0, 100, 2},
		{"equal_prompt_limit", 0, 100, 100, 2},
		{"equal_smaller_limit", 200, 100, 100, 2},
		{"one_below_context_limit", 100, 0, 99, 0},
		{"one_below_prompt_limit", 0, 100, 99, 0},
		{"explicit_zero_reachable", 1, 1, 0, 0},
		{"max_int_unreachable", 0, 0, int(^uint(0) >> 1), 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"data":{"id":"provider/boundary","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"context_length":%d,"max_prompt_tokens":%d,"pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{"min_prompt_tokens":%d,"completion":"0"}]}}]}}`, scenario.context, scenario.prompt, scenario.threshold))
			offers, err := normalizeEndpointPricing("provider/boundary", "text", "output", body)
			require.NoError(t, err)
			selected := model.SelectCatalogOffer(offers)
			require.NotNil(t, selected)
			for _, price := range selected.Prices {
				if price.Component == "output" {
					require.NotNil(t, price.AmountUSD)
					assert.Equal(t, scenario.expected, *price.AmountUSD)
				}
			}
		})
	}
	for _, gate := range []string{`"utc_days":["sunday"]`, `"utc_start":0,"utc_end":1200`} {
		body := []byte(`{"data":{"id":"provider/boundary","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"context_length":1,"max_prompt_tokens":1,"pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{` + gate + `,"completion":"0"}]}}]}}`)
		offers, err := normalizeEndpointPricing("provider/boundary", "text", "output", body)
		require.NoError(t, err)
		require.Len(t, offers, 2)
		selected := model.SelectCatalogOffer(offers)
		require.NotNil(t, selected)
		assert.Zero(t, *selected.Prices[0].AmountUSD)
	}
}

func TestConsumerAudioPricingPersonalAndPublicFullAndPage(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ModelCostFXRow{}))
	previousCountToken := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = previousCountToken })
	previousQuota := common.QuotaPerUnit
	previousExchange := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuota
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousExchange
	})
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory; model.InvalidatePricingCache() })
	now := time.Now().Unix()
	require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{Source: "cbr-xml-daily.ru", EffectiveAt: now, FetchedAt: now, Rates: map[string]float64{"USD": 90}}, 0))
	require.NoError(t, db.Create(&model.User{Id: 2198, Username: "audio-pricing-user", Password: "fixture", Group: "default", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 998, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-realtime-mini,gpt-4o-transcribe-diarize,gpt-4o-mini-tts,gpt-transcribe"}).Error)
	for _, name := range []string{"gpt-realtime-mini", "gpt-4o-transcribe-diarize", "gpt-4o-mini-tts", "gpt-transcribe"} {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: 998, Enabled: true}).Error)
	}
	withTieredBillingConfig(t, map[string]string{"gpt-realtime-mini": "tiered_expr", "gpt-4o-transcribe-diarize": "tiered_expr", "gpt-4o-mini-tts": "tiered_expr", "gpt-transcribe": "tiered_expr"}, map[string]string{"gpt-realtime-mini": `tier("base",p*.6+c*2.4+cr*.06+ai*10+ao*20+img*.8)`, "gpt-4o-transcribe-diarize": `tier("base",p*2.5+c*10)`, "gpt-4o-mini-tts": `tier("base",p*.6+c*12+ao*12)`, "gpt-transcribe": `tier("base",p*4.5)`})
	model.InitChannelCache()
	model.RefreshPricing()
	var expected []consumerPricingModel
	for _, handler := range []gin.HandlerFunc{GetEffectivePricing, GetEffectivePricingByGroup} {
		for _, query := range []string{"", "?limit=100"} {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/pricing"+query, nil)
			ctx.Set("id", 2198)
			handler(ctx)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			var envelope struct {
				Data struct {
					Data []consumerPricingModel `json:"data"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.Len(t, envelope.Data.Data, 4)
			if expected == nil {
				expected = envelope.Data.Data
			} else {
				assert.ElementsMatch(t, expected, envelope.Data.Data)
			}
			for _, item := range envelope.Data.Data {
				require.Len(t, item.GroupPrices, 1)
				require.Len(t, item.GroupPrices[0].Operations, 1)
				operation := item.GroupPrices[0].Operations[0]
				prices := map[string]consumerUnitPrice{}
				for _, price := range operation.Prices {
					prices[price.Component] = price
				}
				if item.ModelName == "gpt-realtime-mini" {
					assert.Equal(t, "realtime", operation.Operation)
					require.Len(t, prices, 6)
					for _, price := range prices {
						assert.Equal(t, "million_tokens", price.Unit)
						require.NotNil(t, price.AmountRub)
					}
				} else if item.ModelName == "gpt-4o-mini-tts" {
					assert.Equal(t, "speech_generation", operation.Operation)
					require.Len(t, prices, 1)
					assert.Equal(t, "million_tokens", prices["input"].Unit)
					require.NotNil(t, prices["input"].AmountRub)
					assert.InDelta(t, 54.0, *prices["input"].AmountRub, 1e-9)
				} else {
					assert.Equal(t, "transcription", operation.Operation)
					assert.Equal(t, "million_tokens", prices["input"].Unit)
					require.NotNil(t, prices["input"].AmountRub)
					if item.ModelName == "gpt-transcribe" {
						require.Len(t, prices, 1)
						assert.InDelta(t, 405.0, *prices["input"].AmountRub, 1e-9)
					} else {
						require.Len(t, prices, 2)
						assert.Equal(t, "million_tokens", prices["output"].Unit)
						require.NotNil(t, prices["output"].AmountRub)
					}
				}
			}
		}
	}

	captured, err := loadConsumerPricingContext("default", "")
	require.NoError(t, err)
	for _, item := range expected {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		query := url.Values{"model_name": {item.ModelName}, "expected_revision": {captured.metadata.CatalogRevision}}
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/effective-pricing/model?"+query.Encode(), nil)
		ctx.Set("id", 2198)
		GetEffectivePricingModel(ctx)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		var envelope struct {
			Data consumerPricingModelDetail `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
		assert.Equal(t, item, envelope.Data.Model.consumerPricingModel)
		assert.Equal(t, captured.metadata.CatalogRevision, envelope.Data.CatalogRevision)
	}
	for _, tc := range []struct {
		name, revision string
		status         int
	}{
		{"not-a-visible-model", "", http.StatusNotFound},
		{"gpt-realtime-mini", "stale", http.StatusConflict},
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		query := url.Values{"model_name": {tc.name}}
		if tc.revision != "" {
			query.Set("expected_revision", tc.revision)
		}
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/effective-pricing/model?"+query.Encode(), nil)
		ctx.Set("id", 2198)
		GetEffectivePricingModel(ctx)
		assert.Equal(t, tc.status, recorder.Code, recorder.Body.String())
	}
}

func TestPublicPricingModelsTagsPagingAndSafeTariffs(t *testing.T) {
	ctx := pricingPageFixture()
	ctx.groupRatios["default"] = 3
	for i := range ctx.pricing {
		ctx.pricing[i].Tags = "TOP, text;Featured| audio"
	}
	ctx.pricing[1].Tags = "top image"
	ctx.pricing = append(ctx.pricing, model.Pricing{ModelName: "not-top", Tags: "stopping,text", EnableGroup: []string{"default"}})
	before, err := common.Marshal(ctx.pricing)
	require.NoError(t, err)
	query, err := parsePublicPricingModelsQuery("tag=%20TOP%20&tag=TEXT&page_size=1")
	require.NoError(t, err)
	page := buildPublicPricingModelsPage(ctx, query)
	assert.Equal(t, 2, page.TotalCount)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "a-text", page.Data[0].ModelName)
	require.Len(t, page.Data[0].Tariff.Operations, 1)
	operation := page.Data[0].Tariff.Operations[0]
	assert.Equal(t, 600., *operation.Prices[0].AmountRub)
	query.page = 2
	page = buildPublicPricingModelsPage(ctx, query)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "z-text", page.Data[0].ModelName)
	query.page = 9007199254740991
	assert.Empty(t, buildPublicPricingModelsPage(ctx, query).Data)
	query.tags = []string{"unknown"}
	assert.Zero(t, buildPublicPricingModelsPage(ctx, query).TotalCount)
	after, err := common.Marshal(ctx.pricing)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
	bytes, err := common.Marshal(page)
	require.NoError(t, err)
	for _, forbidden := range []string{"amount_usd", "ratio", "formula", "accounting", "billing_sources", "expression", "group_prices"} {
		assert.NotContains(t, string(bytes), `"`+forbidden+`"`)
	}
	query, err = parsePublicPricingModelsQuery("tag=top")
	require.NoError(t, err)
	assert.Equal(t, 10, query.pageSize)
	assert.Equal(t, 1, query.page)
	ctx.pricing = nil
	for i := range 12 {
		ctx.pricing = append(ctx.pricing, model.Pricing{ModelName: fmt.Sprintf("model-%02d", i), Tags: "top", EnableGroup: []string{"default"}})
	}
	page = buildPublicPricingModelsPage(ctx, query)
	assert.Len(t, page.Data, 10)
	assert.Equal(t, 12, page.TotalCount)
}

func TestPublicPricingModelsInvalidQuery(t *testing.T) {
	for _, raw := range []string{"", "tag=", "tag=top,text", "tag=top%20text", "tag=top;", "tag=%zz", "tag=top&p=0", "tag=top&p=-1", "tag=top&p=1.1", "tag=top&p=+1", "tag=top&p=1&p=2", "tag=top&page_size=0", "tag=top&page_size=101", "tag=top&page_size=1&page_size=1", "tag=top&group=default", "tag=top&using_group=premium", "tag=top&limit=10", "tag=top&p=9007199254740992"} {
		t.Run(raw, func(t *testing.T) { _, err := parsePublicPricingModelsQuery(raw); require.Error(t, err) })
	}
}

func TestPublicPricingModelsConditionalAlternativesAndUnknown(t *testing.T) {
	ctx := pricingPageFixture()
	ctx.groupRatios["default"] = 2
	ctx.fxFactor = .9
	offers := []model.CatalogOffer{
		{ID: "conditional:a", Operation: "speech_generation", Mode: "sync", MainComponent: "input", PublicConditional: true, PublicConditions: []billingexpr.TierCondition{{Var: "len", Op: "<=", Value: 100}}, Prices: []model.CatalogPrice{{Component: "input", Unit: "million_characters", AmountUSD: float(0)}}},
		{ID: "conditional:b", Operation: "speech_generation", Mode: "sync", MainComponent: "input", PublicConditional: true, PublicConditions: []billingexpr.TierCondition{{Var: "len", Op: "<=", Value: 100, Negated: true}}, Prices: []model.CatalogPrice{{Component: "input", Unit: "million_characters", AmountUSD: float(5)}}},
		{ID: "unknown", Operation: "transcription", Mode: "sync", MainComponent: "audio_input", Prices: []model.CatalogPrice{{Component: "audio_input", Unit: "minute"}}},
	}
	item := model.Pricing{PublicCatalogOffersByGroup: map[string][]model.CatalogOffer{"default": offers}}
	result := buildPublicModelTariff(ctx, item)
	require.Len(t, result.Operations, 2)
	speech := result.Operations[0]
	assert.True(t, speech.IsFrom)
	assert.False(t, speech.IsFree)
	require.Len(t, speech.Tiers, 2)
	assert.Equal(t, 0., *speech.Tiers[0].Prices[0].AmountRub)
	assert.Equal(t, 900., *speech.Tiers[1].Prices[0].AmountRub)
	assert.Equal(t, "million_characters", speech.Tiers[1].Prices[0].Unit)
	assert.Equal(t, "not", speech.Tiers[1].Condition.Kind)
	unknown := result.Operations[1]
	assert.False(t, unknown.IsFree)
	require.Len(t, unknown.Tiers, 1)
	assert.Nil(t, unknown.Tiers[0].Prices[0].AmountRub)
	guard := publicPricingGuard(billingexpr.TierCondition{Op: "||", Operands: []billingexpr.TierCondition{{Var: "hour", Arg: "UTC", Op: "<", Value: 4}, {Op: "&&", Operands: []billingexpr.TierCondition{{Var: "p", Op: ">", Value: 3}, {Var: "c", Op: "==", Value: 0}}}}, Negated: true})
	assert.Equal(t, "not", guard.Kind)
	require.Len(t, guard.Conditions, 1)
	assert.Equal(t, "any_of", guard.Conditions[0].Kind)
	assert.Equal(t, "all_of", guard.Conditions[0].Conditions[1].Kind)
}

func TestPublicNativePricingGuardsRetainCalendarAndThreshold(t *testing.T) {
	body := []byte(`{"data":{"id":"provider/guarded","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{"min_prompt_tokens":100,"utc_start":2200,"utc_end":200,"utc_days":["monday"],"completion":"0"}]}}]}}`)
	offers, err := normalizeEndpointPricing("provider/guarded", "text", "output", body)
	require.NoError(t, err)
	require.Len(t, offers, 2)
	for _, offer := range offers {
		require.NotEmpty(t, offer.PublicConditions)
		assert.True(t, offer.PublicConditional)
	}
	encoded, err := common.Marshal(offers)
	require.NoError(t, err)
	var restored []model.CatalogOffer
	require.NoError(t, common.Unmarshal(encoded, &restored))
	assert.Equal(t, offers, restored)
	ctx := pricingPageFixture()
	item := model.Pricing{PublicCatalogOffersByGroup: map[string][]model.CatalogOffer{"default": restored}}
	tariff := buildPublicModelTariff(ctx, item)
	require.Len(t, tariff.Operations, 1)
	require.Len(t, tariff.Operations[0].Tiers, 2)
	wire, err := common.Marshal(tariff)
	require.NoError(t, err)
	assert.Contains(t, string(wire), "utc_minute")
	assert.Contains(t, string(wire), "weekday")
	assert.Contains(t, string(wire), "any_of")
	assert.Contains(t, string(wire), "input")
	assert.NotContains(t, string(wire), "amount_usd")
	assert.NotContains(t, string(wire), "native:")
}

func TestPublicNativeFreshCatalogThroughFetchAndSnapshot(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	previousCatalog := previousOptions[model.PricingCatalogOptionKey]
	if previousCatalog == "" {
		previousCatalog = `{"version":1,"entries":{}}`
	}
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		require.NoError(t, model.UpdateOption(model.PricingCatalogOptionKey, previousCatalog))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.ModelCostFXRow{}))
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory; model.InvalidatePricingCache() })
	previousQuota := common.QuotaPerUnit
	previousExchange := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuota
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousExchange
	})
	now := time.Now().Unix()
	require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{Source: "cbr-xml-daily.ru", EffectiveAt: now, FetchedAt: now, Rates: map[string]float64{"USD": 90}}, 0))
	channel := &model.Channel{Id: 990, Type: constant.ChannelTypeOpenRouter, Status: common.ChannelStatusEnabled, Key: "secret-fixture", Models: "provider/model", Group: "default"}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "provider/model", ChannelId: channel.Id, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "provider/model", Tags: "top text", Status: 1, NameRule: model.NameRuleExact}).Error)
	client := service.GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	oldTransport := client.Transport
	t.Cleanup(func() { client.Transport = oldTransport })
	attempts := 0
	client.Transport = pricingCatalogTestTransport(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{"id":"provider/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"endpoints":[{"tag":"native","pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{"min_prompt_tokens":100,"completion":"0"}]}}]}}`))}, nil
	})
	entry, err := fetchPricingCatalogSource(context.Background(), pricingCatalogSource{channel: channel, upstream: "provider/model", endpoint: constant.EndpointTypeOpenAI, fingerprint: model.PricingCatalogSourceFingerprint(channel, "provider/model", constant.EndpointTypeOpenAI)})
	require.NoError(t, err)
	require.Len(t, entry.Offers, 1)
	require.Len(t, entry.Offers[0].PublicAlternatives, 2)
	require.NoError(t, model.PublishPricingCatalogEntry(entry))
	model.InitChannelCache()
	model.RefreshPricing()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/effective/models?tag=top&tag=text", nil)
	GetPublicPricingModels(ctx)
	require.Equal(t, 200, recorder.Code, recorder.Body.String())
	var wire struct {
		Data publicPricingModelsPage `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &wire))
	require.Len(t, wire.Data.Data, 1)
	operations := wire.Data.Data[0].Tariff.Operations
	require.Len(t, operations, 1)
	require.Len(t, operations[0].Tiers, 2)
	assert.True(t, operations[0].IsFrom)
	assert.False(t, operations[0].IsFree)
	for _, tier := range operations[0].Tiers {
		require.NotEmpty(t, tier.Prices)
		require.NotNil(t, tier.Condition)
		for _, price := range tier.Prices {
			require.NotNil(t, price.AmountRub)
		}
	}
	assert.Equal(t, 1, attempts)
	require.Len(t, entry.Offers[0].PublicAlternatives, 2)
	assert.Equal(t, "native:0:0", entry.Offers[0].PublicAlternatives[0].ID)
	revision := wire.Data.CatalogRevision
	require.NoError(t, db.Model(&model.Model{}).Where("model_name = ?", "provider/model").Update("tags", "top video").Error)
	model.RefreshPricing()
	for _, category := range []string{"text", "video"} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/effective/models?tag=top&tag="+category, nil)
		GetPublicPricingModels(ctx)
		require.Equal(t, 200, recorder.Code, recorder.Body.String())
		var updated struct {
			Data publicPricingModelsPage `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &updated))
		assert.NotEqual(t, revision, updated.Data.CatalogRevision)
		if category == "text" {
			assert.Empty(t, updated.Data.Data)
		} else {
			require.Len(t, updated.Data.Data, 1)
			assert.Equal(t, []string{"top", "video"}, updated.Data.Data[0].Tags)
		}
	}
	assert.Equal(t, []string{"text", "top"}, wire.Data.Data[0].Tags)
	assert.Equal(t, 1, attempts)
	imageChannel := &model.Channel{Id: 992, Type: constant.ChannelTypeOpenRouter, Status: common.ChannelStatusEnabled, Key: "fixture", Models: "google/gemini-image", Group: "default"}
	require.NoError(t, db.Create(imageChannel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "google/gemini-image", ChannelId: 992, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "google/gemini-image", Tags: "top image", Status: 1, NameRule: model.NameRuleExact}).Error)
	textOffers, err := normalizeEndpointPricing("google/gemini-image", "text", "output", []byte(`{"data":{"id":"google/gemini-image","architecture":{"input_modalities":["text","image"],"output_modalities":["text","image"]},"endpoints":[{"tag":"studio","pricing":{"prompt":"0.0000005","completion":"0.000003","image_output":"0.00006"}}]}}`))
	require.NoError(t, err)
	imageOffers, err := normalizeImagePricing("google/gemini-image", []byte(`{"id":"google/gemini-image","endpoints":[{"provider_tag":"studio","pricing":[{"billable":"output_image","unit":"token","cost_usd":0.00006}]}]}`))
	require.NoError(t, err)
	for endpoint, offers := range map[constant.EndpointType][]model.CatalogOffer{constant.EndpointTypeOpenAI: textOffers, constant.EndpointTypeImageGeneration: imageOffers} {
		require.NoError(t, model.PublishPricingCatalogEntry(model.PricingCatalogEntry{ChannelID: 992, UpstreamModel: "google/gemini-image", Endpoint: endpoint, Source: "https://openrouter.ai/api/v1/models/google/gemini-image/endpoints", FetchedAt: time.Now().UTC().Format(time.RFC3339Nano), Fingerprint: model.PricingCatalogSourceFingerprint(imageChannel, "google/gemini-image", endpoint), Offers: selectPricingCatalogOffers(offers)}))
	}
	model.InitChannelCache()
	model.RefreshPricing()
	imageRecorder := httptest.NewRecorder()
	imageContext, _ := gin.CreateTestContext(imageRecorder)
	imageContext.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/effective/models?tag=top&tag=image", nil)
	GetPublicPricingModels(imageContext)
	require.Equal(t, http.StatusOK, imageRecorder.Code, imageRecorder.Body.String())
	require.NoError(t, common.Unmarshal(imageRecorder.Body.Bytes(), &wire))
	require.Len(t, wire.Data.Data, 1)
	var imagePrices []consumerUnitPrice
	for _, operation := range wire.Data.Data[0].Tariff.Operations {
		if operation.Operation == "image_generation" {
			imagePrices = operation.Prices
		}
	}
	require.Len(t, imagePrices, 2)
	for _, price := range imagePrices {
		require.NotNil(t, price.AmountRub)
		assert.Equal(t, "million_tokens", price.Unit)
		if price.Component == "input" {
			assert.InDelta(t, 45., *price.AmountRub, 1e-9)
		} else {
			assert.Equal(t, "image_output", price.Component)
			assert.InDelta(t, 5400., *price.AmountRub, 1e-9)
		}
	}

}

func TestEffectivePricingOverviewMatchesNativePagesFromCapturedContext(t *testing.T) {
	context := effectivePricingPageContext{
		metadata:     consumerPricingResponse{SchemaVersion: "v2", CatalogRevision: "captured-A", Currency: "RUB", UserGroup: "default", AutoGroups: []string{}},
		usableGroups: map[string]string{"default": "Default"}, groupRatios: map[string]float64{"default": 1}, fxFactor: 1,
		vendors: []model.PricingVendor{{ID: 7, Name: "Creator"}},
	}
	for _, modality := range []string{"TEXT", "IMAGE", "VIDEO", "EMBEDDINGS", "RERANK", "OTHER"} {
		operation := map[string]string{"TEXT": "text", "IMAGE": "image_generation", "VIDEO": "text_to_video", "EMBEDDINGS": "embeddings", "RERANK": "rerank", "OTHER": "transcription"}[modality]
		for _, name := range []string{"b", "A", "a", "unknown"} {
			var amount *float64
			if name != "unknown" {
				amount = float(0)
			}
			context.pricing = append(context.pricing, model.Pricing{ModelName: modality + "/" + name, VendorID: 7, EnableGroup: []string{"default"}, CatalogOffersByGroup: map[string][]model.CatalogOffer{"default": {{ID: name, Operation: operation, Mode: "sync", MainComponent: "input", Prices: []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: amount}}}}}})
		}
	}
	for _, search := range []string{"", "/a", "unknown"} {
		query := effectivePricingPageQuery{limit: 2, search: search, orderBy: "NAME", direction: "asc"}
		overview := buildEffectivePricingOverview(context, query)
		require.Len(t, overview.Sections, 6)
		assert.Equal(t, "captured-A", overview.CatalogRevision)
		for _, section := range overview.Sections {
			query.modality = section.Modality
			page := buildEffectivePricingPage(context, query, context.project)
			assert.Equal(t, page.TotalCount, section.TotalCount)
			assert.Equal(t, page.Data, section.Data)
			assert.LessOrEqual(t, len(section.Data), 2)
		}
	}
	// A new captured context cannot affect a previously constructed overview.
	first := buildEffectivePricingOverview(context, effectivePricingPageQuery{limit: 1, orderBy: "NAME", direction: "asc"})
	context.metadata.CatalogRevision = "captured-B"
	context.groupRatios = map[string]float64{"default": 9}
	second := buildEffectivePricingOverview(context, effectivePricingPageQuery{limit: 1, orderBy: "NAME", direction: "asc"})
	assert.Equal(t, "captured-A", first.CatalogRevision)
	assert.Equal(t, "captured-B", second.CatalogRevision)
}

func TestEffectivePricingOverviewUsesOneRevisionDuringControlledRebuild(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ModelCostFXRow{}))
	previousQuota, previousExchange := common.QuotaPerUnit, operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuota
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousExchange
	})

	now := time.Now().Unix()
	require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{Source: "cbr-xml-daily.ru", EffectiveAt: now, FetchedAt: now, Rates: map[string]float64{"USD": 90}}, 0))
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory; model.InvalidatePricingCache() })
	names := []string{"chat-fixture", "gpt-image-1", "sora-2", "text-embedding-3-small", "rerank-fixture", "whisper-1"}
	require.NoError(t, db.Create(&model.Channel{Id: 999, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: strings.Join(names, ","), Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 1000, Type: constant.ChannelTypeJina, Status: common.ChannelStatusEnabled, Models: "rerank-fixture", Group: "default"}).Error)
	for index, name := range names {
		channelID := 999
		if index == 4 {
			channelID = 1000
		}
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: channelID, Enabled: true}).Error)
		endpoints := []string{"openai", "image-generation", "openai-video", "embeddings", string(constant.EndpointTypeJinaRerank), "audio-transcription"}
		require.NoError(t, db.Create(&model.Model{ModelName: name, Description: "captured-old", Endpoints: fmt.Sprintf(`{"%s":"/fixture"}`, endpoints[index])}).Error)
	}
	model.InitChannelCache()
	model.RefreshPricing()
	require.NoError(t, db.Model(&model.Model{}).Where("model_name IN ?", names).Update("description", "captured-new").Error)
	entered, release := make(chan struct{}), make(chan struct{})
	releaseBuild := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseBuild)
	var once sync.Once
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:overview_build_gate", func(tx *gorm.DB) {
		if tx.Statement.Table == "abilities" {
			once.Do(func() { close(entered); <-release })
		}
	}))
	t.Cleanup(func() { db.Callback().Row().Remove("test:overview_build_gate") })
	model.InvalidatePricingCache()

	var httpAttempts atomic.Int64
	router := gin.New()
	router.GET("/api/pricing/effective/overview", func(c *gin.Context) { httpAttempts.Add(1); GetEffectivePricingOverview(c) })
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = time.Second
	type overviewResult struct {
		Search  string
		Data    effectivePricingOverviewData
		Elapsed time.Duration
		Err     error
	}
	invoke := func(search string) overviewResult {
		result := overviewResult{Search: search}
		started := time.Now()
		response, err := client.Get(server.URL + "/api/pricing/effective/overview?limit=1&search=" + url.QueryEscape(search))
		if err != nil {
			result.Err = err
			return result
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			result.Err = err
			return result
		}
		if response.StatusCode != http.StatusOK {
			result.Err = fmt.Errorf("overview HTTP %d: %s", response.StatusCode, body)
			return result
		}
		var wire struct {
			Data effectivePricingOverviewData `json:"data"`
		}
		result.Err = common.Unmarshal(body, &wire)
		result.Data = wire.Data
		result.Elapsed = time.Since(started)
		return result
	}
	answers := make(chan overviewResult, 2)
	for _, search := range []string{"", "fixture"} {
		go func() { answers <- invoke(search) }()
	}
	var first, searchResult overviewResult
	for range 2 {
		answer := <-answers
		require.NoError(t, answer.Err)
		if answer.Search == "" {
			first = answer
		} else {
			searchResult = answer
		}
	}
	assert.EqualValues(t, 2, httpAttempts.Load())
	assert.Equal(t, first.Data.CatalogRevision, searchResult.Data.CatalogRevision)
	for _, section := range searchResult.Data.Sections {
		assert.LessOrEqual(t, len(section.Data), 1)
		for _, item := range section.Data {
			assert.Equal(t, "captured-old", item.Description)
		}
	}
	t.Logf("parallel overview/search: HTTP attempts=%d, overview=%s, search=%s, one revision=%s", httpAttempts.Load(), first.Elapsed, searchResult.Elapsed, first.Data.CatalogRevision)
	select {
	case <-entered:
	case <-time.After(time.Second):
		releaseBuild()
		t.Fatal("background rebuild did not reach barrier")
	}
	releaseBuild()
	model.RefreshPricing()
	second := invoke("")
	require.NoError(t, second.Err)
	require.Len(t, first.Data.Sections, 6)
	require.Len(t, second.Data.Sections, 6)
	assert.NotEqual(t, first.Data.CatalogRevision, second.Data.CatalogRevision)
	for index, section := range first.Data.Sections {
		require.Len(t, section.Data, 1, section.Modality)
		assert.Equal(t, "captured-old", section.Data[0].Description)
		require.Len(t, second.Data.Sections[index].Data, 1)
		assert.Equal(t, "captured-new", second.Data.Sections[index].Data[0].Description)
	}
}

func TestLegacyPricingCapturesEndpointMapWithModels(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory; model.InvalidatePricingCache() })
	require.NoError(t, db.Create(&model.User{Id: 2200, Username: "snapshot-endpoint-user", Password: "fixture", Group: "default", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 1001, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "snapshot-endpoint-model", Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "snapshot-endpoint-model", ChannelId: 1001, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "snapshot-endpoint-model", Description: "captured-old", Endpoints: `{"fixture-old":"/old-path"}`}).Error)
	model.InitChannelCache()
	model.RefreshPricing()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:legacy_pricing_capture", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			once.Do(func() { close(entered); <-release })
		}
	}))
	t.Cleanup(func() { db.Callback().Query().Remove("test:legacy_pricing_capture") })
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
	ctx.Set("id", 2200)
	done := make(chan struct{})
	go func() { GetPricing(ctx); close(done) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("request did not reach captured-snapshot barrier")
	}
	require.NoError(t, db.Model(&model.Model{}).Where("model_name = ?", "snapshot-endpoint-model").Updates(map[string]any{"description": "captured-new", "endpoints": `{"fixture-new":"/new-path"}`}).Error)
	model.InvalidatePricingCache()
	model.RefreshPricing()
	close(release)
	<-done
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var wire struct {
		Data      []model.Pricing                `json:"data"`
		Endpoints map[string]common.EndpointInfo `json:"supported_endpoint"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &wire))
	require.Len(t, wire.Data, 1)
	assert.Equal(t, "captured-old", wire.Data[0].Description)
	assert.Contains(t, wire.Data[0].SupportedEndpointTypes, constant.EndpointType("fixture-old"))
	assert.Equal(t, "/old-path", wire.Endpoints["fixture-old"].Path)
	assert.NotContains(t, wire.Endpoints, "fixture-new")
	current, _, _, err := model.GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	require.Len(t, current, 1)
	assert.Equal(t, "captured-new", current[0].Description)
}

func TestUpstreamCatalogConsumerProjection(t *testing.T) {
	raw, err := os.ReadFile("../model/testdata/upstream_catalog/aggregator.json")
	require.NoError(t, err)
	doc, err := catalogsync.DecodeDocument(raw, catalogsync.Reference{ID: "router/aggregated", Hash: catalogsync.Hash(raw)})
	require.NoError(t, err)
	a, b := doc.Offers[0], doc.Offers[1]
	b.Mode = nil
	makeRates := func(input, output string) []catalogsync.Component {
		rates := append([]catalogsync.Component(nil), a.Components...)
		rates[0].AmountUSD = &input
		rates[1].AmountUSD = &output
		rates[1].Billable = common.GetPointer("output_tokens")
		rates[1].Direction = common.GetPointer("output")
		rates[2].Billable = common.GetPointer("input_cache_read")
		rates[1].Normalization.Comparable = true
		rates[1].Normalization.Effective = true
		return rates
	}
	a.Components = makeRates("1", "9")
	b.Components = makeRates("7", "3")
	doc.Offers = []catalogsync.Offer{a, b}
	override := `{"provider":{"only":["b"]}}`
	route := model.UpstreamCatalogRoute{Operation: "embeddings", Mode: "sync", ModelID: doc.ID, DocumentHash: catalogsync.Hash(raw), Document: doc, ParamOverride: &override}
	item := model.Pricing{ModelName: "my-alias", DisplayName: "Google: Readable name", EnableGroup: []string{"default"}, UpstreamCatalogRoutesByGroup: map[string][]model.UpstreamCatalogRoute{"default": {route}}}
	public := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{})
	require.Len(t, public, 1)
	minima := catalogMinima(public)
	require.Len(t, minima, 1)
	amounts := map[string]float64{}
	for _, price := range minima[0].Prices {
		require.NotNil(t, price.AmountRub)
		amounts[price.Component] = *price.AmountRub
	}
	assert.Equal(t, float64(700), amounts["input"])
	assert.Equal(t, float64(300), amounts["output"])
	assert.Equal(t, float64(0), amounts["cache_read"])
	private := buildConsumerPricingModel(map[string]string{"default": "Default"}, map[string]float64{"default": 1}, 1, item, true, pricingUserContext{id: 17, group: "default"})
	require.Len(t, private.GroupPrices, 1)
	require.Len(t, private.GroupPrices[0].ProviderOffers, 1)
	assert.Equal(t, "b", *private.GroupPrices[0].ProviderOffers[0].Provider.ID)
	assert.Equal(t, "7", *private.GroupPrices[0].ProviderOffers[0].Prices[0].AmountUSD)
	assert.Equal(t, "700", *private.GroupPrices[0].ProviderOffers[0].Prices[0].AmountRub)
	assert.JSONEq(t, string(b.Components[2].Conditions), string(private.GroupPrices[0].ProviderOffers[0].Prices[2].Conditions))
	assert.Equal(t, "my-alias", private.ModelName)
	assert.Equal(t, "Google: Readable name", private.DisplayName)
	context := effectivePricingPageContext{
		userID: 17, metadata: consumerPricingResponse{SchemaVersion: "v2", CatalogRevision: "captured-revision", UserGroup: "default", Currency: "RUB", AutoGroups: []string{"default"}},
		pricing:      []model.Pricing{item, {ModelName: "hidden", EnableGroup: []string{"premium"}}},
		usableGroups: map[string]string{"default": "Default"}, groupRatios: map[string]float64{"default": 1}, fxFactor: 1,
	}
	page := buildEffectivePricingPage(context, effectivePricingPageQuery{limit: 20, orderBy: "NAME"}, context.project)
	require.Len(t, page.Data, 1)
	assert.Empty(t, page.Data[0].GroupPrices[0].ProviderOffers)
	assert.Equal(t, private.GroupPrices[0].Operations, page.Data[0].GroupPrices[0].Operations)
	pageWire, err := common.Marshal(page)
	require.NoError(t, err)
	assert.NotContains(t, string(pageWire), "provider_offers")
	assert.NotContains(t, string(pageWire), "Provider B")
	detail, found := buildEffectivePricingModelDetail(context, "my-alias")
	require.True(t, found)
	assert.Equal(t, "captured-revision", detail.CatalogRevision)
	assert.Equal(t, private, detail.Model.consumerPricingModel)
	for _, name := range []string{"hidden", "missing", "my", doc.ID} {
		_, found := buildEffectivePricingModelDetail(context, name)
		assert.False(t, found, name)
	}
	tariff := buildCatalogModelTariff(effectivePricingPageContext{groupRatios: map[string]float64{"default": 1}, fxFactor: 1}, item, "default")
	wire, err := common.Marshal(tariff)
	require.NoError(t, err)
	assert.NotContains(t, string(wire), "Provider A")
	assert.NotContains(t, string(wire), "provider_offers")
	require.NotEmpty(t, tariff.Operations)
	assert.False(t, tariff.Operations[0].IsFree)

	t.Run("legacy client provider forwarding preserves catalog prices", func(t *testing.T) {
		forwarding := `{"operations":[{"mode":"move","from":"extra_body.provider","to":"provider","conditions":[{"path":"extra_body.provider","mode":"full","value":null,"invert":true,"pass_missing_key":false}]},{"mode":"delete","path":"extra_body","conditions":[{"path":"extra_body","mode":"full","value":{},"pass_missing_key":false}]}]}`
		route.ParamOverride = &forwarding
		item.UpstreamCatalogRoutesByGroup["default"] = []model.UpstreamCatalogRoute{route}
		offers := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{})
		require.Len(t, offers, 2)
		for _, offer := range offers {
			assert.Equal(t, "eligible", offer.Availability)
		}
		minimum := catalogMinima(offers)
		require.Len(t, minimum, 1)
		for _, price := range minimum[0].Prices {
			if price.Component == "input" {
				require.NotNil(t, price.AmountRub)
				assert.Equal(t, float64(100), *price.AmountRub)
			}
		}
		pinned := strings.Replace(forwarding, `{"operations":`, `{"provider":{"only":["b"]},"operations":`, 1)
		route.ParamOverride = &pinned
		item.UpstreamCatalogRoutesByGroup["default"] = []model.UpstreamCatalogRoute{route}
		offers = upstreamProviderOffers(item, "default", 1, true, pricingUserContext{})
		require.Len(t, offers, 1)
		assert.Equal(t, "b", *offers[0].Provider.ID)
		assert.Equal(t, "eligible", offers[0].Availability)
		for _, invalid := range []string{
			strings.Replace(forwarding, `"from":"extra_body.provider"`, `"from":"request_headers.provider"`, 1),
			strings.Replace(forwarding, `"invert":true`, `"invert":false`, 1),
			strings.Replace(forwarding, `"pass_missing_key":false`, `"pass_missing_key":true`, 1),
		} {
			route.ParamOverride = &invalid
			item.UpstreamCatalogRoutesByGroup["default"] = []model.UpstreamCatalogRoute{route}
			assert.Empty(t, catalogMinima(upstreamProviderOffers(item, "default", 1, true, pricingUserContext{})))
		}
	})

	t.Run("unknown routing constraints do not advertise a guaranteed minimum", func(t *testing.T) {
		policy := `{"provider":{"only":["b"],"quantizations":["fp8"]}}`
		route.ParamOverride = &policy
		item.UpstreamCatalogRoutesByGroup["default"] = []model.UpstreamCatalogRoute{route}
		offers := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{})
		require.Len(t, offers, 1)
		assert.Equal(t, "unknown", offers[0].Availability)
		require.NotNil(t, offers[0].Prices[0].AmountRub)
		assert.Empty(t, catalogMinima(offers))
	})
	t.Run("request conditional branches stay unknown but user conditions are evaluated", func(t *testing.T) {
		policy := `{"operations":[{"path":"provider.only","mode":"set","value":["b"],"conditions":[{"path":"request_headers.x-route","mode":"full","value":"b"}]}]}`
		route.ParamOverride = &policy
		item.UpstreamCatalogRoutesByGroup["default"] = []model.UpstreamCatalogRoute{route}
		offers := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{id: 17, group: "default"})
		require.Len(t, offers, 2)
		for _, offer := range offers {
			assert.Equal(t, "unknown", offer.Availability)
		}
		assert.Empty(t, catalogMinima(offers))
		policy = strings.ReplaceAll(policy, "request_headers.x-route", "user_id")
		policy = strings.ReplaceAll(policy, `"value":"b"`, `"value":17`)
		offers = upstreamProviderOffers(item, "default", 1, true, pricingUserContext{id: 17, group: "default"})
		require.Len(t, offers, 1)
		assert.Equal(t, "b", *offers[0].Provider.ID)
		offers = upstreamProviderOffers(item, "default", 1, true, pricingUserContext{id: 18, group: "default"})
		require.Len(t, offers, 2)
	})
	t.Run("relay shorthand user conditions are preserved", func(t *testing.T) {
		policy := `{"operations":[{"path":"provider.only","mode":"set","value":["b"],"conditions":{"user_id":17}}]}`
		route.ParamOverride = &policy
		item.UpstreamCatalogRoutesByGroup["default"] = []model.UpstreamCatalogRoute{route}
		offers := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{id: 17, group: "default"})
		require.Len(t, offers, 1)
		assert.Equal(t, "b", *offers[0].Provider.ID)
		assert.Equal(t, "eligible", offers[0].Availability)
		anonymous := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{group: "default"})
		require.Len(t, anonymous, 2)
		for _, offer := range anonymous {
			assert.Equal(t, "unknown", offer.Availability)
		}
	})
	t.Run("alternative eligible channel wins regardless of route order", func(t *testing.T) {
		restricted := route
		policy := `{"provider":{"quantizations":["fp8"]}}`
		restricted.ParamOverride = &policy
		unrestricted := route
		unrestricted.ParamOverride = nil
		for _, routes := range [][]model.UpstreamCatalogRoute{{restricted, unrestricted}, {unrestricted, restricted}} {
			item.UpstreamCatalogRoutesByGroup["default"] = routes
			offers := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{})
			require.Len(t, offers, 2)
			for _, offer := range offers {
				assert.Equal(t, "eligible", offer.Availability)
			}
			require.Len(t, catalogMinima(offers), 1)
		}
	})
	t.Run("unqualified prices never become batch tariffs", func(t *testing.T) {
		batch := route
		batch.Mode = "batch"
		batch.ParamOverride = nil
		item.UpstreamCatalogRoutesByGroup["default"] = []model.UpstreamCatalogRoute{batch}
		assert.Empty(t, upstreamProviderOffers(item, "default", 1, true, pricingUserContext{}))
		mode := "batch"
		doc.Offers[1].Mode = &mode
		offers := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{})
		require.Len(t, offers, 1)
		assert.Equal(t, "batch", offers[0].Mode)
		doc.Offers[1].Mode = nil
	})
	t.Run("null provider and unknown amounts preserve meaning", func(t *testing.T) {
		doc.Offers = []catalogsync.Offer{a}
		doc.Offers[0].Provider = nil
		doc.Offers[0].Components[0].AmountUSD = nil
		route.ParamOverride = nil
		item.UpstreamCatalogRoutesByGroup["default"] = []model.UpstreamCatalogRoute{route}
		offers := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{})
		require.Len(t, offers, 1)
		assert.Nil(t, offers[0].Provider)
		assert.Nil(t, offers[0].Prices[0].AmountRub)
		assert.Equal(t, "0", *offers[0].Prices[2].AmountRub)
	})
	t.Run("same offer id does not hide different variants", func(t *testing.T) {
		doc.Offers = []catalogsync.Offer{a, b}
		doc.Offers[1].ID = a.ID
		offers := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{})
		require.Len(t, offers, 2)
		assert.NotEqual(t, offers[0].ID, offers[1].ID)
	})
}

func TestUpstreamCatalogDetailDropsModelPagePricesShadowedByGenericSchedule(t *testing.T) {
	raw, err := os.ReadFile("../model/testdata/upstream_catalog/aggregator.json")
	require.NoError(t, err)
	doc, err := catalogsync.DecodeDocument(raw, catalogsync.Reference{ID: "router/aggregated", Hash: catalogsync.Hash(raw)})
	require.NoError(t, err)
	meter := func(source, parameter, billable, amount, conditions string) catalogsync.Component {
		rate := catalogsync.Component{Parameter: parameter, ComponentID: `{"billable":"` + billable + `"}`, Billable: common.GetPointer(billable), Direction: common.GetPointer("input"), Modality: common.GetPointer("text"), Unit: "million_tokens", Quantity: "1", Variant: json.RawMessage(`{}`), Conditions: json.RawMessage(conditions), Provenance: json.RawMessage(`{"scope":"endpoint","source":"` + source + `"}`)}
		if billable == "" {
			rate.ComponentID, rate.Billable = parameter, nil
		}
		if amount != "" {
			rate.AmountUSD = common.GetPointer(amount)
			rate.Normalization.Comparable, rate.Normalization.Effective = true, true
		}
		return rate
	}
	generic := meter("generic", "prompt", "input_text", "0.6628", `{}`)
	pricesFor := func(validRatio bool, components ...catalogsync.Component) []consumerProviderPrice {
		offer := doc.Offers[0]
		offer.Components = components
		document := *doc
		document.Offers = []catalogsync.Offer{offer}
		route := model.UpstreamCatalogRoute{Operation: "embeddings", Mode: "sync", ModelID: doc.ID, DocumentHash: catalogsync.Hash(raw), Document: &document}
		item := model.Pricing{ModelName: "detail", EnableGroup: []string{"default"}, UpstreamCatalogRoutesByGroup: map[string][]model.UpstreamCatalogRoute{"default": {route}}}
		offers := upstreamProviderOffers(item, "default", 1, validRatio, pricingUserContext{})
		require.Len(t, offers, 1)
		return offers[0].Prices
	}
	parameters := func(prices []consumerProviderPrice) []string {
		result := []string{}
		for _, price := range prices {
			result = append(result, price.Parameter)
		}
		return result
	}
	for _, tc := range []struct {
		name       string
		validRatio bool
		mutate     func(*catalogsync.Component)
		expected   []string
	}{
		{"exact duplicate keeps generic", true, func(*catalogsync.Component) {}, []string{"prompt"}},
		{"usd fallback without rouble amounts", false, func(*catalogsync.Component) {}, []string{"prompt"}},
		{"unconditional base price beside a context schedule", true, func(rate *catalogsync.Component) { rate.Conditions = json.RawMessage(`{}`) }, []string{"prompt"}},
		{"different amount", true, func(rate *catalogsync.Component) { rate.AmountUSD = common.GetPointer("0.6629") }, []string{"prompt"}},
		{"own context window", true, func(rate *catalogsync.Component) {
			rate.Conditions = json.RawMessage(`{"contextMin":0,"contextMax":8192,"utcStart":0,"utcEnd":2400}`)
		}, []string{"prompt"}},
		{"different variant", true, func(rate *catalogsync.Component) { rate.Variant = json.RawMessage(`{"service_tier":"flex"}`) }, []string{"prompt"}},
		{"different quantity", true, func(rate *catalogsync.Component) { rate.Quantity = "1000" }, []string{"prompt"}},
		{"billable alias of the same component", true, func(rate *catalogsync.Component) {
			rate.Billable, rate.ComponentID = common.GetPointer("input_tokens"), `{"billable":"input_tokens"}`
		}, []string{"prompt"}},
		{"different unit stays", true, func(rate *catalogsync.Component) { rate.Unit = "thousand_tokens" }, []string{"prompt", "openai:prompt_tokens"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			window := `{"contextMin":0,"contextMax":272000,"utcStart":0,"utcEnd":2400}`
			scheduled := meter("generic", "prompt", "input_text", "0.6628", window)
			page := meter("model_page", "openai:prompt_tokens", "input_text", "0.6628", window)
			tc.mutate(&page)
			prices := pricesFor(tc.validRatio, scheduled, page)
			require.NotEmpty(t, prices)
			assert.Equal(t, tc.expected, parameters(prices))
			assert.Contains(t, string(prices[0].Provenance), `"source":"generic"`)
		})
	}
	t.Run("model page without generic pair stays", func(t *testing.T) {
		page := meter("model_page", "openai:completion_tokens", "output_text", "1.3256", `{}`)
		page.Direction = common.GetPointer("output")
		prices := pricesFor(true, generic, page)
		assert.Equal(t, []string{"prompt", "openai:completion_tokens"}, parameters(prices))
		require.NotNil(t, prices[1].AmountRub)
		assert.Equal(t, "132.56", *prices[1].AmountRub)
	})
	t.Run("unknown meters without amounts stay", func(t *testing.T) {
		prices := pricesFor(true, meter("generic", "display:0:0:Input Price", "", "", `{}`), meter("model_page", "display:0:0:Input Price", "", "", `{}`))
		require.Len(t, prices, 2)
		for _, price := range prices {
			assert.Equal(t, "unknown", price.Component)
			assert.Nil(t, price.AmountRub)
		}
	})
}

func TestUpstreamCatalogDetailFoldsModelScopeOffersIntoProviderOffers(t *testing.T) {
	rate := func(scope, source, parameter, billable, unit, amount, conditions string) catalogsync.Component {
		component := catalogsync.Component{Parameter: parameter, ComponentID: `{"billable":"` + billable + `","unit":"` + unit + `"}`, Billable: common.GetPointer(billable), Direction: common.GetPointer("output"), Modality: common.GetPointer("video"), Unit: unit, Quantity: "1", Variant: json.RawMessage(`{}`), Conditions: json.RawMessage(conditions), Provenance: json.RawMessage(`{"scope":"` + scope + `","source":"` + source + `"}`)}
		if amount != "" {
			component.AmountUSD = common.GetPointer(amount)
			component.Normalization.Comparable, component.Normalization.Effective = true, true
		}
		return component
	}
	provider := func(tag string, variant string, capabilities string, components ...catalogsync.Component) catalogsync.Offer {
		return catalogsync.Offer{ID: tag, Scope: "endpoint", Provider: &catalogsync.Provider{ID: common.GetPointer(tag), Name: common.GetPointer(tag), Tag: common.GetPointer(tag)}, Variant: json.RawMessage(variant), Eligibility: &catalogsync.Eligibility{Eligible: true}, Capabilities: json.RawMessage(capabilities), Components: components}
	}
	modelScope := func(id, capabilities string, components ...catalogsync.Component) catalogsync.Offer {
		return catalogsync.Offer{ID: id, Scope: "model", Variant: json.RawMessage(`{}`), Capabilities: json.RawMessage(capabilities), Limitations: []string{"model_level_no_provider_binding"}, Components: components}
	}
	minimum := `{"chargeRelationship":"minimum"}`
	standard := `{"variant":"standard"}`
	runwayCapabilities := `{"allowed_passthrough_parameters":["keyframes"],"context_length":0}`
	videoCapabilities := `{"allowed_passthrough_parameters":["contentModeration","keyframes"],"seed":true}`
	mergedCapabilities := `{"allowed_passthrough_parameters":["keyframes"],"context_length":0,"seed":true}`
	display := rate("endpoint", "model_page", "display:0:0:Video Output", "output_video", "second", "0.28", `{}`)
	perSecond := rate("endpoint", "model_page", "runway_video:cents_per_second_output", "output_video", "second", "0.280", `{}`)
	endpointMinimum := rate("endpoint", "model_page", "runway_video:minimum_cents_per_generation", "minimum_generation_charge", "request", "0.56", minimum)
	modelMinimum := rate("model", "video_api", "minimum_cents_per_generation", "minimum_generation_charge", "request", "0.56", minimum)
	unpricedPerSecond := rate("model", "video_api", "cents_per_second_output", "output_video", "source_defined", "", `{}`)
	generic := modelScope("generic", `{}`, rate("model", "generic", "prompt", "input_text", "source_defined", "", `{}`), rate("model", "generic", "completion", "output_text", "source_defined", "", `{}`))
	flexOutput := rate("endpoint", "model_page", "flex:output", "output_video", "second", "0.2", `{}`)
	flexOutput.Variant = json.RawMessage(`{"service_tier":"flex"}`)
	modelOutput := rate("model", "video_api", "cents_per_second_output", "output_video", "second", "0.28", `{}`)
	conflictingOutput := rate("model", "video_api", "cents_per_second_output", "output_video", "second", "0.4", `{}`)
	uncarriable := rate("model", "video_api", "cents_per_second_output", "output_video", "source_defined", "0.28", `{}`)

	type expectedOffer struct {
		scope, provider, capabilities string
		parameters                    []string
	}
	for _, tc := range []struct {
		name      string
		documents [][]catalogsync.Offer
		expected  []expectedOffer
	}{
		{"aleph: model offers without new prices fold into the Runway offer", [][]catalogsync.Offer{{provider("runway", standard, runwayCapabilities, display, perSecond, endpointMinimum), modelScope("video", videoCapabilities, modelMinimum, unpricedPerSecond), generic}}, []expectedOffer{
			{"endpoint", "runway", mergedCapabilities, []string{"display:0:0:Video Output", "runway_video:minimum_cents_per_generation"}},
		}},
		{"gen-4.5: unpriced model offers fold into the Runway offer", [][]catalogsync.Offer{{provider("runway", standard, runwayCapabilities, display, perSecond), modelScope("video", videoCapabilities, unpricedPerSecond), generic}}, []expectedOffer{
			{"endpoint", "runway", mergedCapabilities, []string{"display:0:0:Video Output"}},
		}},
		{"model price missing from the provider offer is carried into it", [][]catalogsync.Offer{{provider("runway", standard, runwayCapabilities, display), modelScope("video", videoCapabilities, modelMinimum, unpricedPerSecond), generic}}, []expectedOffer{
			{"endpoint", "runway", mergedCapabilities, []string{"display:0:0:Video Output", "minimum_cents_per_generation"}},
		}},
		{"each provider offer receives only its missing slots", [][]catalogsync.Offer{{provider("runway", standard, runwayCapabilities, display, endpointMinimum), provider("flex", `{"service_tier":"flex"}`, `{"seed":false}`, flexOutput), modelScope("video", videoCapabilities, modelOutput, modelMinimum)}}, []expectedOffer{
			{"endpoint", "runway", mergedCapabilities, []string{"display:0:0:Video Output", "runway_video:minimum_cents_per_generation"}},
			{"endpoint", "flex", `{"allowed_passthrough_parameters":["contentModeration","keyframes"],"seed":false}`, []string{"flex:output", "cents_per_second_output", "minimum_cents_per_generation"}},
		}},
		{"conflicting model amount keeps the model offer separate", [][]catalogsync.Offer{{provider("runway", standard, runwayCapabilities, display), modelScope("video", videoCapabilities, conflictingOutput, modelMinimum)}}, []expectedOffer{
			{"endpoint", "runway", runwayCapabilities, []string{"display:0:0:Video Output"}},
			{"model", "", videoCapabilities, []string{"cents_per_second_output", "minimum_cents_per_generation"}},
		}},
		{"priced rate that cannot be carried keeps the model offer separate", [][]catalogsync.Offer{{provider("runway", standard, runwayCapabilities, display), modelScope("video", videoCapabilities, uncarriable)}}, []expectedOffer{
			{"endpoint", "runway", runwayCapabilities, []string{"display:0:0:Video Output"}},
			{"model", "", videoCapabilities, []string{"cents_per_second_output"}},
		}},
		{"model offers of another document are not folded", [][]catalogsync.Offer{{provider("runway", standard, runwayCapabilities, display)}, {modelScope("video", videoCapabilities, modelMinimum)}}, []expectedOffer{
			{"endpoint", "runway", runwayCapabilities, []string{"display:0:0:Video Output"}},
			{"model", "", videoCapabilities, []string{"minimum_cents_per_generation"}},
		}},
		{"model-only description stays unchanged", [][]catalogsync.Offer{{modelScope("video", videoCapabilities, modelMinimum, unpricedPerSecond), generic}}, []expectedOffer{
			{"model", "", videoCapabilities, []string{"minimum_cents_per_generation", "cents_per_second_output"}},
			{"model", "", `{}`, []string{"prompt", "completion"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := []model.UpstreamCatalogRoute{}
			for index, offers := range tc.documents {
				document := catalogsync.Document{ID: "runway/aleph-2", Offers: offers}
				routes = append(routes, model.UpstreamCatalogRoute{Operation: "video_generation", Mode: "sync", ModelID: document.ID, DocumentHash: catalogsync.Hash([]byte(tc.name + strconv.Itoa(index))), Document: &document})
			}
			item := model.Pricing{ModelName: "aleph", EnableGroup: []string{"default"}, UpstreamCatalogRoutesByGroup: map[string][]model.UpstreamCatalogRoute{"default": routes}}
			detail := buildConsumerPricingModel(map[string]string{"default": "Default"}, map[string]float64{"default": 1}, 1, item, true)
			summary := buildConsumerPricingModel(map[string]string{"default": "Default"}, map[string]float64{"default": 1}, 1, item, false)
			require.Len(t, detail.GroupPrices, 1)
			require.Len(t, summary.GroupPrices, 1)
			assert.Equal(t, summary.GroupPrices[0].Operations, detail.GroupPrices[0].Operations, "folding is presentation only and must not change minima")
			actual := []expectedOffer{}
			for _, offer := range detail.GroupPrices[0].ProviderOffers {
				parameters := []string{}
				for _, price := range offer.Prices {
					parameters = append(parameters, price.Parameter)
				}
				tag := ""
				if offer.Provider != nil {
					tag = *offer.Provider.Tag
				}
				actual = append(actual, expectedOffer{offer.Scope, tag, string(offer.Capabilities), parameters})
			}
			assert.ElementsMatch(t, tc.expected, actual)
		})
	}
}

func TestUpstreamCatalogDetailKeepsPricedCopyOfDuplicatePrice(t *testing.T) {
	priced := catalogsync.Component{Parameter: "runway_video:cents_per_second_output", ComponentID: "output_video", Billable: common.GetPointer("output_video"), Direction: common.GetPointer("output"), Modality: common.GetPointer("video"), AmountUSD: common.GetPointer("0.28"), Unit: "second", Quantity: "1", Variant: json.RawMessage(`{}`), Conditions: json.RawMessage(`{}`), Provenance: json.RawMessage(`{"scope":"endpoint","source":"model_page"}`)}
	priced.Normalization.Comparable, priced.Normalization.Effective = true, true
	unnormalized := priced
	unnormalized.Parameter = "display:0:0:Video Output"
	unnormalized.Normalization.Comparable, unnormalized.Normalization.Effective = false, false
	unnormalized.Provenance = json.RawMessage(`not json`)
	build := func(details bool, components ...catalogsync.Component) consumerPricingModel {
		document := catalogsync.Document{ID: "runway/gen-4.5", Offers: []catalogsync.Offer{{ID: "runway", Scope: "endpoint", Provider: &catalogsync.Provider{Tag: common.GetPointer("runway")}, Variant: json.RawMessage(`{"variant":"standard"}`), Eligibility: &catalogsync.Eligibility{Eligible: true}, Components: components}}}
		route := model.UpstreamCatalogRoute{Operation: "video_generation", Mode: "sync", ModelID: document.ID, DocumentHash: catalogsync.Hash([]byte("duplicate")), Document: &document}
		item := model.Pricing{ModelName: "gen", EnableGroup: []string{"default"}, UpstreamCatalogRoutesByGroup: map[string][]model.UpstreamCatalogRoute{"default": {route}}}
		return buildConsumerPricingModel(map[string]string{"default": "Default"}, map[string]float64{"default": 1}, 1, item, details)
	}
	baseline := build(false, priced)
	require.NotEmpty(t, baseline.GroupPrices[0].Operations)
	for _, details := range []bool{false, true} {
		deduplicated := build(details, unnormalized, priced)
		assert.Equal(t, baseline.GroupPrices[0].Operations, deduplicated.GroupPrices[0].Operations)
		if details {
			prices := deduplicated.GroupPrices[0].ProviderOffers[0].Prices
			require.Len(t, prices, 1)
			assert.Equal(t, "runway_video:cents_per_second_output", prices[0].Parameter)
			assert.NotNil(t, prices[0].AmountRub)
		}
	}
}

func TestSyncUpstreamCatalogAcquisitionAndGenerationFence(t *testing.T) {
	db := openChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CatalogSource{}, &model.CatalogRevision{}, &model.CatalogDocument{}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	manifestRaw, err := os.ReadFile("../model/testdata/upstream_catalog/manifest.json")
	require.NoError(t, err)
	var manifest catalogsync.Manifest
	require.NoError(t, common.Unmarshal(manifestRaw, &manifest))
	require.NoError(t, manifest.Validate())
	wire := map[string]string{"/v1/catalog/manifest": string(manifestRaw)}
	for i, name := range []string{"direct", "aggregator", "alias"} {
		raw, err := os.ReadFile("../model/testdata/upstream_catalog/" + name + ".json")
		require.NoError(t, err)
		require.Equal(t, manifest.Models[i].Hash, catalogsync.Hash(raw))
		wire["/v1/catalog/documents/"+manifest.Models[i].Hash] = string(raw)
	}
	directPath := "/v1/catalog/documents/" + manifest.Models[0].Hash
	var manifestCalls, documentCalls atomic.Int64
	var failDirect atomic.Bool
	// Each client has its own transport; immutable wire bodies are safe for the
	// document workers. The optional barrier runs exactly at Manifest GET.
	newClient := func(manifestBarrier func(*http.Request) error) *catalogsync.Client {
		client, err := catalogsync.NewClient("https://catalog.example.test", &http.Client{Transport: pricingCatalogTestTransport(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet {
				return nil, fmt.Errorf("unexpected catalog method %s", request.Method)
			}
			path := request.URL.Path
			if path == "/v1/catalog/manifest" {
				manifestCalls.Add(1)
				if manifestBarrier != nil {
					if err := manifestBarrier(request); err != nil {
						return nil, err
					}
				}
			} else {
				documentCalls.Add(1)
			}
			body, ok := wire[path]
			if !ok {
				return nil, fmt.Errorf("unexpected catalog URL %s", request.URL)
			}
			status := http.StatusOK
			if path == directPath && failDirect.Load() {
				status, body = http.StatusServiceUnavailable, `{"error":"fixture acquisition failed"}`
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		})})
		require.NoError(t, err)
		return client
	}
	client := newClient(nil)
	source := model.CatalogSource{ID: model.CatalogHash([]byte(t.Name())), Kind: "openrouter", BaseURL: client.BaseURL()}
	requestedAlias := map[string]bool{"router/latest": true}
	initial, err := service.SyncUpstreamCatalog(ctx, source, requestedAlias, client)
	require.NoError(t, err)
	assert.True(t, initial.Publication.Changed)
	assert.Equal(t, 1, initial.ImportedModels)
	assert.Empty(t, initial.MissingModels)
	assert.Equal(t, 2, initial.Publication.NewDocuments) // alias plus declared target
	assert.EqualValues(t, 1, manifestCalls.Load())
	assert.EqualValues(t, 2, documentCalls.Load())
	selection, err := model.ReadCatalogSelection(ctx, source.ID, initial.Publication.Revision)
	require.NoError(t, err)
	assert.Equal(t, []model.CatalogModelReference{{ID: manifest.Models[1].ID, Hash: manifest.Models[1].Hash}, {ID: manifest.Models[2].ID, Hash: manifest.Models[2].Hash}}, selection.Models)
	stored, err := model.GetCatalogDocuments(ctx, source.ID, []string{manifest.Models[1].Hash, manifest.Models[2].Hash})
	require.NoError(t, err)
	require.Len(t, stored, 2)
	for hash, row := range stored {
		assert.Equal(t, wire["/v1/catalog/documents/"+hash], row.Payload)
	}

	t.Run("unchanged_selection_fetches_only_manifest", func(t *testing.T) {
		before, err := model.EnsureCatalogSource(ctx, source)
		require.NoError(t, err)
		result, err := service.SyncUpstreamCatalog(ctx, source, requestedAlias, client)
		require.NoError(t, err)
		assert.False(t, result.Publication.Changed)
		assert.Equal(t, initial.Publication.Revision, result.Publication.Revision)
		assert.Zero(t, result.Publication.NewDocuments)
		assert.Equal(t, 2, result.Publication.ReusedDocuments)
		assert.EqualValues(t, 2, manifestCalls.Load())
		assert.EqualValues(t, 2, documentCalls.Load())
		after, err := model.EnsureCatalogSource(ctx, source)
		require.NoError(t, err)
		assert.Equal(t, before.Generation+1, after.Generation)
		var revisions, documents int64
		require.NoError(t, db.Model(&model.CatalogRevision{}).Count(&revisions).Error)
		require.NoError(t, db.Model(&model.CatalogDocument{}).Count(&documents).Error)
		assert.EqualValues(t, 1, revisions)
		assert.EqualValues(t, 2, documents)
	})

	t.Run("failed_document_preserves_head", func(t *testing.T) {
		before, err := model.EnsureCatalogSource(ctx, source)
		require.NoError(t, err)
		failDirect.Store(true)
		defer failDirect.Store(false)
		_, err = service.SyncUpstreamCatalog(ctx, source, map[string]bool{"acme/direct": true}, client)
		require.ErrorContains(t, err, "catalog HTTP status 503")
		after, err := model.EnsureCatalogSource(ctx, source)
		require.NoError(t, err)
		assert.Equal(t, before, after)
		var revisions, documents int64
		require.NoError(t, db.Model(&model.CatalogRevision{}).Count(&revisions).Error)
		require.NoError(t, db.Model(&model.CatalogDocument{}).Count(&documents).Error)
		assert.EqualValues(t, 1, revisions)
		assert.EqualValues(t, 2, documents)
	})

	t.Run("generation_is_captured_before_manifest_get", func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		defer releaseOnce.Do(func() { close(release) })
		staleClient := newClient(func(request *http.Request) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-request.Context().Done():
				return request.Context().Err()
			}
		})
		staleResult := make(chan error, 1)
		go func() {
			_, err := service.SyncUpstreamCatalog(ctx, source, requestedAlias, staleClient)
			staleResult <- err
		}()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("stale sync did not reach Manifest GET before deadline")
		}
		// The older acquisition is still inside Manifest GET. A complete newer
		// selection must win even though the older response arrives afterwards.
		newer, err := service.SyncUpstreamCatalog(ctx, source, map[string]bool{"acme/direct": true}, client)
		require.NoError(t, err)
		require.True(t, newer.Publication.Changed)
		require.NotEqual(t, initial.Publication.Revision, newer.Publication.Revision)
		winner, err := model.EnsureCatalogSource(ctx, source)
		require.NoError(t, err)
		releaseOnce.Do(func() { close(release) })
		select {
		case err := <-staleResult:
			require.ErrorIs(t, err, model.ErrCatalogSourceChanged)
		case <-ctx.Done():
			t.Fatal("stale sync did not finish before deadline")
		}
		after, err := model.EnsureCatalogSource(ctx, source)
		require.NoError(t, err)
		assert.Equal(t, winner, after)
		current, err := model.ReadCatalogSelection(ctx, source.ID, after.Revision)
		require.NoError(t, err)
		assert.Equal(t, []model.CatalogModelReference{{ID: manifest.Models[0].ID, Hash: manifest.Models[0].Hash}}, current.Models)
		var revisions, documents int64
		require.NoError(t, db.Model(&model.CatalogRevision{}).Count(&revisions).Error)
		require.NoError(t, db.Model(&model.CatalogDocument{}).Count(&documents).Error)
		assert.EqualValues(t, 2, revisions)
		assert.EqualValues(t, 3, documents)
	})
}

func TestUpstreamCatalogLiveIntegration(t *testing.T) {
	base := os.Getenv("CATALOG_SYNC_TEST_API_URL")
	if base == "" {
		t.Skip("CATALOG_SYNC_TEST_API_URL not configured")
	}
	t.Setenv(model.CatalogAPIURLEnv, base)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMemory := common.MemoryCacheEnabled
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.MemoryCacheEnabled = previousMemory
		model.InvalidatePricingCache()
	})
	db := setupModelListControllerTestDB(t)
	common.MemoryCacheEnabled = false
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.ModelCostFXRow{}, &model.CatalogSource{}, &model.CatalogRevision{}, &model.CatalogDocument{}))
	source, enabled, err := model.ConfiguredCatalogSource()
	require.NoError(t, err)
	require.True(t, enabled)
	client, err := catalogsync.NewClient(base, http.DefaultClient)
	require.NoError(t, err)
	requested := map[string]bool{"google/gemini-2.5-flash": true}
	first, err := service.SyncUpstreamCatalog(context.Background(), source, requested, client)
	require.NoError(t, err)
	assert.Equal(t, 1, first.ImportedModels)
	assert.Equal(t, 1, first.Publication.NewDocuments)
	second, err := service.SyncUpstreamCatalog(context.Background(), source, requested, client)
	require.NoError(t, err)
	assert.Equal(t, first.Publication.Revision, second.Publication.Revision)
	assert.Zero(t, second.Publication.NewDocuments)
	mapping := `{"my-gemini":"google/gemini-2.5-flash"}`
	require.NoError(t, db.Create(&model.Channel{Id: 8172, Type: constant.ChannelTypeOpenRouter, Key: "unused", Status: common.ChannelStatusEnabled, Models: "my-gemini", ModelMapping: &mapping}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "my-gemini", ChannelId: 8172, Enabled: true}).Error)
	model.RefreshPricing()
	pricing := model.GetPricing()
	var selected *model.Pricing
	for i := range pricing {
		if pricing[i].ModelName == "my-gemini" {
			selected = &pricing[i]
		}
	}
	require.NotNil(t, selected)
	assert.Equal(t, "Google: Gemini 2.5 Flash", selected.DisplayName)
	routes := selected.UpstreamCatalogRoutesForGroup("default")
	require.NotEmpty(t, routes)
	assert.Equal(t, "google/gemini-2.5-flash", routes[0].ModelID)
	require.NotEmpty(t, routes[0].Document.Offers)
	projected := buildConsumerPricingModel(map[string]string{"default": "Default"}, map[string]float64{"default": 1}, 1, *selected, true)
	require.Len(t, projected.GroupPrices, 1)
	assert.NotEmpty(t, projected.GroupPrices[0].ProviderOffers)
	t.Logf("real HTTP -> immutable DB -> mapped NewAPI model: localRevision=%s offers=%d", first.Publication.Revision, len(projected.GroupPrices[0].ProviderOffers))
}

func TestEffectivePricingModelQuery(t *testing.T) {
	for _, raw := range []string{"", "model_name=", "model_name=a&model_name=b", "model_name=a&group=premium", "model_name=a&expected_revision=", "model_name=a&expected_revision=x&expected_revision=y"} {
		values, err := url.ParseQuery(raw)
		require.NoError(t, err)
		_, _, err = parseEffectivePricingModelQuery(values)
		require.Error(t, err, raw)
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "/api/user/effective-pricing/model?"+raw, nil)
		GetEffectivePricingModel(context)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, raw)
	}
	name, revision, err := parseEffectivePricingModelQuery(url.Values{"model_name": {"provider/model:batch"}, "expected_revision": {"captured"}})
	require.NoError(t, err)
	assert.Equal(t, "provider/model:batch", name)
	assert.Equal(t, "captured", revision)
}

// TestCatalogSemanticSnapshotProjection exercises the consumer projection with an
// unchanged published document, including Flex, native meters and context tiers.
func TestCatalogSemanticSnapshotProjection(t *testing.T) {
	raw, err := os.ReadFile("../model/testdata/upstream_catalog/gpt-5.4-semantic-poc.json")
	require.NoError(t, err)
	require.Equal(t, "c701c04af8a12644c4514f72de8269d6c01d23674d39757c511ce64641fe4452", catalogsync.Hash(raw))
	doc, err := catalogsync.DecodeDocument(raw, catalogsync.Reference{ID: "openai/gpt-5.4", Hash: catalogsync.Hash(raw)})
	require.NoError(t, err)
	route := model.UpstreamCatalogRoute{Operation: "text", Mode: "sync", ModelID: doc.ID, DocumentHash: catalogsync.Hash(raw), Document: doc}
	item := model.Pricing{ModelName: "gpt-5.4", EnableGroup: []string{"default"}, UpstreamCatalogRoutesByGroup: map[string][]model.UpstreamCatalogRoute{"default": {route}}}
	offers := upstreamProviderOffers(item, "default", 1, true, pricingUserContext{})
	// This immutable document includes one explicitly identified Batch sibling.
	// It must survive in Batch detail, but must not lower synchronous minima.
	batchRoute := route
	batchRoute.Mode, batchRoute.ModelID = "batch", doc.ID+":batch"
	batchItem := item
	batchItem.UpstreamCatalogRoutesByGroup = map[string][]model.UpstreamCatalogRoute{"default": {batchRoute}}
	batchOffers := upstreamProviderOffers(batchItem, "default", 1, true, pricingUserContext{})
	require.Len(t, batchOffers, 1)
	assert.Equal(t, "batch", batchOffers[0].Mode)
	require.Len(t, offers, len(doc.Offers)-1)
	for _, offer := range offers {
		assert.Equal(t, "sync", offer.Mode)
	}

	t.Run("semantic aliases produce one minimum for each component", func(t *testing.T) {
		minima := catalogMinima(offers)
		require.Len(t, minima, 1)
		amounts := map[string]float64{}
		for _, price := range minima[0].Prices {
			require.NotNil(t, price.AmountRub)
			assert.NotContains(t, price.Component, ":")
			amounts[price.Component] = *price.AmountRub
		}
		// Existing channels strip service_tier; opt-in tiers cannot lower their minimum.
		assert.Equal(t, map[string]float64{"input": 250, "output": 1500, "cache_read": 25, "web_search": 1}, amounts)
		wire, err := common.Marshal(minima)
		require.NoError(t, err)
		assert.NotContains(t, string(wire), "openai_responses:")
		t.Logf("normalized minima: %s", wire)
	})

	t.Run("normalization preserves every source amount condition and identity except shadowed model-page prices", func(t *testing.T) {
		before, after := []string{}, []string{}
		for _, original := range doc.Offers {
			scheduled := map[string]bool{}
			for _, rate := range original.Components {
				var provenance struct {
					Source string `json:"source"`
				}
				require.NoError(t, common.Unmarshal(rate.Provenance, &provenance))
				if provenance.Source == "generic" {
					scheduled[catalogComponentName(rate)+"/"+rate.Unit] = true
				}
			}
			kept := []catalogsync.Component{}
			for _, rate := range original.Components {
				var provenance struct {
					Source string `json:"source"`
				}
				require.NoError(t, common.Unmarshal(rate.Provenance, &provenance))
				component := catalogComponentName(rate)
				if provenance.Source == "model_page" && component != "unknown" && scheduled[component+"/"+rate.Unit] {
					continue
				}
				kept = append(kept, rate)
			}
			wire, err := common.Marshal([]any{original.Provider, original.Variant, kept})
			require.NoError(t, err)
			before = append(before, catalogsync.Hash(wire))
		}
		for _, offer := range append(append([]consumerProviderOffer{}, offers...), batchOffers...) {
			rates := make([]upstreamRate, 0, len(offer.Prices))
			for _, price := range offer.Prices {
				rates = append(rates, price.upstreamRate)
			}
			wire, err := common.Marshal([]any{offer.Provider, offer.Variant, rates})
			require.NoError(t, err)
			after = append(after, catalogsync.Hash(wire))
		}
		assert.ElementsMatch(t, before, after)
	})

	t.Run("each priced component of a provider has one price per context window", func(t *testing.T) {
		for _, offer := range offers {
			if offer.Provider == nil || offer.Provider.Tag == nil {
				continue
			}
			windows := map[string]int{}
			for _, price := range offer.Prices {
				if price.Component == "unknown" || price.AmountRub == nil {
					continue
				}
				assert.NotContains(t, string(price.Provenance), `"source":"model_page"`, *offer.Provider.Tag+" "+price.Parameter)
				windows[price.Component+"/"+price.Unit+"/"+string(price.Conditions)]++
			}
			for window, count := range windows {
				assert.Equal(t, 1, count, window)
			}
		}
	})

	t.Run("Flex standard and long context retain distinct price conditions", func(t *testing.T) {
		got := map[string]map[string]bool{}
		for _, offer := range offers {
			if offer.Provider == nil || offer.Provider.Tag == nil {
				continue
			}
			id := *offer.Provider.Tag
			if id != "openai" && id != "openai/flex" {
				continue
			}
			got[id] = map[string]bool{}
			for _, price := range offer.Prices {
				if price.Parameter != "prompt" {
					continue
				}
				var condition struct {
					Min int `json:"contextMin"`
					Max int `json:"contextMax"`
				}
				require.NoError(t, common.Unmarshal(price.Conditions, &condition))
				got[id][fmt.Sprintf("%d..%d=%s", condition.Min, condition.Max, *price.AmountUSD)] = true
			}
		}
		assert.Equal(t, map[string]bool{"0..272000=2.5": true, "272001..922000=5": true}, got["openai"])
		assert.Equal(t, map[string]bool{"0..272000=1.25": true, "272001..922000=2.5": true}, got["openai/flex"])
	})

	t.Run("public schedules partition overlapping contexts without native SKU leakage", func(t *testing.T) {
		tariff := buildCatalogModelTariff(effectivePricingPageContext{groupRatios: map[string]float64{"default": 1}, fxFactor: 1}, item, "default")
		require.Len(t, tariff.Operations, 1)
		operation := tariff.Operations[0]
		assert.True(t, operation.IsFrom)
		require.Len(t, operation.Tiers, 3)
		expected := []map[string]float64{
			{"input": 250, "output": 1500, "cache_read": 25, "web_search": 1},
			{"input": 500, "output": 2250, "cache_read": 50, "web_search": 1},
			{"input": 550, "output": 2475, "cache_read": 55, "web_search": 1},
		}
		for index, tier := range operation.Tiers {
			amounts := map[string]float64{}
			for _, price := range tier.Prices {
				require.NotNil(t, price.AmountRub)
				amounts[price.Component] = *price.AmountRub
			}
			assert.Equal(t, expected[index], amounts)
		}
		assert.Equal(t, float64(272000), *operation.Tiers[0].Condition.Value)
		assert.Equal(t, float64(922001), *operation.Tiers[2].Condition.Conditions[0].Value)
		wire, err := common.Marshal(tariff)
		require.NoError(t, err)
		assert.NotContains(t, string(wire), "openai_responses")
		assert.NotContains(t, string(wire), "provider")
		assert.NotContains(t, string(wire), "utc_day")
	})

	t.Run("allowed service tiers keep their context and request conditions", func(t *testing.T) {
		optIn := item
		optIn.UpstreamCatalogRoutesByGroup = map[string][]model.UpstreamCatalogRoute{"default": {route}}
		optIn.UpstreamCatalogRoutesByGroup["default"][0].ServiceTierEnabled = true
		projection := buildConsumerPricingModel(map[string]string{"default": "Default"}, map[string]float64{"default": 1}, 1, optIn, false)
		require.Len(t, projection.GroupPrices, 1)
		require.Len(t, projection.GroupPrices[0].Operations, 1)
		operation := projection.GroupPrices[0].Operations[0]
		require.Len(t, operation.Tiers, 7)
		wire, err := common.Marshal(operation.Tiers)
		require.NoError(t, err)
		assert.Contains(t, string(wire), `"string_value":"flex"`)
		assert.Contains(t, string(wire), `"string_value":"priority"`)
		assert.Contains(t, string(wire), `"string_value":"default"`)
		for _, tier := range operation.Tiers {
			encoded, err := common.Marshal(tier.Condition)
			require.NoError(t, err)
			assert.Contains(t, string(encoded), `"argument":"service_tier"`)
		}
		for _, price := range operation.Prices {
			if price.Component == "input" {
				assert.Equal(t, float64(125), *price.AmountRub)
			}
		}
		assert.Empty(t, projection.GroupPrices[0].ProviderOffers)
		optIn.UpstreamCatalogRoutesByGroup["default"] = append(optIn.UpstreamCatalogRoutesByGroup["default"], optIn.UpstreamCatalogRoutesByGroup["default"][0])
		duplicateRoute := buildConsumerPricingModel(map[string]string{"default": "Default"}, map[string]float64{"default": 1}, 1, optIn, false)
		assert.Equal(t, projection.GroupPrices, duplicateRoute.GroupPrices)
	})

	t.Run("unknown semantics and unsupported conditions never become public minima", func(t *testing.T) {
		unknown := "unrecognized_billable"
		conflict := "output"
		rates := []catalogsync.Component{
			{Parameter: "openai_responses:prompt_tokens", Billable: &unknown},
			{Parameter: "prompt", Billable: &unknown},
			{Parameter: "prompt", Billable: common.GetPointer("input_text"), Direction: &conflict},
			{Parameter: "prompt", Direction: &conflict},
			{Parameter: "prompt", Modality: common.GetPointer("image")},
		}
		for _, rate := range rates {
			assert.Equal(t, "unknown", catalogComponentName(rate))
		}
		price := offers[0].Prices[0]
		price.Conditions = []byte(`{"region":"us"}`)
		copy := offers[0]
		copy.minimumEligible = true
		copy.Availability = "eligible"
		copy.Eligibility = nil
		copy.Prices = []consumerProviderPrice{price}
		assert.Empty(t, catalogMinima([]consumerProviderOffer{copy})[0].Prices)
		for _, conditions := range []string{`{"contextMin":-1}`, `{"contextMin":100,"contextMax":10}`, `{"utcDay":7}`, `{"utcStart":1260}`, `{"contextMax":9007199254740992}`} {
			price.Conditions = []byte(conditions)
			_, valid := catalogRateWindow(price)
			assert.False(t, valid, conditions)
		}
	})

	t.Run("weekday and time discounts remain conditional", func(t *testing.T) {
		price := offers[0].Prices[0]
		price.Component, price.Unit = "input", "million_tokens"
		amount := "100"
		price.AmountRub = &amount
		price.Conditions = []byte(`{"contextMin":0,"contextMax":8192,"utcDay":1,"utcStart":1200,"utcEnd":1800}`)
		price.Provenance = []byte(`{"source":"generic"}`)
		offer := consumerProviderOffer{minimumEligible: true, Availability: "eligible", Operation: "text", Mode: "sync", Prices: []consumerProviderPrice{price}, Variant: []byte(`{}`)}
		minimum := catalogMinima([]consumerProviderOffer{offer})
		require.Len(t, minimum, 1)
		require.Len(t, minimum[0].Tiers, 1)
		wire, err := common.Marshal(minimum[0].Tiers[0].Condition)
		require.NoError(t, err)
		for _, field := range []string{`"argument":"utc_day"`, `"argument":"utc_hhmm"`, `"value":1200`, `"value":1800`, `"value":1`} {
			assert.Contains(t, string(wire), field)
		}

	})
}

func TestCatalogConsumerAuditCandidate534(t *testing.T) {
	fixturePath := os.Getenv("CATALOG_SEMANTIC_CANDIDATE_FIXTURE")
	if fixturePath == "" {
		t.Skip("immutable full-corpus candidate fixture not configured")
	}
	raw, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	var fixture struct {
		Channel  model.Channel
		Metadata []struct {
			ModelName               string `json:"model_name"`
			Description, Icon, Tags string
			VendorID                int `json:"vendor_id"`
		}
		Vendors             []model.PricingVendor
		Options             map[string]string
		Manifest, Documents string
		AuditRefs           string  `json:"audit_refs"`
		PublicBaseline      string  `json:"public_baseline"`
		FXRateUSDRUB        float64 `json:"fx_rate_usd_rub"`
	}
	require.NoError(t, common.Unmarshal(raw, &fixture))
	previousDB, previousLogDB, previousMemory := model.DB, model.LOG_DB, common.MemoryCacheEnabled
	previousRegistry := jsplugin.DefaultRegistry
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.MemoryCacheEnabled = previousMemory
		jsplugin.DefaultRegistry = previousRegistry
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		model.InvalidatePricingCache()
	})
	db := setupModelListControllerTestDB(t)
	common.MemoryCacheEnabled = false
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.CatalogSource{}, &model.CatalogRevision{}, &model.CatalogDocument{}, &model.ModelCostFXRow{}))
	require.NoError(t, config.GlobalConfig.LoadFromDB(fixture.Options))
	// Production handlers build metadata from authenticated user, group settings
	// and a published FX snapshot. Initialize those inputs, never patch envelopes.
	previousUsable, previousAuto := setting.UserUsableGroups2JSONString(), setting.AutoGroups2JsonString()
	previousGroupRatio, previousGroupGroupRatio := ratio_setting.GroupRatio2JSONString(), ratio_setting.GroupGroupRatio2JSONString()
	previousQuota, previousExchange := common.QuotaPerUnit, operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsable))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAuto))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatio))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(previousGroupGroupRatio))
		common.QuotaPerUnit, operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = previousQuota, previousExchange
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`[]`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	common.QuotaPerUnit, operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 500000, 100
	require.Positive(t, fixture.FXRateUSDRUB)
	// Reuse the existing cache-aware test seed: a new DB starts at version1,
	// while the process may already hold a newer publication from another test.
	seedNativeRouteFX(t, db)
	seededFX, err := model.CurrentModelCostFX("cbr-xml-daily.ru")
	require.NoError(t, err)
	require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{Source: "cbr-xml-daily.ru", EffectiveAt: seededFX.EffectiveAt + 1, FetchedAt: seededFX.FetchedAt + 1, Rates: map[string]float64{"USD": fixture.FXRateUSDRUB}}, seededFX.Version))
	actualFX, err := model.CurrentModelCostFX("cbr-xml-daily.ru")
	require.NoError(t, err)
	require.Equal(t, fixture.FXRateUSDRUB, actualFX.Rates["USD"])
	require.Greater(t, actualFX.Version, seededFX.Version)
	fixtureUser := model.User{Id: 900112, Username: "catalog-http-fixture", Group: "default", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&fixtureUser).Error)

	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	plugin, err := os.ReadFile("../plugins/tasks/openrouter-video/plugin.js")
	require.NoError(t, err)
	_, err = jsplugin.DefaultRegistry.Register(string(plugin), jsplugin.Options{})
	require.NoError(t, err)
	t.Setenv(model.CatalogAPIURLEnv, "https://catalog.fixture")
	source, enabled, err := model.ConfiguredCatalogSource()
	require.NoError(t, err)
	require.True(t, enabled)
	head, err := model.EnsureCatalogSource(context.Background(), source)
	require.NoError(t, err)
	raw, err = os.ReadFile(fixture.Manifest)
	require.NoError(t, err)
	var manifest catalogsync.Manifest
	require.NoError(t, common.Unmarshal(raw, &manifest))
	refs := make(map[string]string)
	selection := model.CatalogSelection{SchemaVersion: 1, NormalizationVersion: manifest.NormalizationVersion}
	documents := make([]model.CatalogDocument, 0, len(manifest.Models))
	for _, ref := range manifest.Models {
		raw, err = os.ReadFile(filepath.Join(fixture.Documents, ref.Hash+".json"))
		require.NoError(t, err)
		require.Equal(t, ref.Hash, catalogsync.Hash(raw))
		_, err = catalogsync.DecodeDocument(raw, ref)
		require.NoError(t, err)
		refs[ref.ID] = ref.Hash
		selection.Models = append(selection.Models, model.CatalogModelReference{ID: ref.ID, Hash: ref.Hash})
		documents = append(documents, model.CatalogDocument{SourceID: source.ID, ModelID: ref.ID, Hash: ref.Hash, Payload: string(raw)})
	}
	raw, err = os.ReadFile(fixture.AuditRefs)
	require.NoError(t, err)
	var audit struct {
		CatalogRevision string
		Documents       []struct{ ID, Hash string }
	}
	require.NoError(t, common.Unmarshal(raw, &audit))
	require.Equal(t, manifest.Revision, audit.CatalogRevision)
	require.Len(t, audit.Documents, 534)
	for _, ref := range audit.Documents {
		require.Equal(t, ref.Hash, refs[ref.ID], ref.ID)
	}
	_, err = model.PublishCatalogSelection(context.Background(), head, selection, manifest.Revision, manifest.FetchedAt, documents)
	require.NoError(t, err)
	fixture.Channel.Key = "unused-fixture-key"
	require.NoError(t, db.Create(&fixture.Channel).Error)
	for _, name := range strings.Split(fixture.Channel.Models, ",") {
		require.NoError(t, db.Create(&model.Ability{Model: name, Group: "default", ChannelId: fixture.Channel.Id, Enabled: true}).Error)
	}
	for _, meta := range fixture.Metadata {
		require.NoError(t, db.Create(&model.Model{ModelName: meta.ModelName, Description: meta.Description, Icon: meta.Icon, Tags: meta.Tags, VendorID: meta.VendorID, Status: 1, NameRule: model.NameRuleExact}).Error)
	}
	for _, vendor := range fixture.Vendors {
		require.NoError(t, db.Create(&model.Vendor{Id: vendor.ID, Name: vendor.Name, Description: vendor.Description, Icon: vendor.Icon}).Error)
	}
	model.RefreshPricing()
	items, _, revision, err := model.GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	context, err := loadConsumerPricingContext(fixtureUser.Group, "")
	require.NoError(t, err)
	context.userID = fixtureUser.Id
	require.Equal(t, "default", context.metadata.UserGroup)
	require.NotNil(t, context.metadata.AutoGroups)
	require.Empty(t, context.metadata.AutoGroups)
	page := buildEffectivePricingPage(context, effectivePricingPageQuery{limit: 1000, orderBy: "NAME"}, context.project)
	require.Equal(t, 534, page.TotalCount)
	require.Len(t, page.Data, 534)
	raw, err = os.ReadFile(fixture.PublicBaseline)
	require.NoError(t, err)
	var baseline []struct{ ModelName, DisplayName, Description, Modality string }
	require.NoError(t, common.Unmarshal(raw, &baseline))
	byName := make(map[string]struct{ DisplayName, Description, Modality string })
	for _, item := range baseline {
		byName[item.ModelName] = struct{ DisplayName, Description, Modality string }{item.DisplayName, item.Description, item.Modality}
	}
	counts := make(map[string]int)
	typeCounts := make(map[string]int)
	diff := []map[string]any{}
	for _, item := range page.Data {
		old, exists := byName[item.ModelName]
		require.True(t, exists, item.ModelName)
		assert.Equal(t, old.DisplayName, item.DisplayName, item.ModelName)
		assert.Equal(t, old.Description, item.Description, item.ModelName)
		counts[item.Modality]++
		typeCounts[item.CatalogType]++
		operations := []string{}
		for _, group := range item.GroupPrices {
			for _, operation := range group.Operations {
				operations = append(operations, operation.Operation+":"+operation.Mode)
			}
		}
		diff = append(diff, map[string]any{"modelName": item.ModelName, "displayName": item.DisplayName, "beforeModality": old.Modality, "afterModality": item.Modality, "candidateOperations": operations, "candidateGroups": item.GroupPrices})
	}
	assert.Equal(t, map[string]int{"TEXT": 342, "IMAGE": 60, "VIDEO": 30, "EMBEDDINGS": 30, "RERANK": 8, "OTHER": 64}, counts)
	assert.Equal(t, map[string]int{"text": 322, "image": 60, "video": 30, "embedding": 30, "rerank": 8, "speech": 34, "transcription": 26, "audio": 4, "decision": 20}, typeCounts)
	for _, item := range page.Data {
		if item.ModelName != "claude-fable-5.1" {
			continue
		}
		detail, found := buildEffectivePricingModelDetail(context, item.ModelName)
		require.True(t, found)
		require.Len(t, detail.Model.GroupPrices, 1)
		modes := make(map[string]float64)
		ids := make(map[string]bool)
		for _, offer := range detail.Model.GroupPrices[0].ProviderOffers {
			assert.False(t, ids[offer.ID], "detail offer IDs must be unique")
			ids[offer.ID] = true
		}
		for _, operation := range detail.Model.GroupPrices[0].Operations {
			for _, price := range operation.Prices {
				if price.Component == "output" && price.AmountRub != nil {
					modes[operation.Mode] = *price.AmountRub
				}
			}
		}
		assert.InDelta(t, 50*82.8504, modes["sync"], 1e-8)
		assert.InDelta(t, 25*82.8504, modes["batch"], 1e-8)
	}
	publicPage := buildPublicPricingModelsPage(context, publicPricingModelsQuery{page: 1, pageSize: 1000})
	require.Equal(t, 534, publicPage.TotalCount)
	details := make([]consumerPricingModelDetail, 0, len(page.Data))
	for _, item := range page.Data {
		detail, found := buildEffectivePricingModelDetail(context, item.ModelName)
		require.True(t, found, item.ModelName)
		for _, group := range detail.Model.GroupPrices {
			ids := make(map[string]bool)
			for _, offer := range group.ProviderOffers {
				assert.False(t, ids[offer.ID], "duplicate detail offer ID: %s", item.ModelName)
				ids[offer.ID] = true
			}
		}
		details = append(details, detail)
	}
	if output := os.Getenv("CATALOG_SEMANTIC_HTTP_OUTPUT"); output != "" {
		// Gin invokes the real controllers. Auth middleware is an explicit fixture
		// precondition; this test does not exercise credentials or live services.
		router := gin.New()
		router.Use(func(c *gin.Context) { c.Set("id", fixtureUser.Id); c.Next() })
		router.GET("/api/pricing/effective", GetEffectivePricingByGroup)
		router.GET("/api/user/effective-pricing", GetEffectivePricing)
		router.GET("/api/user/effective-pricing/model", GetEffectivePricingModel)
		requests := []struct {
			kind, path string
			query      map[string]any
		}{}
		for offset := 0; offset < page.TotalCount; offset += 100 {
			for _, endpoint := range []struct{ kind, path string }{{"public_page", "/api/pricing/effective"}, {"personal_page", "/api/user/effective-pricing"}} {
				requests = append(requests, struct {
					kind, path string
					query      map[string]any
				}{endpoint.kind, fmt.Sprintf("%s?limit=100&offset=%d", endpoint.path, offset), map[string]any{"limit": 100, "offset": offset}})
			}
		}
		for _, item := range page.Data {
			requests = append(requests, struct {
				kind, path string
				query      map[string]any
			}{"detail", "/api/user/effective-pricing/model?" + url.Values{"model_name": {item.ModelName}}.Encode(), map[string]any{"modelName": item.ModelName}})
		}
		captures := make([]map[string]any, 0, len(requests))
		rows := map[string]int{}
		for _, request := range requests {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, request.path, nil))
			require.Equal(t, http.StatusOK, recorder.Code, request.path)
			var response struct {
				Success bool
				Data    struct {
					SchemaVersion             string `json:"schema_version"`
					CatalogRevision           string `json:"catalog_revision"`
					UserGroup                 string `json:"user_group"`
					Currency                  string
					AutoGroups                []string `json:"auto_groups"`
					Data                      []effectivePricingPageItem
					Model                     effectivePricingPageItem
					Offset, Limit, TotalCount int
				}
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			require.NotEmpty(t, response.Data.CatalogRevision)
			require.Equal(t, "v2", response.Data.SchemaVersion)
			require.Equal(t, "RUB", response.Data.Currency)
			if request.kind != "public_page" {
				require.Equal(t, "default", response.Data.UserGroup)
				require.NotNil(t, response.Data.AutoGroups)
			}
			if request.kind == "detail" {
				require.Equal(t, request.query["modelName"], response.Data.Model.ModelName)
				rows[request.kind]++
			} else {
				rows[request.kind] += len(response.Data.Data)
			}
			captures = append(captures, map[string]any{"kind": request.kind, "status": recorder.Code, "query": request.query, "body": json.RawMessage(append([]byte(nil), recorder.Body.Bytes()...))})
		}
		assert.Equal(t, map[string]int{"public_page": 534, "personal_page": 534, "detail": 534}, rows)
		raw, err = common.Marshal(map[string]any{"evidence_class": "local Gin HTTP controllers; synthetic authenticated fixture user900112/group default/published FX; no live network or response metadata patches", "source_revision": manifest.SourceRevision, "candidate_revision": context.metadata.CatalogRevision, "captures": captures})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(output, raw, 0644))
	}
	if output := os.Getenv("CATALOG_SEMANTIC_CANDIDATE_OUTPUT"); output != "" {
		raw, err = common.Marshal(map[string]any{"evidence_class": "offline immutable full534 builder/API projection fixture; not deployed DEV", "catalog_revision": manifest.Revision, "source_revision": manifest.SourceRevision, "candidate_revision": revision, "counts": counts, "catalog_type_counts": typeCounts, "data": page.Data, "public": publicPage.Data, "details": details, "raw_alias_count": len(items), "per_model_diff": diff})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(output, raw, 0644))
	}
	t.Logf("534 exact-hash full builder fixtures: %v; revision=%s", counts, revision)
}

func TestCatalogTypeConsumerWireAndFiltering(t *testing.T) {
	items := []model.Pricing{}
	for i, kind := range []string{"text", "speech", "speech", "transcription", "audio", "decision", "other", ""} {
		items = append(items, model.Pricing{ModelName: fmt.Sprintf("model-%d", i), CatalogType: kind, CatalogModality: "OTHER", EnableGroup: []string{"default"}, Tags: "catalog"})
	}
	context := effectivePricingPageContext{pricing: items, usableGroups: map[string]string{"default": "Default"}, groupRatios: map[string]float64{"default": 1}, fxFactor: 1}
	for _, personal := range []bool{false, true} {
		query, err := parseEffectivePricingPageQuery(url.Values{"limit": {"1"}, "offset": {"1"}, "catalog_type": {"speech"}, "modality": {"OTHER"}}, personal)
		require.NoError(t, err)
		page := buildEffectivePricingPage(context, query, context.project)
		require.Equal(t, 2, page.TotalCount)
		require.Len(t, page.Data, 1)
		assert.Equal(t, "model-2", page.Data[0].ModelName)
		assert.Equal(t, "speech", page.Data[0].CatalogType)
		query.modality = "TEXT"
		assert.Zero(t, buildEffectivePricingPage(context, query, context.project).TotalCount)
	}
	publicQuery, err := parsePublicPricingModelsQuery("tag=catalog&catalog_type=speech&p=2&page_size=1")
	require.NoError(t, err)
	page := buildPublicPricingModelsPage(context, publicQuery)
	require.Equal(t, 2, page.TotalCount)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "speech", page.Data[0].CatalogType)
	for _, item := range items {
		detail, found := buildEffectivePricingModelDetail(context, item.ModelName)
		require.True(t, found)
		assert.Equal(t, item.CatalogType, detail.Model.CatalogType)
		wire, err := common.Marshal(detail.Model)
		require.NoError(t, err)
		if item.CatalogType == "" {
			assert.NotContains(t, string(wire), "catalog_type")
		} else {
			assert.Contains(t, string(wire), `"catalog_type":"`+item.CatalogType+`"`)
		}
	}
	for _, raw := range []string{"catalog_type=unknown", "catalog_type=Speech", "catalog_type=", "catalog_type=audio&catalog_type=speech"} {
		values, err := url.ParseQuery(raw)
		require.NoError(t, err)
		for _, personal := range []bool{true, false} {
			_, err = parseEffectivePricingPageQuery(values, personal)
			require.Error(t, err, raw)
		}
		_, err = parsePublicPricingModelsQuery("tag=catalog&" + raw)
		require.Error(t, err, raw)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/effective/overview?"+raw, nil)
		GetEffectivePricingOverview(c)
		assert.Equal(t, 400, recorder.Code, raw)
	}
	query, err := parseEffectivePricingPageQuery(url.Values{"catalog_type": {"speech"}}, false)
	require.NoError(t, err)
	query.limit = 1
	query.overview = true
	overview := buildEffectivePricingOverview(context, query)
	for _, section := range overview.Sections {
		if section.Modality == "OTHER" {
			assert.Equal(t, 2, section.TotalCount)
			require.Len(t, section.Data, 1)
			assert.Equal(t, "speech", section.Data[0].CatalogType)
		}
	}
}

func TestCatalogMissingTypeConsumerFiltering(t *testing.T) {
	items := []model.Pricing{}
	for i, kind := range []string{"text", "", "other", "", "speech", ""} {
		items = append(items, model.Pricing{ModelName: fmt.Sprintf("model-%d", i), CatalogType: kind, CatalogModality: "OTHER", EnableGroup: []string{"default"}, Tags: "catalog"})
	}
	context := effectivePricingPageContext{pricing: items, usableGroups: map[string]string{"default": "Default"}, groupRatios: map[string]float64{"default": 1}, fxFactor: 1}
	for _, personal := range []bool{false, true} {
		for pageIndex, expected := range [][]string{{"model-1", "model-3"}, {"model-5"}, {}} {
			values := url.Values{"limit": {"2"}, "offset": {strconv.Itoa(pageIndex * 2)}, "catalog_type_missing": {"true"}}
			query, err := parseEffectivePricingPageQuery(values, personal)
			require.NoError(t, err)
			page := buildEffectivePricingPage(context, query, context.project)
			assert.Equal(t, 3, page.TotalCount)
			names := []string{}
			for _, item := range page.Data {
				names = append(names, item.ModelName)
				assert.Empty(t, item.CatalogType)
			}
			assert.Equal(t, expected, names)
		}
		for _, selector := range []string{"", "false"} {
			values := url.Values{"limit": {"10"}}
			if selector != "" {
				values.Set("catalog_type_missing", selector)
			}
			query, err := parseEffectivePricingPageQuery(values, personal)
			require.NoError(t, err)
			assert.Equal(t, 6, buildEffectivePricingPage(context, query, context.project).TotalCount)
		}
		query, err := parseEffectivePricingPageQuery(url.Values{"limit": {"10"}, "catalog_type": {"other"}}, personal)
		require.NoError(t, err)
		page := buildEffectivePricingPage(context, query, context.project)
		require.Len(t, page.Data, 1)
		assert.Equal(t, "model-2", page.Data[0].ModelName)
	}
	for pageIndex, expected := range [][]string{{"model-1", "model-3"}, {"model-5"}, {}} {
		query, err := parsePublicPricingModelsQuery(fmt.Sprintf("tag=catalog&catalog_type_missing=true&page_size=2&p=%d", pageIndex+1))
		require.NoError(t, err)
		page := buildPublicPricingModelsPage(context, query)
		assert.Equal(t, 3, page.TotalCount)
		names := []string{}
		for _, item := range page.Data {
			names = append(names, item.ModelName)
			assert.Empty(t, item.CatalogType)
		}
		assert.Equal(t, expected, names)
	}
	for _, selector := range []string{"", "&catalog_type_missing=false"} {
		query, err := parsePublicPricingModelsQuery("tag=catalog&page_size=10" + selector)
		require.NoError(t, err)
		assert.Equal(t, 6, buildPublicPricingModelsPage(context, query).TotalCount)
	}
	query, err := parseEffectivePricingPageQuery(url.Values{"catalog_type_missing": {"true"}}, false)
	require.NoError(t, err)
	query.limit = 2
	query.overview = true
	overview := buildEffectivePricingOverview(context, query)
	total := 0
	for _, section := range overview.Sections {
		total += section.TotalCount
		for _, item := range section.Data {
			assert.Empty(t, item.CatalogType)
		}
	}
	assert.Equal(t, 3, total)
	for _, raw := range []string{"catalog_type_missing=", "catalog_type_missing=1", "catalog_type_missing=True", "catalog_type_missing=true&catalog_type_missing=false", "catalog_type_missing=false&catalog_type_missing=false", "catalog_type_missing=true&catalog_type=text"} {
		values, err := url.ParseQuery(raw)
		require.NoError(t, err)
		for _, personal := range []bool{false, true} {
			_, err = parseEffectivePricingPageQuery(values, personal)
			require.Error(t, err, raw)
		}
		_, err = parsePublicPricingModelsQuery("tag=catalog&" + raw)
		require.Error(t, err, raw)
		for index, handler := range []gin.HandlerFunc{GetEffectivePricingOverview, GetPublicPricingModels} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			requestQuery := raw
			if index == 1 {
				requestQuery = "tag=catalog&" + raw
			}
			c.Request = httptest.NewRequest(http.MethodGet, "/?"+requestQuery, nil)
			handler(c)
			assert.Equal(t, http.StatusBadRequest, recorder.Code, raw)
		}
	}
	for _, raw := range []string{"", "catalog_type_missing=false", "catalog_type_missing=true", "catalog_type_missing=invalid"} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/?"+raw, nil)
		handled := routeEffectivePricingPage(c, false)
		if raw == "catalog_type_missing=true" || raw == "catalog_type_missing=invalid" {
			assert.True(t, handled)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		} else {
			assert.False(t, handled)
		}
	}
}

func TestCatalogVariantMinimaKeepDistinctPrices(t *testing.T) {
	rate := func(component, unit, amount, variant string) consumerProviderPrice {
		price := consumerProviderPrice{Component: component, AmountRub: &amount}
		price.Unit, price.Quantity = unit, "1"
		price.Variant = []byte(variant)
		price.Provenance = []byte(`{"source":"generic"}`)
		return price
	}
	offer := func(prices ...consumerProviderPrice) consumerProviderOffer {
		return consumerProviderOffer{minimumEligible: true, Availability: "eligible", Operation: "video", Mode: "sync", Variant: []byte(`{}`), Prices: prices}
	}
	tierView := func(t *testing.T, operation consumerPricingOperation) map[string]float64 {
		result := map[string]float64{}
		for _, tier := range operation.Tiers {
			label := "none"
			if tier.Condition != nil {
				require.Equal(t, "comparison", tier.Condition.Kind)
				require.Equal(t, "usage", tier.Condition.Subject)
				require.NotNil(t, tier.Condition.StringValue)
				label = tier.Condition.Argument + "=" + *tier.Condition.StringValue
			}
			require.Len(t, tier.Prices, 1)
			result[label+"/"+tier.Prices[0].Component] = *tier.Prices[0].AmountRub
		}
		return result
	}

	t.Run("resolution variants are separate tiers and the summary is a from price", func(t *testing.T) {
		minima := catalogMinima([]consumerProviderOffer{offer(
			rate("output_video", "second", "4.25", `{"resolution":"480p"}`),
			rate("output_video", "second", "8.5", `{"resolution":"720p"}`),
			rate("output_video", "second", "17", `{"resolution":"1080p"}`),
		)})
		require.Len(t, minima, 1)
		require.Len(t, minima[0].Prices, 1)
		assert.Equal(t, 4.25, *minima[0].Prices[0].AmountRub)
		require.NotNil(t, minima[0].IsFrom)
		assert.True(t, *minima[0].IsFrom)
		assert.Equal(t, map[string]float64{"resolution=480p/output_video": 4.25, "resolution=720p/output_video": 8.5, "resolution=1080p/output_video": 17}, tierView(t, minima[0]))
	})

	t.Run("multilingual and operation variants stay next to the base price", func(t *testing.T) {
		minima := catalogMinima([]consumerProviderOffer{offer(
			rate("input_audio", "second", "0.43", `{}`),
			rate("input_audio", "second", "0.52", `{"multilingual":true}`),
			rate("output_video", "second", "10", `{"resolution":"480p"}`),
			rate("output_video", "second", "12", `{"operation":"reference_to_video"}`),
		)})
		require.Len(t, minima, 1)
		assert.True(t, *minima[0].IsFrom)
		assert.Equal(t, map[string]float64{"none/input_audio": 0.43, "multilingual=true/input_audio": 0.52, "resolution=480p/output_video": 10, "operation=reference_to_video/output_video": 12}, tierView(t, minima[0]))
	})

	t.Run("a single unconditional price is exact", func(t *testing.T) {
		minima := catalogMinima([]consumerProviderOffer{offer(rate("output_video", "second", "5", `{}`))})
		require.Len(t, minima, 1)
		require.NotNil(t, minima[0].IsFrom)
		assert.False(t, *minima[0].IsFrom)
		require.Len(t, minima[0].Tiers, 1)
		assert.Nil(t, minima[0].Tiers[0].Condition)
		same := catalogMinima([]consumerProviderOffer{offer(rate("output_video", "second", "5", `{}`)), offer(rate("output_video", "second", "5", `{}`))})
		assert.False(t, *same[0].IsFrom)
		cheaper := catalogMinima([]consumerProviderOffer{offer(rate("output_video", "second", "5", `{}`)), offer(rate("output_video", "second", "4", `{}`))})
		assert.True(t, *cheaper[0].IsFrom)
		assert.Equal(t, 4., *cheaper[0].Prices[0].AmountRub)
	})

	t.Run("merging an equal local price keeps it exact", func(t *testing.T) {
		amount, other := 5., 6.
		upstream := catalogMinima([]consumerProviderOffer{offer(rate("output_video", "second", "5", `{}`))})
		merged := mergeCatalogMinima([]consumerPricingOperation{{Operation: "video", Mode: "sync", Prices: []consumerUnitPrice{{Component: "output_video", Unit: "second", AmountRub: &amount}}}}, upstream)
		assert.False(t, *merged[0].IsFrom)
		merged = mergeCatalogMinima([]consumerPricingOperation{{Operation: "video", Mode: "sync", Prices: []consumerUnitPrice{{Component: "output_video", Unit: "second", AmountRub: &other}}}}, upstream)
		assert.True(t, *merged[0].IsFrom)
		assert.Equal(t, 5., *merged[0].Prices[0].AmountRub)
	})

	t.Run("nested variant values cannot become an unconditional minimum", func(t *testing.T) {
		minima := catalogMinima([]consumerProviderOffer{offer(rate("output_video", "second", "1", `{"resolution":{"width":640}}`))})
		require.Len(t, minima, 1)
		assert.Empty(t, minima[0].Prices)
	})
}

func TestConsumerPricingTagsPreserveStoredTags(t *testing.T) {
	item := model.Pricing{ModelName: "deepgram/nova-3", Tags: "Transcription,Audio,top,audio", EnableGroup: []string{"default"}}
	projection := buildConsumerPricingModel(map[string]string{"default": "Default"}, map[string]float64{"default": 1}, 1, item, false)
	assert.Equal(t, "Transcription,Audio,top", projection.Tags)

	for _, tc := range []struct{ stored, expected string }{
		{"Open Weights", "Open Weights"},
		{" Open Weights , open weights,1M ,1m", "Open Weights,1M"},
		{"Transcription,Audio,top,audio", "Transcription,Audio,top"},
		{"1M", "1M"},
		{" , ,", ""},
	} {
		assert.Equal(t, tc.expected, consumerPricingTags(tc.stored), tc.stored)
	}
}
