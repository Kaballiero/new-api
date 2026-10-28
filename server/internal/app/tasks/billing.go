package tasks

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	billingmath "github.com/QuantumNous/new-api/internal/adapter/billing"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	runtime "github.com/QuantumNous/new-api/internal/adapter/taskruntime"
	"github.com/QuantumNous/new-api/internal/app/taskbillingruntime"
	"github.com/QuantumNous/new-api/internal/app/usagestorage"
	pricingdomainfx "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/record_midjourney_submission"
	"github.com/QuantumNous/new-api/internal/tasks/refund_midjourney_task"
	"github.com/QuantumNous/new-api/internal/tasks/refund_task"
	"github.com/QuantumNous/new-api/internal/tasks/settle_task"
)

type Billing struct {
	completion settle_task.Usecase
	refund     refund_task.Usecase
	mjrefund   refund_midjourney_task.Usecase
	mjsubmit   record_midjourney_submission.Usecase
}

func NewBilling() *Billing {
	store := storage.BillingStore{Store: NewStore()}
	delta := taskbillingruntime.Delta()
	metadata := usagestorage.TaskBilling()
	diagnostics := runtime.Diagnostics{}
	return &Billing{
		completion: settle_task.NewUsecase(store, delta, metadata, taskbillingruntime.Prices(), runtime.TaskMath{}, billingmath.Expression{}, diagnostics, store, pricingdomainfx.ModelCostFXSource, common.RequestIdKey),
		refund:     refund_task.NewUsecase(store, delta, metadata, diagnostics),
		mjrefund:   refund_midjourney_task.NewUsecase(taskbillingruntime.Midjourney(), store, metadata, diagnostics),
		mjsubmit:   record_midjourney_submission.NewUsecase(store, taskbillingruntime.Midjourney()),
	}
}
func (b *Billing) Settle(ctx context.Context, task *domain.Task, result *domain.TaskInfo, adjust func(*domain.Task, *domain.TaskInfo) int) (bool, error) {
	return b.completion.Execute(ctx, settle_task.Input{Task: task, Result: result, Adjust: adjust})
}
func (b *Billing) Prepare(task *domain.Task) { b.completion.Prepare(task) }
func (b *Billing) Refund(ctx context.Context, task *domain.Task, reason string) bool {
	ok, _ := b.refund.Execute(ctx, refund_task.Input{Task: task, Reason: reason})
	return ok
}
func (b *Billing) IndependentRefund(task *domain.Task) bool {
	return domain.HasIndependentTaskRefundEntitlement(task)
}
func (b *Billing) RefundMidjourney(ctx context.Context, task *domain.Midjourney, reason string) bool {
	ok, _ := b.mjrefund.Execute(ctx, refund_midjourney_task.Input{Task: task, Reason: reason})
	return ok
}
func (b *Billing) PrepareMidjourney(input record_midjourney_submission.Input) (bool, error) {
	return b.mjsubmit.Prepare(input)
}
func (b *Billing) SettleMidjourney(ctx context.Context, input record_midjourney_submission.Input) (bool, error) {
	return b.mjsubmit.Execute(ctx, input)
}

func (b *Billing) SettleDurable(task *domain.Task) error { return b.completion.SettleDurable(task) }
