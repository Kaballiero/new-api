package apply_upstream_model_updates

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
	store   Store
	rules   domain.ModelUpdateRules
	fetch   func(*domain.Channel) ([]string, error)
	refresh func()
	audit   func(map[string]any)
}

func NewUsecase(store Store, rules domain.ModelUpdateRules, fetch func(*domain.Channel) ([]string, error), refresh func(), audit func(map[string]any)) Usecase {
	return Usecase{store: store, rules: rules, fetch: fetch, refresh: refresh, audit: audit}
}
func (usecase Usecase) Apply(
	channel *domain.Channel,
	addModelsInput []string,
	ignoreModelsInput []string,
	removeModelsInput []string,
	applyAll bool,
	requireCheckEnabled bool,
) (result domain.AppliedModelUpdates, err error) {
	if _, err = domain.ParseChannelOtherSettings(channel); err != nil {
		return result, err
	}
	fetchFingerprint, err := usecase.fingerprint(channel)
	if err != nil {
		return result, err
	}
	upstreamModels, err := usecase.fetch(channel)
	if err != nil {
		return result, err
	}

	var freshChannel *domain.Channel
	err = usecase.store.WithLockedModelState(channel.Id, func(locked *domain.Channel, persist func(dto.ChannelOtherSettings, bool) error) error {
		freshChannel = locked
		freshFingerprint, fingerprintErr := usecase.fingerprint(freshChannel)
		if fingerprintErr != nil {
			return fingerprintErr
		}
		if !reflect.DeepEqual(fetchFingerprint, freshFingerprint) {
			return fmt.Errorf("channel fetch configuration changed during upstream model discovery")
		}
		if requireCheckEnabled && freshChannel.Status != common.ChannelStatusEnabled {
			return nil
		}

		settings, settingsErr := domain.ParseChannelOtherSettings(freshChannel)
		if settingsErr != nil {
			return settingsErr
		}
		if requireCheckEnabled && !settings.UpstreamModelUpdateCheckEnabled {
			return nil
		}
		result, err = usecase.rules.ApplyModelDelta(freshChannel, &settings, upstreamModels, addModelsInput, ignoreModelsInput, removeModelsInput, applyAll)
		if err != nil {
			return err
		}

		return persist(settings, result.ModelsChanged || result.MappingChanged)
	})
	if err != nil {
		return result, err
	}
	*channel = *freshChannel
	return result, nil
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
	result, err := usecase.Apply(channel, input.AddModels, input.IgnoreModels, input.RemoveModels, false, false)
	if err != nil {
		return Output{}, &domain.Failure{Cause: err}
	}
	if result.ModelsChanged || result.MappingChanged {
		usecase.refresh()
	}
	usecase.audit(map[string]any{"id": channel.Id})
	return Output{ID: channel.Id, AddedModels: result.AddedModels, RemovedModels: result.RemovedModels, IgnoredModels: result.IgnoredModels, RemainingModels: result.RemainingModels, RemainingRemoveModels: result.RemainingRemoveModels, Models: channel.Models, Settings: channel.OtherSettings}, nil
}
