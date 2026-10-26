package app

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/app/channelruntime"
	"github.com/QuantumNous/new-api/internal/app/notifications"
	"github.com/QuantumNous/new-api/internal/channels/disable_channel"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
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
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
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
	// ChannelListModels returns the built-in OpenAI-compatible model list.
	ChannelListModels func() list_channel_models.Output
}

// ChannelsHTTP holds the channel management handlers. They are built once; a
// usecase that touches channel records is assembled per request because the
// channel store keeps the identity of the records it loaded for one request.
type ChannelsHTTP struct {
	GetChannel               get_channel.HTTP
	RevealChannelKey         reveal_channel_key.HTTP
	DeleteChannel            delete_channel.HTTP
	DeleteDisabledChannels   delete_disabled_channels.HTTP
	DeleteChannelsBatch      delete_channels_batch.HTTP
	DisableTagChannels       disable_tag_channels.HTTP
	EnableTagChannels        enable_tag_channels.HTTP
	EditTagChannels          edit_tag_channels.HTTP
	UpdateChannelStatus      update_channel_status.HTTP
	UpdateChannelStatusBatch update_channel_status_batch.HTTP
	SetChannelTagsBatch      set_channel_tags_batch.HTTP
	GetTagModels             get_tag_models.HTTP
	RepairChannelAbilities   repair_channel_abilities.HTTP
	GetChannelOperations     get_channel_operations.HTTP
	ManageMultiKeys          manage_multi_keys.HTTP
	ListChannels             list_channels.HTTP
	SearchChannels           search_channels.HTTP
	CreateChannel            create_channel.HTTP
	CopyChannel              copy_channel.HTTP
	UpdateChannel            update_channel.HTTP
	PullOllamaModel          pull_ollama_model.HTTP
	DeleteOllamaModel        delete_ollama_model.HTTP
	GetOllamaVersion         get_ollama_version.HTTP
	StreamOllamaModelPull    stream_ollama_model_pull.HTTP
	RefreshCodexCredential   refresh_codex_credential.HTTP
	GetCodexUsage            get_codex_usage.HTTP
	GetCodexResetCredits     get_codex_reset_credits.HTTP
	ResetCodexUsage          reset_codex_usage.HTTP
	FetchUpstreamModels      fetch_upstream_models.HTTP
	PreviewChannelModels     preview_channel_models.HTTP
	UpdateChannelBalance     update_channel_balance.HTTP
	UpdateChannelsBalances   update_channels_balances.HTTP
	TestChannel              test_channel.HTTP
	TestChannels             test_channels.HTTP
	ValidateModels           validate_models.HTTP
	ListChannelModels        list_channel_models.HTTP
	ListEnabledChannelModels list_enabled_channel_models.HTTP
	GetAffinityCacheStats    get_affinity_cache_stats.HTTP
	GetAffinityUsageStats    get_affinity_usage_stats.HTTP
	ClearAffinityCache       clear_affinity_cache.HTTP
	ApplyUpstreamModels      apply_upstream_model_updates.HTTP
	ApplyAllUpstreamModels   apply_all_upstream_model_updates.HTTP
	DetectUpstreamModels     detect_upstream_model_updates.HTTP
	DetectAllUpstreamModels  detect_all_upstream_model_updates.HTTP
}

type channelProxyCache struct{}

func (channelProxyCache) Reset()                  { service.ResetProxyClientCache() }
func (channelProxyCache) Invalidate(proxy string) { service.InvalidateProxyClient(proxy) }

func canChannel(c *gin.Context, permission authz.Permission) func() bool {
	return func() bool { return authz.Can(c.GetInt("id"), c.GetInt("role"), permission) }
}

