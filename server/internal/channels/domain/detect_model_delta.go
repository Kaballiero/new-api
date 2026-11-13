package domain

import (
	"reflect"
	"slices"
	"strings"
)

func (rules ModelUpdateRules) DetectModelDelta(channel *Channel, state *ModelUpdateState, upstreamModels UpstreamModels, allowAutoApply bool) (configChanged bool, autoAdded int, err error) {

	modelMapping, mappingErr := rules.NormalizeChannelModelMapping(channel)
	if mappingErr != nil {
		return false, 0, mappingErr
	}
	pendingAddModels, pendingRemoveModels, conflictModels, collectErr := rules.CollectPendingUpstreamModelChangesFromModels(
		channel.Type,
		channel.GetModels(),
		upstreamModels,
		state.IgnoredModels,
		modelMapping,
	)
	if collectErr != nil {
		return false, 0, collectErr
	}

	// A scheduled sync only adds models, so an addition that replaces a
	// pending removal under its name stays pending for a manual apply.
	autoAddModels := []string{}
	if allowAutoApply && state.AutoSyncEnabled {
		replacements, replacementErr := rules.SameNameReplacements(channel.Type, pendingAddModels, pendingRemoveModels, modelMapping)
		if replacementErr != nil {
			return false, 0, replacementErr
		}
		autoAddModels = SubtractModelNames(pendingAddModels, replacements)
	}
	if len(autoAddModels) > 0 {
		originModels := NormalizeModelNames(channel.GetModels())
		channel.Models = strings.Join(MergeModelNames(originModels, autoAddModels), ",")
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
		state.LastDetectedModels = SubtractModelNames(pendingAddModels, autoAddModels)
	} else {
		state.LastDetectedModels = pendingAddModels
	}
	state.LastRemovedModels = pendingRemoveModels
	state.LastConflictModels = conflictModels
	return configChanged, autoAdded, nil
}
