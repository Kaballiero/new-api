package app

import (
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"

	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	pricingruntime "github.com/QuantumNous/new-api/internal/app/pricingruntime"

	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"

	accountssettings "github.com/QuantumNous/new-api/internal/adapter/settings/accounts"

	metadataupstream "github.com/QuantumNous/new-api/internal/adapter/catalog/upstream"

	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"

	pricingupstream "github.com/QuantumNous/new-api/internal/adapter/pricing/upstream"

	billingapp "github.com/QuantumNous/new-api/internal/app/billing"

	settingsapp "github.com/QuantumNous/new-api/internal/app/settings"

	systemcore "github.com/QuantumNous/new-api/internal/app/system"

	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/enqueue_catalogue_refresh"
	"github.com/QuantumNous/new-api/internal/pricing/fetch_upstream_ratios"
	"github.com/QuantumNous/new-api/internal/pricing/get_group_pricing"
	"github.com/QuantumNous/new-api/internal/pricing/get_model_pricing"
	"github.com/QuantumNous/new-api/internal/pricing/get_personal_pricing"
	"github.com/QuantumNous/new-api/internal/pricing/get_pricing"
	"github.com/QuantumNous/new-api/internal/pricing/get_pricing_config"
	"github.com/QuantumNous/new-api/internal/pricing/get_pricing_overview"
	"github.com/QuantumNous/new-api/internal/pricing/get_public_models"
	"github.com/QuantumNous/new-api/internal/pricing/get_ratio_config"
	"github.com/QuantumNous/new-api/internal/pricing/list_syncable_channels"
	"github.com/QuantumNous/new-api/internal/pricing/preview_pricing"
	"github.com/QuantumNous/new-api/internal/pricing/refresh_catalogue"
	"github.com/QuantumNous/new-api/internal/pricing/reset_model_ratios"
	"github.com/QuantumNous/new-api/internal/pricing/sync_upstream_catalog"
	"github.com/QuantumNous/new-api/internal/pricing/update_pricing_config"
	"github.com/QuantumNous/new-api/internal/system/enqueue_system_task"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/catalogsync"

	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func NewPricingProjection() *domain.Projection {
	return domain.NewProjection(domain.ProjectionRuntime{Capabilities: pricingadapter.Capabilities(),
		UsableGroups: accountssettings.GetUserUsableGroups, GroupRatio: accountssettings.GroupRatio,
		AutoGroups: accountssettings.GetUserAutoGroup, ApplyFX: billingdomain.ApplyBillingFX,
		QuotaUnit:     func() float64 { return common.QuotaPerUnit },
		ApplyOverride: pricingupstream.ApplyOverride,
	})
}

var pricingConsumerCache pricingadapter.ConsumerCache

func newPricingReader() pricingadapter.Reader {
	return pricingadapter.Reader{SnapshotEndpointsRead: pricingruntime.GetPricingCatalogueSnapshotWithEndpoints, GroupRatiosRead: ratio_setting.GetGroupRatioCopy, GroupGroupRatioRead: ratio_setting.GetGroupGroupRatio,
		SnapshotRead: pricingruntime.GetPricingCatalogueSnapshot, FXRead: func() (domain.BillingFXBasis, error) {
			v, err := billingapp.NewCosts().FX.Execute(billingdomain.FXBasis{})
			return domain.BillingFXBasis(v), err
		}, AutoGroupsRead: accountssettings.GetUserAutoGroup, UsableGroupsRead: accountssettings.GetUserUsableGroups, GroupRatioRead: accountssettings.GroupRatio, UserGroupRead: func(id int) (string, error) {
			u, err := accountstorage.Cache().GetUserCache(id)
			if err != nil {
				return "", err
			}
			return u.Group, nil
		}, ContainsGroupRead: ratio_setting.ContainsGroupRatio,
	}
}

// PricingEffects records the management audit events of pricing changes.
type PricingEffects struct {
	Audit func(c *gin.Context, action string, details map[string]any)
}

// PricingHTTP holds the handlers of the pricing reads, the pricing
// configuration and the upstream ratio sync.
type PricingHTTP struct {
	GetPricing           get_pricing.HTTP
	GetPersonalPricing   get_personal_pricing.HTTP
	GetGroupPricing      get_group_pricing.HTTP
	GetModelPricing      get_model_pricing.HTTP
	GetPublicModels      get_public_models.HTTP
	GetPricingOverview   get_pricing_overview.HTTP
	PreviewPricing       preview_pricing.HTTP
	GetRatioConfig       get_ratio_config.HTTP
	ResetModelRatios     reset_model_ratios.HTTP
	GetPricingConfig     get_pricing_config.HTTP
	UpdatePricingConfig  update_pricing_config.HTTP
	FetchUpstreamRatios  fetch_upstream_ratios.HTTP
	ListSyncableChannels list_syncable_channels.HTTP
}

// NewPricingHTTP builds the pricing handlers. Settings, the catalog snapshot
// and the user group are read per request by the injected functions.
func NewPricingHTTP(effects PricingEffects) PricingHTTP {
	reader := newPricingReader()
	projection := NewPricingProjection()
	channels := newPricingRatioChannels()
	return PricingHTTP{
		GetPricing:         get_pricing.NewHTTP(get_pricing.NewUsecase(reader)),
		GetPersonalPricing: get_personal_pricing.NewHTTP(get_personal_pricing.NewUsecase(pricingadapter.Capabilities(), reader, projection, &pricingConsumerCache, reader), catalogsync.ModelTypes),
		GetGroupPricing:    get_group_pricing.NewHTTP(get_group_pricing.NewUsecase(pricingadapter.Capabilities(), reader, projection, &pricingConsumerCache, reader), catalogsync.ModelTypes),
		GetModelPricing:    get_model_pricing.NewHTTP(get_model_pricing.NewUsecase(pricingadapter.Capabilities(), reader, projection, reader)),
		GetPublicModels:    get_public_models.NewHTTP(get_public_models.NewUsecase(pricingadapter.Capabilities(), reader, projection), catalogsync.ModelTypes),
		GetPricingOverview: get_pricing_overview.NewHTTP(get_pricing_overview.NewUsecase(pricingadapter.Capabilities(), reader, projection, reader), catalogsync.ModelTypes),
		PreviewPricing:     preview_pricing.NewHTTP(preview_pricing.NewUsecase(reader, projection)),
		GetRatioConfig: get_ratio_config.NewHTTP(get_ratio_config.NewUsecase(pricingadapter.RatioSettings{Expose: ratio_setting.IsExposeRatioEnabled, Data: func() map[string]any {
			return billing_setting.GetPricingSyncData(map[string]any(ratio_setting.GetExposedData()))
		}})),
		ResetModelRatios: reset_model_ratios.NewHTTP(reset_model_ratios.NewUsecase(pricingadapter.RatioSettings{Defaults: ratio_setting.DefaultModelRatio2JSONString, Write: settingsapp.Default().Write.Execute, Update: ratio_setting.UpdateModelRatioByJSONString})),
		GetPricingConfig: get_pricing_config.NewHTTP(pricingruntime.ModelPricingReader()),
		UpdatePricingConfig: update_pricing_config.NewHTTP(pricingadapter.Capabilities(), pricingruntime.NewPricingOptionsPersistence(), pricingruntime.NewPricingConfiguration(), common.SysError, pricingruntime.RefreshPricing, ratio_setting.InvalidateExposedDataCache, func(c *gin.Context, names []string) {
			effects.Audit(c, "model.pricing.update", map[string]any{"models": names})
		}),
		FetchUpstreamRatios:  newFetchUpstreamRatiosHTTP(channels),
		ListSyncableChannels: list_syncable_channels.NewHTTP(list_syncable_channels.NewUsecase(channels)),
	}
}

func newPricingRatioChannels() pricingadapter.RatioChannels {
	return pricingadapter.RatioChannels{ReadIDs: func(ids []int) ([]fetch_upstream_ratios.Channel, error) {
		rows, err := channelstore.GetChannelsByIds(ids)
		if err != nil {
			return nil, err
		}
		var out []fetch_upstream_ratios.Channel
		for _, ch := range rows {
			out = append(out, fetch_upstream_ratios.Channel{ID: ch.Id, Name: ch.Name, BaseURL: ch.GetBaseURL()})
		}
		return out, nil
	}, ReadAll: func() ([]list_syncable_channels.SyncableChannel, error) {
		rows, err := channelstore.GetAllChannels(0, 0, true, false)
		if err != nil {
			return nil, err
		}
		var out []list_syncable_channels.SyncableChannel
		for _, ch := range rows {
			out = append(out, list_syncable_channels.SyncableChannel{ID: ch.Id, Name: ch.Name, BaseURL: ch.GetBaseURL(), Status: ch.Status, Type: ch.Type})
		}
		return out, nil
	}}
}
func newFetchUpstreamRatiosHTTP(channels pricingadapter.RatioChannels) fetch_upstream_ratios.HTTP {
	return fetch_upstream_ratios.NewHTTP(fetch_upstream_ratios.NewUsecase(pricingadapter.Capabilities(), channels, pricingupstream.RatioClients{HTTP: metadataupstream.NewHTTPClient, Warn: logger.LogWarn, Key: func(id int) (string, error) {
		ch, err := channelstore.GetChannelById(id, true)
		if err != nil {
			return "", fmt.Errorf("failed to get channel key: %w", err)
		}
		key, _, apiErr := channelstore.GetNextEnabledKey(ch)
		if apiErr != nil {
			return "", fmt.Errorf("failed to get enabled channel key: %s", apiErr.Error())
		}
		return key, nil
	}}, func() map[string]any {
		data := billing_setting.GetPricingSyncData(map[string]any(ratio_setting.GetExposedData()))
		data["image_ratio"] = ratio_setting.GetImageRatioCopy()
		data["audio_ratio"] = ratio_setting.GetAudioRatioCopy()
		data["audio_completion_ratio"] = ratio_setting.GetAudioCompletionRatioCopy()
		return data
	}, logger.LogError, logger.LogWarn), common.SysError, pricingadapter.DecodeUpstreamRequest)
}

func NewPricingNativeNormalizer() *domain.NativeNormalizer {
	return pricingupstream.NewNativeNormalizer()
}
func NewRefreshPricingCatalogue() refresh_catalogue.Usecase {
	return refresh_catalogue.NewUsecase(pricingadapter.Capabilities(), pricingupstream.NativeStore{ReadPricing: pricingruntime.GetPricing}, func() (domain.CatalogSource, bool, error) {
		v, on, err := pricingstate.ConfiguredCatalogSource()
		return domain.CatalogSource(v), on, err
	}, func(ctx context.Context, source domain.CatalogSource, requested map[string]bool) (domain.CatalogSyncSummary, error) {
		client, err := catalogsync.NewClient(source.BaseURL, httpclient.GetHttpClient())
		if err != nil {
			return domain.CatalogSyncSummary{}, err
		}
		v, err := sync_upstream_catalog.NewUsecase(pricingadapter.Capabilities(), pricingstate.NewImmutableCatalogPersistence(), time.Now, pricingstate.InvalidatePricingCache).Execute(ctx, source, requested, pricingadapter.CatalogClient{Client: client})
		return domain.CatalogSyncSummary(v), err
	}, pricingruntime.PublishPricingCatalogContext, NewPricingNativeNormalizer(), billing_setting.GetBatchBillingExpr, time.Now)
}

func NewEnqueuePricingCatalogueRefresh() enqueue_catalogue_refresh.Usecase {
	return enqueue_catalogue_refresh.NewUsecase(func() bool { return common.GetEnvOrDefaultBool("PRICING_CATALOG_TASK_ENABLED", true) }, func() error {
		_, err := systemcore.Default().Enqueue.Execute(enqueue_system_task.Input{Type: model.SystemTaskTypePricingCatalog})
		return err
	}, common.SysLog)
}
