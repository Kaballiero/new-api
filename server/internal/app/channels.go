package app

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	channeladapter "github.com/QuantumNous/new-api/internal/adapter/channels"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/channels/apply_all_upstream_model_updates"
	"github.com/QuantumNous/new-api/internal/channels/apply_upstream_model_updates"
	"github.com/QuantumNous/new-api/internal/channels/clear_affinity_cache"
	"github.com/QuantumNous/new-api/internal/channels/copy_channel"
	"github.com/QuantumNous/new-api/internal/channels/create_channel"
	"github.com/QuantumNous/new-api/internal/channels/delete_channel"
	"github.com/QuantumNous/new-api/internal/channels/delete_channels_batch"
	"github.com/QuantumNous/new-api/internal/channels/delete_disabled_channels"
	"github.com/QuantumNous/new-api/internal/channels/delete_ollama_model"
	"github.com/QuantumNous/new-api/internal/channels/detect_all_upstream_model_updates"
	"github.com/QuantumNous/new-api/internal/channels/detect_upstream_model_updates"
	"github.com/QuantumNous/new-api/internal/channels/disable_tag_channels"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/edit_tag_channels"
	"github.com/QuantumNous/new-api/internal/channels/enable_tag_channels"
	"github.com/QuantumNous/new-api/internal/channels/fetch_upstream_models"
	"github.com/QuantumNous/new-api/internal/channels/get_affinity_cache_stats"
	"github.com/QuantumNous/new-api/internal/channels/get_affinity_usage_stats"
	"github.com/QuantumNous/new-api/internal/channels/get_channel"
	"github.com/QuantumNous/new-api/internal/channels/get_channel_operations"
	"github.com/QuantumNous/new-api/internal/channels/get_codex_reset_credits"
	"github.com/QuantumNous/new-api/internal/channels/get_codex_usage"
	"github.com/QuantumNous/new-api/internal/channels/get_ollama_version"
	"github.com/QuantumNous/new-api/internal/channels/get_tag_models"
	"github.com/QuantumNous/new-api/internal/channels/list_channel_models"
	"github.com/QuantumNous/new-api/internal/channels/list_channels"
	"github.com/QuantumNous/new-api/internal/channels/list_enabled_channel_models"
	"github.com/QuantumNous/new-api/internal/channels/manage_multi_keys"
	"github.com/QuantumNous/new-api/internal/channels/preview_channel_models"
	"github.com/QuantumNous/new-api/internal/channels/pull_ollama_model"
	"github.com/QuantumNous/new-api/internal/channels/refresh_codex_credential"
	"github.com/QuantumNous/new-api/internal/channels/repair_channel_abilities"
	"github.com/QuantumNous/new-api/internal/channels/reset_codex_usage"
	"github.com/QuantumNous/new-api/internal/channels/reveal_channel_key"
	"github.com/QuantumNous/new-api/internal/channels/run_channel_tests"
	"github.com/QuantumNous/new-api/internal/channels/scan_upstream_model_updates"
	"github.com/QuantumNous/new-api/internal/channels/search_channels"
	"github.com/QuantumNous/new-api/internal/channels/set_channel_tags_batch"
	"github.com/QuantumNous/new-api/internal/channels/stream_ollama_model_pull"
	"github.com/QuantumNous/new-api/internal/channels/test_channel"
	"github.com/QuantumNous/new-api/internal/channels/test_channels"
	"github.com/QuantumNous/new-api/internal/channels/update_channel"
	"github.com/QuantumNous/new-api/internal/channels/update_channel_balance"
	"github.com/QuantumNous/new-api/internal/channels/update_channel_status"
	"github.com/QuantumNous/new-api/internal/channels/update_channel_status_batch"
	"github.com/QuantumNous/new-api/internal/channels/update_channels_balances"
	"github.com/QuantumNous/new-api/internal/channels/validate_models"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"time"
)

type ChannelEffects struct {
	ModelUpdateThrottle func(int64, int, int) bool
	EnqueueModelUpdate  func() (detect_all_upstream_model_updates.Task, bool, error)

	FetchModels    func(*model.Channel) ([]string, error)
	RefreshRuntime func()

	AutomaticStream func(*model.Channel) bool

	EnqueueTest func() (test_channels.Task, bool, error)

	Probe     func(func(*channeldomain.Channel) *model.Channel) channeldomain.Probe
	ProbeUser func(*gin.Context) (int, error)

	Audit         func(*gin.Context, string, map[string]any)
	ReadAudit     func(*gin.Context)
	SecurityError func(*gin.Context, error)
	Refresh       func()
}
type channelProxyCache struct{}

