package scan_upstream_model_updates

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"reflect"
	"time"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	OtherSettings(*domain.Channel) dto.ChannelOtherSettings
	WithLockedModelState(int, func(*domain.Channel, func(dto.ChannelOtherSettings, bool) error) error) error
	CountEnabled() (int64, error)
	NextEnabled(int) ([]*domain.Channel, int, bool, error)
}
type Usecase struct {
	store            Store
	rules            domain.ModelUpdateRules
	fetch            func(*domain.Channel) ([]string, error)
	refresh          func()
	now, minInterval func() int64
	interval         func() time.Duration
	debug            func() bool
	log              func(string)
	throttle         func(int64, int, int) bool
	notify           func(string, string)
}

func NewUsecase(store Store, rules domain.ModelUpdateRules, fetch func(*domain.Channel) ([]string, error), refresh func(), now, minInterval func() int64, interval func() time.Duration, debug func() bool, log func(string), throttle func(int64, int, int) bool, notify func(string, string)) Usecase {
	return Usecase{store: store, rules: rules, fetch: fetch, refresh: refresh, now: now, minInterval: minInterval, interval: interval, debug: debug, log: log, throttle: throttle, notify: notify}
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

func (usecase Usecase) Execute(ctx context.Context, force bool, allowAutoApply bool, report func(processed, total int)) domain.ModelUpdateSummary {
	checkedChannels := 0
	failedChannels := 0
	failedChannelIDs := make([]int, 0)
	changedChannels := 0
	detectedAddModels := 0
	detectedRemoveModels := 0
	autoAddedModels := 0
	channelSummaries := make([]domain.ModelUpdateChannelSummary, 0)
	addModelSamples := make([]string, 0)
	removeModelSamples := make([]string, 0)
	refreshNeeded := false

	// Count the enabled channels up front so progress can be reported as a
	// percentage; a count error is non-fatal (progress just won't show a %).
	totalChannels, countErr := usecase.store.CountEnabled()
	if countErr != nil {
		totalChannels = 0
	}

	processed := 0

	lastID := 0
scanLoop:
	for {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		channels, nextID, done, err := usecase.store.NextEnabled(lastID)

		if err != nil {
			usecase.log(fmt.Sprintf("upstream model update task query failed: %v", err))
			break
		}
		if len(channels) == 0 {
			break
		}
		lastID = nextID

		for _, channel := range channels {
			if channel == nil {
				continue
			}
			if ctx != nil && ctx.Err() != nil {
				break scanLoop
			}

			processed++
			if report != nil {
				report(processed, int(totalChannels))
			}

			settings, settingsErr := domain.ParseChannelOtherSettings(channel)
			if settingsErr != nil {
				failedChannels++
				failedChannelIDs = append(failedChannelIDs, channel.Id)
				usecase.log(fmt.Sprintf("upstream model update settings invalid: channel_id=%d channel_name=%s err=%v", channel.Id, channel.Name, settingsErr))
				continue
			}
			if !settings.UpstreamModelUpdateCheckEnabled {
				continue
			}

			checkedChannels++
			configChanged, autoAdded, err := usecase.Check(channel, &settings, force, allowAutoApply, true)
			if err != nil {
				failedChannels++
				failedChannelIDs = append(failedChannelIDs, channel.Id)
				usecase.log(fmt.Sprintf("upstream model update check failed: channel_id=%d channel_name=%s err=%v", channel.Id, channel.Name, err))
				continue
			}
			currentAddModels := domain.NormalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels)
			currentRemoveModels := domain.NormalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels)
			currentAddCount := len(currentAddModels) + autoAdded
			currentRemoveCount := len(currentRemoveModels)
			detectedAddModels += currentAddCount
			detectedRemoveModels += currentRemoveCount
			if currentAddCount > 0 || currentRemoveCount > 0 {
				changedChannels++
				channelSummaries = append(channelSummaries, domain.ModelUpdateChannelSummary{
					ChannelName: channel.Name,
					AddCount:    currentAddCount,
					RemoveCount: currentRemoveCount,
				})
			}
			addModelSamples = domain.MergeModelNames(addModelSamples, currentAddModels)
			removeModelSamples = domain.MergeModelNames(removeModelSamples, currentRemoveModels)
			if configChanged {
				refreshNeeded = true
			}
			autoAddedModels += autoAdded

			if usecase.interval() > 0 {
				if ctx == nil {
					time.Sleep(usecase.interval())
				} else {
					select {
					case <-ctx.Done():
						break scanLoop
					case <-time.After(usecase.interval()):
					}
				}
			}
		}

		if done {
			break
		}
	}

	if report != nil && (ctx == nil || ctx.Err() == nil) {
		report(int(totalChannels), int(totalChannels)) // mark complete only when the full scan finished
	}

	if refreshNeeded {
		usecase.refresh()
	}

	summary := domain.ModelUpdateSummary{
		CheckedChannels:      checkedChannels,
		ChangedChannels:      changedChannels,
		DetectedAddModels:    detectedAddModels,
		DetectedRemoveModels: detectedRemoveModels,
		FailedChannels:       failedChannels,
		AutoAddedModels:      autoAddedModels,
	}

	if checkedChannels > 0 || usecase.debug() {
		usecase.log(fmt.Sprintf(
			"upstream model update task done: checked_channels=%d changed_channels=%d detected_add_models=%d detected_remove_models=%d failed_channels=%d auto_added_models=%d",
			checkedChannels,
			changedChannels,
			detectedAddModels,
			detectedRemoveModels,
			failedChannels,
			autoAddedModels,
		))
	}
	if changedChannels > 0 || failedChannels > 0 {
		now := usecase.now()
		if !usecase.throttle(now, changedChannels, failedChannels) {
			usecase.log(fmt.Sprintf(
				"upstream model update notification skipped in 24h window: changed_channels=%d failed_channels=%d",
				changedChannels,
				failedChannels,
			))
			return summary
		}
		usecase.notify(
			"上游模型巡检通知",
			domain.ModelUpdateNotificationContent(
				checkedChannels,
				changedChannels,
				detectedAddModels,
				detectedRemoveModels,
				autoAddedModels,
				failedChannelIDs,
				channelSummaries,
				addModelSamples,
				removeModelSamples,
			),
		)
	}
	return summary
}
