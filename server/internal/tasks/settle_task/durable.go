package settle_task

import (
	"context"
	"fmt"
	"math"

	"github.com/shopspring/decimal"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

func (u Usecase) Prepare(task *domain.Task) {
	r, bc := task.PrivateData.Reconciliation, task.PrivateData.BillingContext
	if bc == nil || (bc.SettlementMode != domain.TaskSettlementOpenRouterCostV1 && bc.SettlementMode != domain.TaskSettlementOpenRouterCostV2) || r == nil {
		return
	}
	r.Required = true
	r.Finalized = false
	if r.SettlementPending || r.MainApplied || r.ReservationPending || r.SubmissionPending ||
		!r.HasUpstreamCost || !r.UpstreamCostConfirmed || r.UpstreamCostUSD < 0 || math.IsNaN(r.UpstreamCostUSD) || math.IsInf(r.UpstreamCostUSD, 0) {
		return
	}
	if bc.SettlementMode == domain.TaskSettlementOpenRouterCostV2 {
		if bc.OpenRouterCost == nil || bc.OpenRouterCost.Version != domain.TaskSettlementOpenRouterCostV2 {
			return
		}
		target, clamp, err := u.math.Provider(*bc.OpenRouterCost, r.UpstreamCostUSD)
		if err != nil {
			return
		}
		if clamp != nil {
			marker, marshalErr := u.math.Saturation(clamp)
			if marshalErr != nil {
				return
			}
			r.QuotaSaturation = marker
			requestID := task.TaskID
			if task.PrivateData.Execution != nil && task.PrivateData.Execution.RequestID != "" {
				requestID = task.PrivateData.Execution.RequestID
			}
			ctx := context.WithValue(context.Background(), u.requestIDKey, requestID)
			u.diagnostics.Warn(ctx, fmt.Sprintf("task %s OpenRouter quota saturation: %v", task.TaskID, clamp))
		}
		r.TargetQuota, r.SettlementPending = target, true
		return
	}
	if _, modern, err := domain.TaskBillingFXFactor(bc, u.fxSource); err != nil || !modern || bc.TieredSnapshot == nil {
		return
	}
	snap := bc.TieredSnapshot
	amount := decimal.NewFromFloat(r.UpstreamCostUSD).Mul(decimal.NewFromFloat(snap.GroupRatio)).Mul(decimal.NewFromFloat(snap.QuotaPerUnit))
	target, clamp := u.math.Decimal(amount)
	if clamp != nil {
		return
	}
	r.TargetQuota = target
	r.SettlementPending = true
}

// SettleDurable applies only the already frozen terminal target.
func (u Usecase) SettleDurable(task *domain.Task) error {
	r := task.PrivateData.Reconciliation
	if r == nil || !r.SettlementPending || r.MainApplied || r.Finalized {
		return nil
	}
	tokenKey, err := u.durable.Apply(task)
	if err != nil {
		return err
	}
	if err := u.durable.SyncCache(task, tokenKey); err != nil {
		return err
	}
	delta := r.TargetQuota - r.ReservedQuota
	if delta != 0 {
		if err := u.usage.Durable(task, delta); err != nil {
			return err
		}
	}
	return u.durable.Finalize(task)
}
