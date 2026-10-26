package apply_upstream_model_updates

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
	if _, err = usecase.store.ModelUpdateState(channel); err != nil {
		return result, err
	}
	fetchFingerprint, err := usecase.store.FetchFingerprint(channel)
	if err != nil {
		return result, err
	}
	upstreamModels, err := usecase.fetch(channel)
	if err != nil {
		return result, err
	}

	var freshChannel *domain.Channel
	err = usecase.store.WithLockedModelState(channel.Id, func(locked *domain.Channel, persist func(domain.ModelUpdateState, bool) error) error {
		freshChannel = locked
		freshFingerprint, fingerprintErr := usecase.store.FetchFingerprint(freshChannel)
		if fingerprintErr != nil {
			return fingerprintErr
		}
		if !reflect.DeepEqual(fetchFingerprint, freshFingerprint) {
			return domain.ErrFetchConfigChanged
		}
		if requireCheckEnabled && freshChannel.Status != constant.ChannelStatusEnabled {
			return nil
		}

		settings, settingsErr := usecase.store.ModelUpdateState(freshChannel)
		if settingsErr != nil {
			return settingsErr
		}
		if requireCheckEnabled && !settings.CheckEnabled {
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

func (usecase Usecase) Execute(input Input) (Output, error) {
	if input.ID <= 0 {
		return Output{}, domain.ErrChannelIDInvalid
	}
	channel, err := usecase.store.Get(input.ID, true)
	if err != nil {
		return Output{}, err
	}
	result, err := usecase.Apply(channel, input.AddModels, input.IgnoreModels, input.RemoveModels, false, false)
	if err != nil {
		return Output{}, err
	}
	if result.ModelsChanged || result.MappingChanged {
		usecase.refresh()
	}
	usecase.audit(map[string]any{"id": channel.Id})
	return Output{ID: channel.Id, AddedModels: result.AddedModels, RemovedModels: result.RemovedModels, IgnoredModels: result.IgnoredModels, RemainingModels: result.RemainingModels, RemainingRemoveModels: result.RemainingRemoveModels, Models: channel.Models, Settings: channel.OtherSettings}, nil
}