func NewChannelsHTTP(effects ChannelEffects) ChannelsHTTP {
	audit := func(c *gin.Context, action string) func(map[string]any) {
		return func(params map[string]any) { effects.Audit(c, action, params) }
	}
	return ChannelsHTTP{
		GetChannel: get_channel.NewHTTP(func(*gin.Context) get_channel.Usecase { return get_channel.NewUsecase(repository.NewStore()) }),
		RevealChannelKey: reveal_channel_key.NewHTTP(func(c *gin.Context) reveal_channel_key.Usecase {
			return reveal_channel_key.NewUsecase(repository.NewStore(), func(id int, name string) {
				effects.Audit(c, "channel.key_view", map[string]any{"id": id, "name": name})
			})
		}, effects.SecurityError),
		DeleteChannel: delete_channel.NewHTTP(func(c *gin.Context) delete_channel.Usecase {
			return delete_channel.NewUsecase(repository.NewStore(), channelProxyCache{}, effects.Refresh, func(id int, name string) {
				effects.Audit(c, "channel.delete", map[string]any{"id": id, "name": name})
			})
		}),
		DeleteDisabledChannels: delete_disabled_channels.NewHTTP(func(c *gin.Context) delete_disabled_channels.Usecase {
			return delete_disabled_channels.NewUsecase(repository.NewStore(), effects.Refresh, service.ResetProxyClientCache, func(count int64) {
				effects.Audit(c, "channel.delete_disabled", map[string]any{"count": count})
			})
		}),
		DeleteChannelsBatch: delete_channels_batch.NewHTTP(func(c *gin.Context) delete_channels_batch.Usecase {
			return delete_channels_batch.NewUsecase(repository.NewStore(), effects.Refresh, service.ResetProxyClientCache, func(count int64) {
				effects.Audit(c, "channel.delete_batch", map[string]any{"count": count})
			})
		}),
		DisableTagChannels: disable_tag_channels.NewHTTP(func(c *gin.Context) disable_tag_channels.Usecase {
			return disable_tag_channels.NewUsecase(repository.NewStore(), effects.Refresh, func(tag string) {
				effects.Audit(c, "channel.tag_disable", map[string]any{"tag": tag})
			})
		}),
		EnableTagChannels: enable_tag_channels.NewHTTP(func(c *gin.Context) enable_tag_channels.Usecase {
			return enable_tag_channels.NewUsecase(repository.NewStore(), effects.Refresh, func(tag string) {
				effects.Audit(c, "channel.tag_enable", map[string]any{"tag": tag})
			})
		}),
		EditTagChannels: edit_tag_channels.NewHTTP(func(c *gin.Context) edit_tag_channels.Usecase {
			return edit_tag_channels.NewUsecase(repository.NewStore(), canChannel(c, authz.ChannelSensitiveWrite), effects.Refresh, func(tag string) {
				effects.Audit(c, "channel.tag_edit", map[string]any{"tag": tag})
			})
		}),
		UpdateChannelStatus: update_channel_status.NewHTTP(func(c *gin.Context) update_channel_status.Usecase {
			return update_channel_status.NewUsecase(repository.NewStore(), effects.Refresh, func(input update_channel_status.Input, changed bool) {
				effects.Audit(c, "channel.status_update", map[string]any{"id": input.ID, "status": input.Status, "changed": changed})
			})
		}),
		UpdateChannelStatusBatch: update_channel_status_batch.NewHTTP(func(c *gin.Context) update_channel_status_batch.Usecase {
			return update_channel_status_batch.NewUsecase(repository.NewStore(), effects.Refresh, func(input update_channel_status_batch.Input, count int) {
				effects.Audit(c, "channel.status_update_batch", map[string]any{"count": count, "total": len(input.Ids), "status": input.Status})
			})
		}),
		SetChannelTagsBatch: set_channel_tags_batch.NewHTTP(func(c *gin.Context) set_channel_tags_batch.Usecase {
			return set_channel_tags_batch.NewUsecase(repository.NewStore(), effects.Refresh, func(count int) {
				effects.Audit(c, "channel.tag_batch_set", map[string]any{"count": count})
			})
		}),
		GetTagModels: get_tag_models.NewHTTP(func(*gin.Context) get_tag_models.Usecase { return get_tag_models.NewUsecase(repository.NewStore()) }),
		RepairChannelAbilities: repair_channel_abilities.NewHTTP(func(*gin.Context) repair_channel_abilities.Usecase {
			return repair_channel_abilities.NewUsecase(repository.NewStore())
		}),
		GetChannelOperations: get_channel_operations.NewHTTP(get_channel_operations.NewUsecase(func() int { return common.RetryTimes })),
		ManageMultiKeys: manage_multi_keys.NewHTTP(func(c *gin.Context) manage_multi_keys.Usecase {
			return manage_multi_keys.NewUsecase(repository.NewStore(), effects.Refresh, canChannel(c, authz.ChannelSensitiveWrite), func() { effects.ReadAudit(c) }, func(action string, id int) {
				effects.Audit(c, "channel.multi_key_manage", map[string]any{"action": action, "id": id})
			})
		}),
		ListChannels: list_channels.NewHTTP(func(*gin.Context) list_channels.Usecase {
			return list_channels.NewUsecase(repository.NewStore(), common.SysError)
		}),
		SearchChannels: search_channels.NewHTTP(func(*gin.Context) search_channels.Usecase {
			return search_channels.NewUsecase(repository.NewStore())
		}),
		CreateChannel: create_channel.NewHTTP(func(c *gin.Context) create_channel.Usecase {
			return create_channel.NewUsecase(repository.NewStore(), common.GetTimestamp, canChannel(c, authz.TaskPluginBind), audit(c, "channel.create"))
		}),
		CopyChannel: copy_channel.NewHTTP(func(c *gin.Context) copy_channel.Usecase {
			return copy_channel.NewUsecase(repository.NewStore(), canChannel(c, authz.TaskPluginBind), common.GetTimestamp, common.SysError, effects.Refresh, audit(c, "channel.copy"))
		}),
		UpdateChannel: update_channel.NewHTTP(func(c *gin.Context) update_channel.Usecase {
			return update_channel.NewUsecase(repository.NewStore(), channeladapter.ChannelPatchCodec{}, canChannel(c, authz.TaskPluginBind), canChannel(c, authz.ChannelSensitiveWrite), service.NormalizeProxyURL, effects.Refresh, service.InvalidateProxyClient, audit(c, "channel.update"))
		}),
		PullOllamaModel: pull_ollama_model.NewHTTP(func(*gin.Context) pull_ollama_model.Usecase {
			return pull_ollama_model.NewUsecase(repository.NewStore(), channeladapter.NewOllama())
		}),
		DeleteOllamaModel: delete_ollama_model.NewHTTP(func(*gin.Context) delete_ollama_model.Usecase {
			return delete_ollama_model.NewUsecase(repository.NewStore(), channeladapter.NewOllama())
		}),
		GetOllamaVersion: get_ollama_version.NewHTTP(func(*gin.Context) get_ollama_version.Usecase {
			return get_ollama_version.NewUsecase(repository.NewStore(), channeladapter.NewOllama())
		}),
		StreamOllamaModelPull: stream_ollama_model_pull.NewHTTP(func() stream_ollama_model_pull.Store { return repository.NewStore() }, channeladapter.NewOllama()),
		RefreshCodexCredential: refresh_codex_credential.NewHTTP(func(*gin.Context) refresh_codex_credential.Usecase {
			return refresh_codex_credential.NewUsecase(repository.NewStore(), func(ctx context.Context, token, proxy string) (*channeldomain.CodexTokenResult, error) {
				result, err := service.RefreshCodexOAuthTokenWithProxy(ctx, token, proxy)
				if result == nil {
					return nil, err
				}
				return &channeldomain.CodexTokenResult{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresAt: result.ExpiresAt}, err
			}, service.ExtractCodexAccountIDFromJWT, service.ExtractEmailFromJWT, time.Now)
		}),
		GetCodexUsage: get_codex_usage.NewHTTP(func(*gin.Context) get_codex_usage.Usecase {
			return get_codex_usage.NewUsecase(repository.NewStore(), channeladapter.NewCodexUsage(service.FetchCodexWhamUsage), common.SysError, time.Now)
		}),
		GetCodexResetCredits: get_codex_reset_credits.NewHTTP(func(*gin.Context) get_codex_reset_credits.Usecase {
			return get_codex_reset_credits.NewUsecase(repository.NewStore(), channeladapter.NewCodexUsage(service.FetchCodexWhamRateLimitResetCredits), common.SysError, time.Now)
		}),
		ResetCodexUsage: reset_codex_usage.NewHTTP(func(*gin.Context) reset_codex_usage.Usecase {
			return reset_codex_usage.NewUsecase(repository.NewStore(), channeladapter.NewCodexUsage(service.ConsumeCodexWhamRateLimitResetCredit), common.SysError, time.Now)
		}),
		FetchUpstreamModels: fetch_upstream_models.NewHTTP(func(*gin.Context) fetch_upstream_models.Usecase {
			return fetch_upstream_models.NewUsecase(repository.NewStore(), channeladapter.NewModelDiscovery())
		}),
		PreviewChannelModels: preview_channel_models.NewHTTP(func(*gin.Context) preview_channel_models.Usecase { return NewPreviewChannelModelsUsecase() }),
		UpdateChannelBalance: update_channel_balance.NewHTTP(func(*gin.Context) update_channel_balance.Usecase {
			store := repository.NewStore()
			return update_channel_balance.NewUsecase(store, channeladapter.NewBalanceReader(store.Record))
		}),
		UpdateChannelsBalances: update_channels_balances.NewHTTP(func(*gin.Context) update_channels_balances.Usecase { return NewChannelBalancesUsecase() }),
		TestChannel: test_channel.NewHTTP(func(c *gin.Context) test_channel.Usecase {
			store := repository.NewStore()
			return test_channel.NewUsecase(store, effects.Probe(store.Record), func() (int, error) { return effects.ProbeUser(c) }, time.Now)
		}),
		TestChannels: test_channels.NewHTTP(test_channels.NewUsecase(channeladapter.NewTestQueue(effects.EnqueueTest))),
		ValidateModels: validate_models.NewHTTP(func(c *gin.Context) validate_models.Usecase {
			store := repository.NewStore()
			return validate_models.NewUsecase(store, effects.Probe(store.Record), func() (int, error) { return effects.ProbeUser(c) }, func(localErr, upstreamErr error) (channeldomain.ModelLiveness, int) {
				return service.ClassifyModelValidation(localErr, channeladapter.GatewayError(upstreamErr))
			}, common.MaskSensitiveInfo)
		}),
		ListChannelModels:        list_channel_models.NewHTTP(list_channel_models.NewUsecase(effects.ChannelListModels)),
		ListEnabledChannelModels: list_enabled_channel_models.NewHTTP(list_enabled_channel_models.NewUsecase(model.GetEnabledModels)),
		GetAffinityCacheStats: get_affinity_cache_stats.NewHTTP(get_affinity_cache_stats.NewUsecase(func() get_affinity_cache_stats.Output {
			return get_affinity_cache_stats.Output(service.GetChannelAffinityCacheStats())
		})),
		GetAffinityUsageStats: get_affinity_usage_stats.NewHTTP(get_affinity_usage_stats.NewUsecase(func(rule, group, key string) get_affinity_usage_stats.Output {
			return get_affinity_usage_stats.Output(service.GetChannelAffinityUsageCacheStats(rule, group, key))
		})),
		ClearAffinityCache: clear_affinity_cache.NewHTTP(clear_affinity_cache.NewUsecase(channeladapter.NewAffinityCache())),
		ApplyUpstreamModels: apply_upstream_model_updates.NewHTTP(func(c *gin.Context) apply_upstream_model_updates.Usecase {
			return NewApplyChannelModelsUsecase(effects, audit(c, "channel.upstream_apply"))
		}),
		ApplyAllUpstreamModels: apply_all_upstream_model_updates.NewHTTP(func(c *gin.Context) apply_all_upstream_model_updates.Usecase {
			store := repository.NewStore()
			return apply_all_upstream_model_updates.NewUsecase(store, NewChannelModelUpdateRules(store), channelModelFetcher(store, effects), effects.RefreshRuntime, audit(c, "channel.upstream_apply_all"))
		}),
		DetectUpstreamModels: detect_upstream_model_updates.NewHTTP(func(*gin.Context) detect_upstream_model_updates.Usecase {
			return NewDetectChannelModelsUsecase(effects)
		}),
		DetectAllUpstreamModels: detect_all_upstream_model_updates.NewHTTP(func(c *gin.Context) detect_all_upstream_model_updates.Usecase {
			return detect_all_upstream_model_updates.NewUsecase(effects.EnqueueModelUpdate, func(id string) {
				effects.Audit(c, "channel.upstream_detect_all", map[string]any{"task_id": id})
			})
		}),
	}
}

