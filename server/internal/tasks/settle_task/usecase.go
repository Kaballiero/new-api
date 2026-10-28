package settle_task

import (
	"context"
	"fmt"
	"maps"

	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/tasks/domain"

	"github.com/shopspring/decimal"
)

type Input struct {
	Task   *domain.Task
	Result *domain.TaskInfo
	Adjust func(*domain.Task, *domain.TaskInfo) int
}
type Store interface{ UpdateQuota(task *domain.Task) error }
type Billing interface {
	Apply(ctx context.Context, task *domain.Task, delta int) (domain.DeltaResult, error)
}
type Usage interface {
	UserUsed(userID, delta int)
	ChannelUsed(channelID, delta int)
	Adjustment(task *domain.Task, actual, reserved int, reason string, projection *domain.BillingProjection, clamps []*billingdomain.Clamp)
	Durable(task *domain.Task, delta int) error
}
type Prices interface {
	ModelRatio(modelName string) (float64, bool, string)
	GroupRatio(group string) float64
	SpecialRatio(group, userGroup string) (float64, bool)
	UserGroup(userID int) (string, error)
}
type Math interface {
	Float(value float64) (int, *billingdomain.Clamp)
	OtherMultiplier(context *domain.TaskBillingContext) float64
	Decimal(decimal.Decimal) (int, *billingdomain.Clamp)
	Provider(billingdomain.ProviderBasis, float64) (int, *billingdomain.Clamp, error)
	Saturation(*billingdomain.Clamp) (string, error)
}
type Diagnostics interface {
	Info(context.Context, string)
	Warn(context.Context, string)
	Error(context.Context, string)
	Quota(int) string
}
type Durable interface {
	Apply(task *domain.Task) (string, error)
	SyncCache(task *domain.Task, tokenKey string) error
	Finalize(task *domain.Task) error
}
type Usecase struct {
	store        Store
	billing      Billing
	usage        Usage
	prices       Prices
	math         Math
	expression   billingdomain.ExpressionEvaluator
	diagnostics  Diagnostics
	durable      Durable
	fxSource     string
	requestIDKey string
}

func NewUsecase(store Store, billing Billing, usage Usage, prices Prices, math Math, expression billingdomain.ExpressionEvaluator, diagnostics Diagnostics, durable Durable, fxSource, requestIDKey string) Usecase {
	return Usecase{store, billing, usage, prices, math, expression, diagnostics, durable, fxSource, requestIDKey}
}

// Recalculate applies an explicit actual quota and reason. It remains exported for
// the immutable delta oracle in internal/app/taskruntime/tasks_contract_test.go.
func (u Usecase) Recalculate(ctx context.Context, task *domain.Task, actualQuota int, reason string, projection *domain.BillingProjection, clamps ...*billingdomain.Clamp) {
	if actualQuota < 0 {
		return
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		u.diagnostics.Info(ctx, fmt.Sprintf("Task %s reserved quota is accurate (%s, %s)",
			task.TaskID, u.diagnostics.Quota(actualQuota), reason))
		return
	}

	u.diagnostics.Info(ctx, fmt.Sprintf("Task %s quota settlement: delta=%s (actual: %s, reserved: %s, %s)",
		task.TaskID,
		u.diagnostics.Quota(quotaDelta),
		u.diagnostics.Quota(actualQuota),
		u.diagnostics.Quota(preConsumedQuota),
		reason,
	))

	// Apply funding before the late token lookup.
	outcome, billingErr := u.billing.Apply(ctx, task, quotaDelta)
	if !outcome.FundingApplied {
		err := billingErr
		u.diagnostics.Error(ctx, fmt.Sprintf("Failed to adjust settlement funding for task %s: %s", task.TaskID, err.Error()))
		return
	}

	// Token errors were already reported by the task billing adapter.

	task.Quota = actualQuota
	if err := u.store.UpdateQuota(task); err != nil {
		u.diagnostics.Error(ctx, fmt.Sprintf("Failed to persist settlement quota for task %s: %s", task.TaskID, err.Error()))
	}

	// Submission counted the request; completion adjusts usage only.
	u.usage.UserUsed(task.UserId, quotaDelta)
	u.usage.ChannelUsed(task.ChannelId, quotaDelta)

	u.usage.Adjustment(task, actualQuota, preConsumedQuota, reason, projection, clamps)
}

