package systemruntime

import (
	"runtime"
	"time"

	"github.com/QuantumNous/new-api/common"
	accounts "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts"
	outbound "github.com/QuantumNous/new-api/internal/adapter/http/system"
	technical "github.com/QuantumNous/new-api/internal/adapter/runtime/system"
	settings "github.com/QuantumNous/new-api/internal/app/settings"
	core "github.com/QuantumNous/new-api/internal/app/system"
	"github.com/QuantumNous/new-api/internal/app/systemmetrics"
	"github.com/QuantumNous/new-api/internal/system/check_setup"
	"github.com/QuantumNous/new-api/internal/system/clear_disk_cache"
	"github.com/QuantumNous/new-api/internal/system/complete_setup"
	"github.com/QuantumNous/new-api/internal/system/delete_log_files"
	"github.com/QuantumNous/new-api/internal/system/delete_system_instance"
	"github.com/QuantumNous/new-api/internal/system/enqueue_log_cleanup"
	"github.com/QuantumNous/new-api/internal/system/force_gc"
	"github.com/QuantumNous/new-api/internal/system/get_current_system_task"
	"github.com/QuantumNous/new-api/internal/system/get_perf_metrics"
	"github.com/QuantumNous/new-api/internal/system/get_perf_metrics_summary"
	"github.com/QuantumNous/new-api/internal/system/get_performance_stats"
	"github.com/QuantumNous/new-api/internal/system/get_setup"
	"github.com/QuantumNous/new-api/internal/system/get_system_task"
	"github.com/QuantumNous/new-api/internal/system/get_uptime_status"
	"github.com/QuantumNous/new-api/internal/system/list_log_files"
	"github.com/QuantumNous/new-api/internal/system/list_system_instances"
	"github.com/QuantumNous/new-api/internal/system/list_system_tasks"
	"github.com/QuantumNous/new-api/internal/system/reset_performance_stats"
	"github.com/QuantumNous/new-api/internal/system/sweep_stale_instances"
	"github.com/QuantumNous/new-api/internal/system/test_database_health"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type HTTP struct {
	GetSetup        get_setup.Handler
	CompleteSetup   complete_setup.Handler
	Health          test_database_health.Handler
	Instances       list_system_instances.Handler
	DeleteInstances sweep_stale_instances.Handler
	DeleteInstance  delete_system_instance.Handler
	StartCleanup    enqueue_log_cleanup.Handler
	CurrentTask     get_current_system_task.Handler
	ListTasks       list_system_tasks.Handler
	GetTask         get_system_task.Handler
	Stats           get_performance_stats.Handler
	ClearCache      clear_disk_cache.Handler
	ResetStats      reset_performance_stats.Handler
	GC              force_gc.Handler
	LogFiles        list_log_files.Handler
	CleanupFiles    delete_log_files.Handler
	Metrics         get_perf_metrics.Handler
	MetricsSummary  get_perf_metrics_summary.Handler
	Uptime          get_uptime_status.Handler
}

func CheckSetup() {
	_ = check_setup.NewUsecase(core.Default().Store, check_setup.Runtime{RootExists: accounts.BootstrapRoot{}.Exists, SetSetup: technical.Facts{}.SetSetup, Version: technical.Facts{}.Version, Now: common.GetTimestamp, Log: common.SysLog}).Execute()
}
func activeGroups() []string {
	ratios := ratio_setting.GetGroupRatioCopy()
	groups := make([]string, 0, len(ratios)+1)
	for g := range ratios {
		groups = append(groups, g)
	}
	return append(groups, "auto")
}
func NewHTTP() HTTP {
	s := core.Default()
	facts := technical.Facts{}
	root := accounts.BootstrapRoot{}
	files := technical.LogFiles{}
	complete := complete_setup.NewUsecase(root, s.Store, settings.Default().Write, complete_setup.Runtime{SetModes: facts.SetModes, SetSetup: facts.SetSetup, Version: facts.Version, Now: common.GetTimestamp},
		complete_setup.Passwords{Validate: common.ValidateNewAccountPassword, Hash: common.HashAccountPassword})
	return HTTP{
		GetSetup:        get_setup.NewHandler(facts.Setup, root.Exists, facts.Database),
		CompleteSetup:   complete_setup.NewHandler(complete, facts.Setup, root.Exists),
		Health:          test_database_health.NewHandler(s.Store.PingDB, func() any { return middleware.GetStats() }),
		Instances:       list_system_instances.NewHandler(s.Store, common.GetTimestamp, s.Store.JSONValue),
		DeleteInstances: sweep_stale_instances.NewHandler(s.Store, common.GetTimestamp),
		DeleteInstance:  delete_system_instance.NewHandler(s.Store, common.GetTimestamp),
		StartCleanup:    enqueue_log_cleanup.NewHandler(s.StartCleanup, s.Store.JSONValue),
		CurrentTask:     get_current_system_task.NewHandler(s.Store, s.Store.JSONValue),
		ListTasks:       list_system_tasks.NewHandler(s.Store, s.Store.JSONValue),
		GetTask:         get_system_task.NewHandler(s.Store, s.Store.JSONValue),
		Stats:           get_performance_stats.NewHandler(technical.Performance{}),
		ClearCache:      clear_disk_cache.NewHandler(func() error { return common.CleanupOldDiskCacheFiles(10 * time.Minute) }),
		ResetStats:      reset_performance_stats.NewHandler(common.ResetDiskCacheStats),
		GC:              force_gc.NewHandler(runtime.GC),
		LogFiles:        list_log_files.NewHandler(files),
		CleanupFiles:    delete_log_files.NewHandler(delete_log_files.NewUsecase(files, time.Now), files.Directory),
		Metrics:         get_perf_metrics.NewHandler(technical.Metrics{QueryMetrics: systemmetrics.Query, QuerySummary: systemmetrics.QuerySummaryAll}, activeGroups),
		MetricsSummary:  get_perf_metrics_summary.NewHandler(technical.Metrics{QueryMetrics: systemmetrics.Query, QuerySummary: systemmetrics.QuerySummaryAll}, activeGroups),
		Uptime:          get_uptime_status.NewHandler(get_uptime_status.NewUsecase(func() get_uptime_status.Client { return outbound.NewUptimeClient() }), console_setting.GetUptimeKumaGroups),
	}
}
