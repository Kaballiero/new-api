package expire_order

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

// Store locks the order and saves only the original pending-to-expired transition.
type Store interface {
	Expire(run func(domain.ExpireTx) error) error
}
type Input struct{ TradeNo, ExpectedProvider string }
type Usecase struct {
	store  Store
	states domain.OrderStates
	now    func() int64
}

func NewUsecase(store Store, states domain.OrderStates, now func() int64) Usecase {
	return Usecase{store: store, states: states, now: now}
}
func (u Usecase) Execute(in Input) error {
	if in.TradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	return u.store.Expire(func(tx domain.ExpireTx) error {
		order, err := tx.LockOrder(in.TradeNo)
		if err != nil {
			return domain.ErrOrderNotFound
		}
		if in.ExpectedProvider != "" && order.PaymentProvider != in.ExpectedProvider {
			return domain.ErrProviderMismatch
		}
		if order.Status != u.states.Pending {
			return nil
		}
		order.Status = u.states.Expired
		order.CompleteTime = u.now()
		return tx.SaveOrder(order)
	})
}
