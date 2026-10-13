package model

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSoraLegacyPriceBasisFollowsPluginRouteAndUsage(t *testing.T) {
	source, err := os.ReadFile("../plugins/tasks/sora/plugin.js")
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
	channel := &Channel{Type: constant.ChannelTypeOpenAI, ModelMapping: &mapping}
	actual, found, _ := legacyVideoPriceBasis(registry.Generation(), channel, "video-alias")
	require.True(t, found)
	assert.Equal(t, plugin.Meta.LegacyPriceBasis, actual)
	_, found, _ = legacyVideoPriceBasis(registry.Generation(), &Channel{Type: constant.ChannelTypeOpenRouter}, "sora-2")
	assert.False(t, found)
	_, found, _ = legacyVideoPriceBasis(registry.Generation(), channel, "unknown")
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
	actual, found, _ = legacyVideoPriceBasis(registry.Generation(), &Channel{Type: 56}, "sora-2")
	require.True(t, found)
	assert.Equal(t, otherPlugin.Meta.LegacyPriceBasis, actual)
	bound := &Channel{Type: constant.ChannelTypeTaskPlugin}
	bound.SetSetting(dto.ChannelSettings{TaskPluginKey: "other-sora"})
	actual, found, _ = legacyVideoPriceBasis(registry.Generation(), bound, "sora-2")
	require.True(t, found)
	assert.Equal(t, otherPlugin.Meta.LegacyPriceBasis, actual)
	actual, found, _ = legacyVideoPriceBasis(registry.Generation(), channel, "sora-2")
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
	source, err := os.ReadFile("../plugins/tasks/sora/plugin.js")
	require.NoError(t, err)
	previousRegistry := jsplugin.DefaultRegistry
	registry := jsplugin.NewRegistry()
	jsplugin.DefaultRegistry = registry
	t.Cleanup(func() {
		jsplugin.DefaultRegistry = previousRegistry
		InvalidatePricingCache()
	})
	_, err = registry.Register(string(source), jsplugin.Options{})
	require.NoError(t, err)
	orSource := strings.Replace(string(source), `key: "sora"`, `key: "or-video-probe"`, 1)
	orSource = strings.Replace(orSource, "channelTypes: [55, 1]", "channelTypes: [20]", 1)
	_, err = registry.Register(orSource, jsplugin.Options{})
	require.NoError(t, err)

	mapping := `{"video-alias":"sora-2"}`
	require.NoError(t, DB.Create(&Channel{Id: 931, Type: constant.ChannelTypeOpenAI, Key: "test", Status: 1, Models: "sora-2,video-alias,ordinary-model", ModelMapping: &mapping}).Error)
	insertPricingEndpointAbility(t, 931, "sora-2")
	insertPricingEndpointAbility(t, 931, "video-alias")
	insertPricingEndpointAbility(t, 931, "ordinary-model")
	insertPricingEndpointChannel(t, 932, constant.ChannelTypeOpenRouter, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 932, "sora-2")
	InitChannelCache()
	pricing := pricingByModel(GetPricing())
	assert.Equal(t, []string{BillingSourceConfiguredTariff, BillingSourceOpenRouterActualCost}, pricing["sora-2"].BillingSourcesForGroup("default")[constant.EndpointTypeOpenAIVideo])
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
	insertPricingEndpointChannel(t, 942, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{IncomingPath: "/v1/images/generations", UpstreamPath: "/v1/images/generations"}))
	insertPricingEndpointAbility(t, 942, "flux-video-edit")
	InitChannelCache()
	InvalidatePricingCache()
	pricing = pricingByModel(GetPricing())
	_, known = pricing["sora-2"].LegacyVideoPriceBasisForGroup("default")
	assert.False(t, known)
}

