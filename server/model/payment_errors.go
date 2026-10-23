package model

import "errors"

// Legacy user/subscription consumers retain their original sentinel identities.
var ErrPaymentMethodMismatch = errors.New("payment method mismatch")
var ErrWalletQuotaLimitExceeded = errors.New("wallet quota limit exceeded")
