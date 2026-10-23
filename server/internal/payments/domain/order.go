package domain

const (
	PaymentMethodStripe         = "stripe"
	PaymentMethodCreem          = "creem"
	PaymentMethodWaffo          = "waffo"
	PaymentMethodWaffoPancake   = "waffo_pancake"
	PaymentMethodBalance        = "balance"
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderBalance      = "balance"
	StatusPending               = "pending"
	StatusSuccess               = "success"
	StatusFailed                = "failed"
	StatusExpired               = "expired"
)

type Order struct {
	ID, UserID                              int
	Amount                                  int64
	Money                                   float64
	TradeNo, PaymentMethod, PaymentProvider string
	CreateTime, CompleteTime                int64
	Status                                  string
}

type User struct {
	ID                                     int
	Email, Username, Group, StripeCustomer string
}

// Orders reads and writes one order per operation, outside a completion transaction.
type Orders interface {
	Insert(order *Order) error
	Save(order *Order) error
	Find(tradeNo string) *Order
}

// CreditProfile carries optional account facts credited with a payment.
type CreditProfile struct {
	StripeCustomer *string
	Email          *string
}

// Tx performs locked order lookup, save and wallet credit within the same completion transaction.
type Tx interface {
	FindLocked(tradeNo string) (Order, error)
	Save(order *Order) error
	Email(userID int) (string, error)
	Credit(userID int, quota int, profile CreditProfile) error
}

// Transactions commits all order and wallet mutations together.
type Transactions interface {
	Transaction(run func(tx Tx) error) error
}

// Users reads the existing account facts without choosing payment policy.
type Users interface {
	Group(userID int) (string, error)
	Find(userID int) (*User, error)
}

// Admission serializes the same trade number across payments and legacy subscriptions in this process.
type Admission interface {
	Lock(tradeNo string)
	Unlock(tradeNo string)
}

// Subscriptions keeps legacy completion/expiration behind the composition boundary.
type Subscriptions interface {
	Complete(tradeNo string, payload string, provider string) error
	Expire(tradeNo string, provider string) error
	Find(tradeNo string) *Order
}
