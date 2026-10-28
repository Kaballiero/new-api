package domain

import "errors"

var ErrInvalidUserID = errors.New("Invalid user ID")

var (
	ErrInvalidID             = errors.New("Invalid ID")
	ErrTitleRequired         = errors.New("Plan title is required")
	ErrNegativePrice         = errors.New("Price cannot be negative")
	ErrHighPrice             = errors.New("Price cannot exceed 9999")
	ErrNegativePurchaseLimit = errors.New("Purchase limit cannot be negative")
	ErrNegativeTotal         = errors.New("Total quota cannot be negative")
	ErrUpgradeGroupMissing   = errors.New("Upgrade group does not exist")
	ErrDowngradeGroupMissing = errors.New("Downgrade group does not exist")
	ErrInvalidResetSeconds   = errors.New("Custom reset period must be greater than zero seconds")
)

var ErrPurchaseLimit = errors.New("Purchase limit reached for this plan")

var (
	ErrNoActivePlan       = errors.New("This user has no active subscription to this plan")
	ErrOrderNotFound      = errors.New("subscription order not found")
	ErrOrderStatusInvalid = errors.New("subscription order status invalid")
	ErrProviderMismatch   = errors.New("payment method mismatch")
)

var (
	ErrPlanDisabled          = errors.New("Plan is disabled")
	ErrBalanceNegativePrice  = errors.New("Plan price cannot be negative")
	ErrBalanceDisallowed     = errors.New("This plan cannot be purchased with wallet balance")
	ErrInsufficientBalance   = errors.New("Insufficient balance")
	ErrInvalidQuotaUnit      = errors.New("Invalid quota unit configuration")
	ErrInvalidSubscriptionID = errors.New("Invalid subscription ID")
)

var (
	ErrStripePriceMissing    = errors.New("StripePriceId is not configured for this plan")
	ErrStripeConfig          = errors.New("Stripe is not configured or its API key is incorrect")
	ErrStripeWebhook         = errors.New("Stripe webhook is not configured")
	ErrCreemProductMissing   = errors.New("CreemProductId is not configured for this plan")
	ErrCreemWebhook          = errors.New("Creem webhook is not configured")
	ErrPancakeProductMissing = errors.New("WaffoPancakeProductId is not configured for this plan")
	ErrPancakeConfig         = errors.New("Waffo Pancake is not configured or its credentials are incorrect")
	ErrUserMissing           = errors.New("User does not exist")
	ErrPriceLow              = errors.New("Plan amount is too low")
	ErrPayMethod             = errors.New("Payment method does not exist")
	ErrCallbackConfig        = errors.New("Invalid callback address configuration")
	ErrEpayConfig            = errors.New("Payment information is not configured by the administrator")
	ErrCreateOrder           = errors.New("Failed to create order")
	ErrCheckout              = errors.New("Failed to start payment")
)

var (
	ErrProductName    = errors.New("Plan name is required")
	ErrProductAmount  = errors.New("Plan price is required")
	ErrPancakeBinding = errors.New("Waffo Pancake setup is incomplete; bind the gateway in payment settings first")
	ErrCreateProduct  = errors.New("Failed to create plan product")
	ErrProductCatalog = errors.New("Failed to load product list")
)
