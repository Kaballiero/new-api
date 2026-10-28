package pricingstate_test

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	catalogrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	pricingruntime "github.com/QuantumNous/new-api/internal/app/pricingruntime"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetPricingEndpointTestTables(t *testing.T) {
	t.Helper()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	require.NoError(t, legacy.DB.AutoMigrate(&legacy.Channel{}, &channelrecord.Ability{}, &legacy.Model{}, &legacy.Vendor{}))
	for _, table := range []string{"abilities", "channels", "models", "vendors"} {
		require.NoError(t, legacy.DB.Exec("DELETE FROM "+table).Error)
	}
	channelstore.InitChannelCache()
	pricingstate.InvalidatePricingCache()
	t.Cleanup(func() {
		for _, table := range []string{"abilities", "channels", "models", "vendors"} {
			require.NoError(t, legacy.DB.Exec("DELETE FROM "+table).Error)
		}
		channelstore.InitChannelCache()
		pricingstate.InvalidatePricingCache()
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})
}

func insertPricingEndpointChannel(t *testing.T, channelID int, channelType int, settings dto.ChannelOtherSettings) {
	t.Helper()
	channel := &legacy.Channel{
		Id:     channelID,
		Type:   channelType,
		Key:    fmt.Sprintf("key-%d", channelID),
		Status: common.ChannelStatusEnabled,
		Name:   fmt.Sprintf("channel-%d", channelID),
	}
	if settings.AdvancedCustom != nil {
		channel.SetOtherSettings(settings)
	}
	require.NoError(t, legacy.DB.Create(channel).Error)
}

func insertPricingEndpointAbility(t *testing.T, channelID int, modelName string) {
	t.Helper()
	require.NoError(t, legacy.DB.Create(&channelrecord.Ability{
		Group:     "default",
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   true,
	}).Error)
}

func pricingEndpointAdvancedCustomConfig(routes ...dto.AdvancedCustomRoute) dto.ChannelOtherSettings {
	return dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{
			Routes: routes,
		},
	}
}

func pricingEndpointTypesByModel(t *testing.T) map[string][]constant.EndpointType {
	t.Helper()
	channelstore.InitChannelCache()
	return pricingEndpointTypesFromPricing(pricingruntime.GetPricing())
}

func pricingEndpointTypesFromPricing(pricings []pricingdomain.Pricing) map[string][]constant.EndpointType {
	byModel := make(map[string][]constant.EndpointType)
	for _, pricing := range pricings {
		byModel[pricing.ModelName] = pricing.SupportedEndpointTypes
	}
	return byModel
}

func TestPricingAdvancedCustomUsesConfiguredEndpointTypes(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 101, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
		},
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1beta/models/{model}:generateContent",
			Converter:    "openai_responses_to_gemini_generate_content",
			Models:       []string{"re:^gemini-"},
		},
	))
	insertPricingEndpointAbility(t, 101, "gemini-2.5-flash")
	insertPricingEndpointAbility(t, 101, "gpt-4o")

	byModel := pricingEndpointTypesByModel(t)

	assert.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
	}, byModel["gemini-2.5-flash"])
	assert.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
	}, byModel["gpt-4o"])
}

func TestPricingModelMetadataEndpointsMergeWithAdvancedCustomInference(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 103, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1beta/models/{model}:generateContent",
			Converter:    "openai_responses_to_gemini_generate_content",
			Models:       []string{"re:^gemini-"},
		},
	))
	insertPricingEndpointAbility(t, 103, "gemini-2.5-flash")
	require.NoError(t, legacy.DB.Create(&legacy.Model{
		ModelName: "gemini-2.5-flash",
		Endpoints: `{
			"openai": "/v1/chat/completions"
		}`,
		Status:   1,
		NameRule: catalogrecord.NameRuleExact,
	}).Error)

	byModel := pricingEndpointTypesByModel(t)

	assert.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAI,
	}, byModel["gemini-2.5-flash"])
}

func TestPricingModelMetadataEndpointsCanProvideEndpointWithoutChannelInference(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 104, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1beta/models/{model}:generateContent",
			Converter:    "openai_responses_to_gemini_generate_content",
			Models:       []string{"re:^gemini-"},
		},
	))
	insertPricingEndpointAbility(t, 104, "metadata-only-model")
	require.NoError(t, legacy.DB.Create(&legacy.Model{
		ModelName: "metadata-only-model",
		Endpoints: `{
			"openai": "/v1/chat/completions"
		}`,
		Status:   1,
		NameRule: catalogrecord.NameRuleExact,
	}).Error)

	byModel := pricingEndpointTypesByModel(t)

	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, byModel["metadata-only-model"])
}

func TestPricingAdvancedCustomMissingConfigFallsBackToChannelType(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 102, constant.ChannelTypeAdvancedCustom, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 102, "gpt-4o")

	byModel := pricingEndpointTypesByModel(t)

	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, byModel["gpt-4o"])
}

func TestPricingNativeChannelEndpointTypesUnchanged(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 201, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointChannel(t, 202, constant.ChannelTypeGemini, dto.ChannelOtherSettings{})
	insertPricingEndpointChannel(t, 203, constant.ChannelTypeAnthropic, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 201, "gpt-4o")
	insertPricingEndpointAbility(t, 202, "gemini-2.5-flash")
	insertPricingEndpointAbility(t, 203, "claude-3-5-sonnet")

	byModel := pricingEndpointTypesByModel(t)

	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, byModel["gpt-4o"])
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}, byModel["gemini-2.5-flash"])
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAI}, byModel["claude-3-5-sonnet"])
}

