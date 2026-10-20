package app

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	metadataupstream "github.com/QuantumNous/new-api/internal/adapter/catalog/upstream"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	pricingupstream "github.com/QuantumNous/new-api/internal/adapter/pricing/upstream"
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
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/catalogsync"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"time"
)

func NewPricingProjection() *domain.Projection {
	return domain.NewProjection(domain.ProjectionRuntime{
		UsableGroups: service.GetUserUsableGroups, GroupRatio: service.GetUserGroupRatio,
		AutoGroups: service.GetUserAutoGroup, ApplyFX: service.ApplyBillingFX,
		QuotaUnit:     func() float64 { return common.QuotaPerUnit },
		ApplyOverride: pricingupstream.ApplyOverride,
	})
}

var pricingConsumerCache pricingadapter.ConsumerCache

func newPricingReader() pricingadapter.Reader {
	return pricingadapter.Reader{SnapshotEndpointsRead: model.GetPricingCatalogueSnapshotWithEndpoints, GroupRatiosRead: ratio_setting.GetGroupRatioCopy, GroupGroupRatioRead: ratio_setting.GetGroupGroupRatio,
		SnapshotRead: model.GetPricingCatalogueSnapshot, FXRead: func() (domain.BillingFXBasis, error) {
			v, err := service.CurrentBillingFXBasis()
			return domain.BillingFXBasis(v), err
		}, AutoGroupsRead: service.GetUserAutoGroup, UsableGroupsRead: service.GetUserUsableGroups, GroupRatioRead: service.GetUserGroupRatio, UserGroupRead: func(id int) (string, error) {
			u, err := model.GetUserCache(id)
			if err != nil {
				return "", err
			}
			return u.Group, nil
		}, ContainsGroupRead: ratio_setting.ContainsGroupRatio,
	}
}
func NewGetEffectivePricingHTTP() get_personal_pricing.HTTP {
	return get_personal_pricing.NewHTTP(get_personal_pricing.NewUsecase(newPricingReader(), NewPricingProjection(), &pricingConsumerCache, newPricingReader()))
}
func NewGetEffectivePricingByGroupHTTP() get_group_pricing.HTTP {
	return get_group_pricing.NewHTTP(get_group_pricing.NewUsecase(newPricingReader(), NewPricingProjection(), &pricingConsumerCache, newPricingReader()))
}
func NewGetEffectivePricingModelHTTP() get_model_pricing.HTTP {
	return get_model_pricing.NewHTTP(get_model_pricing.NewUsecase(newPricingReader(), NewPricingProjection(), newPricingReader()))
}
func NewGetPublicPricingModelsHTTP() get_public_models.HTTP {
	return get_public_models.NewHTTP(get_public_models.NewUsecase(newPricingReader(), NewPricingProjection()))
}
func NewGetEffectivePricingOverviewHTTP() get_pricing_overview.HTTP {
	return get_pricing_overview.NewHTTP(get_pricing_overview.NewUsecase(newPricingReader(), NewPricingProjection(), newPricingReader()))
}

func NewGetPricingHTTP() get_pricing.HTTP {
	return get_pricing.NewHTTP(get_pricing.NewUsecase(newPricingReader()))
}
func NewGetEffectivePricingPreviewHTTP() preview_pricing.HTTP {
	return preview_pricing.NewHTTP(preview_pricing.NewUsecase(newPricingReader(), NewPricingProjection()))
}
func NewGetRatioConfigHTTP() get_ratio_config.HTTP {
	return get_ratio_config.NewHTTP(get_ratio_config.NewUsecase(pricingadapter.RatioSettings{Expose: ratio_setting.IsExposeRatioEnabled, Data: func() map[string]any {
		return billing_setting.GetPricingSyncData(map[string]any(ratio_setting.GetExposedData()))
	}}))
}
func NewResetModelRatioHTTP() reset_model_ratios.HTTP {
	return reset_model_ratios.NewHTTP(reset_model_ratios.NewUsecase(pricingadapter.RatioSettings{Defaults: ratio_setting.DefaultModelRatio2JSONString, Write: model.UpdateOption, Update: ratio_setting.UpdateModelRatioByJSONString}))
}
func NewGetModelPricingConfigHTTP() get_pricing_config.HTTP {
	return get_pricing_config.NewHTTP(get_pricing_config.NewUsecase(model.NewPricingOptionsPersistence(), model.NewPricingConfiguration(), model.NewPricingUsageSchemas))
}
func NewUpdateModelPricingConfigHTTP(audit func([]string)) update_pricing_config.HTTP {
	return update_pricing_config.NewHTTP(update_pricing_config.NewUsecase(model.NewPricingOptionsPersistence(), model.NewPricingConfiguration(), func() error { return model.ErrModelPricingConflict }, common.SysError, model.RefreshPricing, ratio_setting.InvalidateExposedDataCache, audit), func() error { return model.ErrModelPricingConflict })
}

