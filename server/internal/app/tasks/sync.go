package tasks

import (
	"context"
	"github.com/QuantumNous/new-api/internal/tasks/apply_sync_operator_decision"
	"time"

	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	httpadapter "github.com/QuantumNous/new-api/internal/adapter/http/tasksync"
	runtime "github.com/QuantumNous/new-api/internal/adapter/taskruntime"
	"github.com/QuantumNous/new-api/internal/app/channelruntime"
	"github.com/QuantumNous/new-api/internal/app/notifications"
	"github.com/QuantumNous/new-api/internal/tasks/alert_sync_reconciliation"
	"github.com/QuantumNous/new-api/internal/tasks/begin_sync_task"
	"github.com/QuantumNous/new-api/internal/tasks/commit_sync_reservation"
	"github.com/QuantumNous/new-api/internal/tasks/complete_sync_task"
	"github.com/QuantumNous/new-api/internal/tasks/configure_sync_submission"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/fail_sync_task"
	"github.com/QuantumNous/new-api/internal/tasks/get_sync_task"
	"github.com/QuantumNous/new-api/internal/tasks/reconcile_sync_task"
	"github.com/QuantumNous/new-api/internal/tasks/record_sync_generation"
	"github.com/QuantumNous/new-api/internal/tasks/record_sync_receipt"
	"github.com/QuantumNous/new-api/internal/tasks/refuse_sync_reservation"
	"github.com/QuantumNous/new-api/internal/tasks/release_unidentified_sync_holds"
	"github.com/QuantumNous/new-api/internal/tasks/settle_sync_estimates"
)

// Reconciler assembles durable synchronous-task actions without a gateway dependency.
type Reconciler struct {
	operator   apply_sync_operator_decision.Usecase
	begin      begin_sync_task.Usecase
	load       get_sync_task.Usecase
	commit     commit_sync_reservation.Usecase
	refuse     refuse_sync_reservation.Usecase
	configure  configure_sync_submission.Usecase
	generation record_sync_generation.Usecase
	receipt    record_sync_receipt.Usecase
	complete   complete_sync_task.Usecase
	fail       fail_sync_task.Usecase
	poll       reconcile_sync_task.Usecase
	release    release_unidentified_sync_holds.Usecase
	estimates  settle_sync_estimates.Usecase
	alert      alert_sync_reconciliation.Usecase
}

func NewReconciler() *Reconciler {
	store := storage.SyncStore{BillingStore: storage.BillingStore{Store: NewStore()}}
	projection := storage.SyncProjection{}
	accounts := httpadapter.Accounts{}
	billing := NewBilling()
	return &Reconciler{
		operator:   apply_sync_operator_decision.NewUsecase(store, billing, projection, time.Now),
		begin:      begin_sync_task.NewUsecase(store, accounts, projection, time.Now),
		load:       get_sync_task.NewUsecase(store),
		commit:     commit_sync_reservation.NewUsecase(store, projection, time.Now),
		refuse:     refuse_sync_reservation.NewUsecase(store, projection, time.Now),
		configure:  configure_sync_submission.NewUsecase(store, accounts, time.Now),
		generation: record_sync_generation.NewUsecase(store),
		receipt:    record_sync_receipt.NewUsecase(store, projection, billing, time.Now),
		complete:   complete_sync_task.NewUsecase(store, projection, billing, runtime.Diagnostics{}, time.Now),
		fail:       fail_sync_task.NewUsecase(store, projection, billing, time.Now),
		poll:       reconcile_sync_task.NewUsecase(store, projection, billing, httpadapter.Metadata{}, time.Now),
		release:    release_unidentified_sync_holds.NewUsecase(store, billing, projection, runtime.Diagnostics{}, time.Now),
		estimates:  settle_sync_estimates.NewUsecase(store, billing, projection, reconciliationAlerts{}, runtime.Diagnostics{}, time.Now),
		alert:      alert_sync_reconciliation.NewUsecase(store, reconciliationAlerts{}, time.Now),
	}
}
func (r *Reconciler) Begin(ctx context.Context, input begin_sync_task.Input) (*domain.Task, error) {
	return r.begin.Execute(ctx, input)
}
func (r *Reconciler) Load(ctx context.Context, input domain.SyncIdentity) (*domain.Task, error) {
	return r.load.Execute(ctx, input)
}
func (r *Reconciler) Commit(ctx context.Context, input commit_sync_reservation.Input) error {
	return r.commit.Execute(ctx, input)
}
func (r *Reconciler) Refuse(ctx context.Context, task *domain.Task) error {
	return r.refuse.Execute(ctx, task)
}
func (r *Reconciler) Configure(ctx context.Context, input configure_sync_submission.Input) error {
	return r.configure.Execute(ctx, input)
}
func (r *Reconciler) Generation(ctx context.Context, input record_sync_generation.Input) error {
	return r.generation.Execute(ctx, input)
}
func (r *Reconciler) Receipt(ctx context.Context, input record_sync_receipt.Input) error {
	return r.receipt.Execute(ctx, input)
}
func (r *Reconciler) Complete(ctx context.Context, input complete_sync_task.Input) error {
	return r.complete.Execute(ctx, input)
}
func (r *Reconciler) Fail(ctx context.Context, input fail_sync_task.Input) error {
	return r.fail.Execute(ctx, input)
}
func (r *Reconciler) Poll(ctx context.Context, task *domain.Task) error {
	return r.poll.Execute(ctx, task)
}

func (r *Reconciler) Operator(ctx context.Context, input apply_sync_operator_decision.Input) (domain.OpenRouterSyncOperatorResult, error) {
	return r.operator.Execute(ctx, input)
}
func (r *Reconciler) ReleaseUnidentified(ctx context.Context) error {
	return r.release.Execute(ctx)
}
func (r *Reconciler) SettleEstimates(ctx context.Context) error {
	return r.estimates.Execute(ctx)
}
func (r *Reconciler) AlertManualHolds(ctx context.Context) error {
	return r.alert.Execute(ctx)
}

// reconciliationAlerts sends the manual hold and estimate charge summaries to
// the Telegram chat in the root user's language, like the upstream catalog
// alerts.
type reconciliationAlerts struct{}

func (reconciliationAlerts) ManualHolds(holds []*domain.Task) error {
	subject, content := runtime.ManualHoldsNotification(channelruntime.RootUserLanguage(), holds)
	return notifications.TelegramChat(subject, content)
}

func (reconciliationAlerts) EstimateSettlements(tasks []*domain.Task) error {
	subject, content := runtime.EstimateSettlementsNotification(channelruntime.RootUserLanguage(), tasks)
	return notifications.TelegramChat(subject, content)
}