func TestInitChannelCacheInvalidatesPricingCache(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 301, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
		},
	))
	insertPricingEndpointAbility(t, 301, "gemini-3.5-flash")
	channelstore.InitChannelCache()

	initial := pricingEndpointTypesByModel(t)
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, initial["gemini-3.5-flash"])

	var channel legacy.Channel
	require.NoError(t, legacy.DB.First(&channel, "id = ?", 301).Error)
	channel.SetOtherSettings(pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
		},
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1beta/models/{model}:generateContent",
			Converter:    "openai_responses_to_gemini_generate_content",
			Models:       []string{"re:^gemini-"},
		},
	))
	require.NoError(t, legacy.DB.Model(&legacy.Channel{}).Where("id = ?", 301).Update("settings", channel.OtherSettings).Error)
	channelstore.InitChannelCache()

	updated := pricingEndpointTypesByModel(t)
	assert.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
	}, updated["gemini-3.5-flash"])
}

func TestStartupPricingUsesChannelSourceBeforeChannelCacheInitialization(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 302, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
		},
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1beta/models/{model}:generateContent",
			Converter:    "openai_responses_to_gemini_generate_content",
			Models:       []string{"re:^gemini-"},
		},
	))
	insertPricingEndpointAbility(t, 302, "gemini-3.5-flash")

	staleByModel := pricingEndpointTypesFromPricing(pricingruntime.GetPricing())
	require.Equal(
		t,
		[]constant.EndpointType{
			constant.EndpointTypeOpenAI,
			constant.EndpointTypeOpenAIResponse,
		},
		staleByModel["gemini-3.5-flash"],
		"cold initialization reads the actual source instead of a cache-dependent fallback",
	)

	channelstore.InitChannelCache()

	rebuiltByModel := pricingEndpointTypesFromPricing(pricingruntime.GetPricing())
	assert.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
	}, rebuiltByModel["gemini-3.5-flash"])
}

func TestPricingBillingSourcesFollowChannelsAndExpandAllGroups(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingEndpointChannel(t, 991, constant.ChannelTypeOpenRouter, dto.ChannelOtherSettings{})
	insertPricingEndpointChannel(t, 992, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 991, "same-vendor-model")
	insertPricingEndpointAbility(t, 992, "same-vendor-model")
	setting := `{"task_plugin_key":"openrouter-video"}`
	require.NoError(
		t,
		legacy.DB.Create(&legacy.Channel{
			Id:      993,
			Type:    constant.ChannelTypeTaskPlugin,
			Key:     "video-key",
			Status:  common.ChannelStatusEnabled,
			Name:    "video",
			Setting: &setting,
		}).Error,
	)
	insertPricingEndpointAbility(t, 993, "same-vendor-video")
	for index, channelType := range []int{
		constant.ChannelTypeOpenAI,
		constant.ChannelTypeAnthropic,
		constant.ChannelTypeGemini,
		constant.ChannelTypeAzure,
	} {
		channelID := 994 + index
		modelName := fmt.Sprintf("batch-source-%d", channelType)
		insertPricingEndpointChannel(t, channelID, channelType, dto.ChannelOtherSettings{})
		insertPricingEndpointAbility(t, channelID, modelName)
	}

	require.NoError(t, legacy.DB.Create(&channelrecord.Ability{Group: "all", Model: "only-openrouter", ChannelId: 991, Enabled: true}).Error)
	channelstore.InitChannelCache()
	pricing := pricingByModel(pricingruntime.GetPricing())
	mixed := pricing["same-vendor-model"].BillingSourcesForGroup("default")
	assert.ElementsMatch(
		t,
		[]string{
			pricingdomain.BillingSourceConfiguredTariff,
			pricingdomain.BillingSourceOpenRouterActualCost,
		},
		mixed[constant.EndpointTypeOpenAI],
	)
	assert.ElementsMatch(
		t,
		[]string{
			pricingdomain.BillingSourceConfiguredTariff,
			pricingdomain.BillingSourceOpenRouterActualCost,
		},
		mixed[pricingdomain.PricingEndpointBatch],
	)
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeAnthropic, constant.ChannelTypeGemini} {
		assert.Equal(
			t,
			[]string{pricingdomain.BillingSourceConfiguredTariff},
			pricing[fmt.Sprintf(
				"batch-source-%d",
				channelType,
			)].BillingSourcesForGroup("default")[pricingdomain.PricingEndpointBatch],
		)
	}
	assert.NotContains(
		t,
		pricing[fmt.Sprintf(
			"batch-source-%d",
			constant.ChannelTypeAzure,
		)].BillingSourcesForGroup("default"),
		pricingdomain.PricingEndpointBatch,
	)
	assert.Equal(t, []string{pricingdomain.BillingSourceOpenRouterActualCost}, mixed[constant.EndpointTypeOpenAIResponse])
	assert.Equal(t, []string{pricingdomain.BillingSourceOpenRouterActualCost}, mixed[constant.EndpointTypeEmbeddings])
	universal := pricing["only-openrouter"].BillingSourcesForGroup("premium")
	assert.Equal(t, []string{pricingdomain.BillingSourceOpenRouterActualCost}, universal[constant.EndpointTypeOpenAI])
	assert.Contains(t, universal, pricingdomain.PricingEndpointBatch)
	assert.Equal(
		t,
		[]string{pricingdomain.BillingSourceOpenRouterActualCost},
		pricing["same-vendor-video"].BillingSourcesForGroup("default")[constant.EndpointTypeOpenAIVideo],
	)
	assert.Contains(t, pricing["only-openrouter"].IndicativeBillingExprByEndpoint, pricingdomain.PricingEndpointBatch)
	assert.Empty(t, pricing["only-openrouter"].IndicativeBillingExprByEndpoint[pricingdomain.PricingEndpointBatch])
}
