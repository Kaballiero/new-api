package domain

import "strings"

// PlanRules reads late configuration and the single legacy calendar normalizer.
type PlanRules struct {
	GroupExists          func(group string) bool
	NormalizeResetPeriod func(period string) string
}

func NormalizePlan(plan *SubscriptionPlan, creating bool, rules PlanRules) error {
	if creating {
		plan.Id = 0
	}
	if strings.TrimSpace(plan.Title) == "" {
		return ErrTitleRequired
	}
	if plan.PriceAmount < 0 {
		return ErrNegativePrice
	}
	if plan.PriceAmount > 9999 {
		return ErrHighPrice
	}
	plan.Currency = "USD"
	if creating {
		plan.NormalizeDefaults()
	}
	if plan.DurationUnit == "" {
		plan.DurationUnit = SubscriptionDurationMonth
	}
	if plan.DurationValue <= 0 && plan.DurationUnit != SubscriptionDurationCustom {
		plan.DurationValue = 1
	}
	if plan.MaxPurchasePerUser < 0 {
		return ErrNegativePurchaseLimit
	}
	if plan.TotalAmount < 0 {
		return ErrNegativeTotal
	}
	plan.UpgradeGroup = strings.TrimSpace(plan.UpgradeGroup)
	if plan.UpgradeGroup != "" && !rules.GroupExists(plan.UpgradeGroup) {
		return ErrUpgradeGroupMissing
	}
	plan.DowngradeGroup = strings.TrimSpace(plan.DowngradeGroup)
	if plan.DowngradeGroup != "" && !rules.GroupExists(plan.DowngradeGroup) {
		return ErrDowngradeGroupMissing
	}
	plan.QuotaResetPeriod = rules.NormalizeResetPeriod(plan.QuotaResetPeriod)
	if plan.QuotaResetPeriod == SubscriptionResetCustom && plan.QuotaResetCustomSeconds <= 0 {
		return ErrInvalidResetSeconds
	}
	return nil
}