func TestPricingVideoPluginReplacesOnlyInferredImageEndpoint(t *testing.T) {
	resetPricingEndpointTestTables(t)
	source, err := os.ReadFile("../plugins/tasks/openrouter-video/plugin.js")
	require.NoError(t, err)
	previousRegistry := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	t.Cleanup(func() { jsplugin.DefaultRegistry = previousRegistry; InvalidatePricingCache() })
	_, err = jsplugin.DefaultRegistry.Register(string(source), jsplugin.Options{})
	require.NoError(t, err)
	mapping := `{"flux-3-video":"black-forest-labs/flux-3-video","flux-video-edit":"black-forest-labs/flux-video-edit","flux-video-upscale":"black-forest-labs/flux-video-upscale","grok-imagine-video":"x-ai/grok-imagine-video","grok-imagine-video-1.5":"x-ai/grok-imagine-video-1.5","grok-imagine-video-1.5-lite":"x-ai/grok-imagine-video-1.5-lite"}`
	models := []string{"flux-3-video", "flux-video-edit", "flux-video-upscale", "grok-imagine-video", "grok-imagine-video-1.5", "grok-imagine-video-1.5-lite"}
	require.NoError(t, DB.Create(&Channel{Id: 940, Type: constant.ChannelTypeOpenRouter, Key: "test", Status: 1, Models: strings.Join(models, ","), ModelMapping: &mapping}).Error)
	for _, name := range models {
		insertPricingEndpointAbility(t, 940, name)
	}
	InitChannelCache()
	pricing := pricingByModel(GetPricing())
	for _, name := range models {
		assert.Contains(t, pricing[name].SupportedEndpointTypes, constant.EndpointTypeOpenAIVideo)
		assert.NotContains(t, pricing[name].SupportedEndpointTypes, constant.EndpointTypeImageGeneration)
		assert.NotContains(t, pricing[name].BillingSourcesForGroup("default"), constant.EndpointTypeImageGeneration)
	}
	require.NoError(t, DB.Create(&Model{ModelName: "flux-3-video", Endpoints: `{"image-generation":"/v1/images/generations"}`}).Error)
	insertPricingEndpointChannel(t, 941, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 941, "grok-imagine-video")
	insertPricingEndpointChannel(t, 942, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{IncomingPath: "/v1/images/generations", UpstreamPath: "/v1/images/generations"}))
	insertPricingEndpointAbility(t, 942, "flux-video-edit")
	InitChannelCache()
	InvalidatePricingCache()
	pricing = pricingByModel(GetPricing())
	assert.Contains(t, pricing["flux-3-video"].SupportedEndpointTypes, constant.EndpointTypeImageGeneration)
	assert.Contains(t, pricing["grok-imagine-video"].SupportedEndpointTypes, constant.EndpointTypeImageGeneration)
	assert.Contains(t, pricing["grok-imagine-video"].BillingSourcesForGroup("default"), constant.EndpointTypeImageGeneration)
	assert.Contains(t, pricing["flux-video-edit"].SupportedEndpointTypes, constant.EndpointTypeImageGeneration)
	assert.Equal(t, []string{BillingSourceConfiguredTariff}, pricing["flux-video-edit"].BillingSourcesForGroup("default")[constant.EndpointTypeImageGeneration])
	assert.Equal(t, []string{BillingSourceOpenRouterActualCost}, pricing["flux-video-edit"].BillingSourcesForGroup("default")[constant.EndpointTypeOpenAIVideo])
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

	initialPricing := pricingByModel(GetPricing())
	require.Contains(t, initialPricing, "pricing-usage-model")
	require.Contains(t, initialPricing, "ordinary-model")
	assert.Equal(t, "second", initialPricing["pricing-usage-model"].BillingUsageSchema["seconds"].Unit)
	assert.Equal(t, "Estimated duration.", initialPricing["pricing-usage-model"].BillingUsageSchema["seconds"].Description["en"])
	assert.Equal(t, "生成视频", initialPricing["pricing-usage-model"].BillingUsageSchema["action"].EnumLabels["video"]["zh"])
	assert.Nil(t, initialPricing["ordinary-model"].BillingUsageSchema)

	updatedSource := pricingUsagePluginSource("1.1.0", `{
  seconds: {type: "number", unit: "second", description: "Measured duration."},
  clips: {type: "number", unit: "count", description: "Generated clip count."}
}`)
	_, err = jsplugin.DefaultRegistry.Register(updatedSource, jsplugin.Options{})
	require.NoError(t, err)
	lastGetPricingTime = time.Now().Add(-2 * time.Minute)

	refreshedPricing := pricingByModel(GetPricing())
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
	channel := &Channel{
		Id:           910,
		Type:         constant.ChannelTypeTaskPlugin,
		Key:          "key-910",
		Status:       1,
		Name:         "channel-910",
		Models:       "alias-model,pricing-usage-model",
		ModelMapping: &mapping,
	}
	require.NoError(t, DB.Create(channel).Error)
	insertPricingEndpointAbility(t, 910, "alias-model")
	insertPricingEndpointAbility(t, 910, "pricing-usage-model")
	InitChannelCache()

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
	InvalidatePricingCache()

	pricing := pricingByModel(GetPricing())
	require.Contains(t, pricing, "alias-model")
	require.Contains(t, pricing, "pricing-usage-model")
	assert.Equal(t, "second", pricing["alias-model"].BillingUsageSchema["seconds"].Unit)
	assert.Equal(t, "Estimated duration.", pricing["alias-model"].BillingUsageSchema["seconds"].Description["en"])
	assert.Equal(t, "tiered_expr", pricing["alias-model"].BillingMode)
	assert.Equal(t, `u("seconds")`, pricing["alias-model"].BillingExpr)
	assert.Equal(t, "tiered_expr", pricing["pricing-usage-model"].BillingMode)
	assert.Equal(t, `u("seconds")`, pricing["pricing-usage-model"].BillingExpr)

	ownMapping := `{"alias-own-expr":"pricing-usage-model"}`
	own := &Channel{
		Id:           911,
		Type:         constant.ChannelTypeTaskPlugin,
		Key:          "key-911",
		Status:       1,
		Name:         "channel-911",
		Models:       "alias-own-expr,pricing-usage-model",
		ModelMapping: &ownMapping,
	}
	require.NoError(t, DB.Create(own).Error)
	insertPricingEndpointAbility(t, 911, "alias-own-expr")
	InitChannelCache()
	InvalidatePricingCache()

	refreshed := pricingByModel(GetPricing())
	assert.Equal(t, `u("seconds") * 2`, refreshed["alias-own-expr"].BillingExpr)
	assert.Equal(t, "second", refreshed["alias-own-expr"].BillingUsageSchema["seconds"].Unit)
}

