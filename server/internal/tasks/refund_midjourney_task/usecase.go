package refund_midjourney_task

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

type Input struct {
	Task   *domain.Midjourney
	Reason string
}
type Billing interface {
	CreditWallet(userID, quota int) error
	RefundToken(context.Context, *domain.Midjourney, int)
}
type Store interface {
	UpdateMidjourneyBilling(*domain.Midjourney) error
}
type Usage interface {
	UserUsed(userID, delta int)
	ChannelUsed(channelID, delta int)
	RefundMidjourney(*domain.Midjourney, int, string)
}
type Diagnostics interface {
	Warn(context.Context, string)
	Error(context.Context, string)
}
type Usecase struct {
	billing     Billing
	store       Store
	usage       Usage
	diagnostics Diagnostics
}

func NewUsecase(billing Billing, store Store, usage Usage, diagnostics Diagnostics) Usecase {
	return Usecase{billing, store, usage, diagnostics}
}
func (u Usecase) Execute(ctx context.Context, input Input) (bool, error) {
	task, reason := input.Task, input.Reason
	quota := task.Quota
	if quota == 0 {
		return true, nil
	}

	if err := u.billing.CreditWallet(task.UserId, quota); err != nil {
		u.diagnostics.Warn(ctx, fmt.Sprintf("退还 Midjourney 用户额度失败 task %s: %s", task.MjId, err.Error()))
		return false, nil
	}

	u.billing.RefundToken(ctx, task, quota)

	billingChannelId := task.GetBillingChannelId()
	u.usage.UserUsed(task.UserId, -quota)
	u.usage.ChannelUsed(billingChannelId, -quota)
	u.usage.RefundMidjourney(task, quota, reason)

	task.Quota = 0
	if err := u.store.UpdateMidjourneyBilling(task); err != nil {
		u.diagnostics.Error(ctx, fmt.Sprintf("Midjourney 退款成功但清除 quota 失败 task %s: %s", task.MjId, err.Error()))
	}
	return true, nil
}
