package adjust_quota

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/shopspring/decimal"
)

type QuotaAdjustmentStore interface {
	Transaction(func(QuotaAdjustmentTx) error) error
}

// QuotaAdjustmentTx reports a missing account as domain.ErrAccountNotFound.
type QuotaAdjustmentTx interface {
	LockAccount(int) (QuotaAccount, error)
	UpdateQuota(userID, after int) error
}
type QuotaCache interface {
	Increment(userID int, delta int64) error
}
type QuotaCacheFailureLog interface{ AdjustmentCacheFailed(userID int, cause error) }

// QuotaMath converts a wallet amount using the existing strict quota policy.
type QuotaMath func(decimal.Decimal) (int, error)

type Usecase struct {
	store          QuotaAdjustmentStore
	cache          QuotaCache
	failureLog     QuotaCacheFailureLog
	rootRole       int
	maxWalletQuota int
	math           QuotaMath
}

func (scenario Usecase) Execute(userID, operatorRole int, mode string, value int) (*UserQuotaAdjustment, error) {
	if userID <= 0 || (mode != "add" && mode != "subtract" && mode != "override") || (mode != "override" && value <= 0) {
		if (mode == "add" || mode == "subtract") && value <= 0 {
			return nil, domain.ErrQuotaChangeZero
		}
		return nil, domain.ErrQuotaAdjustmentInvalid
	}
	if value > scenario.maxWalletQuota || value < -scenario.maxWalletQuota {
		return nil, domain.ErrQuotaLimitExceeded
	}

	var adjustment UserQuotaAdjustment
	// rejection keeps a business error of the transaction apart from storage
	// failures, which are classified as domain.Internal.
	var rejection error
	err := scenario.store.Transaction(func(tx QuotaAdjustmentTx) error {
		user, err := tx.LockAccount(userID)
		if err != nil {
			return err
		}
		if operatorRole != scenario.rootRole && operatorRole <= user.Role {
			rejection = domain.ErrManageForbidden
			return rejection
		}
		if user.Quota > scenario.maxWalletQuota || user.Quota < -scenario.maxWalletQuota {
			rejection = domain.ErrQuotaLimitExceeded
			return rejection
		}
		quota := decimal.NewFromInt(int64(value))
		switch mode {
		case "add":
			quota = decimal.NewFromInt(int64(user.Quota)).Add(quota)
		case "subtract":
			quota = decimal.NewFromInt(int64(user.Quota)).Sub(quota)
		}
		after, err := scenario.math(quota)
		if err != nil {
			rejection = domain.ErrQuotaLimitExceeded
			return rejection
		}
		// An unchanged value is a success, also on MySQL where RowsAffected may
		// count only rows that actually changed.
		if after != user.Quota {
			if err := tx.UpdateQuota(userID, after); err != nil {
				return err
			}
		}
		adjustment = UserQuotaAdjustment{UserID: user.ID, Username: user.Username, Role: user.Role, Status: user.Status, Before: user.Quota, After: after}
		return nil
	})
	switch {
	case rejection != nil:
		return nil, rejection
	case errors.Is(err, domain.ErrAccountNotFound):
		return nil, domain.ErrAccountNotFound
	case err != nil:
		return nil, domain.Internal(err)
	}

	// Apply only the committed difference so that pending reservations stay.
	// Both balances are bounded, so the difference fits into int64.
	delta := int64(adjustment.After) - int64(adjustment.Before)
	if delta != 0 {
		if err := scenario.cache.Increment(userID, delta); err != nil {
			scenario.failureLog.AdjustmentCacheFailed(userID, err)
		}
	}
	return &adjustment, nil
}

func NewUsecase(store QuotaAdjustmentStore, cache QuotaCache, failureLog QuotaCacheFailureLog, rootRole, maxWalletQuota int, math QuotaMath) Usecase {
	return Usecase{store: store, cache: cache, failureLog: failureLog, rootRole: rootRole, maxWalletQuota: maxWalletQuota, math: math}
}
