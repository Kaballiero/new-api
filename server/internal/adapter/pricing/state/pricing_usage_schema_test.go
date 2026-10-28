package pricingstate_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	publication "github.com/QuantumNous/new-api/internal/adapter/settings/publication"
	pricingruntime "github.com/QuantumNous/new-api/internal/app/pricingruntime"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSoraLegacyPriceBasisFollowsPluginRouteAndUsage(t *testing.T) {
	source, err := os.ReadFile("../../../../plugins/tasks/sora/plugin.js")
	require.NoError(t, err)
	registry := jsplugin.NewRegistry()
	plugin, err := registry.Register(string(source), jsplugin.Options{})
	require.NoError(t, err)
	for _, action := range []string{constant.TaskActionTextToVideo, constant.TaskActionImageToVideo, constant.TaskActionRemix} {
		usage, err := plugin.Engine.Call(context.Background(), "extractUsage", map[string]any{
			"action": action, "requestBody": map[string]any{"seconds": 8},
		})
		require.NoError(t, err)
		facts, ok := usage.(map[string]any)
		require.True(t, ok)
		basis := plugin.Meta.LegacyPriceBasis[action]
		if action == constant.TaskActionRemix {
			assert.Empty(t, facts)
			assert.Equal(t, "call", basis.Unit)
		} else {
			assert.EqualValues(t, 8, facts[basis.UsageField])
			assert.Equal(t, "second", basis.Unit)
		}
	}
	mapping := `{"video-alias":"sora-2"}`
	channel := &legacy.Channel{Type: constant.ChannelTypeOpenAI, ModelMapping: &mapping}
	actual, found, _ := pricingstate.TestLegacyVideoPriceBasis(registry.Generation(), channel, "video-alias", "")
	require.True(t, found)
	assert.Equal(t, plugin.Meta.LegacyPriceBasis, actual)
	_, found, _ = pricingstate.TestLegacyVideoPriceBasis(registry.Generation(), &legacy.Channel{Type: constant.ChannelTypeOpenRouter}, "sora-2", "")
	assert.False(t, found)
	_, found, _ = pricingstate.TestLegacyVideoPriceBasis(registry.Generation(), channel, "unknown", "")
	assert.False(t, found)
	snapshot := registry.Snapshot()
	require.Len(t, snapshot.Override, 1)
	delete(snapshot.Override[0].LegacyPriceBasis, constant.TaskActionRemix)
	assert.Contains(t, plugin.Meta.LegacyPriceBasis, constant.TaskActionRemix)

	otherSource := strings.Replace(string(source), `key: "sora"`, `key: "other-sora"`, 1)
	otherSource = strings.Replace(otherSource, "channelTypes: [55, 1]", "channelTypes: [56]", 1)
	otherSource = strings.ReplaceAll(otherSource, `{ unit: "second", usageField: "seconds" }`, `{ unit: "call" }`)
	otherPlugin, err := registry.Register(otherSource, jsplugin.Options{})
	require.NoError(t, err)
	actual, found, _ = pricingstate.TestLegacyVideoPriceBasis(registry.Generation(), &legacy.Channel{Type: 56}, "sora-2", "")
	require.True(t, found)
	assert.Equal(t, otherPlugin.Meta.LegacyPriceBasis, actual)
	bound := &legacy.Channel{Type: constant.ChannelTypeTaskPlugin}
	bound.SetSetting(dto.ChannelSettings{TaskPluginKey: "other-sora"})
	actual, found, _ = pricingstate.TestLegacyVideoPriceBasis(registry.Generation(), bound, "sora-2", "other-sora")
	require.True(t, found)
	assert.Equal(t, otherPlugin.Meta.LegacyPriceBasis, actual)
	actual, found, _ = pricingstate.TestLegacyVideoPriceBasis(registry.Generation(), channel, "sora-2", "")
	require.True(t, found)
	assert.Equal(t, plugin.Meta.LegacyPriceBasis, actual)
}

func TestLegacyPriceBasisMetadataRejectsInvalidDeclarations(t *testing.T) {
	source := pricingUsagePluginSource("1.0.0", `{seconds: {type: "number", unit: "second"}}`)
	for _, declaration := range []string{
		`null`, `{}`, `{generate: {unit: "call"}}`,
		`{text_to_video: {unit: "second", usageField: "missing"}}`,
		`{text_to_video: {unit: "call", usageField: "seconds"}}`,
		`{text_to_video: {unit: "token", usageField: "seconds"}}`,
		`{text_to_video: {unit: "second", usageField: "seconds", price: 1}}`,
	} {
		t.Run(declaration, func(t *testing.T) {
			invalid := strings.Replace(source, "usageSchema:", "legacyPriceBasis: "+declaration+", usageSchema:", 1)
			_, err := jsplugin.CompilePlugin(invalid, jsplugin.Options{})
			require.ErrorContains(t, err, "legacyPriceBasis")
		})
	}
	plugin, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
	require.NoError(t, err)
	assert.Nil(t, plugin.Meta.LegacyPriceBasis)
}

func TestPricingSoraBasisFollowsEnabledGroupRoutes(t *testing.T) {
	resetPricingEndpointTestTables(t)
	source, err := os.ReadFile("../../../../plugins/tasks/sora/plugin.js")
	require.NoError(t, err)
	previousRegistry := jsplugin.DefaultRegistry
	registry := jsplugin.NewRegistry()
	jsplugin.DefaultRegistry = registry
	t.Cleanup(func() {
		jsplugin.DefaultRegistry = previousRegistry
		pricingstate.InvalidatePricingCache()
	})
	_, err = registry.Register(string(source), jsplugin.Options{})
	require.NoError(t, err)
	orSource := strings.Replace(string(source), `key: "sora"`, `key: "or-video-probe"`, 1)
	orSource = strings.Replace(orSource, "channelTypes: [55, 1]", "channelTypes: [20]", 1)
	_, err = registry.Register(orSource, jsplugin.Options{})
	require.NoError(t, err)

	mapping := `{"video-alias":"sora-2"}`
	require.NoError(
		t,
		legacy.DB.Create(&legacy.Channel{
			Id:           931,
			Type:         constant.ChannelTypeOpenAI,
			Key:          "test",
			Status:       1,
			Models:       "sora-2,video-alias,ordinary-model",
			ModelMapping: &mapping,
		}).Error,
	)
	insertPricingEndpointAbility(t, 931, "sora-2")
	insertPricingEndpointAbility(t, 931, "video-alias")
	insertPricingEndpointAbility(t, 931, "ordinary-model")
	insertPricingEndpointChannel(t, 932, constant.ChannelTypeOpenRouter, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 932, "sora-2")
	channelstore.InitChannelCache()
	pricing := pricingByModel(pricingruntime.GetPricing())
	assert.Equal(
		t,
		[]string{
			pricingdomain.BillingSourceConfiguredTariff,
			pricingdomain.BillingSourceOpenRouterActualCost,
		},
		pricing["sora-2"].BillingSourcesForGroup("default")[constant.EndpointTypeOpenAIVideo],
	)
	basis, known := pricing["sora-2"].LegacyVideoPriceBasisForGroup("default")
	require.True(t, known)
	assert.Equal(t, "second", basis[constant.TaskActionTextToVideo].Unit)
	aliasBasis, known := pricing["video-alias"].LegacyVideoPriceBasisForGroup("default")
	require.True(t, known)
	assert.Equal(t, basis, aliasBasis)
	assert.NotContains(t, pricing["ordinary-model"].BillingSourcesForGroup("default"), constant.EndpointTypeOpenAIVideo)
	_, known = pricing["sora-2"].LegacyVideoPriceBasisForGroup("premium")
	assert.False(t, known)

	otherSource := strings.Replace(string(source), `key: "sora"`, `key: "other-sora"`, 1)
	otherSource = strings.Replace(otherSource, "channelTypes: [55, 1]", "channelTypes: [56]", 1)
	otherSource = strings.ReplaceAll(otherSource, `{ unit: "second", usageField: "seconds" }`, `{ unit: "call" }`)
	_, err = registry.Register(otherSource, jsplugin.Options{})
	require.NoError(t, err)
	insertPricingEndpointChannel(t, 933, 56, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 933, "sora-2")
	insertPricingEndpointChannel(
		t,
		942,
		constant.ChannelTypeAdvancedCustom,
		pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{
			IncomingPath: "/v1/images/generations",
			UpstreamPath: "/v1/images/generations",
		}),
	)
	insertPricingEndpointAbility(t, 942, "flux-video-edit")
	channelstore.InitChannelCache()
	pricingstate.InvalidatePricingCache()
	pricing = pricingByModel(pricingruntime.GetPricing())
	_, known = pricing["sora-2"].LegacyVideoPriceBasisForGroup("default")
	assert.False(t, known)
}

func TestPricingVideoPluginReplacesOnlyInferredImageEndpoint(t *testing.T) {
	resetPricingEndpointTestTables(t)
	source, err := os.ReadFile("../../../../plugins/tasks/openrouter-video/plugin.js")
	require.NoError(t, err)
	previousRegistry := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	t.Cleanup(func() { jsplugin.DefaultRegistry = previousRegistry; pricingstate.InvalidatePricingCache() })
	_, err = jsplugin.DefaultRegistry.Register(string(source), jsplugin.Options{})
	require.NoError(t, err)
	mapping := `{"flux-3-video":"black-forest-labs/flux-3-video","flux-video-edit":"black-forest-labs/flux-video-edit","flux-video-upscale":"black-forest-labs/flux-video-upscale","grok-imagine-video":"x-ai/grok-imagine-video","grok-imagine-video-1.5":"x-ai/grok-imagine-video-1.5","grok-imagine-video-1.5-lite":"x-ai/grok-imagine-video-1.5-lite"}`
	models := []string{
		"flux-3-video",
		"flux-video-edit",
		"flux-video-upscale",
		"grok-imagine-video",
		"grok-imagine-video-1.5",
		"grok-imagine-video-1.5-lite",
	}
	require.NoError(
		t,
		legacy.DB.Create(&legacy.Channel{
			Id:     940,
			Type:   constant.ChannelTypeOpenRouter,
			Key:    "test",
			Status: 1,
			Models: strings.Join(
				models,
				",",
			),
			ModelMapping: &mapping,
		}).Error,
	)
	for _, name := range models {
		insertPricingEndpointAbility(t, 940, name)
	}
	channelstore.InitChannelCache()
	pricing := pricingByModel(pricingruntime.GetPricing())
	for _, name := range models {
		assert.Contains(t, pricing[name].SupportedEndpointTypes, constant.EndpointTypeOpenAIVideo)
		assert.NotContains(t, pricing[name].SupportedEndpointTypes, constant.EndpointTypeImageGeneration)
		assert.NotContains(t, pricing[name].BillingSourcesForGroup("default"), constant.EndpointTypeImageGeneration)
	}
	require.NoError(t, legacy.DB.Create(&legacy.Model{ModelName: "flux-3-video", Endpoints: `{"image-generation":"/v1/images/generations"}`}).Error)
	insertPricingEndpointChannel(t, 941, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 941, "grok-imagine-video")
	insertPricingEndpointChannel(
		t,
		942,
		constant.ChannelTypeAdvancedCustom,
		pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{
			IncomingPath: "/v1/images/generations",
			UpstreamPath: "/v1/images/generations",
		}),
	)
	insertPricingEndpointAbility(t, 942, "flux-video-edit")
	channelstore.InitChannelCache()
	pricingstate.InvalidatePricingCache()
	pricing = pricingByModel(pricingruntime.GetPricing())
	assert.Contains(t, pricing["flux-3-video"].SupportedEndpointTypes, constant.EndpointTypeImageGeneration)
	assert.Contains(t, pricing["grok-imagine-video"].SupportedEndpointTypes, constant.EndpointTypeImageGeneration)
	assert.Contains(t, pricing["grok-imagine-video"].BillingSourcesForGroup("default"), constant.EndpointTypeImageGeneration)
	assert.Contains(t, pricing["flux-video-edit"].SupportedEndpointTypes, constant.EndpointTypeImageGeneration)
	assert.Equal(
		t,
		[]string{pricingdomain.BillingSourceConfiguredTariff},
		pricing["flux-video-edit"].BillingSourcesForGroup("default")[constant.EndpointTypeImageGeneration],
	)
	assert.Equal(
		t,
		[]string{pricingdomain.BillingSourceOpenRouterActualCost},
		pricing["flux-video-edit"].BillingSourcesForGroup("default")[constant.EndpointTypeOpenAIVideo],
	)
}

