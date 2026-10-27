package mapping

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
)

// BillingFXStatus is the read-only current model-cost FX basis exposed to the
// admin UI. It is intentionally independent from legacy payment exchange-rate
// settings.
type BillingFXStatus struct {
	Available bool     `json:"available"`
	USDRate   *float64 `json:"usd_rate,omitempty"`
}

func NewBillingFXError(message string) error { return &BillingFXError{message: message} }

// BillingFXError is a trusted host accounting admission failure. Its message
// is deliberately safe to present through existing generic pricing envelopes.
type BillingFXError struct{ message string }

func (e *BillingFXError) Error() string { return e.message }

func IsBillingFXError(err error) bool {
	var target *BillingFXError
	return errors.As(err, &target)
}

func IsQuotaClamp(err error) bool {
	var clamp *common.QuotaClamp
	return errors.As(err, &clamp)
}
