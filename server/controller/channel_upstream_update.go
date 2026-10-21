package controller

import (
	"context"
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
	channelUpstreamModelUpdateTaskDefaultIntervalMinutes  = 30
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
	fp, err := domain.BuildModelFetchFingerprint(repository.ToDomain(channel))
	if err != nil {
		return fp, err
	}
	fp.AdvancedCustom = channel.GetOtherSettings().AdvancedCustom
	return fp, nil
}
func checkAndPersistChannelUpstreamModelUpdates(channel *model.Channel, settings *dto.ChannelOtherSettings, force, auto, enabled bool) (bool, int, error) {
	value := repository.ToDomain(channel)
	changed, added, err := app.NewDetectChannelModelsUsecase(channelEffects()).Check(value, settings, force, auto, enabled)
	*channel = *repository.FromDomain(value)
	return changed, added, err
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
func buildUpstreamModelUpdateTaskNotificationContent(checked, changed, added, removed, auto int, failed []int, summaries []upstreamModelUpdateChannelSummary, addSamples, removeSamples []string) string {
	return domain.ModelUpdateNotificationContent(checked, changed, added, removed, auto, failed, summaries, addSamples, removeSamples)
}
func runChannelUpstreamModelUpdateTaskOnce(ctx context.Context, force, auto bool, report func(int, int)) upstreamModelUpdateSummary {
	return app.NewScanChannelModelsUsecase(channelEffects()).Execute(ctx, force, auto, report)
}
func ApplyChannelUpstreamModelUpdates(c *gin.Context) {
	app.NewApplyChannelUpstreamModelUpdatesHTTP(c, channelEffects()).Handle(c)
}
func ApplyAllChannelUpstreamModelUpdates(c *gin.Context) {
	app.NewApplyAllChannelUpstreamModelUpdatesHTTP(c, channelEffects()).Handle(c)
}
func DetectChannelUpstreamModelUpdates(c *gin.Context) {
	app.NewDetectChannelUpstreamModelUpdatesHTTP(c, channelEffects()).Handle(c)
}
func DetectAllChannelUpstreamModelUpdates(c *gin.Context) {
	app.NewDetectAllChannelUpstreamModelUpdatesHTTP(c, channelEffects()).Handle(c)
}

func applyChannelUpstreamModelUpdatesWithMode(channel *model.Channel, add, ignore, remove []string, all, enabled bool) (appliedChannelUpstreamModelUpdates, error) {
	value := repository.ToDomain(channel)
	result, err := app.NewApplyChannelModelsUsecase(nil, channelEffects()).Apply(value, add, ignore, remove, all, enabled)
	*channel = *repository.FromDomain(value)
	return result, err
}
func collectPendingApplyUpstreamModelChanges(settings dto.ChannelOtherSettings) ([]string, []string) {
	return domain.CollectPendingApplyUpstreamModelChanges(settings)
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
