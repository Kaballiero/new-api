package dto

type AmountRequest struct {
	Amount int64 `json:"amount"`
}

type EpayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}
