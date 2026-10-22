package service

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// The frozen architecture allowlist forbids service -> internal imports.
// Keep these legacy task/log constants until those consumers move in 2c / wave 5;
// the new cost rules own their equivalents in billing/domain.
const (
	billingFXRateDenomination = 100.0
	billingFXQuotaPerUnit     = 500_000.0
)

// BillingFXStatus is the read-only current model-cost FX basis exposed to the
// admin UI. It is intentionally independent from legacy payment exchange-rate
// settings.
type BillingFXStatus struct {
	Available bool     `json:"available"`
	USDRate   *float64 `json:"usd_rate,omitempty"`
}

// BillingFXBasis is the validated read-only FX basis used by both consumption
// admission and effective pricing display.
type BillingFXBasis struct {
	USDRate            float64 `json:"usd_rate"`
	Factor             float64 `json:"factor"`
	Source             string  `json:"source"`
	PublicationVersion int64   `json:"publication_version"`
	EffectiveAt        int64   `json:"effective_at"`
	FetchedAt          int64   `json:"fetched_at"`
}

// CurrentBillingFXBasis is the shared frozen-basis reader for Batch.
func CurrentBillingFXBasis() (BillingFXBasis, error) { return BillingGateway.CurrentFX() }
func CaptureBillingFX(info *relaycommon.RelayInfo) (float64, error) {
	return BillingGateway.CaptureFX(info)
}
func ApplyBillingFX(pure, factor float64) (float64, error) {
	return BillingGateway.ApplyFX(pure, factor)
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
