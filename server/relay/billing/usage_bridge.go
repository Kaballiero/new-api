package billing

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	usagegateway "github.com/QuantumNous/new-api/relay/usage"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func (*Gateway) AppendTaskBillingFX(o *model.LogOther, i *relaycommon.RelayInfo, p hosttypes.PriceData, stage string) {
	usagegateway.AppendTaskBillingFX(o, i, p, stage)
}
func (*Gateway) AppendTaskQuotaSaturation(c *gin.Context, i *relaycommon.RelayInfo, o *model.LogOther) {
	usagegateway.AppendTaskQuotaSaturation(c, i, o)
}
func (*Gateway) AppendTaskClamp(o *model.LogOther, c *common.QuotaClamp) {
	usagegateway.AppendTaskClamp(o, c)
}
func (*Gateway) AppendTaskTiered(o *model.LogOther, i *relaycommon.RelayInfo, t *billingexpr.TieredResult) {
	usagegateway.AppendTaskTiered(o, i, t)
}
func (*Gateway) AppendTaskPlugin(o *model.LogOther, p *model.TaskPluginSnapshot) {
	usagegateway.AppendTaskPlugin(o, p)
}

// BRIDGE: relay/mjproxy_handler.go and the service MJ log-privacy regression until wave 5.
func (*Gateway) MidjourneyOther(info *relaycommon.RelayInfo, prices hosttypes.PriceData) *model.LogOther {
	return usagegateway.MidjourneyOther(info, prices)
}