func (channelProxyCache) Reset()                  { service.ResetProxyClientCache() }
func (channelProxyCache) Invalidate(proxy string) { service.InvalidateProxyClient(proxy) }
func NewGetChannelHTTP(c *gin.Context, effects ChannelEffects) get_channel.HTTP {
	store := repository.NewStore()
	_ = store
	return get_channel.NewHTTP(store)
}
func NewGetChannelKeyHTTP(c *gin.Context, effects ChannelEffects) reveal_channel_key.HTTP {
	store := repository.NewStore()
	_ = store
	return reveal_channel_key.NewHTTP(store, func(id int, name string) {
		effects.Audit(c, "channel.key_view", map[string]any{"id": id, "name": name})
	}, effects.SecurityError)
}
func NewDeleteChannelHTTP(c *gin.Context, effects ChannelEffects) delete_channel.HTTP {
	store := repository.NewStore()
	_ = store
	return delete_channel.NewHTTP(store, channelProxyCache{}, effects.Refresh, func(id int, name string) { effects.Audit(c, "channel.delete", map[string]any{"id": id, "name": name}) })
}
func NewDeleteDisabledChannelHTTP(c *gin.Context, effects ChannelEffects) delete_disabled_channels.HTTP {
	store := repository.NewStore()
	_ = store
	return delete_disabled_channels.NewHTTP(store, effects.Refresh, service.ResetProxyClientCache, func(count int64) { effects.Audit(c, "channel.delete_disabled", map[string]any{"count": count}) })
}
func NewDeleteChannelBatchHTTP(c *gin.Context, effects ChannelEffects) delete_channels_batch.HTTP {
	store := repository.NewStore()
	_ = store
	return delete_channels_batch.NewHTTP(store, effects.Refresh, service.ResetProxyClientCache, func(count int64) { effects.Audit(c, "channel.delete_batch", map[string]any{"count": count}) })
}
func NewDisableTagChannelsHTTP(c *gin.Context, effects ChannelEffects) disable_tag_channels.HTTP {
	store := repository.NewStore()
	_ = store
	return disable_tag_channels.NewHTTP(store, effects.Refresh, func(tag string) { effects.Audit(c, "channel.tag_disable", map[string]any{"tag": tag}) })
}
func NewEnableTagChannelsHTTP(c *gin.Context, effects ChannelEffects) enable_tag_channels.HTTP {
	store := repository.NewStore()
	_ = store
	return enable_tag_channels.NewHTTP(store, effects.Refresh, func(tag string) { effects.Audit(c, "channel.tag_enable", map[string]any{"tag": tag}) })
}
func NewUpdateChannelStatusHTTP(c *gin.Context, effects ChannelEffects) update_channel_status.HTTP {
	store := repository.NewStore()
	_ = store
	return update_channel_status.NewHTTP(store, effects.Refresh, func(input update_channel_status.Input, changed bool) {
		effects.Audit(c, "channel.status_update", map[string]any{"id": input.ID, "status": input.Status, "changed": changed})
	})
}
func NewBatchUpdateChannelStatusHTTP(c *gin.Context, effects ChannelEffects) update_channel_status_batch.HTTP {
	store := repository.NewStore()
	_ = store
	return update_channel_status_batch.NewHTTP(store, effects.Refresh, func(input update_channel_status_batch.Input, count int) {
		effects.Audit(c, "channel.status_update_batch", map[string]any{"count": count, "total": len(input.Ids), "status": input.Status})
	})
}
func NewBatchSetChannelTagHTTP(c *gin.Context, effects ChannelEffects) set_channel_tags_batch.HTTP {
	store := repository.NewStore()
	_ = store
	return set_channel_tags_batch.NewHTTP(store, effects.Refresh, func(count int) { effects.Audit(c, "channel.tag_batch_set", map[string]any{"count": count}) })
}
func NewGetTagModelsHTTP(c *gin.Context, effects ChannelEffects) get_tag_models.HTTP {
	store := repository.NewStore()
	_ = store
	return get_tag_models.NewHTTP(store)
}
func NewFixChannelsAbilitiesHTTP(c *gin.Context, effects ChannelEffects) repair_channel_abilities.HTTP {
	store := repository.NewStore()
	_ = store
	return repair_channel_abilities.NewHTTP(store)
}
func NewGetChannelOpsHTTP(c *gin.Context, effects ChannelEffects) get_channel_operations.HTTP {
	store := repository.NewStore()
	_ = store
	return get_channel_operations.NewHTTP(func() int { return common.RetryTimes })
}
func NewManageMultiKeysHTTP(c *gin.Context, effects ChannelEffects) manage_multi_keys.HTTP {
	store := repository.NewStore()
	_ = store
	return manage_multi_keys.NewHTTP(manage_multi_keys.NewUsecase(store, effects.Refresh, func() bool { return authz.Can(c.GetInt("id"), c.GetInt("role"), authz.ChannelSensitiveWrite) }, func() { effects.ReadAudit(c) }, func(action string, id int) {
		effects.Audit(c, "channel.multi_key_manage", map[string]any{"action": action, "id": id})
	}))
}

