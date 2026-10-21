package domain

import (
	"reflect"
	"slices"
	"strings"
)

func (rules ModelUpdateRules) ApplyModelDelta(channel *Channel, state *ModelUpdateState, upstreamModels, addModelsInput, ignoreModelsInput, removeModelsInput []string, applyAll bool) (result AppliedModelUpdates, err error) {

	modelMapping, mappingErr := rules.NormalizeChannelModelMapping(channel)
	if mappingErr != nil {
		return result, mappingErr
	}
	pendingAddModels, pendingRemoveModels, collectErr := rules.CollectPendingUpstreamModelChangesFromModels(
		channel.GetModels(),
		upstreamModels,
		state.IgnoredModels,
		modelMapping,
	)
	if collectErr != nil {
		return result, collectErr
	}
	if conflictErr := rules.RejectSameNameModelMappingReplacement(pendingAddModels, pendingRemoveModels); conflictErr != nil {
		return result, conflictErr
	}
	result.HadPending = len(pendingAddModels) > 0 || len(pendingRemoveModels) > 0
	if applyAll {
		result.AddedModels = pendingAddModels
		result.RemovedModels = pendingRemoveModels
	} else {
		result.AddedModels = IntersectModelNames(addModelsInput, pendingAddModels)
		result.IgnoredModels = IntersectModelNames(ignoreModelsInput, pendingAddModels)
		result.RemovedModels = IntersectModelNames(removeModelsInput, pendingRemoveModels)
	}
	result.RemovedModels = SubtractModelNames(result.RemovedModels, result.AddedModels)

	originModels := NormalizeModelNames(channel.GetModels())
	nextModels := ApplySelectedModelChanges(originModels, result.AddedModels, result.RemovedModels)
	if !slices.Equal(originModels, nextModels) {
		channel.Models = strings.Join(nextModels, ",")
		if canonicalizeErr := rules.Canonicalize(channel); canonicalizeErr != nil {
			return result, canonicalizeErr
		}
		nextModels = NormalizeModelNames(channel.GetModels())
	}
	result.ModelsChanged = !slices.Equal(originModels, nextModels)
	nextModelMapping, mappingErr := rules.NormalizeChannelModelMapping(channel)
	if mappingErr != nil {
		return result, mappingErr
	}
	result.MappingChanged = !reflect.DeepEqual(modelMapping, nextModelMapping)

	state.IgnoredModels = MergeModelNames(state.IgnoredModels, result.IgnoredModels)
	if len(result.AddedModels) > 0 {
		state.IgnoredModels = SubtractModelNames(state.IgnoredModels, result.AddedModels)
	}
	result.RemainingModels = SubtractModelNames(pendingAddModels, append(result.AddedModels, result.IgnoredModels...))
	result.RemainingRemoveModels = SubtractModelNames(pendingRemoveModels, result.RemovedModels)
	state.LastDetectedModels = result.RemainingModels
	state.LastRemovedModels = result.RemainingRemoveModels
	state.LastCheckTime = rules.Now()

	return result, nil
}
