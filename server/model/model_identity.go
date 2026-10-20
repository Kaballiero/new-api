package model

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

func channelModelIdentity() domain.ModelIdentity {
	return domain.ModelIdentity{Errors: func() domain.ModelIdentityErrors {
		return domain.ModelIdentityErrors{ErrCanonicalModelCollision: ErrCanonicalModelCollision, ErrModelAliasCycle: ErrModelAliasCycle, ErrModelMappingConflict: ErrModelMappingConflict, ErrModelMappingCycle: ErrModelMappingCycle, ErrModelMappingSourceEmpty: ErrModelMappingSourceEmpty, ErrModelMappingTargetEmpty: ErrModelMappingTargetEmpty, ErrModelNameEmpty: ErrModelNameEmpty}
	}}
}
func ResolveCanonicalModelName(
	requestedName string,
	aliases map[string]string,
) (string, error) {
	return channelModelIdentity().ResolveCanonicalModelName(requestedName, aliases)
}
func NormalizeChannelModel(
	channelType int,
	upstreamModel string,
) (canonicalModel string, upstreamMapping string, err error) {
	return channelModelIdentity().NormalizeChannelModel(channelType, upstreamModel)
}
func ResolveChannelModelTarget(
	modelName string,
	modelMapping map[string]string,
) (string, error) {
	return channelModelIdentity().ResolveChannelModelTarget(modelName, modelMapping)
}
func CanonicalizeChannelModels(
	channelType int,
	models []string,
	existingMapping map[string]string,
) ([]string, map[string]string, error) {
	return channelModelIdentity().CanonicalizeChannelModels(channelType, models, existingMapping)
}
func (channel *Channel) CanonicalizeModelConfig() error {
	value := record.ToDomain((*record.Channel)(channel))
	err := channelModelIdentity().Canonicalize(value)
	*channel = Channel(*record.FromDomain(value))
	return err
}
