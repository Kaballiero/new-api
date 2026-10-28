package refund_reservation

import (
	"time"

	"github.com/QuantumNous/new-api/internal/billing/domain"
)

type Store interface {
	CreditWallet(int, int) error
	RefundSubscription(string) error
	AdjustSubscription(int, int64) error
	CreditToken(domain.Account, int) error
}

type Usecase struct {
	store  Store
	submit func(func())
	sleep  func(time.Duration)
	log    func(string)
}

func NewUsecase(store Store, submit func(func()), sleep func(time.Duration), log func(string)) Usecase {
	return Usecase{store, submit, sleep, log}
}

func (u Usecase) Execute(r *domain.Reservation) error {
	if !r.NeedsRefund() {
		return nil
	}
	r.Refunded = true
	// Freeze the same scalar refund state before queuing the asynchronous job.
	hold := *r
	u.submit(func() {
		var err error
		if hold.Source == domain.Subscription {
			if hold.Subscription.PreConsumed > 0 {
				for attempt := range 3 {
					err = u.store.RefundSubscription(hold.RequestID)
					if err == nil {
						break
					}
					if attempt < 2 {
						u.sleep(time.Duration(200*(attempt+1)) * time.Millisecond)
					}
				}
			}
		} else if hold.WalletConsumed > 0 {
			// Additive wallet credit has no durable idempotency key; never retry.
			err = u.store.CreditWallet(hold.Account.UserID, hold.WalletConsumed)
		}
		if err != nil {
			u.log("error refunding billing source: " + err.Error())
		}
		if hold.ExtraReserved > 0 && hold.Source == domain.Subscription && hold.Subscription.ID > 0 {
			if err := u.store.AdjustSubscription(hold.Subscription.ID, -int64(hold.ExtraReserved)); err != nil {
				u.log("error refunding subscription extra reserved quota: " + err.Error())
			}
		}
		if hold.TokenConsumed > 0 && !hold.Account.Playground {
			if err := u.store.CreditToken(hold.Account, hold.TokenConsumed); err != nil {
				u.log("error refunding token quota: " + err.Error())
			}
		}
	})
	return nil
}
