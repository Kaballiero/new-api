package get_topup_info

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }

func (h HTTP) Handle(c *gin.Context) {
	info := h.usecase.Execute()
	common.ApiSuccess(
		c,
		gin.H{
			"enable_online_topup":              info.Availability.TopUp(domain.PaymentProviderEpay),
			"enable_stripe_topup":              info.Availability.TopUp(domain.PaymentProviderStripe),
			"enable_creem_topup":               info.Availability.TopUp(domain.PaymentProviderCreem),
			"enable_waffo_topup":               info.Availability.TopUp(domain.PaymentProviderWaffo),
			"enable_waffo_pancake_topup":       info.Availability.TopUp(domain.PaymentProviderWaffoPancake),
			"enable_redemption":                info.Availability.Compliance,
			"payment_compliance_confirmed":     info.Availability.Compliance,
			"payment_compliance_terms_version": info.Terms,
			"waffo_pay_methods":                info.WaffoMethods,
			"creem_products":                   info.CreemProducts,
			"pay_methods":                      info.PayMethods,
			"min_topup":                        info.Minimum,
			"stripe_min_topup":                 info.StripeMinimum,
			"waffo_min_topup":                  info.WaffoMinimum,
			"waffo_pancake_min_topup":          info.PancakeMinimum,
			"amount_options":                   info.AmountOptions,
			"discount":                         info.Discount,
			"topup_link":                       info.TopUpLink,
		},
	)
}