func pricingUsagePluginSource(version, usageSchema string) string {
	return fmt.Sprintf(`
export const meta = {
  apiVersion: 1, key: "pricing-usage-probe", name: "Pricing Usage Probe", version: %q, author: {name: "Test"},
  models: ["pricing-usage-model"], fetchMode: "per_task", usageSchema: %s
};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`, version, usageSchema)
}

func TestPricingCarriesTaskUsageSchemaAndRefreshesWithPluginGeneration(t *testing.T) {
	resetPricingEndpointTestTables(t)
	const pluginKey = "pricing-usage-probe"
	initialSource := pricingUsagePluginSource("1.0.0", `{
  seconds: {type: "number", unit: "second", description: "Estimated duration."},
  action: {enum:["video"],enumLabels:{video:{en:"Generate video",zh:"生成视频"}}}
}`)
	_, err := jsplugin.DefaultRegistry.Register(initialSource, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister(pluginKey) })

	insertPricingEndpointChannel(t, 901, constant.ChannelTypeTaskPlugin, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 901, "pricing-usage-model")
	insertPricingEndpointAbility(t, 901, "ordinary-model")

	initialPricing := pricingByModel(pricingruntime.GetPricing())
	require.Contains(t, initialPricing, "pricing-usage-model")
	require.Contains(t, initialPricing, "ordinary-model")
	assert.Equal(t, "second", initialPricing["pricing-usage-model"].BillingUsageSchema["seconds"].Unit)
	assert.Equal(t, "Estimated duration.", initialPricing["pricing-usage-model"].BillingUsageSchema["seconds"].Description["en"])
	var videoLabel map[string]string
	require.NoError(t, common.Unmarshal(initialPricing["pricing-usage-model"].BillingUsageSchema["action"].EnumLabels["video"], &videoLabel))
	assert.Equal(t, "生成视频", videoLabel["zh"])
	assert.Nil(t, initialPricing["ordinary-model"].BillingUsageSchema)

	updatedSource := pricingUsagePluginSource("1.1.0", `{
  seconds: {type: "number", unit: "second", description: "Measured duration."},
  clips: {type: "number", unit: "count", description: "Generated clip count."}
}`)
	_, err = jsplugin.DefaultRegistry.Register(updatedSource, jsplugin.Options{})
	require.NoError(t, err)
	(*pricingstate.TestLastGetPricingTime) = time.Now().Add(-2 * time.Minute)

	refreshedPricing := pricingByModel(pricingruntime.GetPricing())
	require.Len(t, refreshedPricing["pricing-usage-model"].BillingUsageSchema, 2)
	assert.Equal(t, "Measured duration.", refreshedPricing["pricing-usage-model"].BillingUsageSchema["seconds"].Description["en"])
	assert.Equal(t, "count", refreshedPricing["pricing-usage-model"].BillingUsageSchema["clips"].Unit)
}

