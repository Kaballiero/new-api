package taskruntime

import (
	"context"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/constant"
	taskplugins "github.com/QuantumNous/new-api/internal/adapter/gateway/taskplugins"
	taskstore "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	taskhttp "github.com/QuantumNous/new-api/internal/adapter/http/tasks"
	effects "github.com/QuantumNous/new-api/internal/adapter/taskruntime"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/taskbillingruntime"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	"github.com/QuantumNous/new-api/internal/tasks/poll_midjourney_tasks"
	"github.com/QuantumNous/new-api/internal/tasks/poll_tasks"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
)

// Polling assembles the same one-pass scenarios for scheduled handlers and
// directly linked binaries. The system runner retains timer and lease ownership.
type Polling struct {
	tasks      *poll_tasks.Usecase
	midjourney *poll_midjourney_tasks.Usecase
	store      taskstore.Store
}

func NewPolling() *Polling {
	store := taskstore.NewStore(func() *gorm.DB { return model.DB }, accountstorage.Cache())
	billing := tasksapp.NewBilling()
	diagnostics := effects.Diagnostics{}
	codec := effects.Codec{}
	return &Polling{
		store: store,
		tasks: poll_tasks.NewUsecase(taskstore.PollingStore{Store: store}, effects.PollChannels{}, taskplugins.PollingFactory{},
			billing, tasksapp.NewReconciler(), codec, diagnostics, poll_tasks.Limits{
				TimeoutMinutes: func() int { return constant.TaskTimeoutMinutes }, QueryLimit: func() int { return constant.TaskQueryLimit },
				MaxFailures: func() int { return constant.TaskPollMaxFailures },
			}, time.Now, gopool.Go, taskcommon.BuildProxyURL, taskbillingruntime.Text()),
		midjourney: poll_midjourney_tasks.NewUsecase(taskstore.MidjourneyPollingStore{Store: store}, effects.MidjourneyChannels{},
			taskhttp.MidjourneyPollClient{}, codec, billing, diagnostics, time.Now, taskbillingruntime.Text()),
	}
}

func (p *Polling) HasTasks() bool      { return p.store.HasUnfinishedSyncTasks() }
func (p *Polling) HasMidjourney() bool { return p.store.HasUnfinishedMidjourneyTasks() }

func (p *Polling) RunTasks(ctx context.Context, report func(processed, total int)) poll_tasks.TaskPollSummary {
	summary, _ := p.tasks.Execute(ctx, report)
	return summary
}

func (p *Polling) RunMidjourney(ctx context.Context, report func(processed, total int)) poll_midjourney_tasks.Summary {
	summary, _ := p.midjourney.Execute(ctx, report)
	return summary
}
