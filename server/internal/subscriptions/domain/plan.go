package domain

const (
	SubscriptionDurationYear   = "year"
	SubscriptionDurationMonth  = "month"
	SubscriptionDurationDay    = "day"
	SubscriptionDurationHour   = "hour"
	SubscriptionDurationCustom = "custom"
)

const (
	SubscriptionResetNever   = "never"
	SubscriptionResetDaily   = "daily"
	SubscriptionResetWeekly  = "weekly"
	SubscriptionResetMonthly = "monthly"
	SubscriptionResetCustom  = "custom"
)

type SubscriptionPlan struct {
	Id int

	Title    string
	Subtitle string

	// Display money amount (follow existing code style: float64 for money)
	PriceAmount float64
	Currency    string

	DurationUnit  string
	DurationValue int
	CustomSeconds int64

	Enabled   bool
	SortOrder int

	AllowBalancePay *bool

	// Allow falling back to wallet balance after subscription quota is exhausted (empty = true)
	AllowWalletOverflow *bool

	StripePriceId         string
	CreemProductId        string
	WaffoPancakeProductId string

	// Max purchases per user (0 = unlimited)
	MaxPurchasePerUser int

	// Upgrade user group after purchase (empty = no change)
	UpgradeGroup string

	// Downgrade user group on expiry (empty = revert to the group held before purchase)
	DowngradeGroup string

	// Total quota (amount in quota units, 0 = unlimited)
	TotalAmount int64

	// Quota reset period for plan
	QuotaResetPeriod        string
	QuotaResetCustomSeconds int64

	CreatedAt int64
	UpdatedAt int64
}

type SubscriptionOrder struct {
	Id     int
	UserId int
	PlanId int
	Money  float64

	TradeNo         string
	PaymentMethod   string
	PaymentProvider string
	Status          string
	CreateTime      int64
	CompleteTime    int64

	ProviderPayload string
}

type UserSubscription struct {
	Id     int
	UserId int
	PlanId int

	AmountTotal int64
	AmountUsed  int64

	StartTime int64
	EndTime   int64
	Status    string // active/expired/cancelled

	Source string // order/admin

	LastResetTime int64
	NextResetTime int64

	UpgradeGroup  string
	PrevUserGroup string

	// Downgrade target group on expiry (snapshot from plan; empty = revert to PrevUserGroup)
	DowngradeGroup string

	// Whether wallet fallback is allowed after this subscription's quota is exhausted (snapshot from plan)
	AllowWalletOverflow bool

	CreatedAt int64
	UpdatedAt int64
}

func (p *SubscriptionPlan) NormalizeDefaults() {
	if p.AllowBalancePay == nil {
		value := true
		p.AllowBalancePay = &value
	}
	if p.AllowWalletOverflow == nil {
		value := true
		p.AllowWalletOverflow = &value
	}
}
