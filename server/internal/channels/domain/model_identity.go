package domain

import (
	"fmt"
	"github.com/QuantumNous/new-api/constant"
	"strings"
)

type ModelIdentityErrors struct{ ErrCanonicalModelCollision, ErrModelAliasCycle, ErrModelMappingConflict, ErrModelMappingCycle, ErrModelMappingSourceEmpty, ErrModelMappingTargetEmpty, ErrModelNameEmpty error }
type ModelIdentity struct {
	Errors func() ModelIdentityErrors
	Codec  ModelMappingCodec
}

const anyChannelType = -1

type modelPrefixRule struct {
	name        string
	separator   string
	channelType int
}

var modelPrefixRules = []modelPrefixRule{
	{name: "openrouter", separator: "/", channelType: anyChannelType},
	{name: "litellm", separator: "/", channelType: anyChannelType},
	{name: "ollama", separator: "/", channelType: anyChannelType},
	{name: "deepinfra", separator: "/", channelType: anyChannelType},
	{name: "together", separator: "/", channelType: anyChannelType},
	{name: "vertex", separator: "/", channelType: anyChannelType},
	{name: "bedrock", separator: "/", channelType: anyChannelType},
	{name: "fastgpt", separator: "/", channelType: anyChannelType},
	{name: "openai", separator: "/", channelType: anyChannelType},
	{name: "deepseek", separator: "/", channelType: anyChannelType},
	{name: "google", separator: "/", channelType: anyChannelType},
	{name: "anthropic", separator: "/", channelType: anyChannelType},
	{name: "qwen", separator: "/", channelType: anyChannelType},
	{name: "models", separator: "/", channelType: constant.ChannelTypeGemini},
	{name: "anthropic", separator: ".", channelType: constant.ChannelTypeAws},
	{name: "meta", separator: ".", channelType: constant.ChannelTypeAws},
	{name: "mistral", separator: ".", channelType: constant.ChannelTypeAws},
	{name: "amazon", separator: ".", channelType: constant.ChannelTypeAws},
	{name: "cohere", separator: ".", channelType: constant.ChannelTypeAws},
	{name: "stability", separator: ".", channelType: constant.ChannelTypeAws},
}

func (identity ModelIdentity) ResolveCanonicalModelName(
	requestedName string,
	aliases map[string]string,
) (string, error) {

	if strings.TrimSpace(requestedName) == "" {
		return "", identity.Errors().ErrModelNameEmpty
	}

	return identity.resolveModelAliasChain(aliases, requestedName)

}

func (identity ModelIdentity) resolveModelAliasChain(
	aliases map[string]string,
	requestedName string,
) (string, error) {
	currentName := requestedName
	visited := map[string]bool{currentName: true}

	for {
		nextName, ok := aliases[currentName]
		if !ok {
			return currentName, nil
		}
		if strings.TrimSpace(nextName) == "" {
			return "", identity.Errors().ErrModelNameEmpty
		}
		if nextName == currentName {
			return currentName, nil
		}
		if visited[nextName] {
			return "", identity.Errors().ErrModelAliasCycle
		}
		visited[nextName] = true
		currentName = nextName
	}

}

func (identity ModelIdentity) NormalizeChannelModel(
	channelType int,
	upstreamModel string,
) (canonicalModel string, upstreamMapping string, err error) {

	canonicalModel = strings.TrimSpace(upstreamModel)

	if canonicalModel == "" {
		return "", "", identity.Errors().ErrModelNameEmpty
	}
	if channelType == constant.ChannelTypeOpenRouter {
		canonicalModel, err = identity.normalizeOpenRouterModel(canonicalModel)
		if err != nil {
			return "", "", err
		}
		return canonicalModel, upstreamModel, nil
	}

	for {
		matchedRule := false
		for _, rule := range modelPrefixRules {
			if rule.channelType != anyChannelType && rule.channelType != channelType {
				continue
			}

			prefix := rule.name + rule.separator
			if strings.HasPrefix(canonicalModel, prefix) {
				canonicalModel = strings.TrimPrefix(canonicalModel, prefix)
				matchedRule = true
				break
			}
		}
		if !matchedRule {
			break
		}
	}
	if canonicalModel == "" {
		return "", "", identity.Errors().ErrModelNameEmpty
	}

	return canonicalModel, upstreamModel, nil
}

func (identity ModelIdentity) normalizeOpenRouterModel(modelName string) (string, error) {
	namespace, modelNameWithoutNamespace, hasNamespace := strings.Cut(modelName, "/")
	if !hasNamespace {
		return modelName, nil
	}
	if strings.TrimSpace(modelNameWithoutNamespace) == "" {
		return "", identity.Errors().ErrModelNameEmpty
	}
	if namespace != "openrouter" {
		return modelNameWithoutNamespace, nil
	}

	_, nestedModelName, hasNestedNamespace := strings.Cut(modelNameWithoutNamespace, "/")
	if !hasNestedNamespace {
		return modelName, nil
	}
	if strings.TrimSpace(nestedModelName) == "" {
		return "", identity.Errors().ErrModelNameEmpty
	}
	return nestedModelName, nil
}

