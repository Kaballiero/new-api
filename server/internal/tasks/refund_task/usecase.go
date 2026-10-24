package refund_task

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

type Input struct {
	Task   *domain.Task
	Reason string
}
type Store interface {
	ClaimRefund(task *domain.Task, quota int) (bool, error)
	Transition(task *domain.Task, from domain.TaskStatus) (bool, error)
	UpdateQuota(task *domain.Task) error
}
type Billing interface {
	Apply(ctx context.Context, task *domain.Task, delta int) (domain.DeltaResult, error)
}
type Usage interface {
	UserUsed(userID, delta int)
	ChannelUsed(channelID, delta int)
	Refund(task *domain.Task, quota int, reason string)
}
type Diagnostics interface {
	Warn(context.Context, string)
	Error(context.Context, string)
}
type Usecase struct {
	store       Store
	billing     Billing
	usage       Usage
	diagnostics Diagnostics
}

func NewUsecase(store Store, billing Billing, usage Usage, diagnostics Diagnostics) Usecase {
	return Usecase{store, billing, usage, diagnostics}
}
func (u Usecase) Execute(ctx context.Context, input Input) (bool, error) {
	task, reason := input.Task, input.Reason
	if bc := task.PrivateData.BillingContext; bc != nil && bc.SettlementMode != "" {
		return false, nil
	}
	reconciliation := task.PrivateData.Reconciliation
	if reconciliation != nil {
		if reconciliation.RefundPending || reconciliation.ReservationPending || reconciliation.SubmissionPending || !reconciliation.HasUpstreamCost || !reconciliation.UpstreamCostConfirmed || reconciliation.UpstreamCostUSD != 0 {
			reconciliation.Required = true
			return false, nil
		}
	}
	quota := task.Quota
	if quota == 0 {
		return true, nil
	}
	if reconciliation != nil {
		won, err := u.store.ClaimRefund(task, quota)
		if err != nil || !won {
			return false, nil
		}
	}

	// Refund the recorded wallet or subscription funding first.
	outcome, billingErr := u.billing.Apply(ctx, task, -quota)
	if !outcome.FundingApplied {
		err := billingErr
		u.diagnostics.Warn(ctx, fmt.Sprintf("Failed to refund funding for task %s: %s", task.TaskID, err.Error()))
		return false, nil
	}

	// Retain the token leg error for reconciliation markers.
	tokenErr := billingErr

	// Reverse usage counters without changing the request count.
	u.usage.UserUsed(task.UserId, -quota)
	u.usage.ChannelUsed(task.ChannelId, -quota)

	// Record the existing refund evidence.
	u.usage.Refund(task, quota, reason)

	// Clear the durable marker only after funding succeeded.
	// A marker write failure still warns about a possible repeated refund.
	task.Quota = 0
	if reconciliation != nil {
		if tokenErr != nil {
			return false, nil
		}
		reconciliation.RefundPending = false
		reconciliation.Required = false
		reconciliation.Finalized = true
		if _, err := u.store.Transition(task, task.Status); err != nil {
			u.diagnostics.Error(ctx, fmt.Sprintf("task %s refund completed but reconciliation marker is pending", task.TaskID))
			return false, nil
		}
		return true, nil
	}
	if err := u.store.UpdateQuota(task); err != nil {
		u.diagnostics.Error(ctx, fmt.Sprintf("Refund succeeded but failed to clear quota for task %s: %s", task.TaskID, err.Error()))
	}
	return true, nil
}
