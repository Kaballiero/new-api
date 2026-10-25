package controller

import (
	channeladapter "github.com/QuantumNous/new-api/internal/adapter/channels"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"sync"
)

const (
	channelUpstreamModelUpdateTaskBatchSize               = 100
	channelUpstreamModelUpdateMinCheckIntervalSeconds     = 300
	channelUpstreamModelUpdateNotifySuppressWindowSeconds = 86400
	channelUpstreamModelUpdateNotifyMaxChannelDetails     = 8
	channelUpstreamModelUpdateNotifyMaxModelDetails       = 12
	channelUpstreamModelUpdateNotifyMaxFailedChannelIDs   = 10
)

var fetchChannelUpstreamModelIDsFn = fetchChannelUpstreamModelIDs
var channelUpstreamModelUpdateNotifyState = struct {
	sync.Mutex
	lastNotifiedAt      int64
	lastChangedChannels int
	lastFailedChannels  int
}{}

type channelUpstreamModelFetchFingerprint = domain.ModelFetchFingerprint
type upstreamModelUpdateChannelSummary = domain.ModelUpdateChannelSummary
type appliedChannelUpstreamModelUpdates = domain.AppliedModelUpdates
type upstreamModelUpdateSummary = domain.ModelUpdateSummary

func normalizeModelNames(models []string) []string     { return domain.NormalizeModelNames(models) }
func mergeModelNames(base, appended []string) []string { return domain.MergeModelNames(base, appended) }
func subtractModelNames(base, removed []string) []string {
	return domain.SubtractModelNames(base, removed)
}
func intersectModelNames(base, allowed []string) []string {
	return domain.IntersectModelNames(base, allowed)
}
func applySelectedModelChanges(originModels, addModels, removeModels []string) []string {
	return domain.ApplySelectedModelChanges(originModels, addModels, removeModels)
}
func normalizeChannelModelMapping(channel *model.Channel) (map[string]string, error) {
	return app.NewChannelModelUpdateRules(repository.NewStore()).NormalizeChannelModelMapping(repository.ToDomain(channel))
}
func collectPendingUpstreamModelChangesFromModels(local, upstream, ignored []string, mapping map[string]string) ([]string, []string, error) {
	return app.NewChannelModelUpdateRules(repository.NewStore()).CollectPendingUpstreamModelChangesFromModels(local, upstream, ignored, mapping)
}

