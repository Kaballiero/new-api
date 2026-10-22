package apply_quota_delta

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/billing/domain"
)

type Store interface {
	DebitWallet(int, int) error
	CreditWallet(int, int) error
	AdjustSubscription(int, int64) error
	DebitToken(domain.Account, int) error
	CreditToken(domain.Account, int) error
}

type Input struct {
	Account        domain.Account
	Source         string
	SubscriptionID int
	Delta          int
	// Realtime terminal deltas skip zero wallet/token writes and applied legs.
	Realtime                     bool
	FundingSettled, TokenSettled bool
	FundingDelta, TokenDelta     int
}

type Result struct {
	FundingApplied, TokenApplied bool
	SubscriptionPostDelta        int64
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store} }

func (u Usecase) Execute(input Input) (result Result, err error) {
	fundingDelta, tokenDelta := input.Delta, input.Delta
	if input.Realtime {
		fundingDelta, tokenDelta = input.FundingDelta, input.TokenDelta
	}
	if !input.Realtime || !input.FundingSettled {
		if input.Source == domain.Subscription {
			if input.SubscriptionID == 0 {
				return result, errors.New("subscription id is missing")
			}
			if fundingDelta != 0 || input.Realtime {
				if err := u.store.AdjustSubscription(input.SubscriptionID, int64(fundingDelta)); err != nil {
					return result, err
				}
				result.SubscriptionPostDelta = int64(fundingDelta)
			}
		} else if fundingDelta > 0 {
			err = u.store.DebitWallet(input.Account.UserID, fundingDelta)
		} else if fundingDelta < 0 || !input.Realtime {
			err = u.store.CreditWallet(input.Account.UserID, -fundingDelta)
		}
		if err != nil {
			return result, err
		}
		result.FundingApplied = true
	}
	if input.Account.Playground || input.Realtime && input.TokenSettled {
		return result, nil
	}
	if tokenDelta > 0 {
		err = u.store.DebitToken(input.Account, tokenDelta)
	} else if tokenDelta < 0 || !input.Realtime {
		err = u.store.CreditToken(input.Account, -tokenDelta)
	}
	if err != nil {
		return result, err
	}
	result.TokenApplied = true
	return result, nil
}