func (identity ModelIdentity) ResolveChannelModelTarget(
	modelName string,
	modelMapping map[string]string,
) (string, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return "", identity.Errors().ErrModelNameEmpty
	}

	if err := identity.validateChannelModelMapping(modelMapping); err != nil {
		return "", err
	}

	return identity.resolveChannelModelTarget(modelName, modelMapping)
}

func (identity ModelIdentity) validateChannelModelMapping(modelMapping map[string]string) error {
	for source, target := range modelMapping {
		if strings.TrimSpace(source) == "" {
			return identity.Errors().ErrModelMappingSourceEmpty
		}
		if strings.TrimSpace(target) == "" {
			return identity.Errors().ErrModelMappingTargetEmpty
		}
	}
	return nil
}

func (identity ModelIdentity) resolveChannelModelTarget(modelName string, modelMapping map[string]string) (string, error) {
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
			return "", identity.Errors().ErrModelMappingCycle
		}
		visitedModels[mappedModel] = true
		currentModel = mappedModel
	}
}

func (identity ModelIdentity) CanonicalizeChannelModels(
	channelType int,
	models []string,
	existingMapping map[string]string,
) ([]string, map[string]string, error) {
	if err := identity.validateChannelModelMapping(existingMapping); err != nil {
		return nil, nil, err
	}
	for source := range existingMapping {
		if _, err := identity.resolveChannelModelTarget(source, existingMapping); err != nil {
			return nil, nil, err
		}
	}

	canonicalModels := make([]string, 0, len(models))
	mergedMapping := make(map[string]string, len(existingMapping))
	for source, target := range existingMapping {
		mergedMapping[source] = target
	}

	seenModels := make(map[string]string, len(models))
	for _, modelName := range models {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			return nil, nil, identity.Errors().ErrModelNameEmpty
		}

		canonicalModel := modelName
		upstreamModel, hasManualMapping := existingMapping[modelName]
		terminalTarget := modelName
		if !hasManualMapping {
			var err error
			canonicalModel, upstreamModel, err = identity.NormalizeChannelModel(channelType, modelName)
			if err != nil {
				return nil, nil, err
			}

			terminalTarget, err = identity.resolveChannelModelTarget(upstreamModel, existingMapping)
			if err != nil {
				return nil, nil, err
			}
			if _, exists := existingMapping[canonicalModel]; exists {
				existingTerminalTarget, err := identity.resolveChannelModelTarget(canonicalModel, existingMapping)
				if err != nil {
					return nil, nil, err
				}
				if existingTerminalTarget != terminalTarget {
					return nil, nil, fmt.Errorf(
						"%w: %q maps to %q instead of %q",
						identity.Errors().ErrModelMappingConflict,
						canonicalModel,
						existingTerminalTarget,
						terminalTarget,
					)
				}
			}
		} else {
			var err error
			terminalTarget, err = identity.resolveChannelModelTarget(modelName, existingMapping)
			if err != nil {
				return nil, nil, err
			}
		}

		if seenTarget, exists := seenModels[canonicalModel]; exists {
			if seenTarget == terminalTarget {
				continue
			}
			return nil, nil, fmt.Errorf(
				"%w: %q resolves to both %q and %q",
				identity.Errors().ErrCanonicalModelCollision,
				canonicalModel,
				seenTarget,
				terminalTarget,
			)
		}

		seenModels[canonicalModel] = terminalTarget
		canonicalModels = append(canonicalModels, canonicalModel)
		if canonicalModel != upstreamModel {
			if _, exists := existingMapping[canonicalModel]; !exists {
				mergedMapping[canonicalModel] = upstreamModel
			}
		}
	}

	return canonicalModels, mergedMapping, nil
}

func (identity ModelIdentity) Canonicalize(channel *Channel) error {
	existingMapping := make(map[string]string)
	mappingJSON := strings.TrimSpace(channel.GetModelMapping())
	if mappingJSON != "" {
		var err error
		existingMapping, err = identity.Codec.DecodeModelMapping(mappingJSON)
		if err != nil {
			return fmt.Errorf("invalid model mapping: %w", err)
		}
	}

	models, mapping, err := identity.CanonicalizeChannelModels(channel.Type, channel.GetModels(), existingMapping)
	if err != nil {
		return err
	}

	mappingJSON, err = identity.Codec.EncodeModelMapping(mapping)
	if err != nil {
		return fmt.Errorf("marshal model mapping: %w", err)
	}

	channel.Models = strings.Join(models, ",")
	channel.ModelMapping = &mappingJSON
	return nil
}
