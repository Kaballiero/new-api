package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

// BillingGatewayPort uses gateway types only. Linking relay/billing registers
// its implementation once without a service -> gateway import cycle.
type BillingGatewayPort interface {
	PreConsume(*gin.Context, int, *relaycommon.RelayInfo) *types.NewAPIError
	SettleWss(*gin.Context, *relaycommon.RelayInfo, int) error
	ApplyDelta(*relaycommon.RelayInfo, int, int, bool) (bool, bool, error)
	PostText(*gin.Context, *relaycommon.RelayInfo, *dto.Usage, []string)
	PostWss(*gin.Context, *relaycommon.RelayInfo, string, *dto.RealtimeUsage, string) error
	PreWss(*gin.Context, *relaycommon.RelayInfo, *dto.RealtimeUsage) error
	TokenParams(*dto.Usage, bool, map[string]bool) billingexpr.TokenParams
	CacheCreateTokens(dto.Usage, hosttypes.PriceData) int
	CurrentFX() (BillingFXBasis, error)
	CaptureFX(*relaycommon.RelayInfo) (float64, error)
	ApplyFX(float64, float64) (float64, error)
	AppendTaskBillingFX(*model.LogOther, *relaycommon.RelayInfo, hosttypes.PriceData, string)
	AppendTaskQuotaSaturation(*gin.Context, *relaycommon.RelayInfo, *model.LogOther)
	AppendTaskClamp(*model.LogOther, *common.QuotaClamp)
	AppendTaskTiered(*model.LogOther, *relaycommon.RelayInfo, *billingexpr.TieredResult)
	AppendTaskPlugin(*model.LogOther, *model.TaskPluginSnapshot)
	MidjourneyOther(*relaycommon.RelayInfo, hosttypes.PriceData) *model.LogOther
}

// BRIDGE: service/{batch_billing,task_billing,midjourney,violation_fee,billing_fx,tiered_settle}.go until wave 5;
// immutable internal/billing/{contract,usage_contract}_test.go and controller/wss_characterization_test.go.
var BillingGateway BillingGatewayPort

func PreConsumeBilling(c *gin.Context, quota int, info *relaycommon.RelayInfo) *types.NewAPIError {
	return BillingGateway.PreConsume(c, quota, info)
}

func SettleWssBilling(c *gin.Context, info *relaycommon.RelayInfo, actual int) error {
	return BillingGateway.SettleWss(c, info, actual)
}

type postConsumeQuotaResult struct{ FundingApplied, TokenApplied bool }

func PostConsumeQuota(info *relaycommon.RelayInfo, delta, preConsumed int, notify bool) error {
	_, err := postConsumeQuotaWithResult(info, delta, preConsumed, notify)
	return err
}

func postConsumeQuotaWithResult(info *relaycommon.RelayInfo, delta, preConsumed int, notify bool) (postConsumeQuotaResult, error) {
	funding, token, err := BillingGateway.ApplyDelta(info, delta, preConsumed, notify)
	return postConsumeQuotaResult{funding, token}, err
}
