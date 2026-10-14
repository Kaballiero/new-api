package model

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
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

func TestPricingConfiguredFormulaProjectionUsesActualRouteSource(t *testing.T) {
	resetPricingEndpointTestTables(t)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)); InvalidatePricingCache() })
	names := []string{"direct-openai", "direct-anthropic", "direct-gemini", "actualcost-or"}
	modes := map[string]string{}
	expressions := map[string]string{}
	for index, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeAnthropic, constant.ChannelTypeGemini, constant.ChannelTypeOpenRouter} {
		insertPricingEndpointChannel(t, 980+index, channelType, dto.ChannelOtherSettings{})
		insertPricingEndpointAbility(t, 980+index, names[index])
		modes[names[index]] = "tiered_expr"
		expressions[names[index]] = `tier("base",p*2+c*3)`
	}
	modeJSON, err := common.Marshal(modes)
	require.NoError(t, err)
	exprJSON, err := common.Marshal(expressions)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": string(modeJSON), "billing_setting.billing_expr": string(exprJSON)}))
	InitChannelCache()
	pricing := pricingByModel(GetPricing())
	for _, name := range names[:3] {
		item := pricing[name]
		require.Contains(t, item.ConfiguredPrices, expressions[name])
		require.Len(t, item.ConfiguredPrices[expressions[name]], 1)
		assert.Len(t, item.ConfiguredPrices[expressions[name]][0], 2)
		selected := SelectCatalogOffer(item.CatalogOffersForGroup("default"))
		require.NotNil(t, selected)
		assert.Equal(t, "text", selected.Operation)
		assert.Len(t, selected.Prices, 2)
	}
	assert.Empty(t, pricing["actualcost-or"].ConfiguredPrices)
	assert.Nil(t, SelectCatalogOffer(pricing["actualcost-or"].CatalogOffersForGroup("default")))
}

func TestPricingOperationCounterProfilesAndFixedPrecedence(t *testing.T) {
	previousCount := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = previousCount })
	channel := &Channel{Type: constant.ChannelTypeOpenAI}
	for _, scenario := range []struct {
		public, upstream, operation, unit string
		ratio, amount                     float64
	}{
		{"tts-1", "tts-1", "speech_generation", "million_characters", 7.5, 15},
		{"tts-1-hd", "tts-1-hd", "speech_generation", "million_characters", 15, 30},
		{"whisper-1", "whisper-1", "transcription", "minute", 15, .03},
	} {
		t.Run(scenario.public, func(t *testing.T) {
			profile := configuredPricingOperation(channel, scenario.public, scenario.upstream, constant.EndpointTypeOpenAI)
			offers := configuredOperationOffers(Pricing{PriceConfigured: true, ModelRatio: scenario.ratio, CompletionRatio: 1}, profile, "sync", "")
			selected := SelectCatalogOffer(offers)
			require.NotNil(t, selected)
			assert.Equal(t, scenario.operation, selected.Operation)
			require.Len(t, selected.Prices, 1)
			assert.Equal(t, scenario.unit, selected.Prices[0].Unit)
			assert.InDelta(t, scenario.amount, *selected.Prices[0].AmountUSD, 1e-12)
		})
	}
	for _, pair := range [][2]string{{"gpt-alias", "tts-1"}, {"alias", "whisper-1"}, {"rune-alias", "gpt-4o-mini-tts"}} {
		profile := configuredPricingOperation(channel, pair[0], pair[1], constant.EndpointTypeOpenAI)
		assert.Nil(t, SelectCatalogOffer(configuredOperationOffers(Pricing{PriceConfigured: true, ModelRatio: .625, CompletionRatio: 4}, profile, "sync", "")))
		fixed := configuredOperationOffers(Pricing{PriceConfigured: true, QuotaType: 1, ModelPrice: .02}, profile, "sync", "")
		require.Len(t, fixed, 1)
		selected := SelectCatalogOffer(fixed)
		require.NotNil(t, selected)
		assert.Equal(t, "call", selected.Prices[0].Unit)
		assert.Equal(t, .02, *selected.Prices[0].AmountUSD)
	}
	constant.CountToken = false
	profile := configuredPricingOperation(channel, "tts-1", "tts-1", constant.EndpointTypeOpenAI)
	assert.Equal(t, "unknown", profile.basis)
}

