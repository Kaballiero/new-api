package channels

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pricingEndpointAdvancedCustomConfig(routes ...dto.AdvancedCustomRoute) dto.ChannelOtherSettings {
	return dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{
			Routes: routes,
		},
	}
}

func TestCacheUpdateChannelSyncsAdvancedCustomConfig(t *testing.T) {
	openChannelUpdateTestDB(t)
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory })
	InitChannelCache()

	channel := &legacy.Channel{
		Id:     401,
		Type:   constant.ChannelTypeAdvancedCustom,
		Key:    "key-401",
		Status: common.ChannelStatusEnabled,
		Name:   "channel-401",
	}
	channel.SetOtherSettings(pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{
		IncomingPath: "/v1/responses",
		UpstreamPath: "/v1beta/models/{model}:generateContent",
		Converter:    "openai_responses_to_gemini_generate_content",
	}))
	cacheUpdateChannelForTest(channel)

	require.NotNil(t, channel2advancedCustomConfig[401])
	assert.Equal(
		t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIResponse},
		channel2advancedCustomConfig[401].SupportedEndpointTypesForModel("gemini-3.5-flash"),
	)

	channel.SetOtherSettings(pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{
		IncomingPath: "/v1/chat/completions",
		UpstreamPath: "/v1/chat/completions",
	}))
	cacheUpdateChannelForTest(channel)

	require.NotNil(t, channel2advancedCustomConfig[401])
	assert.Equal(
		t,
		[]constant.EndpointType{constant.EndpointTypeOpenAI},
		channel2advancedCustomConfig[401].SupportedEndpointTypesForModel("gemini-3.5-flash"),
	)

	channel.Type = constant.ChannelTypeOpenAI
	cacheUpdateChannelForTest(channel)

	assert.Nil(t, channel2advancedCustomConfig[401])
}
