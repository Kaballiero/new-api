package taskrecord

import (
	"context"

	tasksstore "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/tasks/record_midjourney_submission"
	"github.com/QuantumNous/new-api/model"
	billinggateway "github.com/QuantumNous/new-api/relay/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

var midjourneyNotifications = billinggateway.New()
var taskBilling = tasksapp.NewBilling()

func midjourneyInput(info *relaycommon.RelayInfo, task *model.Midjourney) record_midjourney_submission.Input {
	input := record_midjourney_submission.Input{Task: tasksstore.MidjourneyToDomain(task), HasRelayInfo: info != nil}
	if info != nil {
		input.HasChannelMeta = info.ChannelMeta != nil
		if info.ChannelMeta != nil {
			input.ChannelID = info.ChannelId
		}
		input.BillingSource = info.BillingSource
		input.SubscriptionID = info.SubscriptionId
		input.Account = billingdomain.Account{UserID: info.UserId, TokenID: info.TokenId, TokenKey: info.TokenKey, Unlimited: info.TokenUnlimited, Playground: info.IsPlayground}
	}
	return input
}
func PrepareMidjourneyTaskBilling(info *relaycommon.RelayInfo, task *model.Midjourney, quota int, shouldBill bool) (bool, error) {
	input := midjourneyInput(info, task)
	input.Quota, input.ShouldBill = quota, shouldBill
	prepared, err := taskBilling.PrepareMidjourney(input)
	if task != nil {
		*task = *tasksstore.MidjourneyFromDomain(input.Task)
	}
	return prepared, err
}
func SettleMidjourneyTaskBilling(info *relaycommon.RelayInfo, task *model.Midjourney, prepared bool) (bool, error) {
	input := midjourneyInput(info, task)
	input.Prepared = prepared
	input.Notify = func() error { return midjourneyNotifications.NotifyQuota(info, input.Task.Quota, 0, false) }
	billed, err := taskBilling.SettleMidjourney(context.Background(), input)
	if task != nil {
		*task = *tasksstore.MidjourneyFromDomain(input.Task)
	}
	return billed, err
}
