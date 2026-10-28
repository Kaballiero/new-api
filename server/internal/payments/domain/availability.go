package domain

import "strings"

type Availability struct {
	Compliance                                      bool
	StripeAPI, StripeWebhook, StripePrice           string
	CreemAPI, CreemProducts, CreemWebhook           string
	WaffoEnabled                                    bool
	WaffoAPI, WaffoPrivate, WaffoPublic             string
	PancakeMerchant, PancakePrivate, PancakeProduct string
	EpayAddress, EpayID, EpayKey                    string
	EpayMethods                                     int
}

func (a Availability) TopUp(provider string) bool {
	if !a.Compliance {
		return false
	}
	switch provider {
	case PaymentProviderStripe:
		return strings.TrimSpace(a.StripeAPI) != "" && strings.TrimSpace(a.StripeWebhook) != "" && strings.TrimSpace(a.StripePrice) != ""
	case PaymentProviderCreem:
		p := strings.TrimSpace(a.CreemProducts)
		return strings.TrimSpace(a.CreemAPI) != "" && p != "" && p != "[]"
	case PaymentProviderWaffo:
		return a.WaffoEnabled && a.Configured(provider)
	case PaymentProviderWaffoPancake:
		return strings.TrimSpace(a.PancakeMerchant) != "" && strings.TrimSpace(a.PancakePrivate) != "" && strings.TrimSpace(a.PancakeProduct) != ""
	case PaymentProviderEpay:
		return a.Configured(provider) && a.EpayMethods > 0
	}
	return false
}

func (a Availability) Configured(provider string) bool {
	switch provider {
	case PaymentProviderStripe:
		return strings.TrimSpace(a.StripeWebhook) != ""
	case PaymentProviderCreem:
		return strings.TrimSpace(a.CreemWebhook) != ""
	case PaymentProviderWaffo:
		return strings.TrimSpace(a.WaffoAPI) != "" && strings.TrimSpace(a.WaffoPrivate) != "" && strings.TrimSpace(a.WaffoPublic) != ""
	case PaymentProviderWaffoPancake:
		return a.TopUp(provider)
	case PaymentProviderEpay:
		return strings.TrimSpace(a.EpayAddress) != "" && strings.TrimSpace(a.EpayID) != "" && strings.TrimSpace(a.EpayKey) != ""
	}
	return false
}

func (a Availability) Webhook(provider string) bool {
	return a.TopUp(provider) && (provider != PaymentProviderCreem || a.Configured(provider))
}
