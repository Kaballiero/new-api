package billing

import (
	"fmt"

	"github.com/QuantumNous/new-api/gateway/mapping"
	app "github.com/QuantumNous/new-api/internal/app/billing"
	"github.com/QuantumNous/new-api/internal/billing/charge_violation_fee"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// ChargeViolationFeeIfNeeded charges an additional fee after the normal flow finishes (including refund).
// It uses Grok fee settings as the fee policy.
func ChargeViolationFeeIfNeeded(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, apiErr *types.NewAPIError) bool {
	if ctx == nil || relayInfo == nil || apiErr == nil {
		return false
	}
	//if relayInfo.IsPlayground {
	//	return false
	//}

	groupRatio := relayInfo.PriceData.GroupRatioInfo.GroupRatio
	result, err := app.ViolationFee.Execute(charge_violation_fee.Input{
		Chargeable: apiErr.GetErrorCode() == types.ErrorCodeViolationFeeGrokCSAM || mapping.HasCSAMViolationMarker(apiErr), GroupRatio: groupRatio,
	}, func(quota int) error { return PostConsumeQuota(relayInfo, quota, 0, true) })
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("failed to charge violation fee: %s", err.Error()))
		return false
	}
	if !result.Charged {
		return false
	}
	recordViolationFee(ctx, relayInfo, result.Quota, apiErr, result.Amount, groupRatio)

	return true
}
