package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/samber/lo"
)

type ModelMappingErrors struct{ ErrModelNameEmpty, ErrModelMappingCycle, ErrModelMappingSourceEmpty, ErrModelMappingTargetEmpty, ErrModelMappingConflict error }
type ModelUpdateRules struct {
	Errors       func() ModelMappingErrors
	Codec        ModelMappingCodec
	Canonicalize func(*Channel) error
	// CanonicalModel is the canonical channel name of an upstream model
	// of a channel type (ModelIdentity.NormalizeChannelModel).
	CanonicalModel func(channelType int, upstreamModel string) (string, error)
	Now            func() int64
}
type AppliedModelUpdates struct {
	AddedModels           []string
	RemovedModels         []string
	IgnoredModels         []string
	RemainingModels       []string
	RemainingRemoveModels []string
	ModelsChanged         bool
	MappingChanged        bool
	HadPending            bool
}

func NormalizeModelNames(models []string) []string {
	return lo.Uniq(lo.FilterMap(models, func(model string, _ int) (string, bool) {
		trimmed := strings.TrimSpace(model)
		return trimmed, trimmed != ""
	}))
}
func MergeModelNames(base []string, appended []string) []string {
	merged := NormalizeModelNames(base)
	seen := make(map[string]struct{}, len(merged))
	for _, model := range merged {
		seen[model] = struct{}{}
	}
	for _, model := range NormalizeModelNames(appended) {
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		merged = append(merged, model)
	}
	return merged
}
func SubtractModelNames(base []string, removed []string) []string {
	removeSet := make(map[string]struct{}, len(removed))
	for _, model := range NormalizeModelNames(removed) {
		removeSet[model] = struct{}{}
	}
	return lo.Filter(NormalizeModelNames(base), func(model string, _ int) bool {
		_, ok := removeSet[model]
		return !ok
	})
}
func IntersectModelNames(base []string, allowed []string) []string {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, model := range NormalizeModelNames(allowed) {
		allowedSet[model] = struct{}{}
	}
	return lo.Filter(NormalizeModelNames(base), func(model string, _ int) bool {
		_, ok := allowedSet[model]
		return ok
	})
}
func ApplySelectedModelChanges(originModels []string, addModels []string, removeModels []string) []string {
	// Add wins when the same model appears in both selected lists.
	normalizedAdd := NormalizeModelNames(addModels)
	normalizedRemove := SubtractModelNames(NormalizeModelNames(removeModels), normalizedAdd)
	return SubtractModelNames(MergeModelNames(originModels, normalizedAdd), normalizedRemove)
}

// ModelUpdateState is the upstream model update tracking of a channel. The
// gateway stores it among the other channel settings. CatalogSyncEnabled takes
// the upstream models from the upstream catalog instead of the provider model
// list. LastConflictModels are the upstream models that were not offered for
// addition because their canonical name is taken by another target.
type ModelUpdateState struct {
	CheckEnabled       bool
	AutoSyncEnabled    bool
	CatalogSyncEnabled bool
	LastCheckTime      int64
	LastDetectedModels []string
	LastRemovedModels  []string
	LastConflictModels []string
	IgnoredModels      []string
}

// UpstreamModels are the models of a channel's upstream. Listed are the models
// the upstream lists; Addable is the subset of Listed that may be added to the
// channel. Shortcuts are model id suffixes that route a listed model: a target
// with one of them is listed when the target without it is. A channel model
// whose target is not listed is a removal candidate, so an empty Listed means
// the upstream lists no models and every channel model is a removal candidate.
type UpstreamModels struct {
	Listed    []string
	Addable   []string
	Shortcuts []string
}

