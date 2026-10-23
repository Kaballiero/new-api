package payments

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"strings"
)

type Settings struct{}

func (Settings) Availability() domain.Availability {
	a := domain.Availability{Compliance: operation_setting.IsPaymentComplianceConfirmed(), StripeAPI: setting.StripeApiSecret, StripeWebhook: setting.StripeWebhookSecret, StripePrice: setting.StripePriceId,
		CreemAPI: setting.CreemApiKey, CreemProducts: setting.CreemProducts, CreemWebhook: setting.CreemWebhookSecret, WaffoEnabled: setting.WaffoEnabled,
		WaffoAPI: setting.WaffoApiKey, WaffoPrivate: setting.WaffoPrivateKey, WaffoPublic: setting.WaffoPublicCert,
		PancakeMerchant: setting.WaffoPancakeMerchantID, PancakePrivate: setting.WaffoPancakePrivateKey, PancakeProduct: setting.WaffoPancakeProductID,
		EpayAddress: operation_setting.PayAddress, EpayID: operation_setting.EpayId, EpayKey: operation_setting.EpayKey, EpayMethods: len(operation_setting.PayMethods)}
	if setting.WaffoSandbox {
		a.WaffoAPI = setting.WaffoSandboxApiKey
		a.WaffoPrivate = setting.WaffoSandboxPrivateKey
		a.WaffoPublic = setting.WaffoSandboxPublicCert
	}
	return a
}
func (Settings) ReturnPath(suffix string) string {
	return strings.TrimRight(system_setting.ServerAddress, "/") + suffix
}
func (Settings) CallbackAddress() string {
	if operation_setting.CustomCallbackAddress == "" {
		return system_setting.ServerAddress
	}
	return operation_setting.CustomCallbackAddress
}
func (Settings) QuotaPerUnit() float64 { return common.QuotaPerUnit }
func (Settings) TokenDisplay() bool {
	return operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens
}
func (Settings) GroupRatio(group string) float64 { return common.GetTopupGroupRatio(group) }
func (Settings) Discount(amount int64) float64 {
	if value, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok && value > 0 {
		return value
	}
	return 1
}
func (Settings) MinTopUp(provider string) int64 {
	switch provider {
	case domain.PaymentProviderStripe:
		return int64(setting.StripeMinTopUp)
	case domain.PaymentProviderWaffo:
		return int64(setting.WaffoMinTopUp)
	case domain.PaymentProviderWaffoPancake:
		return int64(setting.WaffoPancakeMinTopUp)
	default:
		return int64(operation_setting.MinTopUp)
	}
}
func (Settings) UnitPrice(provider string) float64 {
	switch provider {
	case domain.PaymentProviderStripe:
		return setting.StripeUnitPrice
	case domain.PaymentProviderWaffo:
		return setting.WaffoUnitPrice
	case domain.PaymentProviderWaffoPancake:
		return setting.WaffoPancakeUnitPrice
	default:
		return operation_setting.Price
	}
}

func (Settings) ContainsMethod(method string) bool {
	return operation_setting.ContainsPayMethod(method)
}
