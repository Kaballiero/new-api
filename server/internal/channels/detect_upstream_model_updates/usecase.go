package detect_upstream_model_updates

import (
	"context"
	"reflect"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	ModelUpdateState(*domain.Channel) (domain.ModelUpdateState, error)
	FetchFingerprint(*domain.Channel) (domain.ModelFetchFingerprint, error)
	WithLockedModelState(int, func(*domain.Channel, func(domain.ModelUpdateState, bool) error) error) error
}
type Usecase struct {
	store            Store
	rules            domain.ModelUpdateRules
	fetch            func(context.Context, *domain.Channel) (domain.UpstreamModels, error)
	refresh          func()
	now, minInterval func() int64
}

func NewUsecase(store Store, rules domain.ModelUpdateRules, fetch func(context.Context, *domain.Channel) (domain.UpstreamModels, error), refresh func(), now, minInterval func() int64) Usecase {
	return Usecase{store: store, rules: rules, fetch: fetch, refresh: refresh, now: now, minInterval: minInterval}
}
func (usecase Usecase) Check(
	ctx context.Context,
	channel *domain.Channel,
	settings *domain.ModelUpdateState,
	force bool,
	allowAutoApply bool,
	requireEnabled bool,
) (applied domain.AppliedModelUpdates, err error) {
	now := usecase.now()
	if !force {
		minInterval := usecase.minInterval()
		if settings.LastCheckTime > 0 &&
			now-settings.LastCheckTime < minInterval {
			return domain.AppliedModelUpdates{}, nil
		}
	}

	fetchFingerprint, err := usecase.store.FetchFingerprint(channel)
	if err != nil {
		return domain.AppliedModelUpdates{}, err
	}
	upstreamModels, fetchErr := usecase.fetch(ctx, channel)

	var freshChannel *domain.Channel
	var freshSettings domain.ModelUpdateState
	freshCheckDisabled := false
	persistErr := usecase.store.WithLockedModelState(channel.Id, func(locked *domain.Channel, persist func(domain.ModelUpdateState, bool) error) error {
		freshChannel = locked
		freshFingerprint, fingerprintErr := usecase.store.FetchFingerprint(freshChannel)
		if fingerprintErr != nil {
			return fingerprintErr
		}
		if !reflect.DeepEqual(fetchFingerprint, freshFingerprint) {
			return domain.ErrFetchConfigChanged
		}
		if requireEnabled && freshChannel.Status != constant.ChannelStatusEnabled {
			freshCheckDisabled = true
			return nil
		}

		freshSettings, err = usecase.store.ModelUpdateState(freshChannel)
		if err != nil {
			return err
		}
		if !force {
			if !freshSettings.CheckEnabled {
				freshCheckDisabled = true
				return nil
			}
			minInterval := usecase.minInterval()
			if freshSettings.LastCheckTime > 0 &&
				now-freshSettings.LastCheckTime < minInterval {
				return nil
			}
		}
		freshSettings.LastCheckTime = now
		if fetchErr != nil {
			return persist(freshSettings, false)
		}

		applied, err = usecase.rules.DetectModelDelta(freshChannel, &freshSettings, upstreamModels, allowAutoApply)
		if err != nil {
			return err
		}
		return persist(freshSettings, applied.ModelsChanged || applied.MappingChanged)
	})
	if persistErr != nil {
		return domain.AppliedModelUpdates{}, persistErr
	}
	if freshChannel != nil {
		*channel = *freshChannel
		*settings = freshSettings
	}
	if freshCheckDisabled {
		return domain.AppliedModelUpdates{}, nil
	}
	if fetchErr != nil {
		return domain.AppliedModelUpdates{}, fetchErr
	}
	return applied, nil
}

func (usecase Usecase) Execute(ctx context.Context, input Input) (Output, error) {
	if input.ID <= 0 {
		return Output{}, domain.ErrChannelIDInvalid
	}
	channel, err := usecase.store.Get(input.ID, true)
	if err != nil {
		return Output{}, err
	}
	settings, err := usecase.store.ModelUpdateState(channel)
	if err != nil {
		return Output{}, err
	}
	applied, err := usecase.Check(ctx, channel, &settings, true, false, false)
	if err != nil {
		return Output{}, err
	}
	if applied.ModelsChanged || applied.MappingChanged {
		usecase.refresh()
	}
	return Output{ChannelID: channel.Id, ChannelName: channel.Name, AddModels: domain.NormalizeModelNames(settings.LastDetectedModels), RemoveModels: domain.NormalizeModelNames(settings.LastRemovedModels), ConflictModels: domain.NormalizeModelNames(settings.LastConflictModels),
		ReserveRefusedModels: domain.NormalizeModelNames(applied.ReserveRefusedModels), LastCheckTime: settings.LastCheckTime, AutoAddedModels: len(applied.AddedModels)}, nil
}