func CollectPendingApplyUpstreamModelChanges(state ModelUpdateState) (pendingAddModels []string, pendingRemoveModels []string) {
	return NormalizeModelNames(state.LastDetectedModels), NormalizeModelNames(state.LastRemovedModels)
}
func (rules ModelUpdateRules) NormalizeChannelModelMapping(channel *Channel) (map[string]string, error) {
	if channel == nil || channel.ModelMapping == nil {
		return nil, nil
	}
	rawMapping := *channel.ModelMapping
	trimmedMapping := strings.TrimSpace(rawMapping)
	if trimmedMapping == "" || trimmedMapping == "{}" {
		return nil, nil
	}
	parsed, err := rules.Codec.DecodeModelMapping(rawMapping)
	if err != nil {
		return nil, fmt.Errorf("invalid model mapping: %w", err)
	}
	if err := rules.ValidateResolvableChannelModelMapping(parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}
func (rules ModelUpdateRules) ValidateResolvableChannelModelMapping(modelMapping map[string]string) error {
	if err := rules.ValidateChannelModelMappingEntries(modelMapping); err != nil {
		return err
	}
	for source := range modelMapping {
		if _, err := rules.ResolveExactChannelModelTarget(source, modelMapping); err != nil {
			return err
		}
	}
	return nil
}
func (rules ModelUpdateRules) ResolveExactChannelModelTarget(modelName string, modelMapping map[string]string) (string, error) {
	if strings.TrimSpace(modelName) == "" {
		return "", rules.Errors().ErrModelNameEmpty
	}

	currentModel := modelName
	visitedModels := map[string]bool{currentModel: true}
	for {
		mappedModel, exists := modelMapping[currentModel]
		if !exists {
			return currentModel, nil
		}
		if mappedModel == currentModel {
			return currentModel, nil
		}
		if visitedModels[mappedModel] {
			return "", rules.Errors().ErrModelMappingCycle
		}
		visitedModels[mappedModel] = true
		currentModel = mappedModel
	}
}
func (rules ModelUpdateRules) ValidateChannelModelMappingEntries(modelMapping map[string]string) error {
	for source, target := range modelMapping {
		if strings.TrimSpace(source) == "" {
			return rules.Errors().ErrModelMappingSourceEmpty
		}
		if strings.TrimSpace(target) == "" {
			return rules.Errors().ErrModelMappingTargetEmpty
		}
	}
	return nil
}

// CollectPendingUpstreamModelChangesFromModels finds the upstream models to add
// and the channel models to remove. An addable model whose canonical channel
// name is held by a mapping source or a kept channel model with another target
// is not offered for addition: it is returned among the conflicts. A name held
// only by a removal candidate is not a conflict, because removing that model
// frees the name.
func (rules ModelUpdateRules) CollectPendingUpstreamModelChangesFromModels(
	channelType int,
	localModels []string,
	upstreamModels UpstreamModels,
	ignoredModels []string,
	modelMapping map[string]string,
) (pendingAddModels []string, pendingRemoveModels []string, conflictModels []string, err error) {
	if err := rules.ValidateResolvableChannelModelMapping(modelMapping); err != nil {
		return nil, nil, nil, err
	}
	localModels = NormalizeModelNames(localModels)
	listedModels := NormalizeModelNames(upstreamModels.Listed)
	listedSet := make(map[string]struct{}, len(listedModels))
	for _, modelName := range listedModels {
		listedSet[modelName] = struct{}{}
	}

	normalizedIgnoredModels := NormalizeModelNames(ignoredModels)
	coveredUpstreamSet := make(map[string]struct{}, len(localModels))
	heldNames := make(map[string]string, len(modelMapping)+len(localModels))
	for source := range modelMapping {
		heldNames[source], err = rules.ResolveExactChannelModelTarget(source, modelMapping)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	for _, modelName := range localModels {
		terminalTarget, resolveErr := rules.ResolveExactChannelModelTarget(modelName, modelMapping)
		if resolveErr != nil {
			return nil, nil, nil, resolveErr
		}
		coveredUpstreamSet[terminalTarget] = struct{}{}
		_, listed := listedSet[terminalTarget]
		if !listed && !slices.ContainsFunc(upstreamModels.Shortcuts, func(shortcut string) bool {
			base, routed := strings.CutSuffix(terminalTarget, shortcut)
			_, baseListed := listedSet[base]
			return routed && baseListed
		}) {
			pendingRemoveModels = append(pendingRemoveModels, modelName)
			continue
		}
		canonicalModel, canonicalErr := rules.canonicalChannelModel(channelType, modelName, modelMapping)
		if canonicalErr != nil {
			return nil, nil, nil, canonicalErr
		}
		if _, held := heldNames[canonicalModel]; !held {
			heldNames[canonicalModel] = terminalTarget
		}
	}

	for _, modelName := range NormalizeModelNames(upstreamModels.Addable) {
		if _, ok := coveredUpstreamSet[modelName]; ok {
			continue
		}
		if lo.ContainsBy(normalizedIgnoredModels, func(ignoredModel string) bool {
			if regexBody, ok := strings.CutPrefix(ignoredModel, "regex:"); ok {
				matched, err := regexp.MatchString(strings.TrimSpace(regexBody), modelName)
				return err == nil && matched
			}
			return ignoredModel == modelName
		}) {
			continue
		}
		terminalTarget, resolveErr := rules.ResolveExactChannelModelTarget(modelName, modelMapping)
		if resolveErr != nil {
			return nil, nil, nil, resolveErr
		}
		canonicalModel, canonicalErr := rules.canonicalChannelModel(channelType, modelName, modelMapping)
		if canonicalErr != nil {
			return nil, nil, nil, canonicalErr
		}
		if heldTarget, held := heldNames[canonicalModel]; held && heldTarget != terminalTarget {
			conflictModels = append(conflictModels, modelName)
			continue
		}
		heldNames[canonicalModel] = terminalTarget
		pendingAddModels = append(pendingAddModels, modelName)
	}
	return NormalizeModelNames(pendingAddModels), NormalizeModelNames(pendingRemoveModels), NormalizeModelNames(conflictModels), nil
}

// SameNameReplacements are the pending additions whose canonical channel name
// is a pending removal: such an addition replaces that model under its name,
// so it can be added only together with the removal.
func (rules ModelUpdateRules) SameNameReplacements(channelType int, pendingAddModels, pendingRemoveModels []string, modelMapping map[string]string) ([]string, error) {
	replacements := []string{}
	for _, modelName := range pendingAddModels {
		canonicalModel, err := rules.canonicalChannelModel(channelType, modelName, modelMapping)
		if err != nil {
			return nil, err
		}
		if slices.Contains(pendingRemoveModels, canonicalModel) {
			replacements = append(replacements, modelName)
		}
	}
	return replacements, nil
}

// canonicalChannelModel is the name a channel model takes after
// canonicalization: a mapping source keeps its name, any other model takes the
// canonical name of the upstream model.
func (rules ModelUpdateRules) canonicalChannelModel(channelType int, modelName string, modelMapping map[string]string) (string, error) {
	if _, mapped := modelMapping[modelName]; mapped {
		return modelName, nil
	}
	return rules.CanonicalModel(channelType, modelName)
}

func (rules ModelUpdateRules) RejectSameNameModelMappingReplacement(pendingAddModels []string, pendingRemoveModels []string) error {
	conflictingModels := IntersectModelNames(pendingAddModels, pendingRemoveModels)
	if len(conflictingModels) == 0 {
		return nil
	}
	return fmt.Errorf(
		"%w: canonical model %q is pending both addition and removal",
		rules.Errors().ErrModelMappingConflict,
		conflictingModels[0],
	)
}
