package domain

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"reflect"
	"slices"
	"strings"
)

func (rules ModelUpdateRules) ApplyModelDelta(channel *Channel, settings *dto.ChannelOtherSettings, upstreamModels, addModelsInput, ignoreModelsInput, removeModelsInput []string, applyAll bool) (result AppliedModelUpdates, err error) {

	modelMapping, mappingErr := rules.NormalizeChannelModelMapping(channel)
	if mappingErr != nil {
		return result, mappingErr
	}
	pendingAddModels, pendingRemoveModels, collectErr := rules.CollectPendingUpstreamModelChangesFromModels(
		channel.GetModels(),
		upstreamModels,
		settings.UpstreamModelUpdateIgnoredModels,
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

	settings.UpstreamModelUpdateIgnoredModels = MergeModelNames(settings.UpstreamModelUpdateIgnoredModels, result.IgnoredModels)
	if len(result.AddedModels) > 0 {
		settings.UpstreamModelUpdateIgnoredModels = SubtractModelNames(settings.UpstreamModelUpdateIgnoredModels, result.AddedModels)
	}
	result.RemainingModels = SubtractModelNames(pendingAddModels, append(result.AddedModels, result.IgnoredModels...))
	result.RemainingRemoveModels = SubtractModelNames(pendingRemoveModels, result.RemovedModels)
	settings.UpstreamModelUpdateLastDetectedModels = result.RemainingModels
	settings.UpstreamModelUpdateLastRemovedModels = result.RemainingRemoveModels
	settings.UpstreamModelUpdateLastCheckTime = rules.Now()

	return result, nil
}
