package billing

import (
	adapter "github.com/QuantumNous/new-api/internal/adapter/billing"
	"github.com/QuantumNous/new-api/internal/billing/charge_violation_fee"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

var ViolationFee = charge_violation_fee.NewUsecase(func() charge_violation_fee.Settings {
	settings := model_setting.GetGrokSettings()
	if settings == nil {
		return charge_violation_fee.Settings{}
	}
	return charge_violation_fee.Settings{Enabled: settings.ViolationDeductionEnabled, Amount: func() float64 { return settings.ViolationDeductionAmount }}
}, adapter.ViolationFeeQuota)
