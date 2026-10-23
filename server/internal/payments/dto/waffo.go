package dto

type WaffoPayRequest struct {
	Amount         int64  `json:"amount"`
	PayMethodIndex *int   `json:"pay_method_index"`
	PayMethodType  string `json:"pay_method_type"`
	PayMethodName  string `json:"pay_method_name"`
}
