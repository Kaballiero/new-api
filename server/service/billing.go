package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

// BillingLifecyclePort uses gateway types only. Linking relay/billing registers
// its implementation once without a service -> gateway import cycle.
type BillingLifecyclePort interface {
	PreConsume(*gin.Context, int, *relaycommon.RelayInfo) *types.NewAPIError
	Settle(*gin.Context, *relaycommon.RelayInfo, int) error
	SettleWss(*gin.Context, *relaycommon.RelayInfo, int) error
	ApplyDelta(*relaycommon.RelayInfo, int, int, bool) (bool, bool, error)
}

// BRIDGE: service consumers until wave 2b2/5 move them.
var BillingLifecycle BillingLifecyclePort

// OpenRouterSettlementOutcome is the projection consumed by existing cost logs.
type OpenRouterSettlementOutcome struct {
	TargetQuota         int    `json:"target_quota"`
	ReservedQuota       int    `json:"reserved_quota"`
	FundingAppliedQuota int    `json:"funding_applied_quota"`
	TokenAppliedQuota   int    `json:"token_applied_quota"`
	SettlementStatus    string `json:"settlement_status"`
	FailureStage        string `json:"failure_stage,omitempty"`
}

type wssBillingSettler interface {
	RecordWssDirectCharge(int, bool, bool)
	SettleWss(int) error
}

// BRIDGE: service consumers until wave 2b2/5 move them.
func PreConsumeBilling(c *gin.Context, quota int, info *relaycommon.RelayInfo) *types.NewAPIError {
	return BillingLifecycle.PreConsume(c, quota, info)
}

// BRIDGE: service consumers until wave 2b2/5 move them.
func SettleBilling(c *gin.Context, info *relaycommon.RelayInfo, actual int) error {
	return BillingLifecycle.Settle(c, info, actual)
}

// BRIDGE: service consumers until wave 2b2/5 move them.
func SettleWssBilling(c *gin.Context, info *relaycommon.RelayInfo, actual int) error {
	return BillingLifecycle.SettleWss(c, info, actual)
}

type postConsumeQuotaResult struct{ FundingApplied, TokenApplied bool }

// BRIDGE: service consumers until wave 2b2/5 move them.
func PostConsumeQuota(info *relaycommon.RelayInfo, delta, preConsumed int, notify bool) error {
	_, err := postConsumeQuotaWithResult(info, delta, preConsumed, notify)
	return err
}

// BRIDGE: service consumers until wave 2b2/5 move them.
func postConsumeQuotaWithResult(info *relaycommon.RelayInfo, delta, preConsumed int, notify bool) (postConsumeQuotaResult, error) {
	funding, token, err := BillingLifecycle.ApplyDelta(info, delta, preConsumed, notify)
	return postConsumeQuotaResult{funding, token}, err
}