func NewGetAllChannelsHTTP(c *gin.Context, effects ChannelEffects) list_channels.HTTP {
	store := repository.NewStore()
	_ = store
	return list_channels.NewHTTP(list_channels.NewUsecase(store, common.SysError))
}

func NewSearchChannelsHTTP(c *gin.Context, effects ChannelEffects) search_channels.HTTP {
	store := repository.NewStore()
	_ = store
	return search_channels.NewHTTP(search_channels.NewUsecase(store))
}

func NewAddChannelHTTP(c *gin.Context, effects ChannelEffects) create_channel.HTTP {
	store := repository.NewStore()
	_ = store
	return create_channel.NewHTTP(create_channel.NewUsecase(store, common.GetTimestamp, func() bool { return authz.Can(c.GetInt("id"), c.GetInt("role"), authz.TaskPluginBind) }, func(params map[string]any) { effects.Audit(c, "channel.create", params) }))
}

func NewCopyChannelHTTP(c *gin.Context, effects ChannelEffects) copy_channel.HTTP {
	store := repository.NewStore()
	_ = store
	return copy_channel.NewHTTP(copy_channel.NewUsecase(store, func() bool { return authz.Can(c.GetInt("id"), c.GetInt("role"), authz.TaskPluginBind) }, common.GetTimestamp, common.SysError, effects.Refresh, func(params map[string]any) { effects.Audit(c, "channel.copy", params) }))
}

func NewEditTagChannelsHTTP(c *gin.Context, effects ChannelEffects) edit_tag_channels.HTTP {
	store := repository.NewStore()
	_ = store
	return edit_tag_channels.NewHTTP(edit_tag_channels.NewUsecase(store, func() bool { return authz.Can(c.GetInt("id"), c.GetInt("role"), authz.ChannelSensitiveWrite) }, effects.Refresh, func(tag string) { effects.Audit(c, "channel.tag_edit", map[string]any{"tag": tag}) }))
}

func NewUpdateChannelHTTP(c *gin.Context, effects ChannelEffects) update_channel.HTTP {
	store := repository.NewStore()
	_ = store
	return update_channel.NewHTTP(update_channel.NewUsecase(store, func() bool { return authz.Can(c.GetInt("id"), c.GetInt("role"), authz.TaskPluginBind) }, func() bool { return authz.Can(c.GetInt("id"), c.GetInt("role"), authz.ChannelSensitiveWrite) }, service.NormalizeProxyURL, effects.Refresh, service.InvalidateProxyClient, func(params map[string]any) { effects.Audit(c, "channel.update", params) }))
}

func NewOllamaPullModelHTTP(c *gin.Context, effects ChannelEffects) pull_ollama_model.HTTP {
	store := repository.NewStore()
	_ = store
	return pull_ollama_model.NewHTTP(pull_ollama_model.NewUsecase(store, channeladapter.NewOllama()))
}