// NewChannelBalancesUsecase updates the balance of every enabled single-key
// channel. Exhausted channels are disabled with a reason in the root user's
// language.
func NewChannelBalancesUsecase() update_channels_balances.Usecase {
	store := repository.NewStore()
	return update_channels_balances.NewUsecase(store, channeladapter.NewBalanceReader(store.Record), func(channel *channeldomain.Channel) {
		record := store.Record(channel)
		reason := i18n.Translate(channelruntime.RootUserLanguage(), i18n.MsgChannelBalanceExhausted)
		channelruntime.Disable(disable_channel.Input{ChannelID: record.Id, ChannelName: record.Name, UsingKey: "", AutoBan: record.GetAutoBan()}, reason)
	}, func() { time.Sleep(common.RequestInterval) })
}

func NewChannelTestTaskUsecase(effects ChannelEffects) run_channel_tests.Usecase {
	store := repository.NewStore()
	return run_channel_tests.NewUsecase(store, effects.Probe(store.Record), func() (int, error) { return effects.ProbeUser(nil) }, func(channel *channeldomain.Channel) bool { return effects.AutomaticStream(store.Record(channel)) }, channeladapter.TestFailures{}, func() bool { return common.AutomaticDisableChannelEnabled }, channelruntime.Enable, func() float64 { return common.ChannelDisableThreshold }, operation_setting.NormalizeChannelTestConcurrency, func() time.Duration { return common.RequestInterval }, func() string { return operation_setting.GetMonitorSetting().ChannelTestMode }, func() int { return operation_setting.GetMonitorSetting().ChannelTestConcurrency }, func() {
		lang := channelruntime.RootUserLanguage()
		notifications.Root(dto.NotifyTypeChannelTest, i18n.Translate(lang, i18n.MsgChannelTestDoneSubject), i18n.Translate(lang, i18n.MsgChannelTestDoneContent))
	})
}

