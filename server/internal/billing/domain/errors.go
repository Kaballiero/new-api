package domain

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	QueryFailed             ErrorKind = "query_failed"
	UpdateFailed            ErrorKind = "update_failed"
	WalletEmpty             ErrorKind = "wallet_empty"
	WalletBelowEstimate     ErrorKind = "wallet_below_estimate"
	WalletReserveRejected   ErrorKind = "wallet_reserve_rejected"
	SubscriptionUnavailable ErrorKind = "subscription_unavailable"
	TokenReserveFailed      ErrorKind = "token_reserve_failed"
	TokenInsufficient       ErrorKind = "token_insufficient"
	NegativeQuota           ErrorKind = "negative_quota"
)

type Failure struct {
	Kind                ErrorKind
	Cause               error
	Remaining, Required int
}

func (e *Failure) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return fmt.Sprintf("billing %s", e.Kind)
}
func (e *Failure) Unwrap() error { return e.Cause }

func IsInsufficient(err error) bool {
	var failure *Failure
	return errors.As(err, &failure) && (failure.Kind == WalletEmpty || failure.Kind == WalletBelowEstimate || failure.Kind == SubscriptionUnavailable)
}
