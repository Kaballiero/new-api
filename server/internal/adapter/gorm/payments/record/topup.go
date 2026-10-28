package record

import "github.com/QuantumNous/new-api/internal/payments/domain"

type TopUp struct {
	Id              int     `json:"id"`
	UserId          int     `json:"user_id" gorm:"index"`
	Amount          int64   `json:"amount"`
	Money           float64 `json:"money"`
	TradeNo         string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	CreateTime      int64   `json:"create_time"`
	CompleteTime    int64   `json:"complete_time"`
	Status          string  `json:"status"`
}

const (
	PaymentMethodStripe         = domain.PaymentMethodStripe
	PaymentMethodCreem          = domain.PaymentMethodCreem
	PaymentMethodWaffo          = domain.PaymentMethodWaffo
	PaymentMethodWaffoPancake   = domain.PaymentMethodWaffoPancake
	PaymentMethodBalance        = domain.PaymentMethodBalance
	PaymentProviderEpay         = domain.PaymentProviderEpay
	PaymentProviderStripe       = domain.PaymentProviderStripe
	PaymentProviderCreem        = domain.PaymentProviderCreem
	PaymentProviderWaffo        = domain.PaymentProviderWaffo
	PaymentProviderWaffoPancake = domain.PaymentProviderWaffoPancake
	PaymentProviderBalance      = domain.PaymentProviderBalance
)
