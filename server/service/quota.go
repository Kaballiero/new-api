package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// BRIDGE: immutable WSS characterization and billing cache-inference contract.
func PreWssConsumeQuota(c *gin.Context, info *relaycommon.RelayInfo, u *dto.RealtimeUsage) error {
	return BillingGateway.PreWss(c, info, u)
}
func PostWssConsumeQuota(c *gin.Context, info *relaycommon.RelayInfo, m string, u *dto.RealtimeUsage, extra string) error {
	return BillingGateway.PostWss(c, info, m, u, extra)
}
func CalcOpenRouterCacheCreateTokens(u dto.Usage, p types.PriceData) int {
	return BillingGateway.CacheCreateTokens(u, p)
}
