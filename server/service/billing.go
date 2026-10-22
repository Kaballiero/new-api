package service

import (
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
	Settle(*gin.Context, *relaycommon.RelayInfo, int) error
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
}

// BRIDGE: service/{text_quota,quota,batch_billing,midjourney,violation_fee}.go,
// internal/billing/contract_test.go and controller/wss_characterization_test.go until 2c / wave 5.
var BillingGateway BillingGatewayPort

// OpenRouterSettlementOutcome is the projection consumed by existing cost logs.
type OpenRouterSettlementOutcome struct {
	TargetQuota         int    `json:"target_quota"`
	ReservedQuota       int    `json:"reserved_quota"`
	FundingAppliedQuota int    `json:"funding_applied_quota"`
	TokenAppliedQuota   int    `json:"token_applied_quota"`
	SettlementStatus    string `json:"settlement_status"`
	FailureStage        string `json:"failure_stage,omitempty"`
}

func PreConsumeBilling(c *gin.Context, quota int, info *relaycommon.RelayInfo) *types.NewAPIError {
	return BillingGateway.PreConsume(c, quota, info)
}

func SettleBilling(c *gin.Context, info *relaycommon.RelayInfo, actual int) error {
	return BillingGateway.Settle(c, info, actual)
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
