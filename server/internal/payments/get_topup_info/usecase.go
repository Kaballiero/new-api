package get_topup_info

import (
	"strconv"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Info struct {
	Availability                                         domain.Availability
	PayMethods                                           []map[string]string
	WaffoMethods                                         any
	Terms, CreemProducts, TopUpLink                      string
	Minimum, StripeMinimum, WaffoMinimum, PancakeMinimum int
	AmountOptions                                        []int
	Discount                                             map[int]float64
}

// Settings reads the current dashboard information each time it is requested.
type Settings interface {
	Info() Info
	WaffoMethods() any
}

type Usecase struct{ settings Settings }

func NewUsecase(settings Settings) Usecase { return Usecase{settings: settings} }

func (u Usecase) Execute() Info {
	info := u.settings.Info()
	if !info.Availability.Compliance {
		info.PayMethods = []map[string]string{}
	}
	for _, method := range []struct {
		provider, name, color string
		minimum               int
	}{
		{domain.PaymentProviderStripe, "Stripe", "#635BFF", info.StripeMinimum},
		{domain.PaymentProviderWaffoPancake, "Waffo Pancake", "#F97316", info.PancakeMinimum},
		{domain.PaymentProviderWaffo, "Waffo (Global Payment)", "#3B82F6", info.WaffoMinimum},
	} {
		if !info.Availability.TopUp(method.provider) {
			continue
		}
		found := false
		for _, existing := range info.PayMethods {
			if existing["type"] == method.provider {
				found = true
				break
			}
		}
		if !found {
			info.PayMethods = append(
				info.PayMethods,
				map[string]string{"name": method.name, "type": method.provider, "color": method.color, "min_topup": strconv.Itoa(method.minimum)},
			)
		}
	}
	if info.Availability.TopUp(domain.PaymentProviderWaffo) {
		info.WaffoMethods = u.settings.WaffoMethods()
	}
	return info
}