// byTokens recalculates completion cost from observed token consumption.
// Completion pricing is current policy while FX remains frozen.
// The delta supports both wallet and subscription funding.
func (u Usecase) byTokens(ctx context.Context, task *domain.Task, totalTokens int) bool {
	if totalTokens <= 0 {
		return false
	}
	factor, modern, err := domain.TaskBillingFXFactor(task.PrivateData.BillingContext, u.fxSource)
	if err != nil {
		u.diagnostics.Warn(ctx, fmt.Sprintf("Task %s has invalid billing FX history: %s", task.TaskID, err.Error()))
		return false
	}

	modelName := domain.TaskModelName(task)

	// Read current model pricing.
	modelRatio, hasRatioSetting, _ := u.prices.ModelRatio(modelName)
	// Only ratio-priced models support token-based recalculation.
	if !hasRatioSetting || modelRatio <= 0 {
		return false
	}

	// Resolve the current user and group policy.
	group := task.Group
	if group == "" {
		userGroup, err := u.prices.UserGroup(task.UserId)
		if err == nil {
			group = userGroup
		}
	}
	if group == "" {
		return false
	}

	groupRatio := u.prices.GroupRatio(group)
	userGroupRatio, hasUserGroupRatio := u.prices.SpecialRatio(group, group)

	var finalGroupRatio float64
	if hasUserGroupRatio {
		finalGroupRatio = userGroupRatio
	} else {
		finalGroupRatio = groupRatio
	}

	// Reuse validated duration, discount and other multipliers.
	otherMultiplier := u.math.OtherMultiplier(task.PrivateData.BillingContext)

	// Modern tasks retain the submit FX factor, while model and group policy is
	// intentionally selected at completion. Decimal factors avoid an unnecessary
	// binary64 overflow before the checked quota conversion. F1 must retain the
	// legacy binary64 multiplication order, including its integer boundaries.
	var actualQuota int
	var clamp *billingdomain.Clamp
	if modern && factor != 1 {
		quota := decimal.NewFromInt(int64(totalTokens)).
			Mul(decimal.NewFromFloat(modelRatio)).
			Mul(decimal.NewFromFloat(finalGroupRatio)).
			Mul(decimal.NewFromFloat(otherMultiplier)).
			Mul(decimal.NewFromFloat(factor))
		quotaFloat, _ := quota.Float64()
		actualQuota, clamp = u.math.Float(quotaFloat)
	} else {
		actualQuota, clamp = u.math.Float(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)
	}

	reason := fmt.Sprintf("Token recalculation: tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", totalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	// Completion fields are current pricing policy, while billing_submit_group
	// in admin_info remains immutable submission provenance.
	projection := (*domain.BillingProjection)(nil)
	if modern {
		projection = &domain.BillingProjection{ModelRatio: modelRatio, GroupRatio: finalGroupRatio, HasSpecialRatio: hasUserGroupRatio, SpecialRatio: userGroupRatio}
	}
	u.Recalculate(ctx, task, actualQuota, reason, projection, clamp)
	return true
}

func (u Usecase) Execute(ctx context.Context, input Input) (bool, error) {
	task, taskResult, adjust := input.Task, input.Result, input.Adjust
	if bc := task.PrivateData.BillingContext; bc != nil && bc.SettlementMode != "" {
		if bc.SettlementMode != domain.TaskSettlementOpenRouterCostV1 && bc.SettlementMode != domain.TaskSettlementOpenRouterCostV2 {
			return true, nil
		}
		return true, u.SettleDurable(task)
	}
	if reconciliation := task.PrivateData.Reconciliation; reconciliation != nil {
		// The frozen customer tariff is independent of purchase-cost evidence.
		if task.Status == domain.TaskStatusFailure && reconciliation.HasUpstreamCost && reconciliation.UpstreamCostConfirmed && reconciliation.UpstreamCostUSD == 0 && !reconciliation.ReservationPending && !reconciliation.SubmissionPending {
			return false, nil
		}
		return true, nil
	}
	if _, modern, err := domain.TaskBillingFXFactor(task.PrivateData.BillingContext, u.fxSource); modern && err != nil {
		return false, err
	}
	if bc := task.PrivateData.BillingContext; bc != nil && bc.TieredSnapshot != nil {
		// Expressions settle success; failures remain eligible for caller refunds.
		if task.Status == domain.TaskStatusFailure {
			return false, nil
		}
		usageFacts := make(map[string]any, len(bc.TieredSnapshot.UsageFacts)+len(taskResult.UsageFacts))
		maps.Copy(usageFacts, bc.TieredSnapshot.UsageFacts)
		maps.Copy(usageFacts, taskResult.UsageFacts)
		result, err := u.expression.Compute(bc.TieredSnapshot, billingdomain.TokenParams{}, billingdomain.RequestInput{Usage: usageFacts})
		if err != nil {
			u.diagnostics.Warn(ctx, fmt.Sprintf("Task %s expression settlement failed; reserved quota retained: %v", task.TaskID, err))
			return true, nil
		}
		if result.Clamp != nil {
			u.diagnostics.Warn(ctx, fmt.Sprintf("Task %s expression settlement quota saturated: %+v", task.TaskID, result.Clamp))
		}
		bc.TieredSnapshot.UsageFacts = usageFacts
		bc.TieredSnapshot.EstimatedTier = result.MatchedTier
		u.Recalculate(ctx, task, result.ActualQuotaAfterGroup, "Task usage expression settlement", nil, result.Clamp)
		return true, nil
	}
	// Per-call success keeps its hold; the caller refunds eligible failures.
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		u.diagnostics.Info(ctx, fmt.Sprintf("Task %s uses per-call billing; skipping quota adjustment", task.TaskID))
		return false, nil
	}
	// Provider completion adjustment has priority over token repricing.
	if actualQuota := adjust(task, taskResult); actualQuota > 0 {
		u.Recalculate(ctx, task, actualQuota, "Adaptor billing adjustment", nil)
		return true, nil
	}
	// Fall back to observed token usage.
	tokens := taskResult.TotalTokens
	if tokens == 0 && taskResult.CompletionTokens > 0 {
		tokens = taskResult.CompletionTokens
	}
	if tokens > 0 {
		return u.byTokens(ctx, task, tokens), nil
	}
	return false, nil
}