func NewOllamaDeleteModelHTTP(c *gin.Context, effects ChannelEffects) delete_ollama_model.HTTP {
	store := repository.NewStore()
	_ = store
	return delete_ollama_model.NewHTTP(delete_ollama_model.NewUsecase(store, channeladapter.NewOllama()))
}

func NewOllamaVersionHTTP(c *gin.Context, effects ChannelEffects) get_ollama_version.HTTP {
	store := repository.NewStore()
	_ = store
	return get_ollama_version.NewHTTP(get_ollama_version.NewUsecase(store, channeladapter.NewOllama()))
}

func NewOllamaPullModelStreamHTTP(c *gin.Context, effects ChannelEffects) stream_ollama_model_pull.HTTP {
	store := repository.NewStore()
	_ = store
	return stream_ollama_model_pull.NewHTTP(store, channeladapter.NewOllama())
}

func NewRefreshCodexChannelCredentialHTTP(c *gin.Context, effects ChannelEffects) refresh_codex_credential.HTTP {
	store := repository.NewStore()
	_ = store
	return refresh_codex_credential.NewHTTP(refresh_codex_credential.NewUsecase(store, func(ctx context.Context, token, proxy string) (*channeldomain.CodexTokenResult, error) {
		result, err := service.RefreshCodexOAuthTokenWithProxy(ctx, token, proxy)
		if result == nil {
			return nil, err
		}
		return &channeldomain.CodexTokenResult{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresAt: result.ExpiresAt}, err
	}, service.ExtractCodexAccountIDFromJWT, service.ExtractEmailFromJWT, time.Now))
}

func NewGetCodexChannelUsageHTTP(c *gin.Context, effects ChannelEffects) get_codex_usage.HTTP {
	store := repository.NewStore()
	_ = store
	return get_codex_usage.NewHTTP(get_codex_usage.NewUsecase(store, channeladapter.NewCodexUsage(service.FetchCodexWhamUsage), common.SysError, time.Now))
}

func NewGetCodexChannelRateLimitResetCreditsHTTP(c *gin.Context, effects ChannelEffects) get_codex_reset_credits.HTTP {
	store := repository.NewStore()
	_ = store
	return get_codex_reset_credits.NewHTTP(get_codex_reset_credits.NewUsecase(store, channeladapter.NewCodexUsage(service.FetchCodexWhamRateLimitResetCredits), common.SysError, time.Now))
}

func NewResetCodexChannelUsageHTTP(c *gin.Context, effects ChannelEffects) reset_codex_usage.HTTP {
	store := repository.NewStore()
	_ = store
	return reset_codex_usage.NewHTTP(reset_codex_usage.NewUsecase(store, channeladapter.NewCodexUsage(service.ConsumeCodexWhamRateLimitResetCredit), common.SysError, time.Now))
}

func NewFetchUpstreamModelsHTTP(c *gin.Context, effects ChannelEffects) fetch_upstream_models.HTTP {
	store := repository.NewStore()
	_ = store
	return fetch_upstream_models.NewHTTP(fetch_upstream_models.NewUsecase(store, channeladapter.NewModelDiscovery()))
}

func NewFetchModelsHTTP(c *gin.Context, effects ChannelEffects) preview_channel_models.HTTP {
	store := repository.NewStore()
	_ = store
	return preview_channel_models.NewHTTP(preview_channel_models.NewUsecase(store, channeladapter.NewModelDiscovery()))
}

func NewUpdateChannelBalanceHTTP(c *gin.Context, effects ChannelEffects) update_channel_balance.HTTP {
	store := repository.NewStore()
	return update_channel_balance.NewHTTP(update_channel_balance.NewUsecase(store, channeladapter.NewBalanceReader(store.Record)))
}
func NewChannelBalancesUsecase() update_channels_balances.Usecase {
	store := repository.NewStore()
	return update_channels_balances.NewUsecase(store, channeladapter.NewBalanceReader(store.Record), func(channel *channeldomain.Channel, reason string) {
		record := store.Record(channel)
		service.DisableChannel(*types.NewChannelError(record.Id, record.Type, record.Name, record.ChannelInfo.IsMultiKey, "", record.GetAutoBan()), reason)
	}, func() { time.Sleep(common.RequestInterval) })
}
func NewUpdateAllChannelsBalanceHTTP(c *gin.Context, effects ChannelEffects) update_channels_balances.HTTP {
	return update_channels_balances.NewHTTP(NewChannelBalancesUsecase())
}

