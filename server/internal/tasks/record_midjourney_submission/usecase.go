package record_midjourney_submission

import (
	"context"
	"errors"
	"fmt"

	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

type Input struct {
	Task                                               *domain.Midjourney
	Quota                                              int
	ShouldBill, Prepared, HasRelayInfo, HasChannelMeta bool
	BillingSource                                      string
	SubscriptionID, ChannelID                          int
	Account                                            billingdomain.Account
	Notify                                             func() error
}
type Billing interface {
	Apply(context.Context, billingdomain.Account, string, int, int) (domain.DeltaResult, error)
}
type Store interface {
	UpdateMidjourneyBilling(*domain.Midjourney) error
}
type Usecase struct {
	store   Store
	billing Billing
}

func NewUsecase(store Store, billing Billing) Usecase { return Usecase{store, billing} }
func (u Usecase) Prepare(input Input) (bool, error) {
	task, quota, shouldBill := input.Task, input.Quota, input.ShouldBill
	if task == nil {
		return false, errors.New("Midjourney task is nil")
	}
	task.Quota = 0
	task.TokenId = 0
	task.BillingChannelId = 0
	if !shouldBill {
		return false, nil
	}
	if !input.HasRelayInfo {
		return false, errors.New("relay info is nil")
	}
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	if input.BillingSource == billingdomain.Subscription {
		return false, errors.New("legacy Midjourney billing does not support subscriptions")
	}

	task.Quota = quota
	task.BillingChannelId = task.ChannelId
	if input.HasChannelMeta && input.ChannelID > 0 {
		task.BillingChannelId = input.ChannelID
	}
	return true, nil
}

// SettleMidjourneyTaskBilling charges a persisted legacy task and records the applied stages.
func (u Usecase) Execute(ctx context.Context, input Input) (bool, error) {
	task, prepared := input.Task, input.Prepared
	if !prepared {
		return false, nil
	}
	if !input.HasRelayInfo {
		return false, errors.New("relay info is nil")
	}
	if task == nil || task.Id == 0 {
		return false, errors.New("Midjourney task must be persisted before billing")
	}

	result, billingErr := u.billing.Apply(ctx, input.Account, input.BillingSource, input.SubscriptionID, task.Quota)
	if billingErr == nil && input.Notify != nil {
		billingErr = input.Notify()
	}
	if !result.FundingApplied {
		task.Quota = 0
		task.TokenId = 0
		task.BillingChannelId = 0
		if updateErr := u.store.UpdateMidjourneyBilling(task); updateErr != nil {
			return false, errors.Join(billingErr, fmt.Errorf("clear Midjourney billing state: %w", updateErr))
		}
		return false, billingErr
	}

	task.TokenId = 0
	if result.TokenApplied {
		task.TokenId = input.Account.TokenID
	}
	if updateErr := u.store.UpdateMidjourneyBilling(task); updateErr != nil {
		return true, errors.Join(billingErr, fmt.Errorf("update Midjourney billing state: %w", updateErr))
	}
	return true, billingErr
}
