package payments

import (
	"errors"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/payments/record"
	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type SubscriptionHistory struct {
	UserID                 int
	Money                  float64
	TradeNo, PaymentMethod string
	CreateTime             int64
}

func UpsertSubscriptionHistory(tx *gorm.DB, order SubscriptionHistory) error {
	now := common.GetTimestamp()
	var topup record.TopUp
	if err := tx.Where("trade_no = ?", order.TradeNo).First(&topup).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			topup = record.TopUp{UserId: order.UserID, Amount: 0, Money: order.Money, TradeNo: order.TradeNo, PaymentMethod: order.PaymentMethod, CreateTime: order.CreateTime, CompleteTime: now, Status: domain.StatusSuccess}
			return tx.Create(&topup).Error
		}
		return err
	}
	topup.Money = order.Money
	if topup.PaymentMethod == "" {
		topup.PaymentMethod = order.PaymentMethod
	} else if topup.PaymentMethod != order.PaymentMethod {
		return domain.ErrProviderMismatch
	}
	if topup.CreateTime == 0 {
		topup.CreateTime = order.CreateTime
	}
	topup.CompleteTime = now
	topup.Status = domain.StatusSuccess
	return tx.Save(&topup).Error
}
