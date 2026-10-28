package billing

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	adapter "github.com/QuantumNous/new-api/internal/adapter/billing"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/billing/prepare_tiered_cost"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func BuildTieredTokenParams(usage *dto.Usage, claude bool, used map[string]bool) billingexpr.TokenParams {
	return billingexpr.TokenParams(domain.BuildTieredTokenParams(CostUsage(usage), claude, used))
}

// TryTieredSettle maps the channel-test controller's frozen request into billing.
func TryTieredSettle(info *relaycommon.RelayInfo, params billingexpr.TokenParams) (bool, int, *billingexpr.TieredResult) {
	ok, quota, result, _ := domain.SettleTiered(adapter.Expression{}, costSnapshot(info.TieredBillingSnapshot), costRequest(info.BillingRequestInput), info.FinalPreConsumedQuota, domain.TokenParams(params), false)
	if result != nil {
		noteQuotaClamp(info, gatewayClamp(result.Clamp))
	}
	return ok, quota, gatewayTiered(result)
}
func CalcOpenRouterCacheCreateTokens(u dto.Usage, p hosttypes.PriceData) int {
	return domain.CalcOpenRouterCacheCreateTokens(*CostUsage(&u), costPrices(p), common.QuotaPerUnit, adapter.CostMath{})
}
func refreshTieredBillingGroup(relayInfo *relaycommon.RelayInfo) (*billingexpr.BillingSnapshot, error) {
	if relayInfo == nil {
		return nil, nil
	}
	snap := relayInfo.TieredBillingSnapshot
	if snap == nil || snap.BillingMode != "tiered_expr" {
		return nil, nil
	}

	updated, err := defaultGateway.costs.Prepare.Execute(prepare_tiered_cost.Input{Snapshot: costSnapshot(snap), GroupRatio: relayInfo.PriceData.EffectiveGroupRatio(), PureGroupRatio: relayInfo.PriceData.GroupRatioInfo.GroupRatio, FXFactor: relayInfo.BillingFXFactor})
	if err != nil {
		return nil, fxError(err)
	}
	snap.GroupRatio = updated.GroupRatio
	snap.EstimatedQuotaAfterGroup = updated.EstimatedQuotaAfterGroup

	return snap, nil
}

// PrepareTieredBillingForSelectedGroup refreshes routing-dependent billing
// state before an upstream attempt. An existing session reserves any higher
// estimate before sending. If the initial group was free and skipped
// pre-consume, switching to a paid group creates the session at that point.
func PrepareTieredBillingForSelectedGroup(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo != nil && c != nil && c.GetInt("channel_type") == constant.ChannelTypeOpenRouter {
		relayInfo.TieredBillingSnapshot = nil
		relayInfo.OpenRouterPricingDeferred = true
		return nil
	}
	snap, err := refreshTieredBillingGroup(relayInfo)
	if err != nil {
		if admissionErr := BillingAdmissionError(c, relayInfo, err); admissionErr != nil {
			return admissionErr
		}
		return types.NewErrorWithStatusCode(
			err,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if snap == nil {
		return nil
	}
	if relayInfo.QuotaClamp != nil {
		return BillingAdmissionError(c, relayInfo, relayInfo.QuotaClamp)
	}
	if snap.GroupRatio == 0 {
		// Paid-to-free keeps FreeModel as-is: FreeModel means "pre-consume was
		// skipped", which is not true once a session exists, and settlement
		// already yields 0 for a zero group ratio.
		return nil
	}

	// The selected group is paid; clear a FreeModel flag frozen when the
	// initial group was free so downstream state stays consistent.
	relayInfo.PriceData.FreeModel = false

	if relayInfo.Billing == nil {
		return PreConsumeBilling(c, snap.EstimatedQuotaAfterGroup, relayInfo)
	}
	if err := relayInfo.Billing.Reserve(snap.EstimatedQuotaAfterGroup); err != nil {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	relayInfo.FinalPreConsumedQuota = relayInfo.Billing.GetPreConsumedQuota()
	return nil
}
