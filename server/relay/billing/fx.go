package billing

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	mapping "github.com/QuantumNous/new-api/gateway/mapping"
	app "github.com/QuantumNous/new-api/internal/app/billing"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

func fxError(err error) error {
	var failure *domain.FXError
	if errors.As(err, &failure) {
		return mapping.NewBillingFXError(failure.Error())
	}
	return err
}
func (g *Gateway) CaptureFX(info *relaycommon.RelayInfo) (float64, error) {
	if info == nil {
		return 0, mapping.NewBillingFXError("model cost FX basis is invalid")
	}
	basis, err := g.costs.FX.Execute(domain.FXBasis{USDRate: info.BillingFXRate, Factor: info.BillingFXFactor, Source: info.BillingFXSource, PublicationVersion: info.BillingFXPublicationVersion, EffectiveAt: info.BillingFXEffectiveAt, FetchedAt: info.BillingFXFetchedAt})
	if err != nil {
		return 0, fxError(err)
	}
	if info.BillingFXFactor == 0 {
		info.BillingFXRate = basis.USDRate
		info.BillingFXFactor = basis.Factor
		info.BillingFXSource = basis.Source
		info.BillingFXPublicationVersion = basis.PublicationVersion
		info.BillingFXEffectiveAt = basis.EffectiveAt
		info.BillingFXFetchedAt = basis.FetchedAt
	}
	return basis.Factor, nil
}
func (g *Gateway) ApplyFX(pure, factor float64) (float64, error) {
	value, err := domain.ApplyBillingFX(pure, factor)
	return value, fxError(err)
}
func CaptureBillingFX(info *relaycommon.RelayInfo) (float64, error) {
	return defaultGateway.CaptureFX(info)
}
func ApplyBillingFX(pure, factor float64) (float64, error) {
	return defaultGateway.ApplyFX(pure, factor)
}
func CurrentBillingFXStatus() mapping.BillingFXStatus {
	s, err := app.CostFXSettings().Read()
	if err != nil {
		return mapping.BillingFXStatus{}
	}
	rate, err := domain.PublishedUSD(s)
	if err != nil {
		return mapping.BillingFXStatus{}
	}
	return mapping.BillingFXStatus{Available: true, USDRate: &rate}
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
	if mapping.IsBillingFXError(err) {
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