func collectPendingUpstreamModelChanges(channel *model.Channel, settings dto.ChannelOtherSettings) ([]string, []string, error) {
	upstream, err := fetchChannelUpstreamModelIDs(channel)
	if err != nil {
		return nil, nil, err
	}
	mapping, err := normalizeChannelModelMapping(channel)
	if err != nil {
		return nil, nil, err
	}
	return collectPendingUpstreamModelChangesFromModels(channel.GetModels(), upstream, settings.UpstreamModelUpdateIgnoredModels, mapping)
}
func parseOpenAIModelIDs(body []byte) ([]string, error) {
	return channeladapter.ParseOpenAIModelIDs(body)
}
func sanitizeFetchModelsError(err error, key string) error {
	return channeladapter.SanitizeFetchModelsError(err, key)
}
func sanitizeAdvancedCustomRequestError(err error, key, requestURL string) error {
	return channeladapter.SanitizeAdvancedCustomRequestError(err, key, requestURL)
}
func fetchChannelUpstreamModelIDs(channel *model.Channel) ([]string, error) {
	return channeladapter.FetchChannelUpstreamModelIDs(channel)
}
func buildChannelUpstreamModelFetchFingerprint(channel *model.Channel) (channelUpstreamModelFetchFingerprint, error) {
	return repository.NewStore().FetchFingerprint(repository.ToDomain(channel))
}
func checkAndPersistChannelUpstreamModelUpdates(channel *model.Channel, settings *dto.ChannelOtherSettings, force, auto, enabled bool) (bool, int, error) {
	value := repository.ToDomain(channel)
	state := modelUpdateState(*settings)
	changed, added, err := app.NewDetectChannelModelsUsecase(channelEffects()).Check(value, &state, force, auto, enabled)
	*channel = *repository.FromDomain(value)
	settings.UpstreamModelUpdateCheckEnabled = state.CheckEnabled
	settings.UpstreamModelUpdateAutoSyncEnabled = state.AutoSyncEnabled
	settings.UpstreamModelUpdateLastCheckTime = state.LastCheckTime
	settings.UpstreamModelUpdateLastDetectedModels = state.LastDetectedModels
	settings.UpstreamModelUpdateLastRemovedModels = state.LastRemovedModels
	settings.UpstreamModelUpdateIgnoredModels = state.IgnoredModels
	return changed, added, err
}
func modelUpdateState(settings dto.ChannelOtherSettings) domain.ModelUpdateState {
	return domain.ModelUpdateState{CheckEnabled: settings.UpstreamModelUpdateCheckEnabled, AutoSyncEnabled: settings.UpstreamModelUpdateAutoSyncEnabled, LastCheckTime: settings.UpstreamModelUpdateLastCheckTime, LastDetectedModels: settings.UpstreamModelUpdateLastDetectedModels, LastRemovedModels: settings.UpstreamModelUpdateLastRemovedModels, IgnoredModels: settings.UpstreamModelUpdateIgnoredModels}
}
func refreshChannelRuntimeCache() { app.RefreshChannelRuntimeCache(enqueuePricingCatalogRefresh) }
func shouldSendUpstreamModelUpdateNotification(now int64, changedChannels int, failedChannels int) bool {
	if changedChannels <= 0 && failedChannels <= 0 {
		return true
	}

	channelUpstreamModelUpdateNotifyState.Lock()
	defer channelUpstreamModelUpdateNotifyState.Unlock()

	if domain.RepeatedModelUpdateNotification(now, changedChannels, failedChannels, channelUpstreamModelUpdateNotifyState.lastNotifiedAt, channelUpstreamModelUpdateNotifyState.lastChangedChannels, channelUpstreamModelUpdateNotifyState.lastFailedChannels, channelUpstreamModelUpdateNotifySuppressWindowSeconds) {
		return false
	}

	channelUpstreamModelUpdateNotifyState.lastNotifiedAt = now
	channelUpstreamModelUpdateNotifyState.lastChangedChannels = changedChannels
	channelUpstreamModelUpdateNotifyState.lastFailedChannels = failedChannels
	return true
}
func buildUpstreamModelUpdateTaskNotificationContent(lang string, checked, changed, added, removed, auto int, failed []int, summaries []upstreamModelUpdateChannelSummary, addSamples, removeSamples []string) string {
	_, content := channeladapter.ModelUpdateNotification(lang, domain.ModelUpdateReport{CheckedChannels: checked, ChangedChannels: changed, DetectedAddModels: added, DetectedRemoveModels: removed, AutoAddedModels: auto, FailedChannelIDs: failed, Channels: summaries, AddModelSamples: addSamples, RemoveModelSamples: removeSamples})
	return content
}
func ApplyChannelUpstreamModelUpdates(c *gin.Context) {
	channelsHTTP().ApplyUpstreamModels.Handle(c)
}
func ApplyAllChannelUpstreamModelUpdates(c *gin.Context) {
	channelsHTTP().ApplyAllUpstreamModels.Handle(c)
}
func DetectChannelUpstreamModelUpdates(c *gin.Context) {
	channelsHTTP().DetectUpstreamModels.Handle(c)
}
func DetectAllChannelUpstreamModelUpdates(c *gin.Context) {
	channelsHTTP().DetectAllUpstreamModels.Handle(c)
}

func applyChannelUpstreamModelUpdatesWithMode(channel *model.Channel, add, ignore, remove []string, all, enabled bool) (appliedChannelUpstreamModelUpdates, error) {
	value := repository.ToDomain(channel)
	result, err := app.NewApplyChannelModelsUsecase(channelEffects(), nil).Apply(value, add, ignore, remove, all, enabled)
	*channel = *repository.FromDomain(value)
	return result, err
}
func collectPendingApplyUpstreamModelChanges(settings dto.ChannelOtherSettings) ([]string, []string) {
	return domain.CollectPendingApplyUpstreamModelChanges(modelUpdateState(settings))
}
func applyChannelUpstreamModelUpdates(
	channel *model.Channel,
	addModelsInput []string,
	ignoreModelsInput []string,
	removeModelsInput []string,
) (
	addedModels []string,
	removedModels []string,
	remainingModels []string,
	remainingRemoveModels []string,
	modelsChanged bool,
	err error,
) {
	result, err := applyChannelUpstreamModelUpdatesWithMode(
		channel,
		addModelsInput,
		ignoreModelsInput,
		removeModelsInput,
		false,
		false,
	)
	return result.AddedModels,
		result.RemovedModels,
		result.RemainingModels,
		result.RemainingRemoveModels,
		result.ModelsChanged,
		err
}
