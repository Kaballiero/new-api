package domain

import (
	"fmt"
	"math"

	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
)

// TaskBillingFXFactor validates modern durable task history. Both new members
// are required together; their joint absence is the only legacy marker.
func TaskBillingFXFactor(bc *TaskBillingContext, source string) (float64, bool, error) {
	if bc == nil || (!bc.HasBillingFX() && !bc.HasSubmitGroup()) {
		return 1, false, nil
	}
	if !bc.HasBillingFX() || !bc.HasSubmitGroup() || !bc.HasCompleteBillingFX() || !bc.HasCompleteSubmitGroup() || bc.BillingFX == nil || bc.SubmitGroup == nil {
		return 0, true, fmt.Errorf("task billing FX history is incomplete")
	}
	fx := bc.BillingFX
	if fx.SchemaVersion != 1 || fx.Source != source || fx.Rate <= 0 || math.IsNaN(fx.Rate) || math.IsInf(fx.Rate, 0) ||
		fx.PublicationVersion < 0 || fx.EffectiveAt <= 0 || fx.FetchedAt <= 0 {
		return 0, true, fmt.Errorf("task billing FX history is invalid")
	}
	factor := fx.Rate / billingdomain.FXRateDenomination
	if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		return 0, true, fmt.Errorf("task billing FX history is invalid")
	}
	group := bc.SubmitGroup
	if group.PureRatio < 0 || math.IsNaN(group.PureRatio) || math.IsInf(group.PureRatio, 0) {
		return 0, true, fmt.Errorf("task billing submit group is invalid")
	}
	if group.HasSpecialRatio != (group.SpecialRatio != nil) {
		return 0, true, fmt.Errorf("task billing submit group is incomplete")
	}
	if group.SpecialRatio != nil && (*group.SpecialRatio < 0 || math.IsNaN(*group.SpecialRatio) ||
		math.IsInf(*group.SpecialRatio, 0) || *group.SpecialRatio != group.PureRatio) {
		return 0, true, fmt.Errorf("task billing special group is invalid")
	}
	effective := group.PureRatio
	if effective != 0 && factor != 1 {
		effective *= factor
		if effective <= 0 || math.IsNaN(effective) || math.IsInf(effective, 0) {
			return 0, true, fmt.Errorf("task billing effective group is invalid")
		}
	}
	if bc.GroupRatio != effective {
		return 0, true, fmt.Errorf("task billing effective group does not match FX history")
	}
	if snapshot := bc.TieredSnapshot; snapshot != nil &&
		(snapshot.GroupRatio != effective || snapshot.QuotaPerUnit != billingdomain.FXQuotaPerUnit) {
		return 0, true, fmt.Errorf("task billing tiered snapshot does not match FX history")
	}
	return factor, true, nil
}

// HasIndependentTaskRefundEntitlement identifies failed task modes whose
// existing refund contract uses the already saved quota without repricing.
func HasIndependentTaskRefundEntitlement(task *Task) bool {
	if task == nil || task.Status != TaskStatusFailure || task.Quota == 0 {
		return false
	}
	bc := task.PrivateData.BillingContext
	return bc != nil && bc.SettlementMode == "" && (bc.PerCallBilling || bc.TieredSnapshot != nil)
}

type DeltaResult struct{ FundingApplied, TokenApplied bool }
type BillingProjection struct {
	ModelRatio, GroupRatio, SpecialRatio float64
	HasSpecialRatio                      bool
}

func TaskModelName(task *Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}
