package controller

import (
	"context"
	"sync"

	"github.com/QuantumNous/new-api/internal/app"
	"github.com/QuantumNous/new-api/internal/app/systemruntime"
	taskruntime "github.com/QuantumNous/new-api/internal/app/taskruntime"
	systemdomain "github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/internal/tasks/poll_midjourney_tasks"
)

var taskPolling = sync.OnceValue(taskruntime.NewPolling)

func runMidjourneyTaskUpdateOnce(ctx context.Context, report func(processed, total int)) poll_midjourney_tasks.Summary {
	return taskPolling().RunMidjourney(ctx, report)
}

func runChannelUpstreamModelUpdateTaskOnce(ctx context.Context, force, auto bool, report func(int, int)) upstreamModelUpdateSummary {
	return app.NewScanChannelModelsUsecase(channelEffects()).Execute(ctx, force, auto, report)
}

func scheduledTaskHandlers() []systemdomain.SystemTaskHandler {
	var handlers []systemdomain.SystemTaskHandler
	systemruntime.RegisterScheduledSystemTasks(channelEffects(), func(h systemdomain.SystemTaskHandler) { handlers = append(handlers, h) })
	return handlers
}