func pricingByModel(pricings []Pricing) map[string]Pricing {
	result := make(map[string]Pricing, len(pricings))
	for _, pricing := range pricings {
		result[pricing.ModelName] = pricing
	}
	return result
}

func TestPricingHeygenReferenceOperationsUseEligibleTariff(t *testing.T) {
	source, err := os.ReadFile("../plugins/tasks/openrouter-video/plugin.js")
	require.NoError(t, err)
	plugin, err := jsplugin.CompilePlugin(string(source), jsplugin.Options{})
	require.NoError(t, err)
	require.NotNil(t, plugin.Meta.PriceCatalog)
	offers := plugin.Meta.PriceCatalog.Models["heygen/heygen-video-1"]
	require.NotEmpty(t, offers)
	for _, scenario := range []struct {
		operation string
		amountUSD float64
	}{{"text_to_video", .02}, {"image_to_video", .04}, {"video_to_video", .04}} {
		t.Run(scenario.operation, func(t *testing.T) {
			eligible := make([]CatalogOffer, 0)
			for _, offer := range offers {
				if offer.Operation == scenario.operation {
					eligible = append(eligible, offer)
				}
			}
			selected := SelectCatalogOffer(eligible)
			require.NotNil(t, selected)
			require.Len(t, selected.Prices, 1)
			assert.Equal(t, "video_output", selected.Prices[0].Component)
			assert.Equal(t, "second", selected.Prices[0].Unit)
			require.NotNil(t, selected.Prices[0].AmountUSD)
			assert.Equal(t, scenario.amountUSD, *selected.Prices[0].AmountUSD)
		})
	}
}
