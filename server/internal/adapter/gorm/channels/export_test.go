package channels

import (
	common "github.com/QuantumNous/new-api/common"
	constant "github.com/QuantumNous/new-api/constant"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	"github.com/QuantumNous/new-api/logger"
	legacy "github.com/QuantumNous/new-api/model"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
)

var TestOpenChannelUpdateDB = openChannelUpdateTestDB

func ResolveCanonicalModelName(
	requestedName string,
	aliases map[string]string,
) (string, error) {
	return channelModelIdentity().ResolveCanonicalModelName(requestedName, aliases)
}

func NormalizeChannelModel(
	channelType int,
	upstreamModel string,
) (canonicalModel string, upstreamMapping string, err error) {
	return channelModelIdentity().NormalizeChannelModel(channelType, upstreamModel)
}

func ResolveChannelModelTarget(
	modelName string,
	modelMapping map[string]string,
) (string, error) {
	return channelModelIdentity().ResolveChannelModelTarget(modelName, modelMapping)
}

func CanonicalizeChannelModels(
	channelType int,
	models []string,
	existingMapping map[string]string,
) ([]string, map[string]string, error) {
	return channelModelIdentity().CanonicalizeChannelModels(channelType, models, existingMapping)
}

func cacheUpdateChannelForTest(channel *legacy.Channel) {
	if !common.MemoryCacheEnabled {
		return
	}
	identity, err := pricingstate.PricingChannelInputIdentity(channel)
	if err != nil {
		return
	}
	channelSyncLock.Lock()
	pricingChanged := channelPricingIdentities[channel.Id] != identity
	if channelPricingIdentities == nil {
		channelPricingIdentities = make(map[int]string)
	}
	channelPricingIdentities[channel.Id] = identity

	if channel == nil {
		channelSyncLock.Unlock()
		return
	}

	if channelsIDM == nil {
		channelsIDM = make(map[int]*legacy.Channel)
	}
	if oldChannel, ok := channelsIDM[channel.Id]; ok {
		logger.LogDebug(
			nil,
			"CacheUpdateChannel before: id=%d, name=%s, status=%d, polling_index=%d",
			channel.Id,
			channel.Name,
			channel.Status,
			oldChannel.ChannelInfo.MultiKeyPollingIndex,
		)
	}
	channelsIDM[channel.Id] = channel
	if channel2advancedCustomConfig == nil {
		channel2advancedCustomConfig = make(map[int]*kitdto.AdvancedCustomConfig)
	}
	delete(channel2advancedCustomConfig, channel.Id)
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		if config := NewChannelPersistence().GetOtherSettings(channel).AdvancedCustom; config != nil {
			channel2advancedCustomConfig[channel.Id] = config
		}
	}
	logger.LogDebug(
		nil,
		"CacheUpdateChannel after: id=%d, name=%s, status=%d, polling_index=%d",
		channel.Id,
		channel.Name,
		channel.Status,
		channel.ChannelInfo.MultiKeyPollingIndex,
	)

	channelSyncLock.Unlock()
	if pricingChanged {
		pricingstate.InvalidatePricingCache()
	}
}