func NewTestChannelHTTP(c *gin.Context, effects ChannelEffects) test_channel.HTTP {
	store := repository.NewStore()
	return test_channel.NewHTTP(test_channel.NewUsecase(store, effects.Probe(store.Record), func() (int, error) { return effects.ProbeUser(c) }, time.Now))
}

func NewTestAllChannelsHTTP(c *gin.Context, effects ChannelEffects) test_channels.HTTP {
	return test_channels.NewHTTP(test_channels.NewUsecase(channeladapter.NewTestQueue(effects.EnqueueTest)))
}

func NewModelValidationUsecase(c *gin.Context, effects ChannelEffects) validate_models.Usecase {
	store := repository.NewStore()
	return validate_models.NewUsecase(store, effects.Probe(store.Record), func() (int, error) { return effects.ProbeUser(c) }, service.ClassifyModelValidation)
}
func NewValidateModelsHTTP(c *gin.Context, effects ChannelEffects) validate_models.HTTP {
	return validate_models.NewHTTP(NewModelValidationUsecase(c, effects))
}

func NewChannelListModelsHTTP(read func() list_channel_models.Output) list_channel_models.HTTP {
	return list_channel_models.NewHTTP(list_channel_models.NewUsecase(read))
}
func NewEnabledListModelsHTTP() list_enabled_channel_models.HTTP {
	return list_enabled_channel_models.NewHTTP(list_enabled_channel_models.NewUsecase(model.GetEnabledModels))
}
func NewGetChannelAffinityCacheStatsHTTP() get_affinity_cache_stats.HTTP {
	return get_affinity_cache_stats.NewHTTP(get_affinity_cache_stats.NewUsecase(func() get_affinity_cache_stats.Output {
		return get_affinity_cache_stats.Output(service.GetChannelAffinityCacheStats())
	}))
}
func NewGetChannelAffinityUsageCacheStatsHTTP() get_affinity_usage_stats.HTTP {
	return get_affinity_usage_stats.NewHTTP(get_affinity_usage_stats.NewUsecase(func(rule, group, key string) get_affinity_usage_stats.Output {
		return get_affinity_usage_stats.Output(service.GetChannelAffinityUsageCacheStats(rule, group, key))
	}))
}
func NewClearChannelAffinityCacheHTTP() clear_affinity_cache.HTTP {
	return clear_affinity_cache.NewHTTP(clear_affinity_cache.NewUsecase(channeladapter.NewAffinityCache()))
}

func NewChannelTestTaskUsecase(effects ChannelEffects) run_channel_tests.Usecase {
	store := repository.NewStore()
	return run_channel_tests.NewUsecase(store, effects.Probe(store.Record), func() (int, error) { return effects.ProbeUser(nil) }, func(channel *channeldomain.Channel) bool { return effects.AutomaticStream(store.Record(channel)) }, service.ShouldDisableChannel, service.ShouldEnableChannel, func() bool { return common.AutomaticDisableChannelEnabled }, service.EnableChannel, func() float64 { return common.ChannelDisableThreshold }, operation_setting.NormalizeChannelTestConcurrency, func() time.Duration { return common.RequestInterval }, func() string { return operation_setting.GetMonitorSetting().ChannelTestMode }, func() int { return operation_setting.GetMonitorSetting().ChannelTestConcurrency }, func() {
		service.NotifyRootUser(dto.NotifyTypeChannelTest, "通道测试完成", "所有通道测试已完成")
	})
}