func newPricingRatioChannels() pricingadapter.RatioChannels {
	return pricingadapter.RatioChannels{ReadIDs: func(ids []int) ([]fetch_upstream_ratios.Channel, error) {
		rows, err := model.GetChannelsByIds(ids)
		if err != nil {
			return nil, err
		}
		var out []fetch_upstream_ratios.Channel
		for _, ch := range rows {
			out = append(out, fetch_upstream_ratios.Channel{ID: ch.Id, Name: ch.Name, BaseURL: ch.GetBaseURL()})
		}
		return out, nil
	}, ReadAll: func() ([]dto.SyncableChannel, error) {
		rows, err := model.GetAllChannels(0, 0, true, false)
		if err != nil {
			return nil, err
		}
		var out []dto.SyncableChannel
		for _, ch := range rows {
			out = append(out, dto.SyncableChannel{ID: ch.Id, Name: ch.Name, BaseURL: ch.GetBaseURL(), Status: ch.Status, Type: ch.Type})
		}
		return out, nil
	}}
}
func NewFetchUpstreamRatiosHTTP() fetch_upstream_ratios.HTTP {
	return fetch_upstream_ratios.NewHTTP(fetch_upstream_ratios.NewUsecase(newPricingRatioChannels(), pricingupstream.RatioClients{HTTP: metadataupstream.NewHTTPClient, Warn: logger.LogWarn, Key: func(id int) (string, error) {
		ch, err := model.GetChannelById(id, true)
		if err != nil {
			return "", fmt.Errorf("failed to get channel key: %w", err)
		}
		key, _, apiErr := ch.GetNextEnabledKey()
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
	}, logger.LogError, logger.LogWarn), common.SysError)
}
func NewGetSyncableChannelsHTTP() list_syncable_channels.HTTP {
	return list_syncable_channels.NewHTTP(list_syncable_channels.NewUsecase(newPricingRatioChannels()))
}

func NewPricingNativeNormalizer() *domain.NativeNormalizer {
	return pricingupstream.NewNativeNormalizer()
}
func NewRefreshPricingCatalogue() refresh_catalogue.Usecase {
	return refresh_catalogue.NewUsecase(pricingupstream.NativeStore{}, func() (domain.CatalogSource, bool, error) {
		v, on, err := model.ConfiguredCatalogSource()
		return domain.CatalogSource(v), on, err
	}, func(ctx context.Context, source domain.CatalogSource, requested map[string]bool) (domain.CatalogSyncSummary, error) {
		client, err := catalogsync.NewClient(source.BaseURL, service.GetHttpClient())
		if err != nil {
			return domain.CatalogSyncSummary{}, err
		}
		v, err := sync_upstream_catalog.NewUsecase(model.NewImmutableCatalogPersistence(), func() error { return model.ErrCatalogSourceChanged }, time.Now, model.InvalidatePricingCache).Execute(ctx, source, requested, client)
		return domain.CatalogSyncSummary(v), err
	}, model.PublishPricingCatalogContext, NewPricingNativeNormalizer(), billing_setting.GetBatchBillingExpr, time.Now)
}

func NewEnqueuePricingCatalogueRefresh() enqueue_catalogue_refresh.Usecase {
	return enqueue_catalogue_refresh.NewUsecase(func() bool { return common.GetEnvOrDefaultBool("PRICING_CATALOG_TASK_ENABLED", true) }, func() error {
		_, _, err := service.EnqueueSystemTask(model.SystemTaskTypePricingCatalog, nil)
		return err
	}, common.SysLog)
}
