package tasks

import (
	"context"
	"time"

	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	httpadapter "github.com/QuantumNous/new-api/internal/adapter/http/tasksync"
	runtime "github.com/QuantumNous/new-api/internal/adapter/taskruntime"
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
)

// Reconciler assembles durable synchronous-task actions without a gateway dependency.
type Reconciler struct {
	begin      begin_sync_task.Usecase
	load       get_sync_task.Usecase
	commit     commit_sync_reservation.Usecase
	configure  configure_sync_submission.Usecase
	generation record_sync_generation.Usecase
	receipt    record_sync_receipt.Usecase
	complete   complete_sync_task.Usecase
	fail       fail_sync_task.Usecase
	poll       reconcile_sync_task.Usecase
}

func NewReconciler() *Reconciler {
	store := storage.SyncStore{BillingStore: storage.BillingStore{Store: NewStore()}}
	projection := storage.SyncProjection{}
	accounts := httpadapter.Accounts{}
	billing := NewBilling()
	return &Reconciler{
		begin:      begin_sync_task.NewUsecase(store, accounts, projection, time.Now),
		load:       get_sync_task.NewUsecase(store),
		commit:     commit_sync_reservation.NewUsecase(store, projection, time.Now),
		configure:  configure_sync_submission.NewUsecase(store, accounts, time.Now),
		generation: record_sync_generation.NewUsecase(store),
		receipt:    record_sync_receipt.NewUsecase(store, projection, time.Now),
		complete:   complete_sync_task.NewUsecase(store, projection, billing, runtime.Diagnostics{}, time.Now),
		fail:       fail_sync_task.NewUsecase(store, projection, billing, time.Now),
		poll:       reconcile_sync_task.NewUsecase(store, projection, billing, httpadapter.Metadata{}, time.Now),
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
