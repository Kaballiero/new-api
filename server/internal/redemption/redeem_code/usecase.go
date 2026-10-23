package redeem_code

import "github.com/QuantumNous/new-api/internal/redemption/domain"

// Tx locks and consumes a code and credits its beneficiary within the single redemption transaction.
type Tx interface {
	FindLocked(key string) (domain.Code, error)
	MarkUsed(redemption domain.Redemption) (bool, error)
	CreditWallet(userID int, quota int) error
}

// Store commits or rolls back the complete redemption mutation as one transaction.
type Store interface {
	Transaction(run func(tx Tx) error) error
}

// Receipt records the beneficiary credit outside the transaction, after commit and cache sync.
type Receipt interface {
	Record(userID int, code domain.Code)
}

type Input struct {
	Key    string
	UserID int
}

type Timing struct {
	// Now reads the expiry and mutation timestamps inside the transaction.
	Now func() int64
	// Wait preserves the admission delay before opening the transaction.
	Wait func()
}

type Usecase struct {
	store   Store
	receipt Receipt
	// cache synchronizes the credited quota after commit, before the receipt.
	cache func(userID int, quota int)
	// failed logs a transaction failure after rollback or an unsuccessful commit.
	failed func(err error)
	timing Timing
}

func NewUsecase(store Store, receipt Receipt, cache func(userID int, quota int), failed func(err error), timing Timing) Usecase {
	return Usecase{store: store, receipt: receipt, cache: cache, failed: failed, timing: timing}
}

func (u Usecase) Execute(in Input) (int, error) {
	if in.Key == "" {
		return 0, domain.ErrMissingKey
	}
	if in.UserID == 0 {
		return 0, domain.ErrUserID
	}
	u.timing.Wait()
	var code domain.Code
	err := u.store.Transaction(func(tx Tx) error {
		var err error
		code, err = tx.FindLocked(in.Key)
		if err != nil {
			return domain.ErrInvalidCode
		}
		if code.Status != domain.StatusEnabled {
			return domain.ErrUsed
		}
		if code.ExpiredTime != 0 && code.ExpiredTime < u.timing.Now() {
			return domain.ErrExpired
		}
		changed, err := tx.MarkUsed(domain.Redemption{ID: code.ID, UserID: in.UserID, FromStatus: domain.StatusEnabled,
			ToStatus: domain.StatusUsed, RedeemedTime: u.timing.Now()})
		if err != nil {
			return err
		}
		if !changed {
			return domain.ErrUsed
		}
		return tx.CreditWallet(in.UserID, code.Quota)
	})
	if err != nil {
		u.failed(err)
		return 0, domain.ErrRedeemFailed
	}
	u.cache(in.UserID, code.Quota)
	u.receipt.Record(in.UserID, code)
	return code.Quota, nil
}
