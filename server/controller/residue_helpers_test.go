package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channeladapter "github.com/QuantumNous/new-api/internal/adapter/channels"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	oauthadapter "github.com/QuantumNous/new-api/internal/adapter/identity/oauth"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	upstream "github.com/QuantumNous/new-api/internal/adapter/pricing/upstream"
	"github.com/QuantumNous/new-api/internal/apikeys/get_key_status"
	"github.com/QuantumNous/new-api/internal/app"
	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
	channelcore "github.com/QuantumNous/new-api/internal/app/channelcore"
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	auditdomain "github.com/QuantumNous/new-api/internal/audit/domain"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/preview_channel_models"
	"github.com/QuantumNous/new-api/internal/channels/update_channel"
	"github.com/QuantumNous/new-api/internal/channels/validate_models"
	"github.com/QuantumNous/new-api/internal/identity/start_oauth_flow"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/refresh_catalogue"
	pricingquery "github.com/QuantumNous/new-api/internal/pricing/transport"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

func writeAuthSessionError(c *gin.Context, err error) { identityErrors.WriteSessionError(c, err) }

type OpenAISubscriptionResponse = channeladapter.OpenAISubscriptionResponse

type OpenAIUsageDailyCost = channeladapter.OpenAIUsageDailyCost

type OpenAICreditGrants = channeladapter.OpenAICreditGrants

type OpenAIUsageResponse = channeladapter.OpenAIUsageResponse

type OpenAISBUsageResponse = channeladapter.OpenAISBUsageResponse

type AIProxyUserOverviewResponse = channeladapter.AIProxyUserOverviewResponse

type API2GPTUsageResponse = channeladapter.API2GPTUsageResponse

type APGC2DGPTUsageResponse = channeladapter.APGC2DGPTUsageResponse

type SiliconFlowUsageResponse = channeladapter.SiliconFlowUsageResponse

type DeepSeekUsageResponse = channeladapter.DeepSeekUsageResponse

type OpenRouterCreditResponse = channeladapter.OpenRouterCreditResponse

func deepSeekBalanceUSD(response DeepSeekUsageResponse) (float64, error) {
	return channeladapter.DeepSeekBalanceUSD(response)
}

// validateChannel applies the shared channel validation.
func validateChannel(channel *model.Channel, isAdd bool) error {
	value := repository.ToDomain(channel)
	err := domain.ValidateChannel(value, isAdd, repository.NewStore())
	if channel != nil {
		*channel = *repository.FromDomain(value)
	}
	return err
}

func isManageableChannelStatus(status int) bool {
	return status == common.ChannelStatusEnabled || status == common.ChannelStatusManuallyDisabled
}

func buildAdvancedCustomModelPreviewChannel(req FetchModelsRequest) (*model.Channel, error) {
	value, err := app.NewPreviewChannelModelsUsecase().Preview(preview_channel_models.FetchModelsRequest(req))
	return repository.FromDomain(value), err
}

var channelSensitiveFields = update_channel.SensitiveFields

var channelOperationalFields = update_channel.OperationalFields

var channelReadOnlyFields = update_channel.ReadOnlyFields

var channelNonSensitiveFields = update_channel.NonSensitiveFields

func channelHasSensitiveChanges(channel *PatchChannel, origin *model.Channel, fields map[string]any) bool {
	patch := update_channel.PatchChannel{Channel: *repository.ToDomain(&channel.Channel), MultiKeyMode: channel.MultiKeyMode, KeyMode: channel.KeyMode}
	return update_channel.HasSensitiveChanges(&patch, repository.ToDomain(origin), fields)
}

func clearChannelReadOnlyFields(channel *PatchChannel, fields map[string]any) {
	patch := update_channel.PatchChannel{Channel: *repository.ToDomain(&channel.Channel), MultiKeyMode: channel.MultiKeyMode, KeyMode: channel.KeyMode}
	update_channel.ClearReadOnlyFields(&patch, fields)
	channel.Channel = *repository.FromDomain(&patch.Channel)
}

type channelUpstreamModelFetchFingerprint = domain.ModelFetchFingerprint

type upstreamModelUpdateChannelSummary = domain.ModelUpdateChannelSummary

