package taskruntime

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/constant"
	taskplugins "github.com/QuantumNous/new-api/internal/adapter/gateway/taskplugins"
	taskstore "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	taskhttp "github.com/QuantumNous/new-api/internal/adapter/http/tasks"
	effects "github.com/QuantumNous/new-api/internal/adapter/taskruntime"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/taskbillingruntime"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/poll_midjourney_tasks"
	"github.com/QuantumNous/new-api/internal/tasks/poll_tasks"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

// Polling assembles the same one-pass scenarios for scheduled handlers and
// directly linked binaries. The system runner retains timer and lease ownership.
type Polling struct {
	tasks      *poll_tasks.Usecase
	midjourney *poll_midjourney_tasks.Usecase
	reconciler *tasksapp.Reconciler
	store      taskstore.Store
}

func NewPolling() *Polling {
	store := taskstore.NewStore(func() *gorm.DB { return model.DB }, accountstorage.Cache())
	billing := tasksapp.NewBilling()
	diagnostics := effects.Diagnostics{}
	codec := effects.Codec{}
	reconciler := tasksapp.NewReconciler()
	return &Polling{
		store:      store,
		reconciler: reconciler,
		tasks: poll_tasks.NewUsecase(taskstore.PollingStore{Store: store}, effects.PollChannels{}, taskplugins.PollingFactory{},
			billing, reconciler, codec, diagnostics, poll_tasks.Limits{
				TimeoutMinutes: func() int { return constant.TaskTimeoutMinutes }, QueryLimit: func() int { return constant.TaskQueryLimit },
				MaxFailures: func() int { return constant.TaskPollMaxFailures },
			}, time.Now, gopool.Go, taskcommon.BuildProxyURL, taskbillingruntime.Text()),
		midjourney: poll_midjourney_tasks.NewUsecase(taskstore.MidjourneyPollingStore{Store: store}, effects.MidjourneyChannels{},
			taskhttp.MidjourneyPollClient{}, codec, billing, diagnostics, time.Now, taskbillingruntime.Text()),
	}
}

// HasTasks also schedules a pass when an unknown-submission hold has outlived
// the auto-release delay, so an otherwise idle system still releases it.
func (p *Polling) HasTasks() bool {
	unfinished := p.store.HasUnfinishedSyncTasks()
	dueHolds := p.store.HasUnknownSubmissionHolds(time.Now().Add(-domain.OpenRouterSyncAutoReleaseDelay).Unix())
	return unfinished || dueHolds
}
func (p *Polling) HasMidjourney() bool { return p.store.HasUnfinishedMidjourneyTasks() }

func (p *Polling) RunTasks(ctx context.Context, report func(processed, total int)) poll_tasks.TaskPollSummary {
	summary, _ := p.tasks.Execute(ctx, report)
	if err := p.reconciler.ReleaseUnidentified(ctx); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("OpenRouter sync automatic release pass failed: %v", err))
	}
	return summary
}

func (p *Polling) RunMidjourney(ctx context.Context, report func(processed, total int)) poll_midjourney_tasks.Summary {
	summary, _ := p.midjourney.Execute(ctx, report)
	return summary
}
