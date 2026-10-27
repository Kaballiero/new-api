package systemruntime

import (
	pricingfx "github.com/QuantumNous/new-api/internal/app/pricingfx"

	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/QuantumNous/new-api/internal/app/batchruntime"
	core "github.com/QuantumNous/new-api/internal/app/system"
	taskruntime "github.com/QuantumNous/new-api/internal/app/taskruntime"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/logger"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const channelUpstreamModelUpdateTaskDefaultIntervalMinutes = 30

var taskPolling = sync.OnceValue(taskruntime.NewPolling)

// RegisterScheduledSystemTasks wires the periodic channel test, upstream model
// update, provider_batch_poll, and async task polling (Midjourney / Suno / video) jobs into the
// system task framework so a DB lease dedups execution across multiple master
// instances and each run is recorded as one task row. Call this before
// core.StartSystemTaskRunner.
func RegisterScheduledSystemTasks(effects app.ChannelEffects, register func(domain.SystemTaskHandler)) {
	_ = core.Default()
	register(modelCostFXTaskHandler{})
	register(batchruntime.NewSystemHandler(batchruntime.Default().Poll))
	register(channelTestHandler{effects: effects})
	register(modelUpdateHandler{effects: effects})
	register(midjourneyPollHandler{})
	register(asyncTaskPollHandler{})
	register(pricingCatalogHandler{})
}

// channelTestHandler runs the scheduled "test all channels" job. Enablement and
// cadence still come from the monitor settings; only the execution path moved
// into the system task runner.
type channelTestHandler struct{ effects app.ChannelEffects }

func (channelTestHandler) Type() string { return domain.SystemTaskTypeChannelTest }

func (channelTestHandler) Enabled() bool {
	return operation_setting.GetMonitorSetting().AutoTestChannelEnabled
}

func (channelTestHandler) Interval() time.Duration {
	minutes := operation_setting.GetMonitorSetting().AutoTestChannelMinutes
	if minutes <= 0 {
		minutes = 10
	}
	return time.Duration(minutes * float64(time.Minute))
}

func (channelTestHandler) NewPayload() any { return nil }

// channelTestTaskPayload controls one channel_test run. A nil/empty payload is a
// scheduled run, which uses the configured monitor ChannelTestMode and does not
// notify. A manual "test all channels" trigger sets Mode=scheduled_all and
// Notify=true to reproduce the legacy manual behavior (test every channel and
// notify root on completion).
type channelTestTaskPayload struct {
	Mode   string `json:"mode,omitempty"`
	Notify bool   `json:"notify,omitempty"`
}

func (h channelTestHandler) Run(ctx context.Context, task *domain.SystemTask, runnerID string) {
	payload := channelTestTaskPayload{}
	if err := core.Default().Store.Decode(task.Payload, &payload); err != nil {
		finishSystemTaskHandler(task, runnerID, domain.SystemTaskStatusFailed, nil, err)
		return
	}
	summary, err := app.NewChannelTestTaskUsecase(h.effects).Execute(ctx, payload.Mode, payload.Notify, core.NewSystemTaskProgressReporter(task, runnerID))
	if err != nil {
		finishSystemTaskHandler(task, runnerID, domain.SystemTaskStatusFailed, nil, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, domain.SystemTaskStatusSucceeded, summary, nil)
}

// modelUpdateHandler runs the scheduled upstream model update detection job.
type modelUpdateHandler struct{ effects app.ChannelEffects }

func (modelUpdateHandler) Type() string { return domain.SystemTaskTypeModelUpdate }

func (modelUpdateHandler) Enabled() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", true)
}

func (modelUpdateHandler) Interval() time.Duration {
	intervalMinutes := common.GetEnvOrDefault(
		"CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_INTERVAL_MINUTES",
		channelUpstreamModelUpdateTaskDefaultIntervalMinutes,
	)
	if intervalMinutes < 1 {
		intervalMinutes = channelUpstreamModelUpdateTaskDefaultIntervalMinutes
	}
	return time.Duration(intervalMinutes) * time.Minute
}

func (modelUpdateHandler) NewPayload() any { return nil }

// modelUpdateTaskPayload controls one model_update run. A scheduled run
// (Manual=false) respects the per-channel minimum check interval and may
// auto-apply detected models when a channel has auto-sync enabled. A manual
// "detect all" trigger sets Manual=true to reproduce the legacy detect-all
// semantics: force a re-check regardless of the interval and never auto-apply,
// so the admin reviews and applies changes explicitly.
type modelUpdateTaskPayload struct {
	Manual bool `json:"manual,omitempty"`
}

func (h modelUpdateHandler) Run(ctx context.Context, task *domain.SystemTask, runnerID string) {
	payload := modelUpdateTaskPayload{}
	if err := core.Default().Store.Decode(task.Payload, &payload); err != nil {
		finishSystemTaskHandler(task, runnerID, domain.SystemTaskStatusFailed, nil, err)
		return
	}
	summary := app.NewScanChannelModelsUsecase(h.effects).Execute(ctx, payload.Manual, !payload.Manual, core.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, domain.SystemTaskStatusSucceeded, summary, nil)
}

// midjourneyPollHandler runs one Midjourney polling pass per scheduled run.
// Enabled() folds the "are there unfinished tasks?" check into enablement so the
// scheduler creates no row when the system is idle; only when at least one
// Midjourney task is in progress does a row get scheduled.
type midjourneyPollHandler struct{}

func (midjourneyPollHandler) Type() string { return domain.SystemTaskTypeMidjourneyPoll }

func (midjourneyPollHandler) Enabled() bool {
	return constant.UpdateTask && taskPolling().HasMidjourney()
}

func (midjourneyPollHandler) Interval() time.Duration { return 15 * time.Second }

func (midjourneyPollHandler) NewPayload() any { return nil }

func (midjourneyPollHandler) Run(ctx context.Context, task *domain.SystemTask, runnerID string) {
	summary := taskPolling().RunMidjourney(ctx, core.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, domain.SystemTaskStatusSucceeded, summary, nil)
}

// asyncTaskPollHandler runs one async-task (Suno/video) polling pass per
// scheduled run. Like midjourneyPollHandler, Enabled() folds in the unfinished
// task existence check so an idle system schedules no rows.
type asyncTaskPollHandler struct{}

func (asyncTaskPollHandler) Type() string { return domain.SystemTaskTypeAsyncTaskPoll }

func (asyncTaskPollHandler) Enabled() bool {
	return constant.UpdateTask && taskPolling().HasTasks()
}

func (asyncTaskPollHandler) Interval() time.Duration { return 15 * time.Second }

func (asyncTaskPollHandler) NewPayload() any { return nil }

func (asyncTaskPollHandler) Run(ctx context.Context, task *domain.SystemTask, runnerID string) {
	summary := taskPolling().RunTasks(ctx, core.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, domain.SystemTaskStatusSucceeded, summary, nil)
}

func finishSystemTaskHandler(task *domain.SystemTask, runnerID string, status domain.SystemTaskStatus, result any, runErr error) {
	errorMessage := ""
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	if err := core.Default().Store.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage); err != nil {
		common.SysLog(fmt.Sprintf("system task %s failed to persist result: %v", task.TaskID, err))
	}
}

