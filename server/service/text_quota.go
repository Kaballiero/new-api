package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// BRIDGE: immutable internal/billing contract boundary.
func PostTextConsumeQuota(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, extra []string) {
	BillingGateway.PostText(c, info, usage, extra)
}