func NewChannelModelUpdateRules(store repository.Store) channeldomain.ModelUpdateRules {
	return channeldomain.ModelUpdateRules{Errors: func() channeldomain.ModelMappingErrors {
		return channeldomain.ModelMappingErrors{ErrModelNameEmpty: model.ErrModelNameEmpty, ErrModelMappingCycle: model.ErrModelMappingCycle, ErrModelMappingSourceEmpty: model.ErrModelMappingSourceEmpty, ErrModelMappingTargetEmpty: model.ErrModelMappingTargetEmpty, ErrModelMappingConflict: model.ErrModelMappingConflict}
	}, Canonicalize: store.Canonicalize, Now: common.GetTimestamp}
}
func NewApplyChannelModelsUsecase(c *gin.Context, effects ChannelEffects) apply_upstream_model_updates.Usecase {
	store := repository.NewStore()
	return apply_upstream_model_updates.NewUsecase(store, NewChannelModelUpdateRules(store), func(channel *channeldomain.Channel) ([]string, error) {
		record := store.Record(channel)
		models, err := effects.FetchModels(record)
		*channel = *repository.ToDomain(record)
		return models, err
	}, effects.RefreshRuntime, func(params map[string]any) { effects.Audit(c, "channel.upstream_apply", params) })
}
func NewApplyChannelUpstreamModelUpdatesHTTP(c *gin.Context, effects ChannelEffects) apply_upstream_model_updates.HTTP {
	return apply_upstream_model_updates.NewHTTP(NewApplyChannelModelsUsecase(c, effects))
}
func NewApplyAllChannelUpstreamModelUpdatesHTTP(c *gin.Context, effects ChannelEffects) apply_all_upstream_model_updates.HTTP {
	store := repository.NewStore()
	return apply_all_upstream_model_updates.NewHTTP(apply_all_upstream_model_updates.NewUsecase(store, NewChannelModelUpdateRules(store), func(channel *channeldomain.Channel) ([]string, error) {
		record := store.Record(channel)
		models, err := effects.FetchModels(record)
		*channel = *repository.ToDomain(record)
		return models, err
	}, effects.RefreshRuntime, func(params map[string]any) { effects.Audit(c, "channel.upstream_apply_all", params) }))
}

func ChannelModelCheckInterval() int64 {
	value := int64(common.GetEnvOrDefault("CHANNEL_UPSTREAM_MODEL_UPDATE_MIN_CHECK_INTERVAL_SECONDS", 300))
	if value < 0 {
		return 300
	}
	return value
}
func NewDetectChannelModelsUsecase(effects ChannelEffects) detect_upstream_model_updates.Usecase {
	store := repository.NewStore()
	return detect_upstream_model_updates.NewUsecase(store, NewChannelModelUpdateRules(store), func(channel *channeldomain.Channel) ([]string, error) {
		record := store.Record(channel)
		models, err := effects.FetchModels(record)
		*channel = *repository.ToDomain(record)
		return models, err
	}, effects.RefreshRuntime, common.GetTimestamp, ChannelModelCheckInterval)
}
func NewDetectChannelUpstreamModelUpdatesHTTP(c *gin.Context, effects ChannelEffects) detect_upstream_model_updates.HTTP {
	return detect_upstream_model_updates.NewHTTP(NewDetectChannelModelsUsecase(effects))
}
func NewScanChannelModelsUsecase(effects ChannelEffects) scan_upstream_model_updates.Usecase {
	store := repository.NewStore()
	return scan_upstream_model_updates.NewUsecase(store, NewChannelModelUpdateRules(store), func(channel *channeldomain.Channel) ([]string, error) {
		record := store.Record(channel)
		models, err := effects.FetchModels(record)
		*channel = *repository.ToDomain(record)
		return models, err
	}, effects.RefreshRuntime, common.GetTimestamp, ChannelModelCheckInterval, func() time.Duration { return common.RequestInterval }, func() bool { return common.DebugEnabled }, common.SysLog, effects.ModelUpdateThrottle, service.NotifyUpstreamModelUpdateWatchers)
}
func NewDetectAllChannelUpstreamModelUpdatesHTTP(c *gin.Context, effects ChannelEffects) detect_all_upstream_model_updates.HTTP {
	return detect_all_upstream_model_updates.NewHTTP(detect_all_upstream_model_updates.NewUsecase(effects.EnqueueModelUpdate, func(id string) { effects.Audit(c, "channel.upstream_detect_all", map[string]any{"task_id": id}) }))
}

func RefreshChannelRuntimeCache(pricing func()) {
	channeladapter.NewRuntimeCache(func() bool { return common.MemoryCacheEnabled }, model.InitChannelCache, pricing, common.SysLog).Refresh()
}

func NewPreviewChannelModelsUsecase() preview_channel_models.Usecase {
	store := repository.NewStore()
	return preview_channel_models.NewUsecase(store, channeladapter.NewModelDiscovery())
}
