package domain

import "math"

const FXRateDenomination = 100.0
const FXQuotaPerUnit = 500000.0

type FXBasis struct {
	USDRate, Factor                            float64
	Source                                     string
	PublicationVersion, EffectiveAt, FetchedAt int64
}
type FXPublication struct {
	Source                          string
	Version, EffectiveAt, FetchedAt int64
	Rates                           map[string]float64
}
type FXSettings struct {
	QuotaUnit, AccountingRate func() float64
	Read                      func() (FXPublication, error)
}
type FXError struct{ Message string }

func (e *FXError) Error() string   { return e.Message }
func fxError(message string) error { return &FXError{Message: message} }

// PublishedUSD validates the published USD quote without applying accounting policy.
func PublishedUSD(publication FXPublication) (float64, error) {
	rate, ok := publication.Rates["USD"]
	if !ok || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, fxError("model cost FX basis is invalid")
	}
	return rate, nil
}
func ValidateFXAccounting(settings FXSettings) error {
	if settings.QuotaUnit() != FXQuotaPerUnit || settings.AccountingRate() != FXRateDenomination {
		return fxError("model cost accounting configuration is invalid")
	}
	return nil
}
func ApplyBillingFX(pure, factor float64) (float64, error) {
	if pure < 0 || math.IsNaN(pure) || math.IsInf(pure, 0) {
		return 0, fxError("model cost FX basis is invalid")
	}
	if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		return 0, fxError("model cost FX basis is invalid")
	}
	if pure == 0 || factor == 1 {
		return pure, nil
	}
	effective := pure * factor
	if effective <= 0 || math.IsNaN(effective) || math.IsInf(effective, 0) {
		return 0, fxError("model cost FX basis is invalid")
	}
	return effective, nil
}
