package increase_reservation

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/billing/domain"
)

type Store interface {
	domain.TokenReserver
	ReserveWallet(int, int) (bool, error)
	DebitWallet(int, int) error
	CreditWallet(int, int) error
	AdjustSubscription(int, int64) error
}

type Input struct {
	Reservation  *domain.Reservation
	Target       int
	AtomicWallet bool
}
type Usecase struct {
	store Store
	log   func(string)
}

func NewUsecase(store Store, log func(string)) Usecase { return Usecase{store, log} }

func (u Usecase) Execute(input Input) error {
	r := input.Reservation
	if r.Trusted && input.AtomicWallet {
		r.Trusted = false
	}
	if r.Settled || r.Refunded || r.Trusted || input.Target <= r.PreConsumed {
		return nil
	}
	if r.Source != domain.Wallet && r.Source != domain.Subscription {
		return &domain.Failure{Kind: domain.UpdateFailed, Cause: fmt.Errorf("unsupported funding source: %s", r.Source)}
	}
	delta := input.Target - r.PreConsumed
	if r.Source == domain.Wallet {
		if input.AtomicWallet {
			reserved, err := u.store.ReserveWallet(r.Account.UserID, delta)
			if err != nil {
				return &domain.Failure{Kind: domain.UpdateFailed, Cause: err}
			}
			if !reserved {
				return &domain.Failure{Kind: domain.WalletReserveRejected}
			}
		} else if err := u.store.DebitWallet(r.Account.UserID, delta); err != nil {
			return &domain.Failure{Kind: domain.UpdateFailed, Cause: err}
		}
		r.WalletConsumed += delta
	} else if err := u.store.AdjustSubscription(r.Subscription.ID, int64(delta)); err != nil {
		return &domain.Failure{Kind: domain.SubscriptionUnavailable, Cause: err}
	}
	if !r.Account.Playground {
		if err := domain.ReserveToken(u.store, r.Account, delta); err != nil {
			if r.Source == domain.Wallet {
				if rollback := u.store.CreditWallet(r.Account.UserID, delta); rollback != nil {
					u.log("error rolling back wallet funding reserve: " + rollback.Error())
				} else {
					r.WalletConsumed -= delta
				}
			} else if rollback := u.store.AdjustSubscription(r.Subscription.ID, -int64(delta)); rollback != nil {
				u.log("error rolling back subscription funding reserve: " + rollback.Error())
			}
			return &domain.Failure{Kind: domain.TokenReserveFailed, Cause: err}
		}
	}
	r.PreConsumed += delta
	r.TokenConsumed += delta
	r.ExtraReserved += delta
	r.SubscriptionPostDelta = 0
	return nil
}
