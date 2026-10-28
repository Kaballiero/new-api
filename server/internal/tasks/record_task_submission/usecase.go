package record_task_submission

import (
	"context"

	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

type Input struct {
	Task                    *domain.Task
	Execution               *domain.TaskExecutionSnapshot
	Source                  string
	SubscriptionID, TokenID int
	NodeName                string
	Billing                 *domain.TaskBillingContext
	CostBasis               *billingdomain.ProviderBasis
	OrderFacts              map[string]any
	AccountKey              string
	Quota                   int
	Action                  string
}
type Usecase struct{}

func NewUsecase() Usecase { return Usecase{} }

// Execute freezes the accepted execution and tariff before the durable insert.
func (Usecase) Execute(_ context.Context, input Input) (*domain.Task, error) {
	task := input.Task
	task.PrivateData.Execution = input.Execution
	task.PrivateData.BillingSource, task.PrivateData.SubscriptionId = input.Source, input.SubscriptionID
	task.PrivateData.TokenId, task.PrivateData.NodeName = input.TokenID, input.NodeName
	task.PrivateData.BillingContext = input.Billing
	execution := input.Execution
	if input.CostBasis != nil || (execution != nil && execution.TaskPlugin != nil && execution.TaskPlugin.Key == "openrouter-video") {
		input.Billing.SettlementMode = domain.TaskSettlementOpenRouterCostV1
		if input.CostBasis != nil {
			basis := *input.CostBasis
			basis.Version = domain.TaskSettlementOpenRouterCostV2
			input.Billing.SettlementMode = domain.TaskSettlementOpenRouterCostV2
			input.Billing.OpenRouterCost = &basis
			input.Billing.OpenRouterOrderFacts = input.OrderFacts
		}
		task.PrivateData.Key = input.AccountKey
		task.PrivateData.Reconciliation = &domain.TaskReconciliation{ReservedQuota: input.Quota, ReservationPending: true, Required: true}
	}
	task.Quota, task.Action = input.Quota, input.Action
	return task, nil
}
