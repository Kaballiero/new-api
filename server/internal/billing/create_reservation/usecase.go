package create_reservation

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/billing/domain"
)

type Store interface {
	domain.TokenReserver
	WalletBalance(int) (int, error)
	ReserveWallet(int, int) (bool, error)
	CreditToken(domain.Account, int) error
	HasSubscription(int) (bool, error)
	AllowsWalletOverflow(int) (bool, error)
	ReserveSubscription(string, int, string, int64) (domain.SubscriptionHold, error)
}

type Input struct {
	Account                          domain.Account
	RequestID, ModelName, Preference string
	Quota, UserQuota, TokenQuota     int
	Force                            bool
}

type Usecase struct {
	store      Store
	trustQuota func() int
	log        func(string)
}

func NewUsecase(store Store, trustQuota func() int, log func(string)) Usecase {
	return Usecase{store, trustQuota, log}
}

func (u Usecase) Execute(input Input) (*domain.Reservation, error) {
	r := &domain.Reservation{Account: input.Account, RequestID: input.RequestID, ModelName: input.ModelName, UserQuota: input.UserQuota}
	preference := domain.NormalizePreference(input.Preference)
	switch preference {
	case "wallet_only":
		return r, u.reserve(r, input, domain.Wallet)
	case "subscription_only":
		return r, u.reserve(r, input, domain.Subscription)
	case "wallet_first":
		err := u.reserve(r, input, domain.Wallet)
		if domain.IsInsufficient(err) {
			err = u.reserve(r, input, domain.Subscription)
		}
		return r, err
	default:
		has, err := u.store.HasSubscription(input.Account.UserID)
		if err != nil {
			return r, &domain.Failure{Kind: domain.QueryFailed, Cause: err}
		}
		if !has {
			return r, u.reserve(r, input, domain.Wallet)
		}
		err = u.reserve(r, input, domain.Subscription)
		if !domain.IsInsufficient(err) {
			return r, err
		}
		allowed, queryErr := u.store.AllowsWalletOverflow(input.Account.UserID)
		if queryErr != nil {
			return r, &domain.Failure{Kind: domain.QueryFailed, Cause: queryErr}
		}
		if allowed {
			return r, u.reserve(r, input, domain.Wallet)
		}
		return r, err
	}
}

// reserve owns initial admission: token first, then the selected funding leg.
// Funding fallback keeps the wallet preflight's UserQuota snapshot as before.
func (u Usecase) reserve(r *domain.Reservation, input Input, source string) error {
	*r = domain.Reservation{Account: input.Account, RequestID: input.RequestID, ModelName: input.ModelName, UserQuota: r.UserQuota, Source: source}
	quota := input.Quota
	if source == domain.Wallet {
		balance, err := u.store.WalletBalance(input.Account.UserID)
		if err != nil {
			return &domain.Failure{Kind: domain.QueryFailed, Cause: err}
		}
		if balance <= 0 {
			return &domain.Failure{Kind: domain.WalletEmpty, Remaining: balance}
		}
		if balance-quota < 0 {
			return &domain.Failure{Kind: domain.WalletBelowEstimate, Remaining: balance, Required: quota}
		}
		r.UserQuota = balance
	} else if quota <= 0 {
		quota = 1
	}
	r.Trusted = domain.ShouldTrust(input.Account, source, input.Force, r.UserQuota, input.TokenQuota, u.trustQuota())
	if r.Trusted {
		quota = 0
	}
	if quota > 0 {
		if err := domain.ReserveToken(u.store, input.Account, quota); err != nil {
			return &domain.Failure{Kind: domain.TokenReserveFailed, Cause: err}
		}
		r.TokenConsumed = quota
	}
	var fundingErr error
	if source == domain.Wallet {
		if quota > 0 {
			reserved, err := u.store.ReserveWallet(input.Account.UserID, quota)
			fundingErr = err
			if err == nil && !reserved {
				fundingErr = &domain.Failure{Kind: domain.WalletEmpty}
			}
			if reserved && err == nil {
				r.WalletConsumed = quota
			}
		}
	} else {
		r.Subscription, fundingErr = u.store.ReserveSubscription(input.RequestID, input.Account.UserID, input.ModelName, int64(quota))
	}
	if fundingErr != nil {
		if r.TokenConsumed > 0 && !input.Account.Playground {
			if err := u.store.CreditToken(input.Account, r.TokenConsumed); err != nil {
				u.log(fmt.Sprintf("error rolling back token quota (userId=%d, tokenId=%d, amount=%d, fundingErr=%s): %s", input.Account.UserID, input.Account.TokenID, r.TokenConsumed, fundingErr, err))
			}
			r.TokenConsumed = 0
		}
		if domain.IsInsufficient(fundingErr) {
			balance, err := u.store.WalletBalance(input.Account.UserID)
			if err != nil {
				balance = 0
			}
			return &domain.Failure{Kind: domain.WalletEmpty, Remaining: balance}
		}
		message := fundingErr.Error()
		if strings.Contains(message, "no active subscription") || strings.Contains(message, "subscription quota insufficient") {
			return &domain.Failure{Kind: domain.SubscriptionUnavailable, Cause: fundingErr}
		}
		return &domain.Failure{Kind: domain.UpdateFailed, Cause: fundingErr}
	}
	r.PreConsumed = quota
	return nil
}
