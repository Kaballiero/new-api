package domain

// DetectModelDelta recomputes the pending upstream model changes of a channel.
// A scheduled run (allowAutoApply) of a channel with auto sync applies the
// pending additions, unless its upstream models are AutoApplyBlocked, and the
// auto removals (autoRemovals). An addition that replaces a pending removal
// under its name stays pending for a manual apply together with that removal.
// Every check marks a channel over the auto removal limit, so a manual check
// reports it as a scheduled run does.
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
	replacements, replacedRemovals, err := rules.SameNameReplacements(channel.Type, pendingAddModels, pendingRemoveModels, modelMapping)
	if err != nil {
		return result, err
	}
	removals, limitCandidates, err := rules.autoRemovals(*state, upstreamModels, pendingRemoveModels, replacedRemovals, modelMapping)
	if err != nil {
		return result, err
	}

	result.RemovalLimitCandidates = limitCandidates
	if allowAutoApply && state.AutoSyncEnabled && !upstreamModels.AutoApplyBlocked {
		result.AddedModels = SubtractModelNames(pendingAddModels, replacements)
		result.RemovedModels = removals
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

// RemovalLimitCandidates are the auto removals over the limit of the pending
// removals the last check of a channel stored, under upstreamModels, as
// DetectModelDelta marks them.
func (rules ModelUpdateRules) RemovalLimitCandidates(channel *Channel, state ModelUpdateState, upstreamModels UpstreamModels) ([]string, error) {
	modelMapping, err := rules.NormalizeChannelModelMapping(channel)
	if err != nil {
		return nil, err
	}
	pendingAddModels, pendingRemoveModels := CollectPendingApplyUpstreamModelChanges(state)
	_, replacedRemovals, err := rules.SameNameReplacements(channel.Type, pendingAddModels, pendingRemoveModels, modelMapping)
	if err != nil {
		return nil, err
	}
	_, limitCandidates, err := rules.autoRemovals(state, upstreamModels, pendingRemoveModels, replacedRemovals, modelMapping)
	return limitCandidates, err
}

// autoRemovals are the pending removals a scheduled run removes from a channel
// with auto sync, auto removal and catalog sync, unless its upstream models are
// AutoApplyBlocked or list no model: all of them except the removals a
// same-name replacement takes and the models that match an ignored model by
// their name or their target. With more than MaxAutoRemovedModels of them
// none is removed: they are returned as limitCandidates.
func (rules ModelUpdateRules) autoRemovals(state ModelUpdateState, upstreamModels UpstreamModels, pendingRemoveModels, replacedRemovals []string, modelMapping map[string]string) (removals, limitCandidates []string, err error) {
	autoRemoveEnabled := state.AutoSyncEnabled && state.AutoRemoveEnabled && state.CatalogSyncEnabled
	if !autoRemoveEnabled || upstreamModels.AutoApplyBlocked || len(upstreamModels.Listed) == 0 {
		return nil, nil, nil
	}
	for _, modelName := range SubtractModelNames(pendingRemoveModels, replacedRemovals) {
		target, err := rules.ResolveExactChannelModelTarget(modelName, modelMapping)
		if err != nil {
			return nil, nil, err
		}
		if matchesIgnoredModel(state.IgnoredModels, modelName) || matchesIgnoredModel(state.IgnoredModels, target) {
			continue
		}
		removals = append(removals, modelName)
	}
	if len(removals) > MaxAutoRemovedModels {
		return nil, removals, nil
	}
	return removals, nil, nil
}