type appliedChannelUpstreamModelUpdates = domain.AppliedModelUpdates

type upstreamModelUpdateSummary = domain.ModelUpdateSummary

func normalizeModelNames(models []string) []string { return domain.NormalizeModelNames(models) }

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

func parseOpenAIModelIDs(body []byte) ([]string, error) {
	return channeladapter.ParseOpenAIModelIDs(body)
}

func sanitizeFetchModelsError(err error, key string) error {
	return channeladapter.SanitizeFetchModelsError(err, key)
}

func sanitizeAdvancedCustomRequestError(err error, key, requestURL string) error {
	return channeladapter.SanitizeAdvancedCustomRequestError(err, key, requestURL)
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

func buildUpstreamModelUpdateTaskNotificationContent(lang string, checked, changed, added, removed, auto int, failed []int, summaries []upstreamModelUpdateChannelSummary, addSamples, removeSamples []string) string {
	_, content := channeladapter.ModelUpdateNotification(lang, domain.ModelUpdateReport{CheckedChannels: checked, ChangedChannels: changed, DetectedAddModels: added, DetectedRemoveModels: removed, AutoAddedModels: auto, FailedChannelIDs: failed, Channels: summaries, AddModelSamples: addSamples, RemoveModelSamples: removeSamples})
	return content
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

func dedupeTrimModels(models []string) []string { return validate_models.NormalizeModels(models) }

type effectivePricingPreviewResponse struct {
	effectivePricingResponse
	AvailableUserGroups []string `json:"available_user_groups"`
}

type oauthStateRequest = start_oauth_flow.Request

type oauthFlowPayload = oauthadapter.FlowPayload

type passkeyFinishRequest struct {
	FlowToken  string          `json:"flow_token"`
	Credential json.RawMessage `json:"credential"`
}

func parsePasskeyFinishRequest(c *gin.Context) (*passkeyFinishRequest, error) {
	var request passkeyFinishRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		return nil, err
	}
	if request.FlowToken == "" || len(request.Credential) == 0 {
		return nil, errors.New("Passkey 流程参数不完整")
	}
	return &request, nil
}

type effectivePricingResponse = pricingdomain.EffectivePricingResponse

type effectiveGroupPricing = pricingdomain.EffectiveGroupPricing

type effectiveUnitPrice = pricingdomain.EffectiveUnitPrice

func buildEffectivePricingResponse(userGroup string, basis pricingdomain.BillingFXBasis, pricing []pricingdomain.Pricing) effectivePricingResponse {
	return app.NewPricingProjection().BuildEffectivePricingResponse(userGroup, basis, pricing)
}

func buildEffectiveGroupPricing(item pricingdomain.Pricing, group string, pureRatio, effectiveRatio float64) effectiveGroupPricing {
	return app.
		NewPricingProjection().BuildEffectiveGroupPricing(item, group, pureRatio, effectiveRatio)
}

type upstreamRate = pricingdomain.UpstreamRate

type consumerProviderPrice = pricingdomain.ConsumerProviderPrice

type consumerProviderOffer = pricingdomain.ConsumerProviderOffer

type pricingUserContext = pricingdomain.PricingUserContext

func catalogComponentName(rate catalogsync.Component) string {
	return pricingdomain.CatalogComponentName(pricingadapter.ToSourceRate(rate))
}

func upstreamProviderOffers(item pricingdomain.Pricing, group string, ratio float64, validRatio bool, user pricingUserContext) []consumerProviderOffer {
	return app.NewPricingProjection().UpstreamProviderOffers(item, group, ratio, validRatio,
		user)
}

func catalogMinima(offers []consumerProviderOffer) []consumerPricingOperation {
	return pricingdomain.CatalogMinima(pricingadapter.Capabilities(), offers)
}

func mergeCatalogMinima(existing, upstream []consumerPricingOperation) []consumerPricingOperation {
	return pricingdomain.MergeCatalogMinima(existing, upstream)
}

const pricingCatalogWorkers = 5

const pricingCatalogBodyLimit = 2 << 20

const pricingCatalogFailureLimit = 20

type pricingCatalogSource struct {
	channel     *model.Channel
	upstream    string
	endpoint    constant.EndpointType
	fingerprint string
}

type pricingCatalogRefreshSummary = pricingdomain.CatalogRefreshSummary

type pricingCatalogFailure = pricingdomain.CatalogRefreshFailure

func pricingNativeSource(v pricingCatalogSource) refresh_catalogue.Source {
	var channel refresh_catalogue.Channel
	if v.channel != nil {
		channel = upstream.NewNativeChannel(v.channel)
	}
	return refresh_catalogue.Source{Channel: channel, Upstream: v.upstream, Endpoint: v.endpoint, Fingerprint: v.fingerprint}
}

func pricingLegacySource(v refresh_catalogue.Source) pricingCatalogSource {
	var channel *model.Channel
	if v.Channel != nil {
		channel = v.Channel.(upstream.NativeChannel).Native()
	}
	return pricingCatalogSource{channel, v.Upstream, v.Endpoint, v.Fingerprint}
}

func pricingCatalogSources(ctx context.Context) ([]pricingCatalogSource, error) {
	rows, err := app.NewRefreshPricingCatalogue().DiscoverSources(ctx)
	if rows == nil {
		return nil, err
	}
	out := make([]pricingCatalogSource, len(rows))
	for i, v := range rows {
		out[i] = pricingLegacySource(v)
	}
	return out, err
}

func refreshPricingCatalogSources(ctx context.Context, sources []pricingCatalogSource, fetch func(context.Context, pricingCatalogSource) (pricingdomain.PricingCatalogEntry, error), publish func([]pricingdomain.PricingCatalogEntry) (int, error)) (pricingCatalogRefreshSummary, error) {
	var rows []refresh_catalogue.Source
	for _, v := range sources {
		rows = append(rows, pricingNativeSource(v))
	}
	return app.NewRefreshPricingCatalogue().RefreshSources(ctx, rows, func(ctx context.Context, v refresh_catalogue.Source) (pricingdomain.PricingCatalogEntry, error) {
		return fetch(ctx, pricingLegacySource(v))
	}, publish)
}

func fetchPricingCatalogSource(ctx context.Context, source pricingCatalogSource) (pricingdomain.PricingCatalogEntry, error) {
	return app.NewRefreshPricingCatalogue().FetchSource(ctx, pricingNativeSource(source))
}

func selectPricingCatalogOffers(offers []pricingdomain.CatalogOffer) []pricingdomain.CatalogOffer {
	return pricingdomain.SelectPricingCatalogOffers(offers)
}

func normalizeEndpointPricing(upstream, operation, main string, body []byte) ([]pricingdomain.CatalogOffer, error) {
	return app.NewPricingNativeNormalizer().NormalizeEndpointPricing(upstream, operation, main, body)
}

func normalizeImagePricing(upstream string, body []byte) ([]pricingdomain.CatalogOffer, error) {
	return app.NewPricingNativeNormalizer().NormalizeImagePricing(upstream, body)
}

// These fields describe the published endpoint schedule, not a request SKU.
type catalogPriceWindow = pricingdomain.CatalogPriceWindow

func catalogRateWindow(price consumerProviderPrice) (catalogPriceWindow, bool) {
	return pricingdomain.CatalogRateWindow(pricingadapter.Capabilities(), price)
}

type consumerPricingResponse = pricingdomain.ConsumerPricingResponse

type consumerPricingModel = pricingdomain.ConsumerPricingModel

type consumerGroupPricing = pricingdomain.ConsumerGroupPricing

type consumerPricingOperation = pricingdomain.ConsumerPricingOperation

type consumerUnitPrice = pricingdomain.ConsumerUnitPrice

func buildConsumerPricingModel(usableGroups map[string]string, groupRatios map[string]float64, fxFactor float64, item pricingdomain.Pricing, details bool, users ...pricingUserContext) consumerPricingModel {
	return app.NewPricingProjection().BuildConsumerPricingModel(usableGroups,
		groupRatios, fxFactor, item, details, users...)
}

func confirmedConsumerPrices(prices []consumerUnitPrice, tiers []publicPricingTier) bool {
	return pricingdomain.ConfirmedConsumerPrices(prices, tiers)
}

func consumerOperations(item pricingdomain.Pricing, group string, ratio float64, validRatio bool) []consumerPricingOperation {
	return pricingdomain.
		ConsumerOperations(item, group, ratio,
			validRatio)
}

type consumerPricingModelDetail = pricingdomain.ConsumerPricingModelDetail

func parseEffectivePricingModelQuery(values url.Values) (string, string, error) {
	return pricingquery.
		ParseEffectivePricingModelQuery(values)
}

func buildEffectivePricingModelDetail(context effectivePricingPageContext, name string) (consumerPricingModelDetail, bool) {
	context.Policy = app.NewPricingProjection()
	return app.
		NewPricingProjection().BuildEffectivePricingModelDetail(pricingdomain.EffectivePricingPageContext(context), name)
}

type effectivePricingCreator = pricingdomain.EffectivePricingCreator

type effectivePricingPageItem = pricingdomain.EffectivePricingPageItem

type effectivePricingPageData = pricingdomain.EffectivePricingPageData

type effectivePricingPageQuery = pricingdomain.EffectivePricingPageQuery

type effectivePricingPageContext pricingdomain.EffectivePricingPageContext

func parseEffectivePricingPageQuery(values url.Values, personal bool) (effectivePricingPageQuery, error) {
	return pricingquery.ParseEffectivePricingPageQuery(catalogsync.ModelTypes(), values, personal)
}

func buildEffectivePricingPage(context effectivePricingPageContext, query effectivePricingPageQuery, project func(pricingdomain.Pricing) consumerPricingModel) effectivePricingPageData {
	context.Policy = app.NewPricingProjection()
	return app.NewPricingProjection().BuildEffectivePricingPage(pricingdomain.EffectivePricingPageContext(context), query, project)
}

func compareEffectivePricingNames(left, right string) int {
	return pricingdomain.CompareEffectivePricingNames(left, right)
}

func effectivePricingModality(item pricingdomain.Pricing) string {
	return pricingdomain.EffectivePricingModality(item)
}

func effectivePricingPageKeys(item consumerPricingModel, orderBy string) (string, *float64) {
	return pricingdomain.EffectivePricingPageKeys(item, orderBy)
}

func trimPricingText(value string) string {
	return pricingdomain.TrimPricingText(value)
}

type effectivePricingOverviewData = pricingdomain.EffectivePricingOverviewData

func buildEffectivePricingOverview(context effectivePricingPageContext, query effectivePricingPageQuery) effectivePricingOverviewData {
	context.Policy = app.NewPricingProjection()
	return app.NewPricingProjection().BuildEffectivePricingOverview(pricingdomain.EffectivePricingPageContext(context), query)
}

func (context effectivePricingPageContext) project(item pricingdomain.Pricing) consumerPricingModel {
	context.Policy = app.NewPricingProjection()
	return pricingdomain.EffectivePricingPageContext(context).Project(item)
}

type publicPricingModelsPage = pricingdomain.PublicPricingModelsPage

type publicModelTariff = pricingdomain.PublicModelTariff

type publicPricingTier = pricingdomain.PublicPricingTier

type publicPricingCondition = pricingdomain.PublicPricingCondition

type publicPricingModelsQuery = pricingdomain.PublicPricingModelsQuery

func parsePublicPricingModelsQuery(raw string) (publicPricingModelsQuery, error) {
	return pricingquery.
		ParsePublicPricingModelsQuery(catalogsync.ModelTypes(), raw)
}

func buildPublicPricingModelsPage(context effectivePricingPageContext, query publicPricingModelsQuery) publicPricingModelsPage {
	context.Policy = app.NewPricingProjection()
	return app.NewPricingProjection().BuildPublicPricingModelsPage(pricingdomain.EffectivePricingPageContext(context), query)
}

func publicPricingGuard(guard billingexpr.TierCondition, units ...map[string]string) publicPricingCondition {
	return pricingdomain.PublicPricingGuard(pricingadapter.ToExpressionCondition(guard), units...)
}

func buildPublicModelTariff(context effectivePricingPageContext, item pricingdomain.Pricing) publicModelTariff {
	context.Policy = app.NewPricingProjection()
	return app.NewPricingProjection().BuildPublicModelTariff(pricingdomain.EffectivePricingPageContext(context), item)
}

func buildCatalogModelTariff(context effectivePricingPageContext, item pricingdomain.Pricing, group string) publicModelTariff {
	context.Policy = app.NewPricingProjection()
	return app.NewPricingProjection().BuildCatalogModelTariff(pricingdomain.EffectivePricingPageContext(context), item, group)
}

func expectedPricingRevision(values url.Values) (string, error) {
	return pricingquery.ExpectedPricingRevision(values)
}

func pricingRevisionMatches(c *gin.Context, expected, actual string) bool {
	if err := pricingdomain.CheckRevision(expected, actual); err != nil {
		pricingquery.WriteError(c, err)
		return false
	}
	return true
}

func consumerPricingRevision(source string, context effectivePricingPageContext) (string, error) {
	return pricingdomain.ConsumerPricingRevision(pricingadapter.Capabilities(), source, pricingdomain.EffectivePricingPageContext(context))
}

type effectivePricingTier = pricingdomain.EffectivePricingTier

// effectivePricingTierCondition describes what selects a tier without exposing
// expression text. Numeric kinds carry an exclusive MinValue and an inclusive
// MaxValue in tokens; time_of_day carries half-open [start, end) hour windows.
type effectivePricingTierCondition = pricingdomain.EffectivePricingTierCondition

type effectivePricingTimeWindow = pricingdomain.EffectivePricingTimeWindow

func valueMap(value any) map[string]any { return pricingdomain.ValueMap(value) }

func buildDifferences(localData map[string]any, channels []struct {
	name string
	data map[string]any
}) map[string]map[string]pricingdomain.RatioDifference {
	var values []pricingdomain.SyncChannel
	for _, ch := range channels {
		values = append(values, pricingdomain.SyncChannel{Name: ch.name, Data: ch.data})
	}
	return pricingdomain.BuildDifferences(localData, values)
}

func writeSecurityOperationError(c *gin.Context, err error) {
	identityErrors.WriteError(c, err)
}

var identityErrors = app.NewIdentityErrors()

var pricingHTTP = httpapi.Pricing

func recordLoginAudit(user *model.User, c *gin.Context) {
	auditapp.Recording().Login(c, auditdomain.Actor{ID: user.Id, Username: user.Username, Role: user.Role}, identityhttp.LoginMethodFromContext(c))
}

func recordUserSecurityAudit(c *gin.Context, id int, action string, params map[string]any) {
	auditapp.Recording().Security(c, id, action, params)
}

func recordManageAudit(c *gin.Context, action string, params map[string]any) {
	auditapp.Recording().Manage(c, c.GetInt("id"), action, params)
}

func setupLogin(user *model.User, c *gin.Context) { httpapi.Identity().PrimaryLogin.Start(user, c) }

var fetchChannelUpstreamModelIDsFn = func(channel *model.Channel) ([]string, error) {
	return channeladapter.FetchChannelUpstreamModelIDs(channel, channelcore.FetchCodexChannelModels)
}

var channelUpstreamModelUpdateNotifyState = &channeladapter.ModelUpdateNotifications{}

func shouldSendUpstreamModelUpdateNotification(now int64, changed, failed int) bool {
	return channelUpstreamModelUpdateNotifyState.Allow(now, changed, failed)
}

func channelEffects() app.ChannelEffects {
	effects := httpapi.ChannelEffects()
	effects.FetchModels = func(channel *model.Channel) ([]string, error) { return fetchChannelUpstreamModelIDsFn(channel) }
	effects.ModelUpdateThrottle = shouldSendUpstreamModelUpdateNotification
	return effects
}

var channelsHTTP = sync.OnceValue(func() app.ChannelsHTTP { return app.NewChannelsHTTP(channelEffects()) })

func fetchChannelUpstreamModelIDs(channel *model.Channel) ([]string, error) {
	return channeladapter.FetchChannelUpstreamModelIDs(channel, channelcore.FetchCodexChannelModels)
}

func GetTokenStatus(c *gin.Context) { get_key_status.NewHTTP(tokenStoreFixture()).Handle(c) }
