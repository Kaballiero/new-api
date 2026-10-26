package detect_upstream_model_updates

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"reflect"
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
	fetch            func(*domain.Channel) ([]string, error)
	refresh          func()
	now, minInterval func() int64
}

func NewUsecase(store Store, rules domain.ModelUpdateRules, fetch func(*domain.Channel) ([]string, error), refresh func(), now, minInterval func() int64) Usecase {
	return Usecase{store: store, rules: rules, fetch: fetch, refresh: refresh, now: now, minInterval: minInterval}
}
func (usecase Usecase) Check(
	channel *domain.Channel,
	settings *domain.ModelUpdateState,
	force bool,
	allowAutoApply bool,
	requireEnabled bool,
) (configChanged bool, autoAdded int, err error) {
	now := usecase.now()
	if !force {
		minInterval := usecase.minInterval()
		if settings.LastCheckTime > 0 &&
			now-settings.LastCheckTime < minInterval {
			return false, 0, nil
		}
	}

	fetchFingerprint, err := usecase.store.FetchFingerprint(channel)
	if err != nil {
		return false, 0, err
	}
	upstreamModels, fetchErr := usecase.fetch(channel)

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

		configChanged, autoAdded, err = usecase.rules.DetectModelDelta(freshChannel, &freshSettings, upstreamModels, allowAutoApply)
		if err != nil {
			return err
		}

		return persist(freshSettings, configChanged)
	})
	if persistErr != nil {
		return false, 0, persistErr
	}
	if freshChannel != nil {
		*channel = *freshChannel
		*settings = freshSettings
	}
	if freshCheckDisabled {
		return false, 0, nil
	}
	if fetchErr != nil {
		return false, 0, fetchErr
	}
	return configChanged, autoAdded, nil
}

func (usecase Usecase) Execute(input Input) (Output, error) {
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
	changed, added, err := usecase.Check(channel, &settings, true, false, false)
	if err != nil {
		return Output{}, err
	}
	if changed {
		usecase.refresh()
	}
	return Output{ChannelID: channel.Id, ChannelName: channel.Name, AddModels: domain.NormalizeModelNames(settings.LastDetectedModels), RemoveModels: domain.NormalizeModelNames(settings.LastRemovedModels), LastCheckTime: settings.LastCheckTime, AutoAddedModels: added}, nil
}
