package subscriptions

import (
	"errors"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	history "github.com/QuantumNous/new-api/internal/adapter/gorm/payments"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/subscriptions"
	paymentsdomain "github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/complete_order"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/expire_order"
	"github.com/QuantumNous/new-api/model"
)

type Orders struct {
	complete complete_order.Usecase
	expire   expire_order.Usecase
	store    storage.Store
}

func NewOrders() Orders {
	store := storage.NewStore(func() *gorm.DB { return model.DB })
	orderStore := storage.NewOrderStore(store, func(tx *gorm.DB, o domain.SubscriptionOrder) error {
		err := history.UpsertSubscriptionHistory(
			tx,
			history.SubscriptionHistory{
				UserID:        o.UserId,
				Money:         o.Money,
				TradeNo:       o.TradeNo,
				PaymentMethod: o.PaymentMethod,
				CreateTime:    o.CreateTime,
			},
		)
		if errors.Is(err, paymentsdomain.ErrProviderMismatch) {
			return domain.ErrProviderMismatch
		}
		return err
	})
	states := domain.OrderStates{
		Pending: paymentsdomain.StatusPending,
		Success: paymentsdomain.StatusSuccess,
		Expired: paymentsdomain.StatusExpired,
	}
	return Orders{
		complete_order.NewUsecase(orderStore, effects{}, states, common.GetTimestamp),
		expire_order.NewUsecase(
			store,
			states,
			common.GetTimestamp,
		),
		store,
	}
}
func (o Orders) Complete(in complete_order.Input) error { return o.complete.Execute(in) }
func (o Orders) Expire(tradeNo, provider string) error {
	return o.expire.Execute(expire_order.Input{TradeNo: tradeNo, ExpectedProvider: provider})
}
func (o Orders) Find(tradeNo string) *domain.SubscriptionOrder { return o.store.FindOrder(tradeNo) }