func NewChannelModelUpdateRules(store repository.Store) channeldomain.ModelUpdateRules {
	return channeldomain.ModelUpdateRules{Codec: store, Errors: func() channeldomain.ModelMappingErrors {
		return channeldomain.ModelMappingErrors{ErrModelNameEmpty: model.ErrModelNameEmpty, ErrModelMappingCycle: model.ErrModelMappingCycle, ErrModelMappingSourceEmpty: model.ErrModelMappingSourceEmpty, ErrModelMappingTargetEmpty: model.ErrModelMappingTargetEmpty, ErrModelMappingConflict: model.ErrModelMappingConflict}
	}, Canonicalize: store.Canonicalize, Now: common.GetTimestamp}
}

// channelModelFetcher fetches the upstream models of a channel through the
// gateway and reloads the channel the fetch may have refreshed.
func channelModelFetcher(store repository.Store, effects ChannelEffects) func(*channeldomain.Channel) ([]string, error) {
	return func(channel *channeldomain.Channel) ([]string, error) {
		record := store.Record(channel)
		models, err := effects.FetchModels(record)
		*channel = *repository.ToDomain(record)
		return models, err
	}
}

func NewApplyChannelModelsUsecase(effects ChannelEffects, audit func(map[string]any)) apply_upstream_model_updates.Usecase {
	store := repository.NewStore()
	return apply_upstream_model_updates.NewUsecase(store, NewChannelModelUpdateRules(store), channelModelFetcher(store, effects), effects.RefreshRuntime, audit)
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
	return detect_upstream_model_updates.NewUsecase(store, NewChannelModelUpdateRules(store), channelModelFetcher(store, effects), effects.RefreshRuntime, common.GetTimestamp, ChannelModelCheckInterval)
}

