package scan_upstream_model_updates

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	ModelUpdateState(*domain.Channel) (domain.ModelUpdateState, error)
	FetchFingerprint(*domain.Channel) (domain.ModelFetchFingerprint, error)
	WithLockedModelState(int, func(*domain.Channel, func(domain.ModelUpdateState, bool) error) error) error
	CountEnabled() (int64, error)
	NextEnabled(int) ([]*domain.Channel, int, bool, error)
}

// SentReports keeps the fingerprint of the last delivered report.
type SentReports interface {
	ReadOption(ctx context.Context, key string) (string, error)
	SaveBulk(ctx context.Context, keys []string, values map[string]string) error
}

const sentReportFingerprintKey = "channel_model_update_report_fingerprint"

type Usecase struct {
	store            Store
	rules            domain.ModelUpdateRules
	fetch            func(context.Context, *domain.Channel) (domain.UpstreamModels, error)
	refresh          func()
	now, minInterval func() int64
	interval         func() time.Duration
	debug            func() bool
	log              func(string)
	sentReports      SentReports
	notify           func(domain.ModelUpdateReport) error
}

func NewUsecase(store Store, rules domain.ModelUpdateRules, fetch func(context.Context, *domain.Channel) (domain.UpstreamModels, error), refresh func(), now, minInterval func() int64, interval func() time.Duration, debug func() bool, log func(string), sentReports SentReports, notify func(domain.ModelUpdateReport) error) Usecase {
	return Usecase{store: store, rules: rules, fetch: fetch, refresh: refresh, now: now, minInterval: minInterval, interval: interval, debug: debug, log: log, sentReports: sentReports, notify: notify}
}
func (usecase Usecase) Check(
	ctx context.Context,
	channel *domain.Channel,
	settings *domain.ModelUpdateState,
	force bool,
	allowAutoApply bool,
	requireEnabled bool,
) (appliedUpdates domain.AppliedModelUpdates, autoApplyBlocked bool, err error) {
	now := usecase.now()
	if !force {
		minInterval := usecase.minInterval()
		if settings.LastCheckTime > 0 &&
			now-settings.LastCheckTime < minInterval {
			// A recent check keeps its pending changes; a catalog channel
			// still reports whether its catalog blocks auto-apply and whether
			// its removals exceed the auto removal limit, so the report does
			// not change between the checks. The catalog is read once per run.
			if !settings.CatalogSyncEnabled {
				return domain.AppliedModelUpdates{}, false, nil
			}
			upstreamModels, err := usecase.fetch(ctx, channel)
			if err != nil {
				return domain.AppliedModelUpdates{}, false, err
			}
			limitCandidates, err := usecase.rules.RemovalLimitCandidates(channel, *settings, upstreamModels)
			return domain.AppliedModelUpdates{RemovalLimitCandidates: limitCandidates}, upstreamModels.AutoApplyBlocked, err
		}
	}

	fetchFingerprint, err := usecase.store.FetchFingerprint(channel)
	if err != nil {
		return domain.AppliedModelUpdates{}, false, err
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

		appliedUpdates, err = usecase.rules.DetectModelDelta(freshChannel, &freshSettings, upstreamModels, allowAutoApply)
		if err != nil {
			return err
		}

		return persist(freshSettings, appliedUpdates.ModelsChanged || appliedUpdates.MappingChanged)
	})
	if persistErr != nil {
		return domain.AppliedModelUpdates{}, false, persistErr
	}
	if freshChannel != nil {
		*channel = *freshChannel
		*settings = freshSettings
	}
	if freshCheckDisabled {
		return domain.AppliedModelUpdates{}, false, nil
	}
	if fetchErr != nil {
		return domain.AppliedModelUpdates{}, false, fetchErr
	}
	return appliedUpdates, upstreamModels.AutoApplyBlocked, nil
}

