package payments

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/get_topup_info"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func (s Settings) Info() get_topup_info.Info {
	return get_topup_info.Info{
		Availability: s.Availability(),
		PayMethods:   operation_setting.PayMethods,

		Terms: operation_setting.CurrentComplianceTermsVersion,

		CreemProducts:  setting.CreemProducts,
		TopUpLink:      common.TopUpLink,
		Minimum:        operation_setting.MinTopUp,
		StripeMinimum:  setting.StripeMinTopUp,
		WaffoMinimum:   setting.WaffoMinTopUp,
		PancakeMinimum: setting.WaffoPancakeMinTopUp,

		AmountOptions: operation_setting.GetPaymentSetting().AmountOptions,
		Discount:      operation_setting.GetPaymentSetting().AmountDiscount,
	}
}

func (Settings) WaffoMethods() any { return setting.GetWaffoPayMethods() }
