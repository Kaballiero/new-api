package domain

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"reflect"
	"slices"
	"strings"
)

func (rules ModelUpdateRules) DetectModelDelta(channel *Channel, settings *dto.ChannelOtherSettings, upstreamModels []string, allowAutoApply bool) (configChanged bool, autoAdded int, err error) {

	modelMapping, mappingErr := rules.NormalizeChannelModelMapping(channel)
	if mappingErr != nil {
		return false, 0, mappingErr
	}
	pendingAddModels, pendingRemoveModels, collectErr := rules.CollectPendingUpstreamModelChangesFromModels(
		channel.GetModels(),
		upstreamModels,
		settings.UpstreamModelUpdateIgnoredModels,
		modelMapping,
	)
	if collectErr != nil {
		return false, 0, collectErr
	}

	if allowAutoApply && settings.UpstreamModelUpdateAutoSyncEnabled && len(pendingAddModels) > 0 {
		if conflictErr := rules.RejectSameNameModelMappingReplacement(pendingAddModels, pendingRemoveModels); conflictErr != nil {
			return false, 0, conflictErr
		}
		originModels := NormalizeModelNames(channel.GetModels())
		channel.Models = strings.Join(MergeModelNames(originModels, pendingAddModels), ",")
		if canonicalizeErr := rules.Canonicalize(channel); canonicalizeErr != nil {
			return false, 0, canonicalizeErr
		}
		nextModels := NormalizeModelNames(channel.GetModels())
		modelsChanged := !slices.Equal(originModels, nextModels)
		nextModelMapping, mappingErr := rules.NormalizeChannelModelMapping(channel)
		if mappingErr != nil {
			return false, 0, mappingErr
		}
		mappingChanged := !reflect.DeepEqual(modelMapping, nextModelMapping)
		configChanged = modelsChanged || mappingChanged
		if modelsChanged {
			autoAdded = len(nextModels) - len(originModels)
		}
		settings.UpstreamModelUpdateLastDetectedModels = []string{}
	} else {
		settings.UpstreamModelUpdateLastDetectedModels = pendingAddModels
	}
	settings.UpstreamModelUpdateLastRemovedModels = pendingRemoveModels
	return configChanged, autoAdded, nil
}
