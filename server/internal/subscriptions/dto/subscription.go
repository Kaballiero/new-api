package dto

import "github.com/QuantumNous/new-api/internal/subscriptions/domain"

type SubscriptionPlan struct {
	Id int `json:"id"`

	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`

	// Display money amount (follow existing code style: float64 for money)
	PriceAmount float64 `json:"price_amount"`
	Currency    string  `json:"currency"`

	DurationUnit  string `json:"duration_unit"`
	DurationValue int    `json:"duration_value"`
	CustomSeconds int64  `json:"custom_seconds"`

	Enabled   bool `json:"enabled"`
	SortOrder int  `json:"sort_order"`

	AllowBalancePay *bool `json:"allow_balance_pay"`

	// Allow falling back to wallet balance after subscription quota is exhausted (empty = true)
	AllowWalletOverflow *bool `json:"allow_wallet_overflow"`

	StripePriceId         string `json:"stripe_price_id"`
	CreemProductId        string `json:"creem_product_id"`
	WaffoPancakeProductId string `json:"waffo_pancake_product_id"`

	// Max purchases per user (0 = unlimited)
	MaxPurchasePerUser int `json:"max_purchase_per_user"`

	// Upgrade user group after purchase (empty = no change)
	UpgradeGroup string `json:"upgrade_group"`

	// Downgrade user group on expiry (empty = revert to the group held before purchase)
	DowngradeGroup string `json:"downgrade_group"`

	// Total quota (amount in quota units, 0 = unlimited)
	TotalAmount int64 `json:"total_amount"`

	// Quota reset period for plan
	QuotaResetPeriod        string `json:"quota_reset_period"`
	QuotaResetCustomSeconds int64  `json:"quota_reset_custom_seconds"`

	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

type UserSubscription struct {
	Id     int `json:"id"`
	UserId int `json:"user_id"`
	PlanId int `json:"plan_id"`

	AmountTotal int64 `json:"amount_total"`
	AmountUsed  int64 `json:"amount_used"`

	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Status    string `json:"status"` // active/expired/cancelled

	Source string `json:"source"` // order/admin

	LastResetTime int64 `json:"last_reset_time"`
	NextResetTime int64 `json:"next_reset_time"`

	UpgradeGroup  string `json:"upgrade_group"`
	PrevUserGroup string `json:"prev_user_group"`

	// Downgrade target group on expiry (snapshot from plan; empty = revert to PrevUserGroup)
	DowngradeGroup string `json:"downgrade_group"`

	// Whether wallet fallback is allowed after this subscription's quota is exhausted (snapshot from plan)
	AllowWalletOverflow bool `json:"allow_wallet_overflow"`

	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

type PlanEnvelope struct {
	Plan SubscriptionPlan `json:"plan"`
}
type Summary struct {
	Subscription *UserSubscription `json:"subscription"`
}

func Plans(rows []domain.SubscriptionPlan) []PlanEnvelope {
	result := make([]PlanEnvelope, 0, len(rows))
	for _, row := range rows {
		result = append(result, PlanEnvelope{Plan: SubscriptionPlan(row)})
	}
	return result
}
func Summaries(rows []domain.UserSubscription) []Summary {
	result := make([]Summary, 0, len(rows))
	for _, row := range rows {
		value := UserSubscription(row)
		result = append(result, Summary{Subscription: &value})
	}
	return result
}

type SubscriptionResetResult struct {
	PlanId           int    `json:"plan_id"`
	MatchedCount     int    `json:"matched_count"`
	ResetCount       int    `json:"reset_count"`
	UserCount        int    `json:"user_count"`
	AdvanceResetTime bool   `json:"advance_reset_time"`
	PlanTitle        string `json:"-"`
	AffectedUserIds  []int  `json:"-"`
}

type AdminUpsertSubscriptionPlanRequest struct {
	Plan SubscriptionPlan `json:"plan"`
}
