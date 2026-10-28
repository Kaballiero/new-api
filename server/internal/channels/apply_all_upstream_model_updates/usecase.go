package apply_all_upstream_model_updates

import (
	"reflect"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	ModelUpdateState(*domain.Channel) (domain.ModelUpdateState, error)
	FetchFingerprint(*domain.Channel) (domain.ModelFetchFingerprint, error)
	WithLockedModelState(int, func(*domain.Channel, func(domain.ModelUpdateState, bool) error) error) error
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

			settings, settingsErr := usecase.store.ModelUpdateState(channel)
			if settingsErr != nil {
				failed = append(failed, channel.Id)
				continue
			}
			if !settings.CheckEnabled {
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