func (pricingCatalogHandler) Type() string { return domain.SystemTaskTypePricingCatalog }
func (pricingCatalogHandler) Enabled() bool {
	return common.GetEnvOrDefaultBool("PRICING_CATALOG_TASK_ENABLED", true)
}
func (pricingCatalogHandler) Interval() time.Duration {
	minutes := common.GetEnvOrDefault("PRICING_CATALOG_TASK_INTERVAL_MINUTES", 30)
	if minutes < 1 {
		minutes = 30
	}
	return time.Duration(minutes) * time.Minute
}
func (pricingCatalogHandler) NewPayload() any { return nil }
func (pricingCatalogHandler) Run(ctx context.Context, task *domain.SystemTask, runnerID string) {
	summary, err := app.NewRefreshPricingCatalogue().Execute(ctx)
	status := domain.SystemTaskStatusSucceeded
	if err != nil {
		status = domain.SystemTaskStatusFailed
	}
	finishSystemTaskHandler(task, runnerID, status, summary, err)
}

type pricingCatalogHandler struct{}

type modelCostFXTaskHandler struct{}

func (modelCostFXTaskHandler) Type() string            { return domain.SystemTaskTypeModelCostFX }
func (modelCostFXTaskHandler) Enabled() bool           { return true }
func (modelCostFXTaskHandler) Interval() time.Duration { return time.Hour }
func (modelCostFXTaskHandler) NewPayload() any         { return nil }

func (modelCostFXTaskHandler) Run(ctx context.Context, task *domain.SystemTask, runnerID string) {
	result, err := pricingfx.Refresh().Execute(ctx)
	if err != nil {
		core.FailSystemTask(task, runnerID, err)
		return
	}
	if err := core.Default().Store.FinishSystemTask(task.TaskID, runnerID, domain.SystemTaskStatusSucceeded, result, ""); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("model cost FX task %s snapshot version %d but finish failed: %v", task.TaskID, result.Version, err))
	}
}