func TestPricingAliasCarriesPluginUsageSchemaAndTailExpr(t *testing.T) {
	resetPricingEndpointTestTables(t)
	const pluginKey = "pricing-usage-probe"
	source := pricingUsagePluginSource("1.0.0", `{
  seconds: {type: "number", unit: "second", description: "Estimated duration."}
}`)
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister(pluginKey) })

	mapping := `{"alias-model":"pricing-usage-model"}`
	channel := &legacy.Channel{
		Id:           910,
		Type:         constant.ChannelTypeTaskPlugin,
		Key:          "key-910",
		Status:       1,
		Name:         "channel-910",
		Models:       "alias-model,pricing-usage-model",
		ModelMapping: &mapping,
	}
	require.NoError(t, legacy.DB.Create(channel).Error)
	insertPricingEndpointAbility(t, 910, "alias-model")
	insertPricingEndpointAbility(t, 910, "pricing-usage-model")
	channelstore.InitChannelCache()

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"pricing-usage-model":"tiered_expr","alias-own-expr":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"pricing-usage-model":"u(\"seconds\")","alias-own-expr":"u(\"seconds\") * 2"}`,
	}))
	pricingstate.InvalidatePricingCache()

	pricing := pricingByModel(pricingruntime.GetPricing())
	require.Contains(t, pricing, "alias-model")
	require.Contains(t, pricing, "pricing-usage-model")
	assert.Equal(t, "second", pricing["alias-model"].BillingUsageSchema["seconds"].Unit)
	assert.Equal(t, "Estimated duration.", pricing["alias-model"].BillingUsageSchema["seconds"].Description["en"])
	assert.Equal(t, "tiered_expr", pricing["alias-model"].BillingMode)
	assert.Equal(t, `u("seconds")`, pricing["alias-model"].BillingExpr)
	assert.Equal(t, "tiered_expr", pricing["pricing-usage-model"].BillingMode)
	assert.Equal(t, `u("seconds")`, pricing["pricing-usage-model"].BillingExpr)

	ownMapping := `{"alias-own-expr":"pricing-usage-model"}`
	own := &legacy.Channel{
		Id:           911,
		Type:         constant.ChannelTypeTaskPlugin,
		Key:          "key-911",
		Status:       1,
		Name:         "channel-911",
		Models:       "alias-own-expr,pricing-usage-model",
		ModelMapping: &ownMapping,
	}
	require.NoError(t, legacy.DB.Create(own).Error)
	insertPricingEndpointAbility(t, 911, "alias-own-expr")
	channelstore.InitChannelCache()
	pricingstate.InvalidatePricingCache()

	refreshed := pricingByModel(pricingruntime.GetPricing())
	assert.Equal(t, `u("seconds") * 2`, refreshed["alias-own-expr"].BillingExpr)
	assert.Equal(t, "second", refreshed["alias-own-expr"].BillingUsageSchema["seconds"].Unit)
}

func pricingByModel(pricings []pricingdomain.Pricing) map[string]pricingdomain.Pricing {
	result := make(map[string]pricingdomain.Pricing, len(pricings))
	for _, pricing := range pricings {
		result[pricing.ModelName] = pricing
	}
	return result
}

func TestPricingHeygenReferenceOperationsUseEligibleTariff(t *testing.T) {
	source, err := os.ReadFile("../../../../plugins/tasks/openrouter-video/plugin.js")
	require.NoError(t, err)
	plugin, err := jsplugin.CompilePlugin(string(source), jsplugin.Options{})
	require.NoError(t, err)
	require.NotNil(t, plugin.Meta.PriceCatalog)
	offers := plugin.Meta.PriceCatalog.Models["heygen/heygen-video-1"]
	require.NotEmpty(t, offers)
	for _, scenario := range []struct {
		operation      string
		amountUSD      float64
		referenceInput bool
	}{{"text_to_video", .02, false}, {"image_to_video", .02, false}, {"video_to_video", .07, true}} {
		t.Run(scenario.operation, func(t *testing.T) {
			eligible := make([]pricingdomain.CatalogOffer, 0)
			for _, offer := range offers {
				if offer.Operation == scenario.operation {
					eligible = append(eligible, pricingadapter.ToTariffOffer(offer))
				}
			}
			selected := pricingstate.SelectCatalogOffer(eligible)
			require.NotNil(t, selected)
			if scenario.referenceInput {
				require.Len(t, selected.Prices, 2)
				assert.Equal(t, "additional_reference_input", selected.Prices[1].Component)
				assert.Equal(t, "image", selected.Prices[1].Unit)
				// A flat price cannot represent the first two reference images free.
				assert.Nil(t, selected.Prices[1].AmountUSD)
			} else {
				require.Len(t, selected.Prices, 1)
			}
			assert.Equal(t, "video_output", selected.Prices[0].Component)
			assert.Equal(t, "second", selected.Prices[0].Unit)
			require.NotNil(t, selected.Prices[0].AmountUSD)
			assert.Equal(t, scenario.amountUSD, *selected.Prices[0].AmountUSD)
		})
	}
}

func TestPricingConfiguredFormulaProjectionUsesActualRouteSource(t *testing.T) {
	resetPricingEndpointTestTables(t)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		pricingstate.InvalidatePricingCache()
	})
	names := []string{"direct-openai", "direct-anthropic", "direct-gemini", "actualcost-or"}
	modes := map[string]string{}
	expressions := map[string]string{}
	for index, channelType := range []int{
		constant.ChannelTypeOpenAI,
		constant.ChannelTypeAnthropic,
		constant.ChannelTypeGemini,
		constant.ChannelTypeOpenRouter,
	} {
		insertPricingEndpointChannel(t, 980+index, channelType, dto.ChannelOtherSettings{})
		insertPricingEndpointAbility(t, 980+index, names[index])
		modes[names[index]] = "tiered_expr"
		expressions[names[index]] = `tier("base",p*2+c*3)`
	}
	modeJSON, err := common.Marshal(modes)
	require.NoError(t, err)
	exprJSON, err := common.Marshal(expressions)
	require.NoError(t, err)
	require.NoError(
		t,
		config.GlobalConfig.LoadFromDB(map[string]string{
			"billing_setting.billing_mode": string(modeJSON),
			"billing_setting.billing_expr": string(exprJSON),
		}),
	)
	channelstore.InitChannelCache()
	pricing := pricingByModel(pricingruntime.GetPricing())
	for _, name := range names[:3] {
		item := pricing[name]
		require.Contains(t, item.ConfiguredPrices, expressions[name])
		require.Len(t, item.ConfiguredPrices[expressions[name]], 1)
		assert.Len(t, item.ConfiguredPrices[expressions[name]][0], 2)
		selected := pricingstate.SelectCatalogOffer(item.CatalogOffersForGroup("default"))
		require.NotNil(t, selected)
		assert.Equal(t, "text", selected.Operation)
		assert.Len(t, selected.Prices, 2)
	}
	assert.Empty(t, pricing["actualcost-or"].ConfiguredPrices)
	assert.Nil(t, pricingstate.SelectCatalogOffer(pricing["actualcost-or"].CatalogOffersForGroup("default")))
}

func TestPricingOperationCounterProfilesAndFixedPrecedence(t *testing.T) {
	previousCount := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = previousCount })
	channel := &legacy.Channel{Type: constant.ChannelTypeOpenAI}
	for _, scenario := range []struct {
		public, upstream, operation, unit string
		ratio, amount                     float64
	}{
		{"tts-1", "tts-1", "speech_generation", "million_characters", 7.5, 15},
		{"tts-1-hd", "tts-1-hd", "speech_generation", "million_characters", 15, 30},
		{"whisper-1", "whisper-1", "transcription", "minute", 15, .03},
	} {
		t.Run(scenario.public, func(t *testing.T) {
			profile := pricingstate.TestConfiguredPricingOperation(channel, scenario.public, scenario.upstream, constant.EndpointTypeOpenAI)
			offers := pricingstate.TestConfiguredOperationOffers(
				pricingdomain.Pricing{
					PriceConfigured: true,
					ModelRatio:      scenario.ratio,
					CompletionRatio: 1,
				},
				profile,
				"sync",
				"",
			)
			selected := pricingstate.SelectCatalogOffer(offers)
			require.NotNil(t, selected)
			assert.Equal(t, scenario.operation, selected.Operation)
			require.Len(t, selected.Prices, 1)
			assert.Equal(t, scenario.unit, selected.Prices[0].Unit)
			assert.InDelta(t, scenario.amount, *selected.Prices[0].AmountUSD, 1e-12)
		})
	}
	for _, pair := range [][2]string{{"gpt-alias", "tts-1"}, {"alias", "whisper-1"}, {"rune-alias", "gpt-4o-mini-tts"}} {
		profile := pricingstate.TestConfiguredPricingOperation(channel, pair[0], pair[1], constant.EndpointTypeOpenAI)
		assert.Nil(
			t,
			pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(
				pricingdomain.Pricing{
					PriceConfigured: true,
					ModelRatio:      .625,
					CompletionRatio: 4,
				},
				profile,
				"sync",
				"",
			)),
		)
		fixed := pricingstate.TestConfiguredOperationOffers(pricingdomain.Pricing{PriceConfigured: true, QuotaType: 1, ModelPrice: .02}, profile, "sync", "")
		require.Len(t, fixed, 1)
		selected := pricingstate.SelectCatalogOffer(fixed)
		require.NotNil(t, selected)
		assert.Equal(t, "call", selected.Prices[0].Unit)
		assert.Equal(t, .02, *selected.Prices[0].AmountUSD)
	}
	constant.CountToken = false
	profile := pricingstate.TestConfiguredPricingOperation(channel, "tts-1", "tts-1", constant.EndpointTypeOpenAI)
	assert.Equal(t, "unknown", pricingstate.OperationBasisForTest(profile))
}

func TestPricingEmbeddingProjectionUsesInputMainAndIndependentBatch(t *testing.T) {
	resetPricingEndpointTestTables(t)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		pricingstate.InvalidatePricingCache()
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":       `{"embedding-alias":"tiered_expr"}`,
		"billing_setting.billing_expr":       `{"embedding-alias":"tier(\"base\",p*.13)"}`,
		"billing_setting.batch_billing_expr": `{"embedding-alias":"tier(\"batch\",p*.065)"}`,
	}))
	mapping := `{"embedding-alias":"text-embedding-3-large"}`
	require.NoError(t, legacy.DB.Create(&legacy.Channel{Id: 988, Type: constant.ChannelTypeOpenAI, Status: 1, ModelMapping: &mapping}).Error)
	insertPricingEndpointAbility(t, 988, "embedding-alias")
	channelstore.InitChannelCache()
	item := pricingByModel(pricingruntime.GetPricing())["embedding-alias"]
	offers := item.CatalogOffersForGroup("default")
	require.Len(t, offers, 2)
	for _, offer := range offers {
		assert.Equal(t, "embeddings", offer.Operation)
		assert.Equal(t, "input", offer.MainComponent)
		selected := pricingstate.SelectCatalogOffer([]pricingdomain.CatalogOffer{offer})
		require.NotNil(t, selected)
		require.Len(t, selected.Prices, 1)
		assert.Equal(t, "input", selected.Prices[0].Component)
		expected := .13
		if offer.Mode == "batch" {
			expected = .065
		}
		assert.Equal(t, expected, *selected.Prices[0].AmountUSD)
	}
}

func TestPricingBatchCanonicalPairIsGroupScopedAndMappingProved(t *testing.T) {
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	require.NoError(
		t,
		config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.batch_billing_expr": `{"public:batch":"tier(\"batch\",provider_cost)"}`}),
	)
	mapping := `{"public":"anthropic/claude-fable-5.1","public:batch":"anthropic/claude-fable-5.1:batch"}`
	channels := map[int]*legacy.Channel{989: {Id: 989, Type: constant.ChannelTypeOpenRouter, Status: 1, ModelMapping: &mapping}}
	abilities := []channelrecord.AbilityWithChannel{
		{Ability: channelrecord.Ability{ChannelId: 989, Model: "public", Group: "default", Enabled: true}, ChannelType: constant.ChannelTypeOpenRouter},
		{
			Ability: channelrecord.Ability{
				ChannelId: 989,
				Model:     "public:batch",
				Group:     "default",
				Enabled:   true,
			},
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		{
			Ability: channelrecord.Ability{
				ChannelId: 989,
				Model:     "public:batch",
				Group:     "premium",
				Enabled:   true,
			},
			ChannelType: constant.ChannelTypeOpenRouter,
		},
	}
	zero := 0.0
	alias := pricingdomain.Pricing{
		ModelName: "public:batch",
		EnableGroup: []string{
			"default",
			"premium",
		},
		CatalogOffersByGroup: map[string][]pricingdomain.CatalogOffer{"default": {{
			ID:            "989:anthropic/claude-fable-5.1:batch:native",
			Operation:     "text",
			Mode:          "batch",
			MainComponent: "output",
			Prices: []pricingdomain.CatalogPrice{{
				Component: "output",
				Unit:      "million_tokens",
				AmountUSD: &zero,
			}},
		}}},
	}
	pricingstate.TestProjectPricingOperations(&alias, abilities, channels, jsplugin.NewRegistry().Generation(), nil, nil)
	assert.Equal(t, map[string]string{"default": "public"}, alias.ConsumerCanonicalModelsByGroup)
	base := pricingdomain.Pricing{ModelName: "public", EnableGroup: []string{"default"}}
	visible := pricingstate.ConsumerPricingCatalogue([]pricingdomain.Pricing{base, alias}, map[string]string{"default": "Default", "premium": "Premium"})
	require.Len(t, visible, 2)
	indexed := pricingByModel(visible)
	require.Len(t, indexed["public"].CatalogOffersForGroup("default"), 1)
	selected := pricingstate.SelectCatalogOffer(indexed["public"].CatalogOffersForGroup("default"))
	require.NotNil(t, selected)
	assert.Zero(t, *selected.Prices[0].AmountUSD)
	assert.Empty(t, indexed["public"].CatalogOffersForGroup("premium"))
	assert.Equal(t, []string{"premium"}, indexed["public:batch"].EnableGroup)
	assert.Empty(t, indexed["public:batch"].CatalogOffersForGroup("premium"), "an unpaired group without rate facts must not fabricate a Batch offer")
	assert.Equal(t, "public:batch", alias.ModelName)
	badMapping := `{"public":"different/model","public:batch":"anthropic/claude-fable-5.1:batch"}`
	channels[989].ModelMapping = &badMapping
	_, paired := pricingstate.TestPairedPricingBatchBase(abilities[1], "anthropic/claude-fable-5.1:batch", abilities, channels)
	assert.False(t, paired)
}

func TestPricingUpstreamIdentityMissingMappingAndMalformedMapping(t *testing.T) {
	channel := &legacy.Channel{}
	name, known := pricingstate.ResolvePricingUpstreamModel(channel, "sora-2")
	assert.True(t, known)
	assert.Equal(t, "sora-2", name)
	for _, mapping := range []string{"", "  ", "{}"} {
		channel.ModelMapping = &mapping
		name, known = pricingstate.ResolvePricingUpstreamModel(channel, "direct")
		assert.True(t, known)
		assert.Equal(t, "direct", name)
	}
	for _, mapping := range []string{"{", `{"a":"b","b":"a"}`} {
		channel.ModelMapping = &mapping
		_, known = pricingstate.ResolvePricingUpstreamModel(channel, "a")
		assert.False(t, known)
	}
	_, known = pricingstate.ResolvePricingUpstreamModel(nil, "a")
	assert.False(t, known)
	_, known = pricingstate.ResolvePricingUpstreamModel(channel, " ")
	assert.False(t, known)
}

func TestPricingNativeImageProofReplacesDefaultsAndPreservesMixedText(t *testing.T) {
	resetPricingEndpointTestTables(t)
	previousState := (*pricingstate.TestPricingCatalogState).Load()
	t.Cleanup(func() {
		(*pricingstate.TestPricingCatalogState).Store(previousState)
		pricingstate.InvalidatePricingCache()
	})
	mapping := `{"pure-image":"sourceful/riverflow","mixed-image":"google/gemini-image"}`
	channel := &legacy.Channel{Id: 992, Type: constant.ChannelTypeOpenRouter, Status: 1, ModelMapping: &mapping}
	require.NoError(t, legacy.DB.Create(channel).Error)
	for _, name := range []string{"pure-image", "mixed-image"} {
		insertPricingEndpointAbility(t, 992, name)
	}
	entries := map[string]pricingdomain.PricingCatalogEntry{}
	amount := .06
	for _, upstream := range []string{"sourceful/riverflow", "google/gemini-image"} {
		image := pricingdomain.PricingCatalogEntry{
			ChannelID:     992,
			UpstreamModel: upstream,
			Endpoint:      constant.EndpointTypeImageGeneration,
			Fingerprint: pricingstate.PricingCatalogSourceFingerprint(
				channel,
				upstream,
				constant.EndpointTypeImageGeneration,
			),
			Offers: []pricingdomain.CatalogOffer{{
				ID:            "native:image",
				Operation:     "image_generation",
				Mode:          "sync",
				MainComponent: "image_output",
				Prices: []pricingdomain.CatalogPrice{{
					Component: "image_output",
					Unit:      "image",
					AmountUSD: &amount,
				}},
			}},
		}
		entries[pricingstate.TestPricingCatalogEntryKey(image)] = image
		text := pricingdomain.PricingCatalogEntry{
			ChannelID:     992,
			UpstreamModel: upstream,
			Endpoint:      constant.EndpointTypeOpenAI,
			Fingerprint: pricingstate.PricingCatalogSourceFingerprint(
				channel,
				upstream,
				constant.EndpointTypeOpenAI,
			),
			Offers: []pricingdomain.CatalogOffer{},
		}
		if upstream == "google/gemini-image" {
			text.Offers = []pricingdomain.CatalogOffer{{
				ID:            "native:text",
				Operation:     "text",
				Mode:          "sync",
				MainComponent: "output",
				Prices: []pricingdomain.CatalogPrice{{
					Component: "output",
					Unit:      "million_tokens",
					AmountUSD: &amount,
				}},
			}}
		}
		entries[pricingstate.TestPricingCatalogEntryKey(text)] = text
	}
	pricingstate.LoadPricingCatalog(pricingdomain.PricingCatalogSnapshot{Version: 1, Entries: entries})
	channelstore.InitChannelCache()
	indexed := pricingByModel(pricingruntime.GetPricing())
	pure := indexed["pure-image"].CatalogOffersForGroup("default")
	require.Len(t, pure, 1)
	assert.Equal(t, "image_generation", pure[0].Operation)
	mixed := indexed["mixed-image"].CatalogOffersForGroup("default")
	require.Len(t, mixed, 2)
	operations := []string{mixed[0].Operation, mixed[1].Operation}
	assert.ElementsMatch(t, []string{"image_generation", "text"}, operations)
}

func TestPricingConfiguredImageUsesActualOutputUmbrellaAndCountBasis(t *testing.T) {
	channel := &legacy.Channel{Type: constant.ChannelTypeOpenAI}
	imageRatio, cacheRatio := 2.0, .25
	for _, name := range []string{"gpt-image-2-2026-04-21", "gpt-image-2.5-flare-2026-09-08", "gpt-image-2.5-sunburst-2026-09-08"} {
		for _, public := range []string{name, "image-alias"} {
			profile := pricingstate.TestConfiguredPricingOperation(channel, public, name, constant.EndpointTypeOpenAI)
			require.Equal(t, "image_generation", pricingstate.OperationNameForTest(profile))
			offers := pricingstate.TestConfiguredOperationOffers(
				pricingdomain.Pricing{
					PriceConfigured: true,
					ModelRatio:      2.5,
					CompletionRatio: 6,
					ImageRatio:      &imageRatio,
					CacheRatio:      &cacheRatio,
				},
				profile,
				"sync",
				"",
			)
			selected := pricingstate.SelectCatalogOffer(offers)
			require.NotNil(t, selected)
			assert.Equal(t, "output", selected.MainComponent)
			prices := map[string]pricingdomain.CatalogPrice{}
			for _, price := range selected.Prices {
				prices[price.Component] = price
				assert.Equal(t, "million_tokens", price.Unit)
			}
			assert.Equal(t, 5.0, *prices["input"].AmountUSD)
			assert.Equal(t, 30.0, *prices["output"].AmountUSD)
			assert.Equal(t, 10.0, *prices["image_input"].AmountUSD)
			assert.Equal(t, 1.25, *prices["cache_read"].AmountUSD)
			assert.NotContains(t, prices, "image_output")
			zero := pricingstate.TestConfiguredOperationOffers(
				pricingdomain.Pricing{
					PriceConfigured: true,
					ModelRatio:      0,
					CompletionRatio: 6,
				},
				profile,
				"sync",
				"",
			)
			free := pricingstate.SelectCatalogOffer(zero)
			require.NotNil(t, free)
			assert.Zero(t, *free.Prices[1].AmountUSD)
			assert.Nil(t, pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(pricingdomain.Pricing{}, profile, "sync", "")))
			fixed := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(
				pricingdomain.Pricing{
					PriceConfigured: true,
					QuotaType:       1,
					ModelPrice:      .04,
				},
				profile,
				"sync",
				"",
			))
			require.NotNil(t, fixed)
			assert.Equal(t, "image", fixed.Prices[0].Unit)
		}
	}
}

func TestPricingConfiguredAudioRatioKeepsCompleteSplitOutputVector(t *testing.T) {
	inputAudioRatio, outputAudioRatio := 2.0, 3.0
	profile := pricingstate.TestConfiguredPricingOperation(
		&legacy.Channel{Type: constant.ChannelTypeOpenAI},
		"gpt-audio",
		"gpt-audio",
		constant.EndpointTypeOpenAI,
	)
	item := pricingdomain.Pricing{
		PriceConfigured:      true,
		ModelRatio:           .625,
		CompletionRatio:      4,
		AudioRatio:           &inputAudioRatio,
		AudioCompletionRatio: &outputAudioRatio,
	}
	offers := pricingstate.TestConfiguredOperationOffers(item, profile, "sync", "")
	selected := pricingstate.SelectCatalogOffer(offers)
	require.NotNil(t, selected)
	prices := map[string]float64{}
	for _, price := range selected.Prices {
		require.NotNil(t, price.AmountUSD)
		assert.Equal(t, "million_tokens", price.Unit)
		prices[price.Component] = *price.AmountUSD
	}
	assert.Equal(t, map[string]float64{"input": 1.25, "output": 5, "audio_input": 2.5, "audio_output": 7.5}, prices)
	textInput, textOutput, audioInput, audioOutput := 11.0, 13.0, 17.0, 19.0
	projectedUSD := (textInput*prices["input"] + textOutput*prices["output"] + audioInput*prices["audio_input"] + audioOutput*prices["audio_output"]) / 1e6
	actualQuota := (textInput + textOutput*item.CompletionRatio + audioInput*inputAudioRatio + audioOutput*inputAudioRatio*outputAudioRatio) * item.ModelRatio
	assert.InDelta(t, actualQuota/500000, projectedUSD, 1e-15)
	zero := 0.0
	item.AudioCompletionRatio = &zero
	freeAudio := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(item, profile, "sync", ""))
	require.NotNil(t, freeAudio)
	assert.Zero(t, *freeAudio.Prices[len(freeAudio.Prices)-1].AmountUSD)
	mini := pricingstate.TestConfiguredPricingOperation(
		&legacy.Channel{Type: constant.ChannelTypeOpenAI},
		"gpt-4o-mini-tts",
		"gpt-4o-mini-tts",
		constant.EndpointTypeOpenAI,
	)
	assert.Nil(t, pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(item, mini, "sync", "")))
}

func TestPricingMiniTTSKeepsTextInputAndExplicitUnknownOutput(t *testing.T) {
	previousCount := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = previousCount })
	expression := `tier("base",p*.6+c*12+ao*12)`
	for _, name := range []string{"gpt-4o-mini-tts", "gpt-4o-mini-tts-2025-03-20", "gpt-4o-mini-tts-2025-12-15"} {
		profile := pricingstate.TestConfiguredPricingOperation(&legacy.Channel{Type: constant.ChannelTypeOpenAI}, name, name, constant.EndpointTypeOpenAI)
		for _, item := range []pricingdomain.Pricing{
			{
				BillingMode:      billing_setting.BillingModeTieredExpr,
				ConfiguredPrices: map[string][][]pricingdomain.ExpressionPrice{expression: pricingstate.TestPricingCapabilities().Expressions.CatalogPrices(expression)},
			},
			{PriceConfigured: true, ModelRatio: .3, CompletionRatio: 20},
		} {
			offers := pricingstate.TestConfiguredOperationOffers(item, profile, "sync", expression)
			selected := pricingstate.SelectCatalogOffer(offers)
			require.NotNil(t, selected)
			assert.Equal(t, "input", selected.MainComponent)
			require.Len(t, selected.Prices, 2)
			assert.Equal(t, "input", selected.Prices[0].Component)
			assert.Equal(t, "million_tokens", selected.Prices[0].Unit)
			assert.Equal(t, .6, *selected.Prices[0].AmountUSD)
			assert.Equal(t, pricingdomain.CatalogPrice{Component: "audio_output", Unit: "unknown"}, selected.Prices[1])
		}
	}
	profile := pricingstate.TestConfiguredPricingOperation(
		&legacy.Channel{Type: constant.ChannelTypeOpenAI},
		"gpt-4o-mini-tts",
		"gpt-4o-mini-tts",
		constant.EndpointTypeOpenAI,
	)
	zeroExpression := `tier("base",p*0+c*12+ao*12)`
	selected := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(
		pricingdomain.Pricing{
			BillingMode:      billing_setting.BillingModeTieredExpr,
			ConfiguredPrices: map[string][][]pricingdomain.ExpressionPrice{zeroExpression: pricingstate.TestPricingCapabilities().Expressions.CatalogPrices(zeroExpression)},
		},
		profile,
		"sync",
		zeroExpression,
	))
	require.NotNil(t, selected)
	assert.Zero(t, *selected.Prices[0].AmountUSD)
	assert.Nil(t, selected.Prices[1].AmountUSD)
	free := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(
		pricingdomain.Pricing{
			PriceConfigured: true,
			ModelRatio:      0,
		},
		profile,
		"sync",
		"",
	))
	require.NotNil(t, free)
	require.Len(t, free.Prices, 1)
	assert.Zero(t, *free.Prices[0].AmountUSD)
	fixedExpression := `fixed(.02)`
	fixed := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(
		pricingdomain.Pricing{
			BillingMode:      billing_setting.BillingModeTieredExpr,
			ConfiguredPrices: map[string][][]pricingdomain.ExpressionPrice{fixedExpression: pricingstate.TestPricingCapabilities().Expressions.CatalogPrices(fixedExpression)},
		},
		profile,
		"sync",
		fixedExpression,
	))
	require.NotNil(t, fixed)
	require.Len(t, fixed.Prices, 1)
	assert.Equal(t, "call", fixed.Prices[0].Unit)
	constant.CountToken = false
	assert.Equal(
		t,
		"unknown",
		pricingstate.OperationBasisForTest(pricingstate.TestConfiguredPricingOperation(
			&legacy.Channel{Type: constant.ChannelTypeOpenAI},
			"gpt-4o-mini-tts",
			"gpt-4o-mini-tts",
			constant.EndpointTypeOpenAI,
		)),
	)
}

func TestPricingCatalogueFingerprintIsCanonicalAndTracksConsumerChanges(t *testing.T) {
	fixture := func() []pricingdomain.Pricing {
		input, output := 1.0, 2.0
		items := []pricingdomain.Pricing{
			{
				ModelName: "alpha",
				VendorID:  1,
				EnableGroup: []string{
					"premium",
					"default",
				},
				SupportedEndpointTypes: []constant.EndpointType{
					constant.EndpointTypeOpenAIResponse,
					constant.EndpointTypeOpenAI,
				},
				ConsumerCanonicalModelsByGroup: map[string]string{
					"premium": "alpha",
					"default": "alpha",
				},
				CatalogOffersByGroup: map[string][]pricingdomain.CatalogOffer{"default": {
					{
						ID:            "b",
						Operation:     "text",
						Mode:          "sync",
						MainComponent: "output",
						Prices: []pricingdomain.CatalogPrice{
							{
								Component: "output",
								Unit:      "million_tokens",
								AmountUSD: &output,
							},
							{
								Component: "input",
								Unit:      "million_tokens",
								AmountUSD: &input,
							},
						},
					},
					{
						ID:            "a",
						Operation:     "text",
						Mode:          "batch",
						MainComponent: "output",
						Prices: []pricingdomain.CatalogPrice{{
							Component: "output",
							Unit:      "million_tokens",
							AmountUSD: &input,
						}},
					},
				}},
			},
			{ModelName: "beta", EnableGroup: []string{"default"}},
		}
		items[0].PublicCatalogOffersByGroup = map[string][]pricingdomain.CatalogOffer{"default": {
			{
				ID:            "video",
				Operation:     "text_to_video",
				Mode:          "sync",
				MainComponent: "video_output",
				Prices: []pricingdomain.CatalogPrice{
					{
						Component: "video_output",
						Unit:      "second",
						AmountUSD: &output,
					},
					{
						Component: "input",
						Unit:      "million_tokens",
						AmountUSD: &input,
					},
				},
			},
			{
				ID:            "image",
				Operation:     "image_generation",
				Mode:          "sync",
				MainComponent: "image_output",
				Prices: []pricingdomain.CatalogPrice{{
					Component: "image_output",
					Unit:      "image",
					AmountUSD: &input,
				}},
			},
		}}
		return items
	}
	vendors := []pricingdomain.PricingVendor{{ID: 2, Name: "Second"}, {ID: 1, Name: "First"}}
	original := fixture()
	before, err := pricingstate.TestPricingCatalogueFingerprint(original, vendors)
	require.NoError(t, err)
	permuted := fixture()
	permuted[0].EnableGroup = []string{"default", "premium"}
	permuted[0].SupportedEndpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}
	offers := permuted[0].CatalogOffersByGroup["default"]
	offers[0].Prices[0], offers[0].Prices[1] = offers[0].Prices[1], offers[0].Prices[0]
	offers[0], offers[1] = offers[1], offers[0]
	publicOffers := permuted[0].PublicCatalogOffersByGroup["default"]
	publicOffers[0].Prices[0], publicOffers[0].Prices[1] = publicOffers[0].Prices[1], publicOffers[0].Prices[0]
	publicOffers[0], publicOffers[1] = publicOffers[1], publicOffers[0]
	permuted[0], permuted[1] = permuted[1], permuted[0]
	after, err := pricingstate.TestPricingCatalogueFingerprint(permuted, []pricingdomain.PricingVendor{vendors[1], vendors[0]})
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.Equal(t, []string{"premium", "default"}, original[0].EnableGroup)
	assert.Equal(t, "output", original[0].CatalogOffersByGroup["default"][0].Prices[0].Component)
	assert.Equal(t, "video", original[0].PublicCatalogOffersByGroup["default"][0].ID)
	assert.Equal(t, "video_output", original[0].PublicCatalogOffersByGroup["default"][0].Prices[0].Component)
	for _, change := range []func([]pricingdomain.Pricing){
		func(items []pricingdomain.Pricing) {
			value := 3.0
			items[0].CatalogOffersByGroup["default"][0].Prices[0].AmountUSD = &value
		},
		func(items []pricingdomain.Pricing) {
			items[0].CatalogOffersByGroup["default"][0].Operation = "embeddings"
		},
		func(items []pricingdomain.Pricing) { items[0].EnableGroup = []string{"default"} },
		func(items []pricingdomain.Pricing) { items[0].Tags = "top text" },
		func(items []pricingdomain.Pricing) { items[0].Description = "new description" },
		func(items []pricingdomain.Pricing) {
			value := 3.0
			items[0].PublicCatalogOffersByGroup["default"][0].Prices[0].AmountUSD = &value
		},
		func(items []pricingdomain.Pricing) {
			items[0].PublicCatalogOffersByGroup["default"][0].Operation = "image_to_video"
		},
		func(items []pricingdomain.Pricing) {
			items[0].PublicCatalogOffersByGroup["premium"] = items[0].PublicCatalogOffersByGroup["default"]
			delete(items[0].PublicCatalogOffersByGroup, "default")
		},
		func(items []pricingdomain.Pricing) {
			items[0].PublicCatalogOffersByGroup["default"][0].PublicConditions = []pricingdomain.ExpressionCondition{{Var: "len", Op: ">", Value: 100}}
		},
		func(items []pricingdomain.Pricing) { items[0].ConsumerCanonicalModelsByGroup["default"] = "other" },
	} {
		changed := fixture()
		change(changed)
		revision, err := pricingstate.TestPricingCatalogueFingerprint(changed, vendors)
		require.NoError(t, err)
		assert.NotEqual(t, before, revision)
	}
}

func TestPricingCatalogueSnapshotKeepsCapturedRowsWithTheirRevision(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingEndpointChannel(t, 994, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 994, "snapshot-model")
	channelstore.InitChannelCache()
	pricingruntime.RefreshPricing()
	rows, vendors, revision, err := pricingruntime.GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	require.NotEmpty(t, revision)
	captured, err := pricingstate.TestPricingCatalogueFingerprint(rows, vendors)
	require.NoError(t, err)
	assert.Equal(t, revision, captured)
	require.NoError(t, legacy.DB.Create(&legacy.Model{ModelName: "snapshot-model", Description: "new description"}).Error)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, legacy.DB.Callback().Row().Before("gorm:row").Register("test:catalog_build_gate", func(tx *gorm.DB) {
		if tx.Statement.Table == "abilities" {
			once.Do(func() { close(entered); <-release })
		}
	}))
	t.Cleanup(func() { legacy.DB.Callback().Row().Remove("test:catalog_build_gate") })
	pricingstate.InvalidatePricingCache()
	_, _, current, err := pricingruntime.GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	assert.Equal(t, revision, current, "hot request keeps last good without waiting for build")
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("background builder did not reach query gate")
	}
	// A real source mutation during the build must fence that publication.
	require.NoError(t, legacy.DB.Model(&legacy.Channel{}).Where("id = ?", 994).Update("key", "changed-fixture-key").Error)
	channelstore.InitChannelCache()
	close(release)
	require.Eventually(t, func() bool { return !(*pricingstate.TestPricingRefreshRunning).Load() }, time.Second, time.Millisecond)
	assert.Equal(t, revision, pricingstate.PublicationRevisionForTest((*pricingstate.TestPublishedPricing).Load()))
	pricingruntime.RefreshPricing()
	_, _, current, err = pricingruntime.GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	assert.NotEqual(t, revision, current)
	captured, err = pricingstate.TestPricingCatalogueFingerprint(rows, vendors)
	require.NoError(t, err)
	assert.Equal(t, revision, captured)
	good := (*pricingstate.TestPublishedPricing).Load()
	require.NoError(t, legacy.DB.Callback().Query().Before("gorm:query").Register("test:catalog_db_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "models" {
			tx.AddError(errors.New("metadata database unavailable"))
		}
	}))
	pricingstate.InvalidatePricingCache()
	pricingruntime.RefreshPricing()
	assert.Same(t, good, (*pricingstate.TestPublishedPricing).Load(), "failed projection build retains last good publication")
	require.NoError(t, legacy.DB.Callback().Query().Remove("test:catalog_db_failure"))
	pricingruntime.RefreshPricing()
	channelstore.InitChannelCache()
	generation := (*pricingstate.TestPricingInputsGeneration).Load()
	channelstore.InitChannelCache()
	assert.Equal(t, generation, (*pricingstate.TestPricingInputsGeneration).Load(), "no-op channel synchronization does not invalidate")
}

func TestPricingBillingOptionsKeepSnapshotForEffectiveNoop(t *testing.T) {
	for _, field := range []string{"billing_mode", "billing_expr", "batch_billing_expr"} {
		t.Run(field, func(t *testing.T) {
			resetPricingEndpointTestTables(t)
			insertPricingEndpointChannel(t, 995, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
			insertPricingEndpointAbility(t, 995, "snapshot-model")
			cfg := config.GlobalConfig.Get("billing_setting")
			previous, err := config.ConfigToMap(cfg)
			require.NoError(t, err)
			previousOptions := common.OptionMap
			common.OptionMap = map[string]string{}
			t.Cleanup(func() {
				require.NoError(t, config.UpdateConfigFromMap(cfg, previous))
				common.OptionMap = previousOptions
				pricingstate.InvalidatePricingCache()
			})
			key := "billing_setting." + field
			value := `{"snapshot-model":"p*1", "other":"p*2"}`
			if field == "billing_mode" {
				value = `{"snapshot-model":"ratio", "other":"tiered_expr"}`
			}
			require.NoError(t, publication.PublishOption(key, value))
			channelstore.InitChannelCache()
			pricingruntime.RefreshPricing()
			rows, _, _, err := pricingruntime.GetPricingCatalogueSnapshot()
			require.NoError(t, err)
			require.NotEmpty(t, rows)
			revision := pricingstate.GetPricingRevision()

			// Database synchronization reapplies the same raw value.
			require.NoError(t, publication.PublishOption(key, value))
			_, _, _, err = pricingruntime.GetPricingCatalogueSnapshot()
			require.NoError(t, err)
			assert.Equal(t, revision, pricingstate.GetPricingRevision(), "unchanged option must retain the published snapshot")

			// JSON whitespace and key order do not change the effective map.
			reordered := `{"other":"p*2","snapshot-model":"p*1"}`
			if field == "billing_mode" {
				reordered = `{"other":"tiered_expr","snapshot-model":"ratio"}`
			}
			require.NoError(t, publication.PublishOption(key, reordered))
			_, _, _, err = pricingruntime.GetPricingCatalogueSnapshot()
			require.NoError(t, err)
			assert.Equal(t, revision, pricingstate.GetPricingRevision(), "equivalent JSON must retain the published snapshot")
			assert.Equal(t, reordered, common.OptionMap[key])

			// The same database value must still restore changed in-memory state.
			require.NoError(t, config.UpdateConfigFromMap(cfg, map[string]string{field: `{}`}))
			require.NoError(t, publication.PublishOption(key, reordered))
			pricingruntime.RefreshPricing()
			current, err := config.ConfigToMap(cfg)
			require.NoError(t, err)
			assert.JSONEq(t, reordered, current[field])
			_, _, _, err = pricingruntime.GetPricingCatalogueSnapshot()
			require.NoError(t, err)
			assert.Greater(t, pricingstate.GetPricingRevision(), revision, "restoring drift must invalidate the snapshot")
			revision = pricingstate.GetPricingRevision()

			// Removing configured models is a real pricing change.
			require.NoError(t, publication.PublishOption(key, `{}`))
			pricingruntime.RefreshPricing()
			_, _, _, err = pricingruntime.GetPricingCatalogueSnapshot()
			require.NoError(t, err)
			assert.Greater(t, pricingstate.GetPricingRevision(), revision)
		})
	}
}

func TestPricingRealtimeProjectsConfiguredTokenCounters(t *testing.T) {
	channel := &legacy.Channel{Type: constant.ChannelTypeOpenAI}
	expression := `tier("base",p*.6+c*2.4+cr*.06+ai*10+ao*20+img*.8)`
	for _, name := range []string{
		"gpt-realtime",
		"gpt-realtime-2025-08-28",
		"gpt-realtime-1.5",
		"gpt-realtime-2",
		"gpt-realtime-2.1",
		"gpt-realtime-mini",
		"gpt-realtime-mini-2025-12-15",
		"gpt-realtime-2.1-mini",
		"gpt-realtime-translate",
		"gpt-realtime-whisper",
	} {
		t.Run(name, func(t *testing.T) {
			profile := pricingstate.TestConfiguredPricingOperation(channel, name, name, constant.EndpointTypeOpenAI)
			item := pricingdomain.Pricing{
				BillingMode:      billing_setting.BillingModeTieredExpr,
				ConfiguredPrices: map[string][][]pricingdomain.ExpressionPrice{expression: pricingstate.TestPricingCapabilities().Expressions.CatalogPrices(expression)},
			}
			selected := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(item, profile, "sync", expression))
			require.NotNil(t, selected)
			assert.Equal(t, "realtime", selected.Operation)
			assert.Equal(t, "input", selected.MainComponent)
			prices := map[string]float64{}
			for _, price := range selected.Prices {
				require.NotNil(t, price.AmountUSD)
				assert.Equal(t, "million_tokens", price.Unit)
				prices[price.Component] = *price.AmountUSD
			}
			assert.Equal(
				t,
				map[string]float64{
					"input":        .6,
					"output":       2.4,
					"cache_read":   .06,
					"audio_input":  10,
					"audio_output": 20,
					"image_input":  .8,
				},
				prices,
			)
		})
	}
	profile := pricingstate.TestConfiguredPricingOperation(channel, "gpt-realtime-whisper", "gpt-realtime-whisper", constant.EndpointTypeOpenAI)
	inputOnly := `tier("base",p*17)`
	selected := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(
		pricingdomain.Pricing{
			BillingMode:      billing_setting.BillingModeTieredExpr,
			ConfiguredPrices: map[string][][]pricingdomain.ExpressionPrice{inputOnly: pricingstate.TestPricingCapabilities().Expressions.CatalogPrices(inputOnly)},
		},
		profile,
		"sync",
		inputOnly,
	))
	require.NotNil(t, selected)
	require.Len(t, selected.Prices, 1)
	assert.Equal(t, 17.0, *selected.Prices[0].AmountUSD)
	audio, completion, cache, image := 2.0, 3.0, .1, 4.0
	item := pricingdomain.Pricing{
		PriceConfigured:      true,
		ModelRatio:           .625,
		CompletionRatio:      4,
		AudioRatio:           &audio,
		AudioCompletionRatio: &completion,
		CacheRatio:           &cache,
		ImageRatio:           &image,
	}
	selected = pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(item, profile, "sync", ""))
	require.NotNil(t, selected)
	prices := map[string]float64{}
	for _, price := range selected.Prices {
		require.NotNil(t, price.AmountUSD)
		prices[price.Component] = *price.AmountUSD
	}
	assert.Equal(t, map[string]float64{"input": 1.25, "output": 5, "audio_input": 2.5, "audio_output": 7.5}, prices)
}

func TestPricingTranscriptionPublishesConfiguredBillingTokens(t *testing.T) {
	channel := &legacy.Channel{Type: constant.ChannelTypeOpenAI}
	expression := `tier("base",p*1.25+c*5)`
	for _, name := range []string{
		"gpt-4o-transcribe",
		"gpt-4o-transcribe-diarize",
		"gpt-4o-mini-transcribe",
		"gpt-4o-mini-transcribe-2025-03-20",
		"gpt-4o-mini-transcribe-2025-12-15",
		"gpt-transcribe",
	} {
		t.Run(name, func(t *testing.T) {
			profile := pricingstate.TestConfiguredPricingOperation(channel, name, name, constant.EndpointTypeOpenAI)
			for _, item := range []pricingdomain.Pricing{
				{
					BillingMode:      billing_setting.BillingModeTieredExpr,
					ConfiguredPrices: map[string][][]pricingdomain.ExpressionPrice{expression: pricingstate.TestPricingCapabilities().Expressions.CatalogPrices(expression)},
				},
				{PriceConfigured: true, ModelRatio: .625, CompletionRatio: 4},
			} {
				selected := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(item, profile, "sync", expression))
				require.NotNil(t, selected)
				assert.Equal(t, "transcription", selected.Operation)
				assert.Equal(t, "input", selected.MainComponent)
				require.Len(t, selected.Prices, 2)
				prices := map[string]pricingdomain.CatalogPrice{}
				for _, price := range selected.Prices {
					prices[price.Component] = price
				}
				assert.Equal(t, "million_tokens", prices["input"].Unit)
				require.NotNil(t, prices["input"].AmountUSD)
				assert.Equal(t, 1.25, *prices["input"].AmountUSD)
				assert.Equal(t, "million_tokens", prices["output"].Unit)
				require.NotNil(t, prices["output"].AmountUSD)
				assert.Equal(t, 5.0, *prices["output"].AmountUSD)
			}
			inputOnly := `tier("base",p*4.5)`
			selected := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(
				pricingdomain.Pricing{
					BillingMode:      billing_setting.BillingModeTieredExpr,
					ConfiguredPrices: map[string][][]pricingdomain.ExpressionPrice{inputOnly: pricingstate.TestPricingCapabilities().Expressions.CatalogPrices(inputOnly)},
				},
				profile,
				"sync",
				inputOnly,
			))
			require.NotNil(t, selected)
			require.Len(t, selected.Prices, 1)
			assert.Equal(t, "million_tokens", selected.Prices[0].Unit)
			assert.Equal(t, 4.5, *selected.Prices[0].AmountUSD)
			fixed := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(
				pricingdomain.Pricing{
					PriceConfigured: true,
					QuotaType:       1,
					ModelPrice:      .02,
				},
				profile,
				"sync",
				"",
			))
			require.NotNil(t, fixed)
			assert.Equal(t, "call", fixed.Prices[0].Unit)
		})
	}
}

func TestPricingOmitsUnchargedUnknownAudioComponents(t *testing.T) {
	previousCount := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = previousCount })
	channel := &legacy.Channel{Type: constant.ChannelTypeOpenAI}
	speech := pricingstate.TestConfiguredPricingOperation(channel, "gpt-4o-mini-tts", "gpt-4o-mini-tts", constant.EndpointTypeOpenAI)
	zero, one := 0.0, 1.0
	for _, scenario := range []struct {
		name          string
		expression    string
		pricing       pricingdomain.Pricing
		unknownOutput bool
	}{
		{name: "input_only_formula", expression: `tier("base",p*.6)`},
		{name: "zero_completion_formula", expression: `tier("base",p*.6+c*0)`},
		{name: "zero_audio_formula", expression: `tier("base",p*.6+ao*0)`},
		{name: "paid_completion_formula", expression: `tier("base",p*.6+c*12+ao*0)`, unknownOutput: true},
		{name: "paid_audio_formula", expression: `tier("base",p*.6+c*0+ao*12)`, unknownOutput: true},
		{name: "zero_output_ratios", pricing: pricingdomain.Pricing{PriceConfigured: true, ModelRatio: .3, AudioRatio: &one, AudioCompletionRatio: &zero}},
		{
			name: "zero_audio_input_ratio",
			pricing: pricingdomain.Pricing{
				PriceConfigured:      true,
				ModelRatio:           .3,
				AudioRatio:           &zero,
				AudioCompletionRatio: &one,
			},
		},
		{
			name: "paid_stream_completion_ratio",
			pricing: pricingdomain.Pricing{
				PriceConfigured:      true,
				ModelRatio:           .3,
				CompletionRatio:      20,
				AudioRatio:           &one,
				AudioCompletionRatio: &zero,
			},
			unknownOutput: true,
		},
		{
			name: "paid_nonstream_audio_ratio",
			pricing: pricingdomain.Pricing{
				PriceConfigured:      true,
				ModelRatio:           .3,
				AudioRatio:           &one,
				AudioCompletionRatio: &one,
			},
			unknownOutput: true,
		},
		{name: "free_model_ratio", pricing: pricingdomain.Pricing{PriceConfigured: true, AudioRatio: &one, AudioCompletionRatio: &one}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			item := scenario.pricing
			if scenario.expression != "" {
				item.BillingMode = billing_setting.BillingModeTieredExpr
				item.ConfiguredPrices = map[string][][]pricingdomain.ExpressionPrice{scenario.expression: pricingstate.TestPricingCapabilities().Expressions.CatalogPrices(scenario.expression)}
			}
			selected := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(item, speech, "sync", scenario.expression))
			require.NotNil(t, selected)
			expectedCount := 1
			if scenario.unknownOutput {
				expectedCount++
			}
			require.Len(t, selected.Prices, expectedCount)
			require.NotNil(t, selected.Prices[0].AmountUSD)
			assert.Equal(t, "million_tokens", selected.Prices[0].Unit)
			if scenario.unknownOutput {
				assert.Equal(t, pricingdomain.CatalogPrice{Component: "audio_output", Unit: "unknown"}, selected.Prices[1])
			}
		})
	}
	transcription := pricingstate.TestConfiguredPricingOperation(
		channel,
		"gpt-4o-mini-transcribe",
		"gpt-4o-mini-transcribe",
		constant.EndpointTypeOpenAI,
	)
	expression := `tier("base",p*0+c*5)`
	for _, item := range []pricingdomain.Pricing{
		{
			BillingMode:      billing_setting.BillingModeTieredExpr,
			ConfiguredPrices: map[string][][]pricingdomain.ExpressionPrice{expression: pricingstate.TestPricingCapabilities().Expressions.CatalogPrices(expression)},
		},
		{PriceConfigured: true, CompletionRatio: 4},
	} {
		selected := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(item, transcription, "sync", expression))
		require.NotNil(t, selected)
		require.Len(t, selected.Prices, 2)
		prices := map[string]pricingdomain.CatalogPrice{}
		for _, price := range selected.Prices {
			prices[price.Component] = price
		}
		require.Contains(t, prices, "input")
		require.NotNil(t, prices["input"].AmountUSD)
		assert.Zero(t, *prices["input"].AmountUSD)
		assert.Equal(t, "million_tokens", prices["input"].Unit)
		require.Contains(t, prices, "output")
		require.NotNil(t, prices["output"].AmountUSD)
	}
}

func TestPublicConfiguredOperationOffersPreserveAudioUnitsAndConditions(t *testing.T) {
	for _, scenario := range []struct {
		basis, unit, component string
		expression             string
	}{
		{"characters", "million_characters", "input", `len <= 100 ? tier("private",p*0) : tier("private",p*5)`},
		{"duration", "minute", "audio_input", `len <= 100 ? tier("private",p*1000) : tier("private",p*2000)`},
		{"tokens", "million_tokens", "input", `(hour("UTC") < 4 || hour("UTC") > 20) ? tier("private",p*1) : tier("private",p*2)`},
	} {
		t.Run(scenario.basis, func(t *testing.T) {
			item := pricingdomain.Pricing{BillingMode: billing_setting.BillingModeTieredExpr}
			offers := pricingstate.TestConfiguredOperationOffers(
				item,
				pricingstate.NewOperationForTest(
					"speech_generation",
					scenario.component,
					scenario.basis,
				),
				"sync",
				scenario.expression,
				true,
			)
			require.Len(t, offers, 2)
			for _, offer := range offers {
				require.Len(t, offer.Prices, 1)
				assert.Equal(t, scenario.unit, offer.Prices[0].Unit)
				assert.Equal(t, scenario.component, offer.Prices[0].Component)
				require.Len(t, offer.PublicConditions, 1)
				assert.True(t, offer.PublicConditional)
			}
			assert.True(t, offers[1].PublicConditions[0].Negated)
			if scenario.basis == "duration" {
				assert.Equal(t, 1., *offers[0].Prices[0].AmountUSD)
			}
			if scenario.basis == "tokens" {
				assert.Equal(t, "||", offers[0].PublicConditions[0].Op)
			}
		})
	}
	unknown := pricingstate.TestConfiguredOperationOffers(
		pricingdomain.Pricing{BillingMode: billing_setting.BillingModeTieredExpr},
		pricingstate.NewOperationForTest(
			"text_to_video",
			"video_output",
			"unknown",
		),
		"sync",
		`tier("private",u("seconds")*.4)`,
		true,
	)
	require.Len(t, unknown, 1)
	assert.Empty(t, unknown[0].Prices)
	assert.True(t, unknown[0].PublicConditional)
}

func TestPublicNativeLegacySnapshotIsUnknownUntilMetadataRefresh(t *testing.T) {
	amount := 1.
	native := pricingdomain.CatalogOffer{
		ID:            "989:provider/model:native",
		Operation:     "text",
		Mode:          "sync",
		MainComponent: "output",
		Prices: []pricingdomain.CatalogPrice{{
			Component: "output",
			Unit:      "million_tokens",
			AmountUSD: &amount,
		}},
	}
	channels := map[int]*legacy.Channel{989: {Id: 989, Type: constant.ChannelTypeOpenRouter, Status: 1}}
	abilities := []channelrecord.AbilityWithChannel{{
		Ability: channelrecord.Ability{
			ChannelId: 989,
			Model:     "provider/model",
			Group:     "default",
			Enabled:   true,
		},
		ChannelType: constant.ChannelTypeOpenRouter,
	}}
	for _, fresh := range []bool{false, true} {
		stored := native
		if fresh {
			stored.PublicAlternatives = []pricingdomain.CatalogOffer{native}
		}
		item := pricingdomain.Pricing{
			ModelName:             "provider/model",
			EnableGroup:           []string{"default"},
			CatalogOffersByGroup:  map[string][]pricingdomain.CatalogOffer{"default": {stored}},
			BillingSourcesByGroup: map[string]map[constant.EndpointType][]string{"default": {constant.EndpointTypeOpenAI: {pricingdomain.BillingSourceOpenRouterActualCost}}},
		}
		pricingstate.TestProjectPricingOperations(&item, abilities, channels, jsplugin.NewRegistry().Generation(), nil, nil)
		require.Len(t, item.CatalogOffersByGroup["default"], 1)
		require.Len(t, item.CatalogOffersByGroup["default"][0].Prices, 1)
		assert.Equal(t, 1., *item.CatalogOffersByGroup["default"][0].Prices[0].AmountUSD)
		require.Len(t, item.PublicCatalogOffersByGroup["default"], 1)
		if fresh {
			require.Len(t, item.PublicCatalogOffersByGroup["default"][0].Prices, 1)
		} else {
			assert.Empty(t, item.PublicCatalogOffersByGroup["default"][0].Prices)
			assert.True(t, item.PublicCatalogOffersByGroup["default"][0].PublicConditional)
		}
	}
}

func TestPricingRebuildNeverRepairsMalformedChannelSnapshots(t *testing.T) {
	for _, field := range []string{"setting", "settings", "model_mapping"} {
		t.Run(field, func(t *testing.T) {
			resetPricingEndpointTestTables(t)
			insertPricingEndpointChannel(t, 1100, constant.ChannelTypeAdvancedCustom, dto.ChannelOtherSettings{})
			insertPricingEndpointAbility(t, 1100, "read-only-source")
			channelstore.InitChannelCache()
			pricingruntime.RefreshPricing()
			good := (*pricingstate.TestPublishedPricing).Load()
			require.NotNil(t, good)
			require.NoError(t, legacy.DB.Model(&legacy.Channel{}).Where("id = ?", 1100).Update(field, "{").Error)
			loaded, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			require.NoError(t, legacy.DB.Callback().Query().After("gorm:query").Register("test:pricing_owned_row", func(tx *gorm.DB) {
				if tx.Statement.Table == "channels" {
					once.Do(func() { close(loaded); <-release })
				}
			}))
			t.Cleanup(func() { legacy.DB.Callback().Query().Remove("test:pricing_owned_row") })
			pricingstate.InvalidatePricingCache()
			done := make(chan struct{})
			go func() { pricingruntime.RefreshPricing(); close(done) }()
			select {
			case <-loaded:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("builder did not capture channel source")
			}
			require.NoError(
				t,
				legacy.DB.Model(&legacy.Channel{}).Where(
					"id = ?",
					1100,
				).Updates(map[string]any{
					"name":       "concurrent-name",
					"key":        "concurrent-key",
					"used_quota": 77,
				}).Error,
			)
			writes := 0
			require.NoError(t, legacy.DB.Callback().Update().Before("gorm:update").Register("test:pricing_read_only", func(tx *gorm.DB) { writes++ }))
			t.Cleanup(func() { legacy.DB.Callback().Update().Remove("test:pricing_read_only") })
			close(release)
			<-done
			assert.Zero(t, writes, "projection is read-only, including malformed configuration")
			assert.Same(t, good, (*pricingstate.TestPublishedPricing).Load(), "a malformed relevant source retains last good publication")
			var current legacy.Channel
			require.NoError(t, legacy.DB.First(&current, 1100).Error)
			assert.Equal(t, "concurrent-name", current.Name)
			assert.Equal(t, "concurrent-key", current.Key)
			assert.Equal(t, int64(77), current.UsedQuota)
			switch field {
			case "setting":
				require.NotNil(t, current.Setting)
				assert.Equal(t, "{", *current.Setting)
			case "settings":
				assert.Equal(t, "{", current.OtherSettings)
			case "model_mapping":
				require.NotNil(t, current.ModelMapping)
				assert.Equal(t, "{", *current.ModelMapping)
			}
			require.NoError(t, legacy.DB.Callback().Update().Remove("test:pricing_read_only"))
			require.NoError(t, legacy.DB.Callback().Query().Remove("test:pricing_owned_row"))
		})
	}
}

func TestPricingColdBuildIgnoresUnusedAndDisabledMalformedChannels(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingEndpointChannel(t, 1110, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 1110, "active-model")
	invalid := "{"
	require.NoError(
		t,
		legacy.DB.Create(&legacy.Channel{
			Id:            1111,
			Type:          constant.ChannelTypeAdvancedCustom,
			Key:           "unused",
			Status:        common.ChannelStatusEnabled,
			Setting:       &invalid,
			OtherSettings: invalid,
		}).Error,
	)
	require.NoError(
		t,
		legacy.DB.Create(&legacy.Channel{
			Id:            1112,
			Type:          constant.ChannelTypeAdvancedCustom,
			Key:           "disabled",
			Status:        common.ChannelStatusManuallyDisabled,
			Setting:       &invalid,
			OtherSettings: invalid,
		}).Error,
	)
	require.NoError(t, legacy.DB.Create(&channelrecord.Ability{Group: "default", Model: "disabled-model", ChannelId: 1112, Enabled: true}).Error)
	previous := (*pricingstate.TestPublishedPricing).Load()
	(*pricingstate.TestPublishedPricing).Store(nil)
	t.Cleanup(func() { (*pricingstate.TestPublishedPricing).Store(previous) })
	writes := 0
	require.NoError(t, legacy.DB.Callback().Update().Before("gorm:update").Register("test:pricing_read_only", func(tx *gorm.DB) { writes++ }))
	t.Cleanup(func() { legacy.DB.Callback().Update().Remove("test:pricing_read_only") })
	pricingstate.InvalidatePricingCache()
	pricingruntime.RefreshPricing()
	require.NotNil(t, (*pricingstate.TestPublishedPricing).Load(), "unused/disabled malformed rows must not block honest cold initialization")
	assert.Zero(t, writes)
	var unchanged []legacy.Channel
	require.NoError(t, legacy.DB.Where("id IN ?", []int{1111, 1112}).Find(&unchanged).Error)
	require.Len(t, unchanged, 2)
	for _, row := range unchanged {
		assert.Equal(t, invalid, row.OtherSettings)
		require.NotNil(t, row.Setting)
		assert.Equal(t, invalid, *row.Setting)
	}
}

func TestPricingNativeGeminiEmbeddingsPreserveInputTariff(t *testing.T) {
	for _, scenario := range []struct {
		public, upstream, expression string
		amount                       float64
	}{
		{"gemini-embedding-001", "gemini-embedding-001", `tier("embedding", p * 0.15)`, .15},
		{"gemini-embedding-2", "gemini-embedding-2", `tier("embedding", p * 0.20)`, .20},
		{"gemini-embedding-2-preview", "gemini-embedding-2-preview", `tier("embedding", p * 0.20)`, .20},
		{"embedding-alias", "gemini-embedding-001", `tier("embedding", p * 0.15 + c * 0)`, .15},
		{"text-embedding-004", "text-embedding-004", `tier("embedding", p * 0.10)`, .10},
	} {
		t.Run(scenario.public, func(t *testing.T) {
			mapping, err := common.Marshal(map[string]string{scenario.public: scenario.upstream})
			require.NoError(t, err)
			mappingJSON := string(mapping)
			channel := &legacy.Channel{Id: 991, Type: constant.ChannelTypeGemini, Status: common.ChannelStatusEnabled, ModelMapping: &mappingJSON}
			item := pricingdomain.Pricing{
				ModelName:        scenario.public,
				PriceConfigured:  true,
				BillingMode:      billing_setting.BillingModeTieredExpr,
				BillingExpr:      scenario.expression,
				ConfiguredPrices: map[string][][]pricingdomain.ExpressionPrice{scenario.expression: pricingstate.TestPricingCapabilities().Expressions.CatalogPrices(scenario.expression)},
			}
			abilities := []channelrecord.AbilityWithChannel{{
				Ability: channelrecord.Ability{
					ChannelId: channel.Id,
					Model:     scenario.public,
					Group:     "default",
					Enabled:   true,
				},
				ChannelType: channel.Type,
			}}
			pricingstate.TestProjectPricingOperations(
				&item,
				abilities,
				map[int]*legacy.Channel{channel.Id: channel},
				jsplugin.NewRegistry().Generation(),
				nil,
				nil,
			)
			for _, offers := range [][]pricingdomain.CatalogOffer{item.CatalogOffersForGroup("default"), item.PublicCatalogOffersByGroup["default"]} {
				selected := pricingstate.SelectCatalogOffer(offers)
				require.NotNil(t, selected)
				assert.Equal(t, "embeddings", selected.Operation)
				assert.Equal(t, "input", selected.MainComponent)
				require.Len(t, selected.Prices, 1)
				assert.Equal(t, "input", selected.Prices[0].Component)
				assert.Equal(t, "million_tokens", selected.Prices[0].Unit)
				require.NotNil(t, selected.Prices[0].AmountUSD)
				assert.Equal(t, scenario.amount, *selected.Prices[0].AmountUSD)
			}
		})
	}
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeOpenAI} {
		profile := pricingstate.TestConfiguredPricingOperation(
			&legacy.Channel{Type: channelType},
			"gemini-2.5-flash",
			"gemini-2.5-flash",
			constant.EndpointTypeOpenAI,
		)
		offers := pricingstate.TestConfiguredOperationOffers(
			pricingdomain.Pricing{
				PriceConfigured: true,
				ModelRatio:      .15,
				CompletionRatio: 2,
			},
			profile,
			"sync",
			"",
		)
		selected := pricingstate.SelectCatalogOffer(offers)
		require.NotNil(t, selected)
		assert.Equal(t, "text", selected.Operation)
		assert.Equal(t, "output", selected.MainComponent)
		require.Len(t, selected.Prices, 2)
		assert.Equal(t, .6, *selected.Prices[1].AmountUSD)
	}
	profile := pricingstate.TestConfiguredPricingOperation(
		&legacy.Channel{Type: constant.ChannelTypeGemini},
		"gemini-embedding-001",
		"gemini-embedding-001",
		constant.EndpointTypeGemini,
	)
	expression := `tier("unsupported", p * .15 + c * .1)`
	offers := pricingstate.TestConfiguredOperationOffers(
		pricingdomain.Pricing{
			BillingMode:      billing_setting.BillingModeTieredExpr,
			ConfiguredPrices: map[string][][]pricingdomain.ExpressionPrice{expression: pricingstate.TestPricingCapabilities().Expressions.CatalogPrices(expression)},
		},
		profile,
		"sync",
		expression,
	)
	assert.Nil(t, pricingstate.SelectCatalogOffer(offers), "payable output must not be silently discarded for an embedding tariff")
}

func TestPricingNativeLyriaSongAndVeoEnumTariffs(t *testing.T) {
	source, err := os.ReadFile("../../../../plugins/tasks/google/plugin.js")
	require.NoError(t, err)
	registry := jsplugin.NewRegistry()
	_, err = registry.Register(string(source), jsplugin.Options{})
	require.NoError(t, err)
	for _, scenario := range []struct {
		name   string
		amount float64
	}{{"lyria-3-clip-preview", .04}, {"lyria-3-pro-preview", .08}, {"lyria-3.5", .08}} {
		t.Run(scenario.name, func(t *testing.T) {
			profile := pricingstate.TestConfiguredPricingOperation(
				&legacy.Channel{Type: constant.ChannelTypeGemini},
				"music-alias",
				scenario.name,
				constant.EndpointTypeGemini,
			)
			selected := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(
				pricingdomain.Pricing{
					PriceConfigured: true,
					QuotaType:       1,
					ModelPrice:      scenario.amount,
				},
				profile,
				"sync",
				"",
			))
			require.NotNil(t, selected)
			assert.Equal(t, "music_generation", selected.Operation)
			assert.Equal(t, "audio_output", selected.MainComponent)
			require.Len(t, selected.Prices, 1)
			assert.Equal(t, "song", selected.Prices[0].Unit)
			assert.Equal(t, scenario.amount, *selected.Prices[0].AmountUSD)
			assert.Nil(
				t,
				pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(
					pricingdomain.Pricing{
						PriceConfigured: true,
						ModelRatio:      0,
					},
					profile,
					"sync",
					"",
				)),
			)
			assert.Nil(t, pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(pricingdomain.Pricing{}, profile, "sync", "")))
			free := pricingstate.SelectCatalogOffer(pricingstate.TestConfiguredOperationOffers(
				pricingdomain.Pricing{
					PriceConfigured: true,
					QuotaType:       1,
					ModelPrice:      0,
				},
				profile,
				"sync",
				"",
			))
			require.NotNil(t, free)
			assert.Zero(t, *free.Prices[0].AmountUSD)
		})
	}
	channel := &legacy.Channel{Id: 992, Type: constant.ChannelTypeGemini, Status: common.ChannelStatusEnabled}
	for _, scenario := range []struct {
		name, expression string
		amounts          []float64
	}{
		{
			"veo-3.1-fast-generate-preview",
			`tier("video", u("seconds") * (u("resolution") == "4k" ? 0.30 : (u("resolution") == "1080p" ? 0.12 : 0.10)))`,
			[]float64{
				.30,
				.12,
				.10,
			},
		},
		{"veo-3.1-generate-preview", `tier("video", u("seconds") * (u("resolution") == "4k" ? 0.60 : 0.40))`, []float64{.60, .40}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			item := pricingdomain.Pricing{ModelName: scenario.name, BillingMode: billing_setting.BillingModeTieredExpr, BillingExpr: scenario.expression}
			abilities := []channelrecord.AbilityWithChannel{{
				Ability: channelrecord.Ability{
					ChannelId: channel.Id,
					Model:     scenario.name,
					Group:     "default",
					Enabled:   true,
				},
				ChannelType: channel.Type,
			}}
			pricingstate.TestProjectPricingOperations(&item, abilities, map[int]*legacy.Channel{channel.Id: channel}, registry.Generation(), nil, nil)
			for _, offers := range [][]pricingdomain.CatalogOffer{item.CatalogOffersForGroup("default"), item.PublicCatalogOffersByGroup["default"]} {
				require.Len(t, offers, len(scenario.amounts)*2)
				for index, amount := range scenario.amounts {
					for actionIndex, action := range []string{constant.TaskActionTextToVideo, constant.TaskActionImageToVideo} {
						offer := offers[index*2+actionIndex]
						assert.Equal(t, action, offer.Operation)
						assert.Equal(t, "video_output", offer.MainComponent)
						require.Len(t, offer.Prices, 1)
						assert.Equal(t, "second", offer.Prices[0].Unit)
						assert.Equal(t, amount, *offer.Prices[0].AmountUSD)
						require.NotEmpty(t, offer.PublicUsageConditions)
						assert.Equal(t, "resolution", offer.PublicUsageConditions[0].Field)
						assert.Equal(t, "4k", offer.PublicUsageConditions[0].Value)
						assert.Equal(t, index > 0, offer.PublicUsageConditions[0].Negated)
					}
				}
			}
		})
	}
	free := pricingstate.TestConfiguredVeoUsageOffers(
		pricingdomain.Pricing{
			BillingMode: billing_setting.BillingModeTieredExpr,
			BillingExpr: `tier("video", u("seconds") * (u("resolution") == "4k" ? 0 : .2))`,
		},
		channel,
		"veo-3.1-generate-preview",
		registry.Generation(),
	)
	require.Len(t, free, 4)
	selected := pricingstate.SelectCatalogOffer(free)
	require.NotNil(t, selected)
	assert.Zero(t, *selected.Prices[0].AmountUSD, "reachable explicit zero remains free")
	for _, expression := range []string{
		`tier("video", u("seconds") * u("seconds"))`,
		`tier("video", u("missing") * .4)`,
		`tier("video", u("seconds") * (u("resolution") == "invalid" ? .3 : .1))`,
		`tier("video", u("seconds") * .4 + 1)`,
		`tier("video", u("seconds") * (u("resolution") == "4k" ? .4 : (u("resolution") == "4k" ? 0 : .2)))`,
	} {
		unknown := pricingstate.TestConfiguredVeoUsageOffers(
			pricingdomain.Pricing{
				BillingMode: billing_setting.BillingModeTieredExpr,
				BillingExpr: expression,
			},
			channel,
			"veo-3.1-generate-preview",
			registry.Generation(),
		)
		require.Len(t, unknown, 2)
		assert.Nil(t, pricingstate.SelectCatalogOffer(unknown))
		assert.Empty(t, unknown[0].Prices)
	}
}

// TestPricingPortParity compares the new fact/codec ports with their existing SDK
// and expression implementations; it does not define a second pricing oracle.
func TestPricingPortParity(t *testing.T) {
	zero := 0.0
	mode := "sync"
	amount := "0.000001"
	document := catalogsync.Document{
		SchemaVersion: 1, ID: "vendor/model", Name: "Model", Description: &mode,
		Author: &catalogsync.Author{Namespace: "vendor"}, Type: "text",
		Modalities: catalogsync.Modalities{Input: []string{}, Output: []string{"text"}},
		Metadata:   []byte(`{"zero":0}`), Capabilities: []byte(`{}`), Limits: []byte(`{}`),
		Offers: []catalogsync.Offer{{ID: "one", Scope: "endpoint", Identity: []byte(`{}`),
			Provider: &catalogsync.Provider{ID: &mode}, Operation: &mode, Mode: &mode,
			Eligibility: &catalogsync.Eligibility{Eligible: true, Reasons: []string{}},
			Components:  []catalogsync.Component{{Parameter: "p", ComponentID: "input", AmountUSD: &amount, Unit: "million_tokens", Quantity: "1"}},
			Coverage:    catalogsync.Coverage{Reasons: []string{}, ExpectedSources: []string{"one"}}, Limitations: []string{}, Facts: []byte(`{}`)}},
		Coverage: catalogsync.Coverage{Offers: "complete"}, Limitations: []string{}, Provenance: []byte(`{}`),
	}
	catalog := jsplugin.PriceCatalog{FetchedAt: "2026-01-01T00:00:00Z", Models: map[string][]jsplugin.CatalogOffer{
		"model": {{
			ID:            "zero",
			Operation:     "text",
			Mode:          "sync",
			MainComponent: "input",
			Prices: []jsplugin.CatalogPrice{{
				Component: "input",
				Unit:      "million_tokens",
				AmountUSD: &zero,
			}},

			PublicConditions: []billingexpr.TierCondition{{Var: "len", Op: ">", Value: 0, Operands: []billingexpr.TierCondition{}}},

			PublicUsageConditions: []jsplugin.CatalogUsageCondition{{
				Field:   "mode",
				Value:   "fast",
				Negated: true,
			}},
			PublicAlternatives: []jsplugin.CatalogOffer{},
		}},
	}}
	schema := jsplugin.UsageFieldSchema{
		Type:        "enum",
		Unit:        "count",
		Enum:        []string{},
		Description: jsplugin.LocalizedText{"en": "Mode"},
		EnumLabels: map[string]jsplugin.LocalizedText{
			"fast": {},
			"slow": nil,
		},
	}
	deps := pricingstate.TestPricingCapabilities()
	for _, tc := range []struct {
		name               string
		native, mapped     any
		expression, decode string
		originalError      string
		originalType       reflect.Type
		binding            string
	}{
		{name: "source-document", native: document, mapped: pricingadapter.ToSourceDocument(document)},
		{name: "plugin-catalog", native: catalog, mapped: pricingadapter.ToTariffCatalog(catalog)},
		{name: "usage-schema", native: schema, mapped: pricingadapter.ToTariffUsageField(schema)},
		{name: "nil-catalog", native: jsplugin.PriceCatalog{}, mapped: pricingadapter.ToTariffCatalog(jsplugin.PriceCatalog{})},
		{
			name:   "empty-catalog",
			native: jsplugin.PriceCatalog{Models: map[string][]jsplugin.CatalogOffer{}},
			mapped: pricingadapter.ToTariffCatalog(jsplugin.PriceCatalog{Models: map[string][]jsplugin.CatalogOffer{}}),
		},
		{name: "fixed-zero", expression: `tier("base", fixed(0))`},
		{name: "context-cache", expression: `len <= 100 ? tier("short", p * 2 + cr * 0.5) : tier("long", p * 4 + cr * 1)`},
		{name: "invalid-expression", expression: `tier(`},

		{name: "binding-root", binding: `[]`},
		{name: "binding-upstreams", binding: `{"upstreams":"bad"}`},
		{name: "binding-field", binding: `{"upstreams":[{"name":4}]}`},
		{name: "binding-null", binding: `null`},
		{name: "binding-empty", binding: `{"channel_ids":[],"upstreams":[],"timeout":0}`},
		{name: "binding-valid", binding: `{"upstreams":[{"name":"x","base_url":"https://example.com"}],"timeout":0}`},
		{name: "decode-empty", decode: `{"version":1,"entries":{}}`},
		{name: "decode-null", decode: `null`},
		{name: "decode-bad-offer", decode: `{"version":1,"entries":{"x":{"offers":[{"prices":[{"amount_usd":"bad"}]}]}}}`},
		{name: "decode-bad-array", decode: `{"entries":{"x":{"offers":"bad"}}}`},
		{
			name:          "decode-root-type",
			decode:        `[]`,
			originalError: "json: cannot unmarshal array into Go value of type domain.PricingCatalogSnapshot",
			originalType:  reflect.TypeFor[pricingdomain.PricingCatalogSnapshot](),
		},
		{
			name:          "decode-map-type",
			decode:        `{"entries":1}`,
			originalError: "json: cannot unmarshal number into Go struct field PricingCatalogSnapshot.entries of type map[string]domain.PricingCatalogEntry",
			originalType:  reflect.TypeFor[map[string]pricingdomain.PricingCatalogEntry](),
		},
		{
			name:          "decode-entry-type",
			decode:        `{"entries":{"x":1}}`,
			originalError: "json: cannot unmarshal number into Go struct field PricingCatalogSnapshot.entries.x of type domain.PricingCatalogEntry",
			originalType:  reflect.TypeFor[pricingdomain.PricingCatalogEntry](),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			native, mapped := tc.native, tc.mapped
			if tc.binding != "" {
				var original dto.UpstreamRequest
				originalContext, _ := gin.CreateTestContext(httptest.NewRecorder())
				originalContext.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.binding))
				originalErr := originalContext.ShouldBindJSON(&original)
				convertedContext, _ := gin.CreateTestContext(httptest.NewRecorder())
				convertedContext.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.binding))
				converted, convertedErr := pricingadapter.DecodeUpstreamRequest(convertedContext.ShouldBindJSON)
				if originalErr != nil {
					require.EqualError(t, convertedErr, originalErr.Error())
				} else {
					require.NoError(t, convertedErr)
				}
				native, mapped = original, converted
			}

			if tc.expression != "" {
				original, originalErr := billingexpr.EnumerateTiers(tc.expression)
				converted, convertedErr := deps.Expressions.EnumerateTiers(tc.expression)
				if originalErr != nil {
					require.EqualError(t, convertedErr, originalErr.Error())
				} else {
					require.NoError(t, convertedErr)
				}
				native, mapped = original, converted
				want, err := common.Marshal(billingexpr.CatalogPriceTiers(tc.expression))
				require.NoError(t, err)
				got, err := common.Marshal(deps.Expressions.CatalogPriceTiers(tc.expression))
				require.NoError(t, err)
				assert.Equal(t, string(want), string(got))
			}
			if tc.decode != "" {
				var original pricingadapter.PricingCatalogSnapshot
				originalErr := common.UnmarshalJsonStr(tc.decode, &original)
				var converted pricingdomain.PricingCatalogSnapshot
				convertedErr := deps.Codec.DecodeCatalogSnapshot(tc.decode, &converted)
				if tc.originalError != "" {
					require.EqualError(t, convertedErr, tc.originalError)
					var originalFacts pricingdomain.PricingCatalogSnapshot
					baselineError := common.UnmarshalJsonStr(tc.decode, &originalFacts)
					require.EqualError(t, baselineError, tc.originalError)
					var gotTypeError, wantTypeError *json.UnmarshalTypeError
					require.ErrorAs(t, convertedErr, &gotTypeError)
					require.ErrorAs(t, baselineError, &wantTypeError)
					assert.Equal(t, tc.originalType, gotTypeError.Type)
					assert.Equal(t, *wantTypeError, *gotTypeError)
				} else if originalErr != nil {
					require.EqualError(t, convertedErr, originalErr.Error())
				} else {
					require.NoError(t, convertedErr)
				}
				native, mapped = original, converted
			}
			want, err := common.Marshal(native)
			require.NoError(t, err)
			got, err := common.Marshal(mapped)
			require.NoError(t, err)
			assert.Equal(t, string(want), string(got))
		})
	}
}
