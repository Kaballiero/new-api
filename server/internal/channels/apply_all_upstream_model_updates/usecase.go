package apply_all_upstream_model_updates

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
	NextEnabled(int) ([]*domain.Channel, int, bool, error)
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

func (usecase Usecase) Execute() (Output, error) {
	results := make([]Result, 0)
	failed := make([]int, 0)
	refreshNeeded := false
	addedModelCount := 0
	removedModelCount := 0

	lastID := 0
	for {
		channels, nextID, done, err := usecase.store.NextEnabled(lastID)
		if err != nil {
			return Output{}, err
		}
		if len(channels) == 0 {
			break
		}
		lastID = nextID

		for _, channel := range channels {
			if channel == nil {
				continue
			}

			settings, settingsErr := domain.ParseChannelOtherSettings(channel)
			if settingsErr != nil {
				failed = append(failed, channel.Id)
				continue
			}
			if !settings.UpstreamModelUpdateCheckEnabled {
				continue
			}

			result, err := usecase.Apply(
				channel,
				nil,
				nil,
				nil,
				true,
				true,
			)
			if err != nil {
				failed = append(failed, channel.Id)
				continue
			}
			if !result.HadPending {
				continue
			}
			if result.ModelsChanged || result.MappingChanged {
				refreshNeeded = true
			}
			addedModelCount += len(result.AddedModels)
			removedModelCount += len(result.RemovedModels)
			results = append(results, Result{
				ChannelID:             channel.Id,
				ChannelName:           channel.Name,
				AddedModels:           result.AddedModels,
				RemovedModels:         result.RemovedModels,
				RemainingModels:       result.RemainingModels,
				RemainingRemoveModels: result.RemainingRemoveModels,
			})
		}

		if done {
			break
		}
	}

	if refreshNeeded {
		usecase.refresh()
	}

	usecase.audit(map[string]any{
		"count": len(results),
	})
	return Output{ProcessedChannels: len(results), AddedModels: addedModelCount, RemovedModels: removedModelCount, FailedChannelIDs: failed, Results: results}, nil
}
