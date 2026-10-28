package domain

import (
	"errors"
	"fmt"
)

var (
	ErrReadQuery            = errors.New("read query error")
	ErrInvalidParameters    = errors.New("Invalid parameters")
	ErrGroup                = errors.New("Failed to get user group")
	ErrLowAmount            = errors.New("Top-up amount is too low")
	ErrUser                 = errors.New("User does not exist")
	ErrChannel              = errors.New("Unsupported payment channel")
	ErrCheckout             = errors.New("Failed to start payment")
	ErrInsert               = errors.New("Failed to create order")
	ErrQuotaRange           = errors.New("Top-up quota exceeds the supported range")
	ErrQuotaPositive        = errors.New("Top-up quota must be positive")
	ErrAmount               = errors.New("Invalid top-up amount")
	ErrMethod               = errors.New("Payment method does not exist")
	ErrEpayConfig           = errors.New("Payment information is not configured")
	ErrStripeMaximum        = errors.New("Top-up amount cannot exceed 10000")
	ErrSuccessURL           = errors.New("Payment success redirect URL is not trusted")
	ErrCancelURL            = errors.New("Payment cancellation redirect URL is not trusted")
	ErrProductRequired      = errors.New("Please select a product")
	ErrProductsConfig       = errors.New("Invalid product configuration")
	ErrProductMissing       = errors.New("Product does not exist")
	ErrWaffoDisabled        = errors.New("Waffo payment is disabled")
	ErrPayMethod            = errors.New("Unsupported payment method")
	ErrPaymentConfig        = errors.New("Invalid payment configuration")
	ErrPancakeConfig        = errors.New("Waffo Pancake configuration is incomplete")
	ErrCredentials          = errors.New("Waffo Pancake credentials are not configured")
	ErrSaveConfig           = errors.New("Failed to save configuration")
	ErrCatalog              = errors.New("Failed to load catalog")
	ErrSearch               = errors.New("Failed to search top-ups")
	ErrTradeNo              = errors.New("Order number is required")
	ErrPaymentTradeNo       = errors.New("Payment order number is required")
	ErrOrderMissing         = errors.New("Top-up order does not exist")
	ErrManualStatus         = errors.New("Only pending orders can be completed manually")
	ErrOrderStatus          = errors.New("Invalid top-up order status")
	ErrRecharge             = errors.New("Top-up failed, please try again later")
	ErrCompliance           = errors.New("Please confirm the compliance statement")
	ErrInvalidQuota         = errors.New("invalid top-up quota")
	ErrWalletLimit          = errors.New("top-up quota limit exceeded")
	ErrProviderMismatch     = errors.New("payment method mismatch")
	ErrNotFound             = errors.New("topup not found")
	ErrNotPending           = errors.New("topup status invalid")
	ErrSessionOnly          = errors.New("This operation requires dashboard session authentication. API access token is not allowed.")
	ErrSubscriptionNotFound = errors.New("subscription order not found")
)

type MinimumAmount struct{ Amount int64 }

func (e MinimumAmount) Error() string {
	return fmt.Sprintf("top-up amount cannot be less than %d", e.Amount)
}

type MaximumAmount struct{ Amount int64 }

func (e MaximumAmount) Error() string {
	return fmt.Sprintf("single top-up amount cannot exceed %d", e.Amount)
}

// Provider verification errors are internal; HTTP handlers preserve provider ACK constants.
var (
	ErrStripeSignature  = errors.New("invalid Stripe signature")
	ErrCreemSignature   = errors.New("invalid Creem signature")
	ErrCreemPayload     = errors.New("invalid Creem payload")
	ErrWaffoSignature   = errors.New("invalid Waffo signature")
	ErrPancakeSignature = errors.New("invalid Pancake signature")
)

// ConfigFailure retains an operational cause while HTTP uses the unchanged save-config message.
type ConfigFailure struct{ Cause error }

func (e ConfigFailure) Error() string { return e.Cause.Error() }
func (e ConfigFailure) Unwrap() error { return ErrSaveConfig }