func (usecase Usecase) Execute(ctx context.Context, force bool, allowAutoApply bool, report func(processed, total int)) domain.ModelUpdateSummary {
	checkedChannels := 0
	failedChannels := 0
	failedChannelIDs := make([]int, 0)
	changedChannels := 0
	detectedAddModels := 0
	detectedRemoveModels := 0
	autoAddedModels := 0
	autoRemovedModels := 0
	channelSummaries := make([]domain.ModelUpdateChannelSummary, 0)
	addModelSamples := make([]string, 0)
	removeModelSamples := make([]string, 0)
	conflictModels := make([]domain.ModelUpdateConflict, 0)
	refreshNeeded := false
	channelQueryFailed := false

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
			channelQueryFailed = true
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

			settings, settingsErr := usecase.store.ModelUpdateState(channel)
			if settingsErr != nil {
				failedChannels++
				failedChannelIDs = append(failedChannelIDs, channel.Id)
				usecase.log(fmt.Sprintf("upstream model update settings invalid: channel_id=%d channel_name=%s err=%v", channel.Id, channel.Name, settingsErr))
				continue
			}
			if !settings.CheckEnabled {
				continue
			}

			checkedChannels++
			appliedUpdates, autoApplyBlocked, err := usecase.Check(ctx, channel, &settings, force, allowAutoApply, true)
			if err != nil {
				failedChannels++
				failedChannelIDs = append(failedChannelIDs, channel.Id)
				usecase.log(fmt.Sprintf("upstream model update check failed: channel_id=%d channel_name=%s err=%v", channel.Id, channel.Name, err))
				continue
			}
			if len(settings.LastConflictModels) > 0 {
				usecase.log(fmt.Sprintf("upstream model update conflicts: channel_id=%d channel_name=%s models=%v", channel.Id, channel.Name, settings.LastConflictModels))
			}
			currentAddModels := domain.NormalizeModelNames(settings.LastDetectedModels)
			currentRemoveModels := domain.NormalizeModelNames(settings.LastRemovedModels)
			if len(appliedUpdates.RemovalLimitCandidates) > 0 {
				usecase.log(fmt.Sprintf("upstream model update removal limit exceeded: channel_id=%d channel_name=%s candidates=%d limit=%d", channel.Id, channel.Name, len(appliedUpdates.RemovalLimitCandidates), domain.MaxAutoRemovedModels))
			}
			currentAddCount := len(currentAddModels) + len(appliedUpdates.AddedModels)
			currentRemoveCount := len(currentRemoveModels) + len(appliedUpdates.RemovedModels)
			detectedAddModels += currentAddCount
			detectedRemoveModels += currentRemoveCount
			if currentAddCount > 0 || currentRemoveCount > 0 {
				changedChannels++
				channelSummaries = append(channelSummaries, domain.ModelUpdateChannelSummary{
					ChannelID:              channel.Id,
					ChannelName:            channel.Name,
					AddCount:               currentAddCount,
					RemoveCount:            currentRemoveCount,
					PendingAddModels:       currentAddModels,
					PendingRemoveModels:    currentRemoveModels,
					AutoApplyBlocked:       autoApplyBlocked,
					AutoAddedModels:        appliedUpdates.AddedModels,
					AutoRemovedModels:      appliedUpdates.RemovedModels,
					RemovalLimitCandidates: appliedUpdates.RemovalLimitCandidates,
				})
			}
			addModelSamples = domain.MergeModelNames(addModelSamples, currentAddModels)
			removeModelSamples = domain.MergeModelNames(removeModelSamples, currentRemoveModels)
			for _, conflictModel := range domain.NormalizeModelNames(settings.LastConflictModels) {
				conflictModels = append(conflictModels, domain.ModelUpdateConflict{ChannelID: channel.Id, ChannelName: channel.Name, Model: conflictModel})
			}
			if appliedUpdates.ModelsChanged || appliedUpdates.MappingChanged {
				refreshNeeded = true
			}
			autoAddedModels += len(appliedUpdates.AddedModels)
			autoRemovedModels += len(appliedUpdates.RemovedModels)

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
		AutoRemovedModels:    autoRemovedModels,
	}

	if checkedChannels > 0 || usecase.debug() {
		usecase.log(fmt.Sprintf(
			"upstream model update task done: checked_channels=%d changed_channels=%d detected_add_models=%d detected_remove_models=%d failed_channels=%d auto_added_models=%d auto_removed_models=%d",
			checkedChannels,
			changedChannels,
			detectedAddModels,
			detectedRemoveModels,
			failedChannels,
			autoAddedModels,
			autoRemovedModels,
		))
	}
	// A scan that stopped early, cancelled or on a failed channel query, has
	// not seen every channel: its report is not sent and the fingerprint of the
	// last delivered report stays.
	if channelQueryFailed || (ctx != nil && ctx.Err() != nil) {
		return summary
	}
	sentFingerprint, err := usecase.sentReports.ReadOption(ctx, sentReportFingerprintKey)
	if err != nil {
		usecase.log(fmt.Sprintf("upstream model update report fingerprint read failed, the report is sent: %v", err))
	}
	// Conflicts need an admin decision, so they are reported even when the
	// scan found nothing else. A scan without findings clears the fingerprint,
	// so a problem that comes back is reported again.
	if changedChannels == 0 && failedChannels == 0 && len(conflictModels) == 0 {
		if sentFingerprint != "" {
			usecase.saveSentReportFingerprint(ctx, "")
		}
		return summary
	}
	modelUpdateReport := domain.ModelUpdateReport{
		CheckedChannels:      checkedChannels,
		ChangedChannels:      changedChannels,
		DetectedAddModels:    detectedAddModels,
		DetectedRemoveModels: detectedRemoveModels,
		AutoAddedModels:      autoAddedModels,
		AutoRemovedModels:    autoRemovedModels,
		FailedChannelIDs:     failedChannelIDs,
		Channels:             channelSummaries,
		AddModelSamples:      addModelSamples,
		RemoveModelSamples:   removeModelSamples,
		ConflictModels:       conflictModels,
	}
	// A report with applied changes is always sent: the fingerprint holds only
	// what waits for a decision, and an applied change does not repeat.
	appliedChanges := autoAddedModels > 0 || autoRemovedModels > 0
	reportFingerprint := domain.ModelUpdateReportFingerprint(modelUpdateReport)
	if !appliedChanges && reportFingerprint == sentFingerprint {
		usecase.log(fmt.Sprintf("upstream model update notification skipped: report unchanged fingerprint=%s", reportFingerprint[:12]))
		return summary
	}
	if err := usecase.notify(modelUpdateReport); err != nil {
		return summary
	}
	usecase.saveSentReportFingerprint(ctx, reportFingerprint)
	return summary
}

func (usecase Usecase) saveSentReportFingerprint(ctx context.Context, fingerprint string) {
	err := usecase.sentReports.SaveBulk(ctx, []string{sentReportFingerprintKey}, map[string]string{sentReportFingerprintKey: fingerprint})
	if err != nil {
		usecase.log(fmt.Sprintf("upstream model update report fingerprint save failed: %v", err))
	}
}