// NewScanChannelModelsUsecase checks the upstream models of all enabled
// channels; the notification goes to the watching administrators in the root
// user's language.
func NewScanChannelModelsUsecase(effects ChannelEffects) scan_upstream_model_updates.Usecase {
	store := repository.NewStore()
	return scan_upstream_model_updates.NewUsecase(store, NewChannelModelUpdateRules(store), channelModelFetcher(store, effects), effects.RefreshRuntime, common.GetTimestamp, ChannelModelCheckInterval, func() time.Duration { return common.RequestInterval }, func() bool { return common.DebugEnabled }, common.SysLog, effects.ModelUpdateThrottle, func(report channeldomain.ModelUpdateReport) {
		subject, content := channeladapter.ModelUpdateNotification(channelruntime.RootUserLanguage(), report)
		notifications.ModelUpdateWatchers(subject, content)
	})
}

func RefreshChannelRuntimeCache(pricing func()) {
	channeladapter.NewRuntimeCache(func() bool { return common.MemoryCacheEnabled }, model.InitChannelCache, pricing, common.SysLog).Refresh()
}

func NewPreviewChannelModelsUsecase() preview_channel_models.Usecase {
	store := repository.NewStore()
	return preview_channel_models.NewUsecase(store, channeladapter.NewModelDiscovery())
}
