package settle_reservation

import (
	"fmt"

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
	Reservation             *domain.Reservation
	Actual                  int
	Realtime, RecordOutcome bool
}
type Usecase struct {
	store Store
	log   func(string)
}

func NewUsecase(store Store, log func(string)) Usecase { return Usecase{store, log} }

func (u Usecase) Execute(input Input) error {
	r := input.Reservation
	if r.Settled {
		return nil
	}
	if input.Realtime {
		return u.settleRealtime(r, input.Actual)
	}
	if input.RecordOutcome {
		r.Outcome = domain.Outcome{TargetQuota: input.Actual, ReservedQuota: r.PreConsumed, FundingAppliedQuota: r.PreConsumed, TokenAppliedQuota: r.TokenConsumed, SettlementStatus: "settled"}
	}
	delta := input.Actual - r.PreConsumed
	if delta == 0 {
		r.Settled = true
		return nil
	}
	if !r.FundingSettled {
		if err := u.adjustFunding(r, delta); err != nil {
			if input.RecordOutcome {
				r.Outcome.SettlementStatus = "funding_delta_failed"
				r.Outcome.FailureStage = "funding_delta"
				r.Settled = true
			}
			return err
		}
		r.FundingSettled = true
	}
	if input.RecordOutcome {
		r.Outcome.FundingAppliedQuota = input.Actual
	}
	var tokenErr error
	if !r.Account.Playground {
		tokenErr = u.adjustToken(r, delta)
		if tokenErr != nil {
			u.log(fmt.Sprintf("error adjusting token quota after funding settled (userId=%d, tokenId=%d, delta=%d): %s", r.Account.UserID, r.Account.TokenID, delta, tokenErr))
		}
	}
	if input.RecordOutcome {
		if tokenErr != nil {
			r.Outcome.SettlementStatus = "token_delta_failed"
			r.Outcome.FailureStage = "token_delta"
		} else if !r.Account.Playground {
			r.Outcome.TokenAppliedQuota = input.Actual
		}
	}
	if r.Source == domain.Subscription {
		r.SubscriptionPostDelta += int64(delta)
	}
	r.Settled = true
	return tokenErr
}

func (u Usecase) settleRealtime(r *domain.Reservation, actual int) error {
	fundingDelta := actual - r.PreConsumed - r.WssFundingDirect
	if !r.WssFundingSettled {
		if err := u.adjustFunding(r, fundingDelta); err != nil {
			return err
		}
		r.FundingSettled, r.WssFundingSettled = true, true
		if r.Source == domain.Subscription {
			r.SubscriptionPostDelta += int64(fundingDelta)
		}
	}
	if r.Account.Playground {
		r.Settled = true
		return nil
	}
	if err := u.adjustToken(r, actual-r.TokenConsumed-r.WssTokenDirect); err != nil {
		return err
	}
	r.Settled = true
	return nil
}

func (u Usecase) adjustFunding(r *domain.Reservation, delta int) error {
	if delta == 0 {
		return nil
	}
	if r.Source == domain.Subscription {
		return u.store.AdjustSubscription(r.Subscription.ID, int64(delta))
	}
	if delta > 0 {
		return u.store.DebitWallet(r.Account.UserID, delta)
	}
	return u.store.CreditWallet(r.Account.UserID, -delta)
}
func (u Usecase) adjustToken(r *domain.Reservation, delta int) error {
	if delta > 0 {
		return u.store.DebitToken(r.Account, delta)
	}
	if delta < 0 {
		return u.store.CreditToken(r.Account, -delta)
	}
	return nil
}
