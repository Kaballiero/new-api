package dto

import (
	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type TopUp struct {
	Id              int     `json:"id"`
	UserId          int     `json:"user_id"`
	Amount          int64   `json:"amount"`
	Money           float64 `json:"money"`
	TradeNo         string  `json:"trade_no"`
	PaymentMethod   string  `json:"payment_method"`
	PaymentProvider string  `json:"payment_provider"`
	CreateTime      int64   `json:"create_time"`
	CompleteTime    int64   `json:"complete_time"`
	Status          string  `json:"status"`
}

func FromOrders(orders []domain.Order) []*TopUp {
	if orders == nil {
		return nil
	}
	rows := make([]*TopUp, len(orders))
	for i, o := range orders {
		rows[i] = &TopUp{
			Id:              o.ID,
			UserId:          o.UserID,
			Amount:          o.Amount,
			Money:           o.Money,
			TradeNo:         o.TradeNo,
			PaymentMethod:   o.PaymentMethod,
			PaymentProvider: o.PaymentProvider,
			CreateTime:      o.CreateTime,
			CompleteTime:    o.CompleteTime,
			Status:          o.Status,
		}
	}
	return rows
}
