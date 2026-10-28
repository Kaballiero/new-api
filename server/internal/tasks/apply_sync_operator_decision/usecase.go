package apply_sync_operator_decision

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/internal/tasks/domain"

	"github.com/shopspring/decimal"
)

type Store interface {
	Unique(string) (*domain.Task, bool, error)
	Feasible(*domain.Task) error
	UpdateRevision(*domain.Task, int) error
}
type Settlement interface {
	Prepare(*domain.Task)
	SettleDurable(*domain.Task) error
}
type Projection interface{ Project(*domain.Task) }
type Input struct {
	TaskID, Action, CostUSD, Evidence string
	OperatorID                        int
}
type Usecase struct {
	store      Store
	settlement Settlement
	projection Projection
	now        func() time.Time
}

func NewUsecase(store Store, settlement Settlement, projection Projection, now func() time.Time) Usecase {
	return Usecase{store, settlement, projection, now}
}
func (u Usecase) Execute(_ context.Context, input Input) (domain.OpenRouterSyncOperatorResult, error) {
	taskID, action, costUSD, evidence, operatorID := input.TaskID, input.Action, input.CostUSD, input.Evidence, input.OperatorID
	result := domain.OpenRouterSyncOperatorResult{TaskID: taskID, Action: action, CostUSD: costUSD}
	evidence = strings.TrimSpace(evidence)
	if evidence == "" || utf8.RuneCountInString(evidence) > 4096 {
		return result, fmt.Errorf("%w: bounded evidence is required", domain.ErrOpenRouterSyncOperatorInvalid)
	}
	if action != domain.OpenRouterSyncOperatorRelease && action != domain.OpenRouterSyncOperatorSettle {
		return result, fmt.Errorf("%w: unknown action", domain.ErrOpenRouterSyncOperatorInvalid)
	}
	if action == domain.OpenRouterSyncOperatorRelease && costUSD == "" {
		costUSD = "0"
	}
	if len(costUSD) > 128 {
		return result, fmt.Errorf("%w: invalid cost_usd", domain.ErrOpenRouterSyncOperatorInvalid)
	}
	cost, err := decimal.NewFromString(costUSD)
	if err != nil || cost.IsNegative() || cost.Exponent() < -12 || cost.Exponent() > 12 || cost.Coefficient().BitLen() > 128 {
		return result, fmt.Errorf("%w: invalid cost_usd", domain.ErrOpenRouterSyncOperatorInvalid)
	}
	if action == domain.OpenRouterSyncOperatorRelease && !cost.IsZero() {
		return result, fmt.Errorf("%w: release requires a zero cost", domain.ErrOpenRouterSyncOperatorInvalid)
	}
	if action == domain.OpenRouterSyncOperatorSettle && (!cost.IsPositive() || cost.GreaterThan(decimal.NewFromInt(domain.OpenRouterSyncOperatorMaxCostUSD))) {
		return result, fmt.Errorf("%w: settle cost must be positive and at most %d USD", domain.ErrOpenRouterSyncOperatorInvalid, domain.OpenRouterSyncOperatorMaxCostUSD)
	}
	costValue := cost.InexactFloat64()
	if math.IsNaN(costValue) || math.IsInf(costValue, 0) {
		return result, fmt.Errorf("%w: invalid cost_usd", domain.ErrOpenRouterSyncOperatorInvalid)
	}
	result.CostUSD = cost.String()

	task, exists, err := u.store.Unique(taskID)
	if err != nil {
		return result, err
	}
	if !exists || task.PrivateData.OpenRouterSyncState() == nil || task.PrivateData.Reconciliation == nil {
		return result, fmt.Errorf("%w: task is not an OpenRouter synchronous task", domain.ErrOpenRouterSyncOperatorConflict)
	}
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	result.Platform, result.ReservedQuota, result.TargetQuota = task.Platform, r.ReservedQuota, r.TargetQuota
	if d := a.OperatorDecision; d != nil {
		if d.Action != action || d.CostUSD != result.CostUSD {
			return result, fmt.Errorf("%w: another operator decision is recorded", domain.ErrOpenRouterSyncOperatorConflict)
		}
		switch {
		case r.Finalized:
			result.AlreadyApplied = true
		case r.MainApplied:
			return result, fmt.Errorf("%w: settlement_repair_required", domain.ErrOpenRouterSyncOperatorConflict)
		case r.SettlementPending:
			if err := u.settlement.SettleDurable(task); err != nil {
				return result, openRouterSyncOperatorSettlementError(task)
			}
		default:
			return result, fmt.Errorf("%w: recorded decision has no pending settlement", domain.ErrOpenRouterSyncOperatorConflict)
		}
		result.BillingState, _ = domain.OpenRouterSyncReconciliation(task)["billing_state"].(string)
		return result, nil
	}
	if !task.OpenRouterSyncOperatorEligible(action) {
		return result, fmt.Errorf("%w: task is not eligible for %s", domain.ErrOpenRouterSyncOperatorConflict, action)
	}
	r.HasUpstreamCost, r.UpstreamCostConfirmed, r.UpstreamCostUSD = true, true, costValue
	u.settlement.Prepare(task)
	if r.QuotaSaturation != "" {
		return result, fmt.Errorf("%w: cost saturates the quota range", domain.ErrOpenRouterSyncOperatorInvalid)
	}
	if !r.SettlementPending {
		return result, fmt.Errorf("%w: settlement basis is unavailable", domain.ErrOpenRouterSyncOperatorConflict)
	}
	// A recorded decision is irreversible, so refuse one that cannot settle.
	if err := u.store.Feasible(task); err != nil {
		return result, fmt.Errorf("%w: settlement is not feasible: %v", domain.ErrOpenRouterSyncOperatorConflict, err)
	}
	a.OperatorDecision = &domain.OpenRouterSyncOperatorDecision{Action: action, CostUSD: result.CostUSD, OperatorID: operatorID, Evidence: evidence, DecidedAt: u.now().Unix()}
	if action == domain.OpenRouterSyncOperatorRelease {
		a.LastError = "operator_released"
		task.FailReason = "OpenRouter reservation released by operator"
	} else {
		a.LastError = "operator_settled"
		task.FailReason = "OpenRouter cost settled by operator"
	}
	result.TargetQuota = r.TargetQuota
	u.projection.Project(task)
	if err := u.store.UpdateRevision(task, a.Revision); err != nil {
		return result, fmt.Errorf("%w: task changed concurrently", domain.ErrOpenRouterSyncOperatorConflict)
	}
	if err := u.settlement.SettleDurable(task); err != nil {
		return result, openRouterSyncOperatorSettlementError(task)
	}
	result.BillingState, _ = domain.OpenRouterSyncReconciliation(task)["billing_state"].(string)
	return result, nil
}

// openRouterSyncOperatorSettlementError separates a retryable settlement
// from one whose balances already moved and needs repair, never a replay.
func openRouterSyncOperatorSettlementError(task *domain.Task) error {
	if task.PrivateData.Reconciliation.MainApplied {
		return fmt.Errorf("%w: settlement_repair_required", domain.ErrOpenRouterSyncOperatorConflict)
	}
	return fmt.Errorf("%w: settlement incomplete; retry the same decision", domain.ErrOpenRouterSyncOperatorConflict)
}
