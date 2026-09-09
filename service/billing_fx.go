package service

import (
	"errors"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

const (
	billingFXRateDenomination = 100.0
	billingFXQuotaPerUnit     = 500_000.0
)

// BillingFXError is a trusted host accounting admission failure. Its message
// is deliberately safe to present through existing generic pricing envelopes.
type BillingFXError struct{ message string }

func (e *BillingFXError) Error() string { return e.message }

func billingFXError(message string) error { return &BillingFXError{message: message} }

func IsBillingFXError(err error) bool {
	var target *BillingFXError
	return errors.As(err, &target)
}

func IsQuotaClamp(err error) bool {
	var clamp *common.QuotaClamp
	return errors.As(err, &clamp)
}

// billingAdmissionError keeps the diagnostic cause available to internal
// callers while preserving the existing bounded public quota message.
type billingAdmissionError struct{ cause error }

func (e *billingAdmissionError) Error() string { return "insufficient quota" }

func (e *billingAdmissionError) Unwrap() error { return e.cause }

func BillingAdmissionError(c *gin.Context, info *relaycommon.RelayInfo, err error) *types.NewAPIError {
	var clamp *common.QuotaClamp
	if errors.As(err, &clamp) {
		if info != nil && info.QuotaClamp == nil {
			info.QuotaClamp = clamp
		}
		if c != nil {
			logger.LogWarn(c, fmt.Sprintf("quota saturation admission refused: op=%s kind=%s clamped=%d", clamp.Op, clamp.Kind, clamp.Clamped))
		}
		return types.NewErrorWithStatusCode(&billingAdmissionError{cause: err}, types.ErrorCodeInsufficientUserQuota, 403, types.ErrOptionWithSkipRetry())
	}
	if IsBillingFXError(err) {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, 503, types.ErrOptionWithSkipRetry())
	}
	return nil
}

func TaskBillingAdmissionError(c *gin.Context, info *relaycommon.RelayInfo, err error) *dto.TaskError {
	if apiErr := BillingAdmissionError(c, info, err); apiErr != nil {
		return &dto.TaskError{Code: string(apiErr.GetErrorCode()), Message: apiErr.Err.Error(), StatusCode: apiErr.StatusCode, LocalError: true, Error: err}
	}
	return nil
}

func validateBillingFXAccounting() error {
	if common.QuotaPerUnit != billingFXQuotaPerUnit {
		return billingFXError("model cost accounting configuration is invalid")
	}
	if operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate != billingFXRateDenomination {
		return billingFXError("model cost accounting configuration is invalid")
	}
	return nil
}

// CaptureBillingFX reads the already-published RAM snapshot once for a relay
// operation. It deliberately performs no database, cache, network, or TTL
// work. A later retry reuses the captured factor even when RAM has changed.
func CaptureBillingFX(info *relaycommon.RelayInfo) (float64, error) {
	if info == nil {
		return 0, billingFXError("model cost FX basis is invalid")
	}
	if err := validateBillingFXAccounting(); err != nil {
		return 0, err
	}
	if info.BillingFXFactor != 0 {
		if info.BillingFXRate <= 0 || math.IsNaN(info.BillingFXRate) || math.IsInf(info.BillingFXRate, 0) ||
			info.BillingFXRate/billingFXRateDenomination != info.BillingFXFactor {
			return 0, billingFXError("captured model cost FX basis is invalid")
		}
		return info.BillingFXFactor, nil
	}
	snapshot, err := model.CurrentModelCostFX(modelCostFXSource)
	if err != nil {
		return 0, billingFXError("model cost FX is unavailable")
	}
	rate, ok := snapshot.Rates["USD"]
	if !ok || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, billingFXError("model cost FX basis is invalid")
	}
	factor := rate / billingFXRateDenomination
	if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		return 0, billingFXError("model cost FX basis is invalid")
	}
	info.BillingFXRate = rate
	info.BillingFXFactor = factor
	info.BillingFXSource = snapshot.Source
	info.BillingFXPublicationVersion = snapshot.Version
	info.BillingFXEffectiveAt = snapshot.EffectiveAt
	info.BillingFXFetchedAt = snapshot.FetchedAt
	return factor, nil
}

// ApplyBillingFX composes the selected pure group ratio with the captured
// factor. Factor one keeps the existing binary64 value exactly.
func ApplyBillingFX(pure, factor float64) (float64, error) {
	if pure < 0 || math.IsNaN(pure) || math.IsInf(pure, 0) {
		return 0, billingFXError("model cost FX basis is invalid")
	}
	if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		return 0, billingFXError("model cost FX basis is invalid")
	}
	if pure == 0 || factor == 1 {
		return pure, nil
	}
	effective := pure * factor
	if effective <= 0 || math.IsNaN(effective) || math.IsInf(effective, 0) {
		return 0, billingFXError("model cost FX basis is invalid")
	}
	return effective, nil
}
