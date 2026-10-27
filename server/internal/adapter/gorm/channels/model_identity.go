package channels

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	legacy "github.com/QuantumNous/new-api/model"
)

func channelModelIdentity() domain.ModelIdentity {
	return domain.ModelIdentity{Codec: record.Codec{}, Errors: func() domain.ModelIdentityErrors {
		return domain.ModelIdentityErrors{
			ErrCanonicalModelCollision: domain.ErrCanonicalModelCollision,
			ErrModelAliasCycle:         domain.ErrModelAliasCycle,
			ErrModelMappingConflict:    domain.ErrModelMappingConflict,
			ErrModelMappingCycle:       domain.ErrModelMappingCycle,
			ErrModelMappingSourceEmpty: domain.ErrModelMappingSourceEmpty,
			ErrModelMappingTargetEmpty: domain.ErrModelMappingTargetEmpty,
			ErrModelNameEmpty:          domain.ErrModelNameEmpty,
		}
	}}
}

func CanonicalizeModelConfig(channel *legacy.Channel) error {
	value := record.ToDomain((*record.Channel)(channel))
	err := channelModelIdentity().Canonicalize(value)
	*channel = legacy.Channel(*record.FromDomain(value))
	return err
}
