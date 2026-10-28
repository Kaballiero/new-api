package domain

import "strings"

const (
	Wallet       = "wallet"
	Subscription = "subscription"
)

type Account struct {
	UserID, TokenID       int
	TokenKey              string
	Unlimited, Playground bool
}

type SubscriptionHold struct {
	ID                                        int
	PreConsumed, AmountTotal, AmountUsedAfter int64
	PlanID                                    int
	PlanTitle                                 string
}

type Outcome struct {
	TargetQuota         int    `json:"target_quota"`
	ReservedQuota       int    `json:"reserved_quota"`
	FundingAppliedQuota int    `json:"funding_applied_quota"`
	TokenAppliedQuota   int    `json:"token_applied_quota"`
	SettlementStatus    string `json:"settlement_status"`
	FailureStage        string `json:"failure_stage,omitempty"`
}

// Reservation is request-local. Its caller serializes lifecycle operations.
// Monetary values retain their original quota-unit integer representation.
type Reservation struct {
	Account                                                       Account
	RequestID, ModelName, Source                                  string
	UserQuota                                                     int
	WalletConsumed                                                int
	Subscription                                                  SubscriptionHold
	SubscriptionPostDelta                                         int64
	PreConsumed, TokenConsumed, ExtraReserved                     int
	Trusted, FundingSettled, WssFundingSettled, Settled, Refunded bool
	WssFundingDirect, WssTokenDirect                              int
	Outcome                                                       Outcome
}

func (r *Reservation) NeedsRefund() bool {
	return !r.Settled && !r.Refunded && !r.FundingSettled &&
		(r.TokenConsumed > 0 || r.Source == Subscription && r.Subscription.PreConsumed > 0)
}

func NormalizePreference(preference string) string {
	switch preference = strings.TrimSpace(preference); preference {
	case "wallet_only", "subscription_only", "wallet_first", "subscription_first":
		return preference
	default:
		return "subscription_first"
	}
}

func ShouldTrust(account Account, source string, force bool, userQuota, tokenQuota, threshold int) bool {
	return !force && threshold > 0 && source == Wallet &&
		(account.Unlimited || tokenQuota > threshold) && userQuota > threshold
}

// TokenReserver is shared by initial admission and additional reservation.
// Storage owns the atomic balance check, including quota held by Batch.
type TokenReserver interface {
	ReserveToken(Account, int) (bool, error)
	TokenRemaining(string) int
}

func ReserveToken(store TokenReserver, account Account, amount int) error {
	if amount < 0 {
		return &Failure{Kind: NegativeQuota}
	}
	if account.Playground {
		return nil
	}
	reserved, err := store.ReserveToken(account, amount)
	if err != nil {
		return err
	}
	if !reserved {
		return &Failure{Kind: TokenInsufficient, Remaining: store.TokenRemaining(account.TokenKey), Required: amount}
	}
	return nil
}