func TestPricingEmbeddingProjectionUsesInputMainAndIndependentBatch(t *testing.T) {
	resetPricingEndpointTestTables(t)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)); InvalidatePricingCache() })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":       `{"embedding-alias":"tiered_expr"}`,
		"billing_setting.billing_expr":       `{"embedding-alias":"tier(\"base\",p*.13)"}`,
		"billing_setting.batch_billing_expr": `{"embedding-alias":"tier(\"batch\",p*.065)"}`,
	}))
	mapping := `{"embedding-alias":"text-embedding-3-large"}`
	require.NoError(t, DB.Create(&Channel{Id: 988, Type: constant.ChannelTypeOpenAI, Status: 1, ModelMapping: &mapping}).Error)
	insertPricingEndpointAbility(t, 988, "embedding-alias")
	InitChannelCache()
	item := pricingByModel(GetPricing())["embedding-alias"]
	offers := item.CatalogOffersForGroup("default")
	require.Len(t, offers, 2)
	for _, offer := range offers {
		assert.Equal(t, "embeddings", offer.Operation)
		assert.Equal(t, "input", offer.MainComponent)
		selected := SelectCatalogOffer([]CatalogOffer{offer})
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
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.batch_billing_expr": `{"public:batch":"tier(\"batch\",provider_cost)"}`}))
	mapping := `{"public":"anthropic/claude-fable-5.1","public:batch":"anthropic/claude-fable-5.1:batch"}`
	channels := map[int]*Channel{989: {Id: 989, Type: constant.ChannelTypeOpenRouter, Status: 1, ModelMapping: &mapping}}
	abilities := []AbilityWithChannel{
		{Ability: Ability{ChannelId: 989, Model: "public", Group: "default", Enabled: true}, ChannelType: constant.ChannelTypeOpenRouter},
		{Ability: Ability{ChannelId: 989, Model: "public:batch", Group: "default", Enabled: true}, ChannelType: constant.ChannelTypeOpenRouter},
		{Ability: Ability{ChannelId: 989, Model: "public:batch", Group: "premium", Enabled: true}, ChannelType: constant.ChannelTypeOpenRouter},
	}
	zero := 0.0
	alias := Pricing{ModelName: "public:batch", EnableGroup: []string{"default", "premium"}, CatalogOffersByGroup: map[string][]CatalogOffer{"default": {{ID: "989:anthropic/claude-fable-5.1:batch:native", Operation: "text", Mode: "batch", MainComponent: "output", Prices: []CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: &zero}}}}}}
	projectPricingOperations(&alias, abilities, channels, jsplugin.NewRegistry().Generation(), nil)
	assert.Equal(t, map[string]string{"default": "public"}, alias.ConsumerCanonicalModelsByGroup)
	base := Pricing{ModelName: "public", EnableGroup: []string{"default"}}
	visible := ConsumerPricingCatalogue([]Pricing{base, alias}, map[string]string{"default": "Default", "premium": "Premium"})
	require.Len(t, visible, 2)
	indexed := pricingByModel(visible)
	require.Len(t, indexed["public"].CatalogOffersForGroup("default"), 1)
	selected := SelectCatalogOffer(indexed["public"].CatalogOffersForGroup("default"))
	require.NotNil(t, selected)
	assert.Zero(t, *selected.Prices[0].AmountUSD)
	assert.Empty(t, indexed["public"].CatalogOffersForGroup("premium"))
	assert.Equal(t, []string{"premium"}, indexed["public:batch"].EnableGroup)
	assert.Equal(t, "batch", indexed["public:batch"].CatalogOffersForGroup("premium")[0].Mode)
	assert.Equal(t, "public:batch", alias.ModelName)
	badMapping := `{"public":"different/model","public:batch":"anthropic/claude-fable-5.1:batch"}`
	channels[989].ModelMapping = &badMapping
	_, paired := pairedPricingBatchBase(abilities[1], "anthropic/claude-fable-5.1:batch", abilities, channels)
	assert.False(t, paired)
}

func TestPricingUpstreamIdentityMissingMappingAndMalformedMapping(t *testing.T) {
	channel := &Channel{}
	name, known := ResolvePricingUpstreamModel(channel, "sora-2")
	assert.True(t, known)
	assert.Equal(t, "sora-2", name)
	for _, mapping := range []string{"", "  ", "{}"} {
		channel.ModelMapping = &mapping
		name, known = ResolvePricingUpstreamModel(channel, "direct")
		assert.True(t, known)
		assert.Equal(t, "direct", name)
	}
	for _, mapping := range []string{"{", `{"a":"b","b":"a"}`} {
		channel.ModelMapping = &mapping
		_, known = ResolvePricingUpstreamModel(channel, "a")
		assert.False(t, known)
	}
	_, known = ResolvePricingUpstreamModel(nil, "a")
	assert.False(t, known)
	_, known = ResolvePricingUpstreamModel(channel, " ")
	assert.False(t, known)
}

func TestPricingNativeImageProofReplacesDefaultsAndPreservesMixedText(t *testing.T) {
	resetPricingEndpointTestTables(t)
	previousState := pricingCatalogState.Load()
	t.Cleanup(func() { pricingCatalogState.Store(previousState); InvalidatePricingCache() })
	mapping := `{"pure-image":"sourceful/riverflow","mixed-image":"google/gemini-image"}`
	channel := &Channel{Id: 992, Type: constant.ChannelTypeOpenRouter, Status: 1, ModelMapping: &mapping}
	require.NoError(t, DB.Create(channel).Error)
	for _, name := range []string{"pure-image", "mixed-image"} {
		insertPricingEndpointAbility(t, 992, name)
	}
	entries := map[string]PricingCatalogEntry{}
	amount := .06
	for _, upstream := range []string{"sourceful/riverflow", "google/gemini-image"} {
		image := PricingCatalogEntry{ChannelID: 992, UpstreamModel: upstream, Endpoint: constant.EndpointTypeImageGeneration, Fingerprint: PricingCatalogSourceFingerprint(channel, upstream, constant.EndpointTypeImageGeneration), Offers: []CatalogOffer{{ID: "native:image", Operation: "image_generation", Mode: "sync", MainComponent: "image_output", Prices: []CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: &amount}}}}}
		entries[pricingCatalogEntryKey(image)] = image
		text := PricingCatalogEntry{ChannelID: 992, UpstreamModel: upstream, Endpoint: constant.EndpointTypeOpenAI, Fingerprint: PricingCatalogSourceFingerprint(channel, upstream, constant.EndpointTypeOpenAI), Offers: []CatalogOffer{}}
		if upstream == "google/gemini-image" {
			text.Offers = []CatalogOffer{{ID: "native:text", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: &amount}}}}
		}
		entries[pricingCatalogEntryKey(text)] = text
	}
	loadPricingCatalog(PricingCatalogSnapshot{Version: 1, Entries: entries})
	InitChannelCache()
	indexed := pricingByModel(GetPricing())
	pure := indexed["pure-image"].CatalogOffersForGroup("default")
	require.Len(t, pure, 1)
	assert.Equal(t, "image_generation", pure[0].Operation)
	mixed := indexed["mixed-image"].CatalogOffersForGroup("default")
	require.Len(t, mixed, 2)
	operations := []string{mixed[0].Operation, mixed[1].Operation}
	assert.ElementsMatch(t, []string{"image_generation", "text"}, operations)
}

func TestPricingConfiguredImageUsesActualOutputUmbrellaAndCountBasis(t *testing.T) {
	channel := &Channel{Type: constant.ChannelTypeOpenAI}
	imageRatio, cacheRatio := 2.0, .25
	for _, name := range []string{"gpt-image-2-2026-04-21", "gpt-image-2.5-flare-2026-09-08", "gpt-image-2.5-sunburst-2026-09-08"} {
		for _, public := range []string{name, "image-alias"} {
			profile := configuredPricingOperation(channel, public, name, constant.EndpointTypeOpenAI)
			require.Equal(t, "image_generation", profile.operation)
			offers := configuredOperationOffers(Pricing{PriceConfigured: true, ModelRatio: 2.5, CompletionRatio: 6, ImageRatio: &imageRatio, CacheRatio: &cacheRatio}, profile, "sync", "")
			selected := SelectCatalogOffer(offers)
			require.NotNil(t, selected)
			assert.Equal(t, "output", selected.MainComponent)
			prices := map[string]CatalogPrice{}
			for _, price := range selected.Prices {
				prices[price.Component] = price
				assert.Equal(t, "million_tokens", price.Unit)
			}
			assert.Equal(t, 5.0, *prices["input"].AmountUSD)
			assert.Equal(t, 30.0, *prices["output"].AmountUSD)
			assert.Equal(t, 10.0, *prices["image_input"].AmountUSD)
			assert.Equal(t, 1.25, *prices["cache_read"].AmountUSD)
			assert.NotContains(t, prices, "image_output")
			zero := configuredOperationOffers(Pricing{PriceConfigured: true, ModelRatio: 0, CompletionRatio: 6}, profile, "sync", "")
			free := SelectCatalogOffer(zero)
			require.NotNil(t, free)
			assert.Zero(t, *free.Prices[1].AmountUSD)
			assert.Nil(t, SelectCatalogOffer(configuredOperationOffers(Pricing{}, profile, "sync", "")))
			fixed := SelectCatalogOffer(configuredOperationOffers(Pricing{PriceConfigured: true, QuotaType: 1, ModelPrice: .04}, profile, "sync", ""))
			require.NotNil(t, fixed)
			assert.Equal(t, "image", fixed.Prices[0].Unit)
		}
	}
}

func TestPricingConfiguredAudioRatioKeepsCompleteSplitOutputVector(t *testing.T) {
	inputAudioRatio, outputAudioRatio := 2.0, 3.0
	profile := configuredPricingOperation(&Channel{Type: constant.ChannelTypeOpenAI}, "gpt-audio", "gpt-audio", constant.EndpointTypeOpenAI)
	item := Pricing{PriceConfigured: true, ModelRatio: .625, CompletionRatio: 4, AudioRatio: &inputAudioRatio, AudioCompletionRatio: &outputAudioRatio}
	offers := configuredOperationOffers(item, profile, "sync", "")
	selected := SelectCatalogOffer(offers)
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
	freeAudio := SelectCatalogOffer(configuredOperationOffers(item, profile, "sync", ""))
	require.NotNil(t, freeAudio)
	assert.Zero(t, *freeAudio.Prices[len(freeAudio.Prices)-1].AmountUSD)
	mini := configuredPricingOperation(&Channel{Type: constant.ChannelTypeOpenAI}, "gpt-4o-mini-tts", "gpt-4o-mini-tts", constant.EndpointTypeOpenAI)
	assert.Nil(t, SelectCatalogOffer(configuredOperationOffers(item, mini, "sync", "")))
}

func TestPricingMiniTTSKeepsTextInputAndExplicitUnknownOutput(t *testing.T) {
	previousCount := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = previousCount })
	expression := `tier("base",p*.6+c*12+ao*12)`
	for _, name := range []string{"gpt-4o-mini-tts", "gpt-4o-mini-tts-2025-03-20", "gpt-4o-mini-tts-2025-12-15"} {
		profile := configuredPricingOperation(&Channel{Type: constant.ChannelTypeOpenAI}, name, name, constant.EndpointTypeOpenAI)
		for _, item := range []Pricing{
			{BillingMode: billing_setting.BillingModeTieredExpr, ConfiguredPrices: map[string][][]billingexpr.DisplayPrice{expression: billingexpr.CatalogPrices(expression)}},
			{PriceConfigured: true, ModelRatio: .3, CompletionRatio: 20},
		} {
			offers := configuredOperationOffers(item, profile, "sync", expression)
			selected := SelectCatalogOffer(offers)
			require.NotNil(t, selected)
			assert.Equal(t, "input", selected.MainComponent)
			require.Len(t, selected.Prices, 2)
			assert.Equal(t, "input", selected.Prices[0].Component)
			assert.Equal(t, "million_tokens", selected.Prices[0].Unit)
			assert.Equal(t, .6, *selected.Prices[0].AmountUSD)
			assert.Equal(t, CatalogPrice{Component: "audio_output", Unit: "unknown"}, selected.Prices[1])
		}
	}
	profile := configuredPricingOperation(&Channel{Type: constant.ChannelTypeOpenAI}, "gpt-4o-mini-tts", "gpt-4o-mini-tts", constant.EndpointTypeOpenAI)
	zeroExpression := `tier("base",p*0+c*12+ao*12)`
	selected := SelectCatalogOffer(configuredOperationOffers(Pricing{BillingMode: billing_setting.BillingModeTieredExpr, ConfiguredPrices: map[string][][]billingexpr.DisplayPrice{zeroExpression: billingexpr.CatalogPrices(zeroExpression)}}, profile, "sync", zeroExpression))
	require.NotNil(t, selected)
	assert.Zero(t, *selected.Prices[0].AmountUSD)
	assert.Nil(t, selected.Prices[1].AmountUSD)
	free := SelectCatalogOffer(configuredOperationOffers(Pricing{PriceConfigured: true, ModelRatio: 0}, profile, "sync", ""))
	require.NotNil(t, free)
	require.Len(t, free.Prices, 1)
	assert.Zero(t, *free.Prices[0].AmountUSD)
	fixedExpression := `fixed(.02)`
	fixed := SelectCatalogOffer(configuredOperationOffers(Pricing{BillingMode: billing_setting.BillingModeTieredExpr, ConfiguredPrices: map[string][][]billingexpr.DisplayPrice{fixedExpression: billingexpr.CatalogPrices(fixedExpression)}}, profile, "sync", fixedExpression))
	require.NotNil(t, fixed)
	require.Len(t, fixed.Prices, 1)
	assert.Equal(t, "call", fixed.Prices[0].Unit)
	constant.CountToken = false
	assert.Equal(t, "unknown", configuredPricingOperation(&Channel{Type: constant.ChannelTypeOpenAI}, "gpt-4o-mini-tts", "gpt-4o-mini-tts", constant.EndpointTypeOpenAI).basis)
}

func TestPricingCatalogueFingerprintIsCanonicalAndTracksConsumerChanges(t *testing.T) {
	fixture := func() []Pricing {
		input, output := 1.0, 2.0
		return []Pricing{
			{ModelName: "alpha", VendorID: 1, EnableGroup: []string{"premium", "default"}, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAI}, ConsumerCanonicalModelsByGroup: map[string]string{"premium": "alpha", "default": "alpha"}, CatalogOffersByGroup: map[string][]CatalogOffer{"default": {{ID: "b", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: &output}, {Component: "input", Unit: "million_tokens", AmountUSD: &input}}}, {ID: "a", Operation: "text", Mode: "batch", MainComponent: "output", Prices: []CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: &input}}}}}},
			{ModelName: "beta", EnableGroup: []string{"default"}},
		}
	}
	vendors := []PricingVendor{{ID: 2, Name: "Second"}, {ID: 1, Name: "First"}}
	original := fixture()
	before, err := pricingCatalogueFingerprint(original, vendors)
	require.NoError(t, err)
	permuted := fixture()
	permuted[0].EnableGroup = []string{"default", "premium"}
	permuted[0].SupportedEndpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}
	offers := permuted[0].CatalogOffersByGroup["default"]
	offers[0].Prices[0], offers[0].Prices[1] = offers[0].Prices[1], offers[0].Prices[0]
	offers[0], offers[1] = offers[1], offers[0]
	permuted[0], permuted[1] = permuted[1], permuted[0]
	after, err := pricingCatalogueFingerprint(permuted, []PricingVendor{vendors[1], vendors[0]})
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.Equal(t, []string{"premium", "default"}, original[0].EnableGroup)
	assert.Equal(t, "output", original[0].CatalogOffersByGroup["default"][0].Prices[0].Component)
	for _, change := range []func([]Pricing){
		func(items []Pricing) {
			value := 3.0
			items[0].CatalogOffersByGroup["default"][0].Prices[0].AmountUSD = &value
		},
		func(items []Pricing) { items[0].CatalogOffersByGroup["default"][0].Operation = "embeddings" },
		func(items []Pricing) { items[0].EnableGroup = []string{"default"} },
		func(items []Pricing) { items[0].Tags = "top text" },
		func(items []Pricing) {
			items[0].PublicCatalogOffersByGroup = map[string][]CatalogOffer{"default": {{PublicConditions: []billingexpr.TierCondition{{Var: "len", Op: ">", Value: 100}}}}}
		},
		func(items []Pricing) { items[0].ConsumerCanonicalModelsByGroup["default"] = "other" },
	} {
		changed := fixture()
		change(changed)
		revision, err := pricingCatalogueFingerprint(changed, vendors)
		require.NoError(t, err)
		assert.NotEqual(t, before, revision)
	}
}

func TestPricingCatalogueSnapshotKeepsCapturedRowsWithTheirRevision(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingEndpointChannel(t, 994, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 994, "snapshot-model")
	InitChannelCache()
	rows, vendors, revision, err := GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	require.NotEmpty(t, revision)
	captured, err := pricingCatalogueFingerprint(rows, vendors)
	require.NoError(t, err)
	assert.Equal(t, revision, captured)
	require.NoError(t, DB.Create(&Model{ModelName: "snapshot-model", Description: "new description"}).Error)
	changed := make(chan string, 1)
	go func() {
		InvalidatePricingCache()
		_, _, current, _ := GetPricingCatalogueSnapshot()
		changed <- current
	}()
	current := <-changed
	assert.NotEqual(t, revision, current)
	captured, err = pricingCatalogueFingerprint(rows, vendors)
	require.NoError(t, err)
	assert.Equal(t, revision, captured)
}

func TestPricingRealtimeProjectsConfiguredTokenCounters(t *testing.T) {
	channel := &Channel{Type: constant.ChannelTypeOpenAI}
	expression := `tier("base",p*.6+c*2.4+cr*.06+ai*10+ao*20+img*.8)`
	for _, name := range []string{"gpt-realtime", "gpt-realtime-2025-08-28", "gpt-realtime-1.5", "gpt-realtime-2", "gpt-realtime-2.1", "gpt-realtime-mini", "gpt-realtime-mini-2025-12-15", "gpt-realtime-2.1-mini", "gpt-realtime-translate", "gpt-realtime-whisper"} {
		t.Run(name, func(t *testing.T) {
			profile := configuredPricingOperation(channel, name, name, constant.EndpointTypeOpenAI)
			item := Pricing{BillingMode: billing_setting.BillingModeTieredExpr, ConfiguredPrices: map[string][][]billingexpr.DisplayPrice{expression: billingexpr.CatalogPrices(expression)}}
			selected := SelectCatalogOffer(configuredOperationOffers(item, profile, "sync", expression))
			require.NotNil(t, selected)
			assert.Equal(t, "realtime", selected.Operation)
			assert.Equal(t, "input", selected.MainComponent)
			prices := map[string]float64{}
			for _, price := range selected.Prices {
				require.NotNil(t, price.AmountUSD)
				assert.Equal(t, "million_tokens", price.Unit)
				prices[price.Component] = *price.AmountUSD
			}
			assert.Equal(t, map[string]float64{"input": .6, "output": 2.4, "cache_read": .06, "audio_input": 10, "audio_output": 20, "image_input": .8}, prices)
		})
	}
	profile := configuredPricingOperation(channel, "gpt-realtime-whisper", "gpt-realtime-whisper", constant.EndpointTypeOpenAI)
	inputOnly := `tier("base",p*17)`
	selected := SelectCatalogOffer(configuredOperationOffers(Pricing{BillingMode: billing_setting.BillingModeTieredExpr, ConfiguredPrices: map[string][][]billingexpr.DisplayPrice{inputOnly: billingexpr.CatalogPrices(inputOnly)}}, profile, "sync", inputOnly))
	require.NotNil(t, selected)
	require.Len(t, selected.Prices, 1)
	assert.Equal(t, 17.0, *selected.Prices[0].AmountUSD)
	audio, completion, cache, image := 2.0, 3.0, .1, 4.0
	item := Pricing{PriceConfigured: true, ModelRatio: .625, CompletionRatio: 4, AudioRatio: &audio, AudioCompletionRatio: &completion, CacheRatio: &cache, ImageRatio: &image}
	selected = SelectCatalogOffer(configuredOperationOffers(item, profile, "sync", ""))
	require.NotNil(t, selected)
	prices := map[string]float64{}
	for _, price := range selected.Prices {
		require.NotNil(t, price.AmountUSD)
		prices[price.Component] = *price.AmountUSD
	}
	assert.Equal(t, map[string]float64{"input": 1.25, "output": 5, "audio_input": 2.5, "audio_output": 7.5}, prices)
}

func TestPricingTranscriptionPublishesConfiguredBillingTokens(t *testing.T) {
	channel := &Channel{Type: constant.ChannelTypeOpenAI}
	expression := `tier("base",p*1.25+c*5)`
	for _, name := range []string{"gpt-4o-transcribe", "gpt-4o-transcribe-diarize", "gpt-4o-mini-transcribe", "gpt-4o-mini-transcribe-2025-03-20", "gpt-4o-mini-transcribe-2025-12-15", "gpt-transcribe"} {
		t.Run(name, func(t *testing.T) {
			profile := configuredPricingOperation(channel, name, name, constant.EndpointTypeOpenAI)
			for _, item := range []Pricing{
				{BillingMode: billing_setting.BillingModeTieredExpr, ConfiguredPrices: map[string][][]billingexpr.DisplayPrice{expression: billingexpr.CatalogPrices(expression)}},
				{PriceConfigured: true, ModelRatio: .625, CompletionRatio: 4},
			} {
				selected := SelectCatalogOffer(configuredOperationOffers(item, profile, "sync", expression))
				require.NotNil(t, selected)
				assert.Equal(t, "transcription", selected.Operation)
				assert.Equal(t, "input", selected.MainComponent)
				require.Len(t, selected.Prices, 2)
				prices := map[string]CatalogPrice{}
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
			selected := SelectCatalogOffer(configuredOperationOffers(Pricing{BillingMode: billing_setting.BillingModeTieredExpr, ConfiguredPrices: map[string][][]billingexpr.DisplayPrice{inputOnly: billingexpr.CatalogPrices(inputOnly)}}, profile, "sync", inputOnly))
			require.NotNil(t, selected)
			require.Len(t, selected.Prices, 1)
			assert.Equal(t, "million_tokens", selected.Prices[0].Unit)
			assert.Equal(t, 4.5, *selected.Prices[0].AmountUSD)
			fixed := SelectCatalogOffer(configuredOperationOffers(Pricing{PriceConfigured: true, QuotaType: 1, ModelPrice: .02}, profile, "sync", ""))
			require.NotNil(t, fixed)
			assert.Equal(t, "call", fixed.Prices[0].Unit)
		})
	}
}

func TestPricingOmitsUnchargedUnknownAudioComponents(t *testing.T) {
	previousCount := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = previousCount })
	channel := &Channel{Type: constant.ChannelTypeOpenAI}
	speech := configuredPricingOperation(channel, "gpt-4o-mini-tts", "gpt-4o-mini-tts", constant.EndpointTypeOpenAI)
	zero, one := 0.0, 1.0
	for _, scenario := range []struct {
		name          string
		expression    string
		pricing       Pricing
		unknownOutput bool
	}{
		{name: "input_only_formula", expression: `tier("base",p*.6)`},
		{name: "zero_completion_formula", expression: `tier("base",p*.6+c*0)`},
		{name: "zero_audio_formula", expression: `tier("base",p*.6+ao*0)`},
		{name: "paid_completion_formula", expression: `tier("base",p*.6+c*12+ao*0)`, unknownOutput: true},
		{name: "paid_audio_formula", expression: `tier("base",p*.6+c*0+ao*12)`, unknownOutput: true},
		{name: "zero_output_ratios", pricing: Pricing{PriceConfigured: true, ModelRatio: .3, AudioRatio: &one, AudioCompletionRatio: &zero}},
		{name: "zero_audio_input_ratio", pricing: Pricing{PriceConfigured: true, ModelRatio: .3, AudioRatio: &zero, AudioCompletionRatio: &one}},
		{name: "paid_stream_completion_ratio", pricing: Pricing{PriceConfigured: true, ModelRatio: .3, CompletionRatio: 20, AudioRatio: &one, AudioCompletionRatio: &zero}, unknownOutput: true},
		{name: "paid_nonstream_audio_ratio", pricing: Pricing{PriceConfigured: true, ModelRatio: .3, AudioRatio: &one, AudioCompletionRatio: &one}, unknownOutput: true},
		{name: "free_model_ratio", pricing: Pricing{PriceConfigured: true, AudioRatio: &one, AudioCompletionRatio: &one}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			item := scenario.pricing
			if scenario.expression != "" {
				item.BillingMode = billing_setting.BillingModeTieredExpr
				item.ConfiguredPrices = map[string][][]billingexpr.DisplayPrice{scenario.expression: billingexpr.CatalogPrices(scenario.expression)}
			}
			selected := SelectCatalogOffer(configuredOperationOffers(item, speech, "sync", scenario.expression))
			require.NotNil(t, selected)
			expectedCount := 1
			if scenario.unknownOutput {
				expectedCount++
			}
			require.Len(t, selected.Prices, expectedCount)
			require.NotNil(t, selected.Prices[0].AmountUSD)
			assert.Equal(t, "million_tokens", selected.Prices[0].Unit)
			if scenario.unknownOutput {
				assert.Equal(t, CatalogPrice{Component: "audio_output", Unit: "unknown"}, selected.Prices[1])
			}
		})
	}
	transcription := configuredPricingOperation(channel, "gpt-4o-mini-transcribe", "gpt-4o-mini-transcribe", constant.EndpointTypeOpenAI)
	expression := `tier("base",p*0+c*5)`
	for _, item := range []Pricing{
		{BillingMode: billing_setting.BillingModeTieredExpr, ConfiguredPrices: map[string][][]billingexpr.DisplayPrice{expression: billingexpr.CatalogPrices(expression)}},
		{PriceConfigured: true, CompletionRatio: 4},
	} {
		selected := SelectCatalogOffer(configuredOperationOffers(item, transcription, "sync", expression))
		require.NotNil(t, selected)
		require.Len(t, selected.Prices, 2)
		prices := map[string]CatalogPrice{}
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
			item := Pricing{BillingMode: billing_setting.BillingModeTieredExpr}
			offers := configuredOperationOffers(item, pricingOperation{"speech_generation", scenario.component, scenario.basis}, "sync", scenario.expression, true)
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
	unknown := configuredOperationOffers(Pricing{BillingMode: billing_setting.BillingModeTieredExpr}, pricingOperation{"text_to_video", "video_output", "unknown"}, "sync", `tier("private",u("seconds")*.4)`, true)
	require.Len(t, unknown, 1)
	assert.Empty(t, unknown[0].Prices)
	assert.True(t, unknown[0].PublicConditional)
}

func TestPublicNativeLegacySnapshotIsUnknownUntilMetadataRefresh(t *testing.T) {
	amount := 1.
	native := CatalogOffer{ID: "989:provider/model:native", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []CatalogPrice{{Component: "output", Unit: "million_tokens", AmountUSD: &amount}}}
	channels := map[int]*Channel{989: {Id: 989, Type: constant.ChannelTypeOpenRouter, Status: 1}}
	abilities := []AbilityWithChannel{{Ability: Ability{ChannelId: 989, Model: "provider/model", Group: "default", Enabled: true}, ChannelType: constant.ChannelTypeOpenRouter}}
	for _, fresh := range []bool{false, true} {
		stored := native
		if fresh {
			stored.PublicAlternatives = []CatalogOffer{native}
		}
		item := Pricing{ModelName: "provider/model", EnableGroup: []string{"default"}, CatalogOffersByGroup: map[string][]CatalogOffer{"default": {stored}}, BillingSourcesByGroup: map[string]map[constant.EndpointType][]string{"default": {constant.EndpointTypeOpenAI: {BillingSourceOpenRouterActualCost}}}}
		projectPricingOperations(&item, abilities, channels, jsplugin.NewRegistry().Generation(), nil)
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
