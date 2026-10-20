package detect_upstream_model_updates

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"reflect"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	OtherSettings(*domain.Channel) dto.ChannelOtherSettings
	WithLockedModelState(int, func(*domain.Channel, func(dto.ChannelOtherSettings, bool) error) error) error
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
	settings *dto.ChannelOtherSettings,
	force bool,
	allowAutoApply bool,
	requireEnabled bool,
) (configChanged bool, autoAdded int, err error) {
	now := usecase.now()
	if !force {
		minInterval := usecase.minInterval()
		if settings.UpstreamModelUpdateLastCheckTime > 0 &&
			now-settings.UpstreamModelUpdateLastCheckTime < minInterval {
			return false, 0, nil
		}
	}

	fetchFingerprint, err := usecase.fingerprint(channel)
	if err != nil {
		return false, 0, err
	}
	upstreamModels, fetchErr := usecase.fetch(channel)

	var freshChannel *domain.Channel
	var freshSettings dto.ChannelOtherSettings
	freshCheckDisabled := false
	persistErr := usecase.store.WithLockedModelState(channel.Id, func(locked *domain.Channel, persist func(dto.ChannelOtherSettings, bool) error) error {
		freshChannel = locked
		freshFingerprint, fingerprintErr := usecase.fingerprint(freshChannel)
		if fingerprintErr != nil {
			return fingerprintErr
		}
		if !reflect.DeepEqual(fetchFingerprint, freshFingerprint) {
			return fmt.Errorf("channel fetch configuration changed during upstream model discovery")
		}
		if requireEnabled && freshChannel.Status != common.ChannelStatusEnabled {
			freshCheckDisabled = true
			return nil
		}

		freshSettings, err = domain.ParseChannelOtherSettings(freshChannel)
		if err != nil {
			return err
		}
		if !force {
			if !freshSettings.UpstreamModelUpdateCheckEnabled {
				freshCheckDisabled = true
				return nil
			}
			minInterval := usecase.minInterval()
			if freshSettings.UpstreamModelUpdateLastCheckTime > 0 &&
				now-freshSettings.UpstreamModelUpdateLastCheckTime < minInterval {
				return nil
			}
		}
		freshSettings.UpstreamModelUpdateLastCheckTime = now
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
func (usecase Usecase) fingerprint(channel *domain.Channel) (domain.ModelFetchFingerprint, error) {
	fingerprint, err := domain.BuildModelFetchFingerprint(channel)
	if err != nil {
		return fingerprint, err
	}
	fingerprint.AdvancedCustom = usecase.store.OtherSettings(channel).AdvancedCustom
	return fingerprint, nil
}

func (usecase Usecase) Execute(input Input) (Output, *domain.Failure) {
	if input.ID <= 0 {
		return Output{}, &domain.Failure{Kind: "invalid", Message: "invalid channel id"}
	}
	channel, err := usecase.store.Get(input.ID, true)
	if err != nil {
		return Output{}, &domain.Failure{Cause: err}
	}
	settings, err := domain.ParseChannelOtherSettings(channel)
	if err != nil {
		return Output{}, &domain.Failure{Cause: err}
	}
	changed, added, err := usecase.Check(channel, &settings, true, false, false)
	if err != nil {
		return Output{}, &domain.Failure{Cause: err}
	}
	if changed {
		usecase.refresh()
	}
	return Output{ChannelID: channel.Id, ChannelName: channel.Name, AddModels: domain.NormalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels), RemoveModels: domain.NormalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels), LastCheckTime: settings.UpstreamModelUpdateLastCheckTime, AutoAddedModels: added}, nil
}
