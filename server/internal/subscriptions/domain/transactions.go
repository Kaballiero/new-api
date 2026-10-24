package domain

// BindTx serializes lifetime purchase admission on the user's row.
type BindTx interface {
	GrantTx
	LockUserID(userID int) error
}

// BalanceTx debits the wallet, grants the subscription and inserts the order atomically.
type BalanceTx interface {
	GrantTx
	PlanByID(planID int) (*SubscriptionPlan, error)
	WalletBalance(userID int) (int, error)
	DebitWallet(userID int, quota int) error
	InsertOrder(order *SubscriptionOrder) error
}

// CompleteTx retains order -> user -> grant -> history -> order write ordering.
type CompleteTx interface {
	GrantTx
	LockOrder(tradeNo string) (*SubscriptionOrder, error)
	LockUserID(userID int) error
	UpsertHistory(order SubscriptionOrder) error
	SaveOrder(order *SubscriptionOrder) error
}

// ExpireTx locks and expires only a pending provider-owned order.
type ExpireTx interface {
	LockOrder(tradeNo string) (*SubscriptionOrder, error)
	SaveOrder(order *SubscriptionOrder) error
}

// ChangeTx keeps cancellation/deletion and the group decision in the same transaction.
type ChangeTx interface {
	GroupTx
	LockSubscription(subscriptionID int) (*UserSubscription, error)
	CancelSubscription(subscriptionID int, now int64) error
	DeleteSubscription(subscriptionID int) error
}

// ResetTx reads the cached plan and locks all matching active subscriptions before saving.
type ResetTx interface {
	PlanByID(planID int) (*SubscriptionPlan, error)
	ResetCandidates(userID int, planID int, now int64) ([]UserSubscription, error)
	SaveSubscription(subscription *UserSubscription) error
}

// ExpiryTx expires every due row for one user before checking the remaining upgraded subscriptions.
type ExpiryTx interface {
	GroupTx
	ExpireUser(userID int, now int64) (int, error)
	LastExpiredTransition(userID int) (*UserSubscription, error)
}

// DueResetTx intentionally rechecks only the identifier and reset time.
type DueResetTx interface {
	LockDueSubscription(subscriptionID int, now int64) (*UserSubscription, error)
	SaveSubscription(subscription *UserSubscription) error
}

type ResetResult struct {
	PlanId           int
	MatchedCount     int
	ResetCount       int
	UserCount        int
	AdvanceResetTime bool
	PlanTitle        string
	AffectedUserIds  []int
}

// OrderStates comes from the existing payment status owner through composition.
type OrderStates struct {
	Pending string
	Success string
	Expired string
	Failed  string
}

// PurchaseReceipt contains only the facts already recorded after a committed purchase.
type PurchaseReceipt struct {
	UserID        int
	PlanTitle     string
	Money         float64
	PaymentMethod string
}

// BalanceReceipt preserves the wallet purchase's separate receipt shape.
type BalanceReceipt struct {
	UserID    int
	PlanTitle string
	Money     float64
	Quota     int
}

type CompletionInput struct{ TradeNo, ProviderPayload, ExpectedProvider, ActualMethod string }
