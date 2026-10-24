package billing

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	usagegateway "github.com/QuantumNous/new-api/relay/usage"
)

// BRIDGE: service/batch_billing.go uses the shared usage serializer;
// remove these methods when batch accounting calls its usage owner directly.
func (*Gateway) AppendTaskClamp(o *model.LogOther, c *common.QuotaClamp) {
	usagegateway.AppendTaskClamp(o, c)
}
func (*Gateway) AppendTaskTiered(o *model.LogOther, i *relaycommon.RelayInfo, t *billingexpr.TieredResult) {
	usagegateway.AppendTaskTiered(o, i, t)
}
