package taskplugins

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/taskplugins"
	technical "github.com/QuantumNous/new-api/internal/adapter/taskplugins"
	channelcore "github.com/QuantumNous/new-api/internal/app/channelcore"
	settingsapp "github.com/QuantumNous/new-api/internal/app/settings"
	"github.com/QuantumNous/new-api/internal/taskplugins/activate_task_plugin"
	"github.com/QuantumNous/new-api/internal/taskplugins/delete_task_plugin_version"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/QuantumNous/new-api/internal/taskplugins/dryrun_task_plugin"
	"github.com/QuantumNous/new-api/internal/taskplugins/fetch_task_plugin_icon"
	"github.com/QuantumNous/new-api/internal/taskplugins/get_marketplace_sources"
	"github.com/QuantumNous/new-api/internal/taskplugins/get_task_plugin"
	"github.com/QuantumNous/new-api/internal/taskplugins/get_task_plugin_runtime"
	"github.com/QuantumNous/new-api/internal/taskplugins/list_bindable_task_plugins"
	"github.com/QuantumNous/new-api/internal/taskplugins/list_task_plugin_versions"
	"github.com/QuantumNous/new-api/internal/taskplugins/list_task_plugins"
	"github.com/QuantumNous/new-api/internal/taskplugins/set_task_plugin_status"
	"github.com/QuantumNous/new-api/internal/taskplugins/sync_task_plugins"
	"github.com/QuantumNous/new-api/internal/taskplugins/update_marketplace_sources"
	"github.com/QuantumNous/new-api/internal/taskplugins/upload_task_plugin"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

func NewStore() storage.Store { return storage.NewStore(func() *gorm.DB { return model.DB }) }

type Core struct {
	State *domain.SyncState
	Sync  sync_task_plugins.Usecase
}

var Default = sync.OnceValue(func() Core {
	state := &domain.SyncState{Hashes: map[string]string{}, Errors: map[string]string{}}
	return Core{State: state, Sync: sync_task_plugins.NewUsecase(NewStore(), technical.Runtime{}, settings(), state, technical.Diagnostics{})}
})

func NewUploadTaskPlugin() upload_task_plugin.Handler {
	return upload_task_plugin.NewHandler(upload_task_plugin.NewUsecase(NewStore(), technical.Runtime{}, Default().Sync))
}
func NewListTaskPlugins() list_task_plugins.Handler {
	return list_task_plugins.NewHandler(list_task_plugins.NewUsecase(NewStore(), technical.Runtime{}, settings(), Default().State))
}
func NewGetTaskPluginRuntime() get_task_plugin_runtime.Handler {
	return get_task_plugin_runtime.NewHandler(get_task_plugin_runtime.NewUsecase(NewStore(), technical.Runtime{}, Default().State))
}
func NewGetTaskPlugin() get_task_plugin.Handler {
	return get_task_plugin.NewHandler(get_task_plugin.NewUsecase(NewStore(), technical.Runtime{}, settings()))
}
func NewActivateTaskPlugin() activate_task_plugin.Handler {
	return activate_task_plugin.NewHandler(activate_task_plugin.NewUsecase(NewStore(), technical.Runtime{}, Default().Sync))
}
func NewDeleteTaskPluginVersion() delete_task_plugin_version.Handler {
	return delete_task_plugin_version.NewHandler(delete_task_plugin_version.NewUsecase(NewStore(), technical.Runtime{}, Default().Sync))
}
func NewSetTaskPluginStatus() set_task_plugin_status.Handler {
	return set_task_plugin_status.NewHandler(set_task_plugin_status.NewUsecase(NewStore(), technical.Runtime{}, settings(), Default().Sync))
}
func NewUpdateTaskPluginMarketplaceSources() update_marketplace_sources.Handler {
	return update_marketplace_sources.NewHandler(update_marketplace_sources.NewUsecase(settings()))
}
func NewGetTaskPluginOptions() list_bindable_task_plugins.Handler {
	return list_bindable_task_plugins.NewHandler(list_bindable_task_plugins.NewUsecase(NewStore(), technical.Runtime{}, settings()))
}

func NewGetTaskPluginVersions() list_task_plugin_versions.Handler {
	return list_task_plugin_versions.NewHandler(NewStore())
}
func NewGetTaskPluginMarketplaceSources() get_marketplace_sources.Handler {
	return get_marketplace_sources.NewHandler(settings())
}
func NewGetTaskPluginIcon() fetch_task_plugin_icon.Handler {
	return fetch_task_plugin_icon.NewHandler(fetch_task_plugin_icon.NewUsecase(NewStore(), settings(), technical.Runtime{}))
}
func NewDryRunTaskPlugin() dryrun_task_plugin.Handler {
	return dryrun_task_plugin.NewHandler(dryrun_task_plugin.NewUsecase(NewStore(), technical.Runtime{}, settings(), technical.Decoder{}))
}
func SyncOnce() {
	if err := Default().Sync.Execute(context.Background()); err != nil {
		common.SysError(err.Error())
	}
}
func StartSync() {
	SyncOnce()
	for range time.NewTicker(30 * time.Second).C {
		SyncOnce()
	}
}

func settings() technical.Settings {
	return technical.Settings{WriteOption: settingsapp.Default().Write.Execute, ChangeStatus: channelcore.UpdateChannelStatus}
}
