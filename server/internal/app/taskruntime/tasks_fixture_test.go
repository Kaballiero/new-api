package taskruntime

import (
	"context"
	"github.com/QuantumNous/new-api/internal/tasks/apply_sync_operator_decision"
	"testing"

	"github.com/QuantumNous/new-api/common"
	artifacts "github.com/QuantumNous/new-api/internal/adapter/artifacts"
	billingadapter "github.com/QuantumNous/new-api/internal/adapter/billing"
	taskplugins "github.com/QuantumNous/new-api/internal/adapter/gateway/taskplugins"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	effects "github.com/QuantumNous/new-api/internal/adapter/taskruntime"
	"github.com/QuantumNous/new-api/internal/app/taskbillingruntime"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	"github.com/QuantumNous/new-api/internal/app/usagestorage"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	pricingdomainfx "github.com/QuantumNous/new-api/internal/pricing/domain"
	taskdomain "github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/settle_task"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/taskrecord"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/stretchr/testify/require"
)

const (
	BillingSourceWallet       = billingdomain.Wallet
	BillingSourceSubscription = billingdomain.Subscription
)

var taskFixtureBilling = tasksapp.NewBilling()
var fixtureStore = tasksapp.NewStore()
var LogTaskConsumption = taskrecord.LogTaskConsumption
var InitTaskSubmission = taskrecord.InitTaskSubmission
var PrepareMidjourneyTaskBilling = taskrecord.PrepareMidjourneyTaskBilling
var SettleMidjourneyTaskBilling = taskrecord.SettleMidjourneyTaskBilling

func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) bool {
	d := record.ToDomain(task)
	ok := taskFixtureBilling.Refund(ctx, d, reason)
	*task = *record.FromDomain(d)
	return ok
}
func RefundMidjourneyQuota(ctx context.Context, task *model.Midjourney, reason string) bool {
	d := storage.MidjourneyToDomain(task)
	ok := taskFixtureBilling.RefundMidjourney(ctx, d, reason)
	*task = *storage.MidjourneyFromDomain(d)
	return ok
}
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actual int, reason string, clamps ...*common.QuotaClamp) {
	d := record.ToDomain(task)
	out := make([]*billingdomain.Clamp, len(clamps))
	for i, c := range clamps {
		out[i] = billingadapter.Clamp(c)
	}
	store := storage.BillingStore{Store: fixtureStore}
	settlement := settle_task.NewUsecase(store, taskbillingruntime.Delta(), usagestorage.TaskBilling(), taskbillingruntime.Prices(),
		effects.TaskMath{}, billingadapter.Expression{}, effects.Diagnostics{}, store, pricingdomainfx.ModelCostFXSource, common.RequestIdKey)
	settlement.Recalculate(ctx, d, actual, reason, nil, out...)
	*task = *record.FromDomain(d)
}
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, tokens int) bool {
	d := record.ToDomain(task)
	handled, _ := taskFixtureBilling.Settle(ctx, d, &taskdomain.TaskInfo{TotalTokens: tokens}, func(*taskdomain.Task, *taskdomain.TaskInfo) int { return 0 })
	*task = *record.FromDomain(d)
	return handled
}

func prepareOpenRouterCostSettlement(task *model.Task) {
	d := record.ToDomain(task)
	taskFixtureBilling.Prepare(d)
	*task = *record.FromDomain(d)
}
func settleOpenRouterCost(task *model.Task) error {
	d := record.ToDomain(task)
	err := taskFixtureBilling.SettleDurable(d)
	*task = *record.FromDomain(d)
	return err
}
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, result *relaycommon.TaskInfo) (bool, error) {
	d := record.ToDomain(task)
	r := taskplugins.TaskInfoToDomain(result)
	handled, err := taskFixtureBilling.Settle(ctx, d, r, func(t *taskdomain.Task, r *taskdomain.TaskInfo) int {
		return adaptor.AdjustBillingOnComplete(record.FromDomain(t), taskplugins.TaskInfoFromDomain(r))
	})
	*task = *record.FromDomain(d)
	return handled, err
}

var BeginOpenRouterSyncTask = taskrecord.BeginOpenRouterSyncTask
var CommitOpenRouterSyncReservation = taskrecord.CommitOpenRouterSyncReservation
var ConfigureOpenRouterSyncSubmission = taskrecord.ConfigureOpenRouterSyncSubmission
var RecordOpenRouterSyncGeneration = taskrecord.RecordOpenRouterSyncGeneration
var RecordOpenRouterSyncSubmission = taskrecord.RecordOpenRouterSyncSubmission
var CompleteOpenRouterSyncTask = taskrecord.CompleteOpenRouterSyncTask
var FailOpenRouterSyncTask = taskrecord.FailOpenRouterSyncTask
var HasOpenRouterSyncTask = relaycommon.HasOpenRouterSyncTask

func pollOpenRouterSync(ctx context.Context, task *model.Task) error {
	d := record.ToDomain(task)
	err := tasksapp.NewReconciler().Poll(ctx, d)
	*task = *record.FromDomain(d)
	return err
}
func openRouterSyncTask(info *relaycommon.RelayInfo) (*model.Task, error) {
	task, _, err := fixtureStore.GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
	return task, err
}

func disableTaskFixtureArtifacts(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { require.NoError(t, artifacts.InitTaskArtifactStore()) })
	t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeUpstream)
	require.NoError(t, artifacts.InitTaskArtifactStore())
}

var ErrOpenRouterSyncOperatorInvalid = taskdomain.ErrOpenRouterSyncOperatorInvalid
var ErrOpenRouterSyncOperatorConflict = taskdomain.ErrOpenRouterSyncOperatorConflict
var HoldOpenRouterSyncTaskForReconciliation = taskrecord.HoldOpenRouterSyncTaskForReconciliation

func ApplyOpenRouterSyncOperatorDecision(taskID, action, cost, evidence string, operatorID int) (taskdomain.OpenRouterSyncOperatorResult, error) {
	return tasksapp.NewReconciler().Operator(context.Background(), apply_sync_operator_decision.Input{TaskID: taskID, Action: action, CostUSD: cost, Evidence: evidence, OperatorID: operatorID})
}
func OpenRouterSyncReconciliation(task *model.Task) map[string]any {
	return taskdomain.OpenRouterSyncReconciliation(record.ToDomain(task))
}
func OpenRouterSyncAdminView(task *model.Task) map[string]any {
	return taskdomain.OpenRouterSyncAdminView(record.ToDomain(task))
}
