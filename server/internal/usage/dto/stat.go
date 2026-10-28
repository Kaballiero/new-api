package dto

type Stat struct {
	Quota       int `json:"quota"`
	RefundQuota int `json:"refund_quota"`
	Rpm         int `json:"rpm"`
	Tpm         int `json:"tpm"`
}
