package domain

// DetectModelDelta recomputes the pending upstream model changes of a channel.
// A scheduled run (allowAutoApply) of a channel with auto sync applies the
// pending additions, unless its upstream models are AutoApplyBlocked; with auto
// removal it also removes the removal candidates, at most MaxAutoRemovedModels
// of them, and none when the upstream lists no models. An addition that replaces a pending removal under its name and that
// removal stay pending for a manual apply, and so does a removal candidate
// that matches an ignored model by its name or its target.
func (rules ModelUpdateRules) DetectModelDelta(channel *Channel, state *ModelUpdateState, upstreamModels UpstreamModels, allowAutoApply bool) (result AppliedModelUpdates, err error) {
	modelMapping, err := rules.NormalizeChannelModelMapping(channel)
	if err != nil {
		return result, err
	}
	pendingAddModels, pendingRemoveModels, conflictModels, err := rules.CollectPendingUpstreamModelChangesFromModels(
		channel.Type,
		channel.GetModels(),
		upstreamModels,
		state.IgnoredModels,
		modelMapping,
	)
	if err != nil {
		return result, err
	}

	if allowAutoApply && state.AutoSyncEnabled && !upstreamModels.AutoApplyBlocked {
		replacements, replacedRemovals, replacementErr := rules.SameNameReplacements(channel.Type, pendingAddModels, pendingRemoveModels, modelMapping)
		if replacementErr != nil {
			return result, replacementErr
		}
		result.AddedModels = SubtractModelNames(pendingAddModels, replacements)
		if state.AutoRemoveEnabled && len(upstreamModels.Listed) > 0 {
			removalCandidates := []string{}
			for _, modelName := range SubtractModelNames(pendingRemoveModels, replacedRemovals) {
				target, resolveErr := rules.ResolveExactChannelModelTarget(modelName, modelMapping)
				if resolveErr != nil {
					return result, resolveErr
				}
				if matchesIgnoredModel(state.IgnoredModels, modelName) || matchesIgnoredModel(state.IgnoredModels, target) {
					continue
				}
				removalCandidates = append(removalCandidates, modelName)
			}
			result.RemovalLimitExceeded = len(removalCandidates) > MaxAutoRemovedModels
			if !result.RemovalLimitExceeded {
				result.RemovedModels = removalCandidates
			}
		}
	}

	result.ModelsChanged, result.MappingChanged, err = rules.applyModelChanges(channel, modelMapping, result.AddedModels, result.RemovedModels)
	if err != nil {
		return result, err
	}
	state.LastDetectedModels = SubtractModelNames(pendingAddModels, result.AddedModels)
	state.LastRemovedModels = SubtractModelNames(pendingRemoveModels, result.RemovedModels)
	state.LastConflictModels = conflictModels
	return result, nil
}
