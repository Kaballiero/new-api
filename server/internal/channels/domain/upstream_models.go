package domain

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/samber/lo"
	"regexp"
	"strings"
)

type ModelMappingErrors struct{ ErrModelNameEmpty, ErrModelMappingCycle, ErrModelMappingSourceEmpty, ErrModelMappingTargetEmpty, ErrModelMappingConflict error }
type ModelUpdateRules struct {
	Errors       func() ModelMappingErrors
	Canonicalize func(*Channel) error
	Now          func() int64
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
// gateway stores it among the other channel settings.
type ModelUpdateState struct {
	CheckEnabled       bool
	AutoSyncEnabled    bool
	LastCheckTime      int64
	LastDetectedModels []string
	LastRemovedModels  []string
	IgnoredModels      []string
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
	parsed := make(map[string]string)
	if err := common.UnmarshalJsonStr(rawMapping, &parsed); err != nil {
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
func (rules ModelUpdateRules) CollectPendingUpstreamModelChangesFromModels(
	localModels []string,
	upstreamModels []string,
	ignoredModels []string,
	modelMapping map[string]string,
) (pendingAddModels []string, pendingRemoveModels []string, err error) {
	if err := rules.ValidateResolvableChannelModelMapping(modelMapping); err != nil {
		return nil, nil, err
	}
	localModels = NormalizeModelNames(localModels)
	upstreamModels = NormalizeModelNames(upstreamModels)
	upstreamSet := make(map[string]struct{}, len(upstreamModels))
	for _, modelName := range upstreamModels {
		upstreamSet[modelName] = struct{}{}
	}

	normalizedIgnoredModels := NormalizeModelNames(ignoredModels)
	coveredUpstreamSet := make(map[string]struct{}, len(localModels))
	for _, modelName := range localModels {
		terminalTarget, resolveErr := rules.ResolveExactChannelModelTarget(modelName, modelMapping)
		if resolveErr != nil {
			return nil, nil, resolveErr
		}
		coveredUpstreamSet[terminalTarget] = struct{}{}
		if _, ok := upstreamSet[terminalTarget]; !ok {
			pendingRemoveModels = append(pendingRemoveModels, modelName)
		}
	}

	pendingAdd := lo.Filter(upstreamModels, func(modelName string, _ int) bool {
		if _, ok := coveredUpstreamSet[modelName]; ok {
			return false
		}
		if lo.ContainsBy(normalizedIgnoredModels, func(ignoredModel string) bool {
			if regexBody, ok := strings.CutPrefix(ignoredModel, "regex:"); ok {
				matched, err := regexp.MatchString(strings.TrimSpace(regexBody), modelName)
				return err == nil && matched
			}
			return ignoredModel == modelName
		}) {
			return false
		}
		return true
	})
	return NormalizeModelNames(pendingAdd), NormalizeModelNames(pendingRemoveModels), nil
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
