package domain

import "errors"

var (
	ErrIDEmpty       = errors.New("id is empty")
	ErrNameLength    = errors.New("invalid redemption name length")
	ErrCountPositive = errors.New("redemption count must be positive")
	ErrCountMax      = errors.New("redemption count exceeds maximum")
	ErrQuotaPositive = errors.New("redemption quota must be positive")
	ErrWalletQuota   = errors.New("wallet quota exceeds maximum")
	ErrExpiry        = errors.New("redemption expiry is in the past")
	ErrCreate        = errors.New("failed to create redemption")
	ErrBatchCount    = errors.New("select between 1 and 1000 redemption codes")
	ErrBatchID       = errors.New("redemption IDs must be positive")
	ErrMissingKey    = errors.New("redemption code not provided")
	ErrUserID        = errors.New("invalid user id")
	ErrInvalidCode   = errors.New("invalid redemption code")
	ErrUsed          = errors.New("redemption code has been used")
	ErrExpired       = errors.New("redemption code has expired")
	ErrRedeemFailed  = errors.New("redeem.failed")
)

// ValidateQuota keeps the positive-credit rule ahead of the shared wallet validator.
func ValidateQuota(quota int, wallet func(quota int) error) error {
	if quota <= 0 {
		return ErrQuotaPositive
	}
	return wallet(quota)
}

type CreateError struct {
	Cause error
}

func (e CreateError) Error() string {
	return e.Cause.Error()
}

func (e CreateError) Unwrap() error {
	return e.Cause
}

func (e CreateError) Is(target error) bool {
	return target == ErrCreate
}
