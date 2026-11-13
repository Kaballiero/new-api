package domain

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

func (rules ModelUpdateRules) ApplyModelDelta(channel *Channel, state *ModelUpdateState, upstreamModels UpstreamModels, addModelsInput, ignoreModelsInput, removeModelsInput []string, applyAll bool) (result AppliedModelUpdates, err error) {

	modelMapping, mappingErr := rules.NormalizeChannelModelMapping(channel)
	if mappingErr != nil {
		return result, mappingErr
	}
	pendingAddModels, pendingRemoveModels, conflictModels, collectErr := rules.CollectPendingUpstreamModelChangesFromModels(
		channel.Type,
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
		releasedMapping := releaseRemovedModelMappings(modelMapping, result.RemovedModels, nextModels)
		if len(releasedMapping) != len(modelMapping) {
			mappingJSON, encodeErr := rules.Codec.EncodeModelMapping(releasedMapping)
			if encodeErr != nil {
				return result, fmt.Errorf("marshal model mapping: %w", encodeErr)
			}
			channel.ModelMapping = &mappingJSON
		}
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
	state.LastConflictModels = conflictModels
	state.LastCheckTime = rules.Now()

	return result, nil
}

// releaseRemovedModelMappings drops the mapping entries of each removed
// model's chain, so their names no longer hold a target: a mapping entry that
// no channel model needs is not kept. The chain is kept from the first entry
// a kept model resolves through.
func releaseRemovedModelMappings(modelMapping map[string]string, removedModels, keptModels []string) map[string]string {
	routedModels := make(map[string]struct{}, len(keptModels))
	for _, modelName := range keptModels {
		currentModel := modelName
		for {
			if _, routed := routedModels[currentModel]; routed {
				break
			}
			routedModels[currentModel] = struct{}{}
			mappedModel, mapped := modelMapping[currentModel]
			if !mapped {
				break
			}
			currentModel = mappedModel
		}
	}

	// A deleted entry ends the walk when the chain comes back to it, so a
	// cycle cannot loop.
	releasedMapping := maps.Clone(modelMapping)
	for _, modelName := range removedModels {
		currentModel := modelName
		for {
			if _, routed := routedModels[currentModel]; routed {
				break
			}
			mappedModel, mapped := releasedMapping[currentModel]
			if !mapped {
				break
			}
			delete(releasedMapping, currentModel)
			currentModel = mappedModel
		}
	}
	return releasedMapping
}
