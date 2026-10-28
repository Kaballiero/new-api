package batch

import (
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type Prices struct{}

func (Prices) Expression(model string) (string, bool) {
	return billing_setting.GetBatchBillingExpr(model)
}
func (Prices) Validate(expression string) error { return billing_setting.SmokeTestExpr(expression) }
func (Prices) Group(user, using string) float64 {
	group, ok := ratio_setting.GetGroupGroupRatio(user, using)
	if !ok {
		group = ratio_setting.GetGroupRatio(using)
	}
	return group
}
