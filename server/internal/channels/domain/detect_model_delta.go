package domain

import (
	"reflect"
	"slices"
	"strings"
)

func (rules ModelUpdateRules) DetectModelDelta(channel *Channel, state *ModelUpdateState, upstreamModels []string, allowAutoApply bool) (configChanged bool, autoAdded int, err error) {

	modelMapping, mappingErr := rules.NormalizeChannelModelMapping(channel)
	if mappingErr != nil {
		return false, 0, mappingErr
	}
	pendingAddModels, pendingRemoveModels, collectErr := rules.CollectPendingUpstreamModelChangesFromModels(
		channel.GetModels(),
		upstreamModels,
		state.IgnoredModels,
		modelMapping,
	)
	if collectErr != nil {
		return false, 0, collectErr
	}

	if allowAutoApply && state.AutoSyncEnabled && len(pendingAddModels) > 0 {
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
		state.LastDetectedModels = []string{}
	} else {
		state.LastDetectedModels = pendingAddModels
	}
	state.LastRemovedModels = pendingRemoveModels
	return configChanged, autoAdded, nil
}
