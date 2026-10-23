package model

import "github.com/QuantumNous/new-api/internal/adapter/gorm/payments/record"

type TopUp = record.TopUp

const (
	PaymentMethodStripe         = record.PaymentMethodStripe
	PaymentMethodCreem          = record.PaymentMethodCreem
	PaymentMethodWaffo          = record.PaymentMethodWaffo
	PaymentMethodWaffoPancake   = record.PaymentMethodWaffoPancake
	PaymentMethodBalance        = record.PaymentMethodBalance
	PaymentProviderEpay         = record.PaymentProviderEpay
	PaymentProviderStripe       = record.PaymentProviderStripe
	PaymentProviderCreem        = record.PaymentProviderCreem
	PaymentProviderWaffo        = record.PaymentProviderWaffo
	PaymentProviderWaffoPancake = record.PaymentProviderWaffoPancake
	PaymentProviderBalance      = record.PaymentProviderBalance
)
