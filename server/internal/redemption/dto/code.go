package dto

import (
	"time"

	"github.com/QuantumNous/new-api/internal/redemption/domain"
)

// Redemption preserves the API shape and decoder type name independently of the stored record.
type Redemption struct {
	Id           int    `json:"id"`
	UserId       int    `json:"user_id"`
	Key          string `json:"key"`
	Status       int    `json:"status"`
	Name         string `json:"name"`
	Quota        int    `json:"quota"`
	CreatedTime  int64  `json:"created_time"`
	RedeemedTime int64  `json:"redeemed_time"`
	Count        int    `json:"count"`
	UsedUserId   int    `json:"used_user_id"`
	DeletedAt    *time.Time
	ExpiredTime  int64 `json:"expired_time"`
}

func FromCode(code domain.Code) Redemption {
	return Redemption{Id: code.ID, UserId: code.IssuerID, Key: code.Key, Status: code.Status, Name: code.Name, Quota: code.Quota,
		CreatedTime: code.CreatedTime, RedeemedTime: code.RedeemedTime, UsedUserId: code.UsedUserID, ExpiredTime: code.ExpiredTime}
}

func FromCodes(codes []domain.Code) []Redemption {
	if codes == nil {
		return nil
	}
	rows := make([]Redemption, len(codes))
	for i := range codes {
		rows[i] = FromCode(codes[i])
	}
	return rows
}
