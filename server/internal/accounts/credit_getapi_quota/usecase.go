package credit_getapi_quota

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/shopspring/decimal"
	"unicode/utf8"
)

type Receipt struct{ UserID, Quota int }
type Transaction interface {
	Receipt(string) (*Receipt, error)
	Balance(userID int, replay bool) (int, error)
	InsertReceipt(userID int, key string, quota int, createdAt int64) error
	UpdateQuota(userID, quota int) error
}
type Store interface {
	CreditTransaction(func(Transaction) error) error
	RetryCredit(error) bool
}
type Usecase struct {
	store        Store
	maxQuota     int
	convert      func(decimal.Decimal) (int, error)
	now          func() int64
	creditCache  func(int, int64) error
	cacheFailure func(int, error)
}

func NewUsecase(store Store, maxQuota int, convert func(decimal.Decimal) (int, error), now func() int64,
	creditCache func(int, int64) error, cacheFailure func(int, error)) Usecase {
	return Usecase{store: store, maxQuota: maxQuota, convert: convert, now: now, creditCache: creditCache, cacheFailure: cacheFailure}
}
func (u Usecase) Execute(in Input) (*GetAPIQuotaCreditResult, error) {
	if in.UserID <= 0 || !utf8.ValidString(in.OperationKey) || utf8.RuneCountInString(in.OperationKey) < 1 || utf8.RuneCountInString(in.OperationKey) > 200 || in.Quota <= 0 {
		return nil, domain.ErrGetAPIInvalidRequest
	}
	if in.Quota > u.maxQuota {
		return nil, domain.ErrQuotaLimitExceeded
	}
	var result GetAPIQuotaCreditResult
	apply := func(tx Transaction) error {
		existing, err := tx.Receipt(in.OperationKey)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.UserID != in.UserID || existing.Quota != in.Quota {
				return domain.ErrGetAPIQuotaCreditConflict
			}
			balance, err := tx.Balance(in.UserID, true)
			if err != nil {
				return err
			}
			result = GetAPIQuotaCreditResult{AlreadyApplied: true, Quota: balance}
			return nil
		}
		balance, err := tx.Balance(in.UserID, false)
		if err != nil {
			return err
		}
		// Insert before capacity validation so a concurrent winner can be replayed
		// in a new transaction, including when it filled the wallet to its limit.
		if err := tx.InsertReceipt(in.UserID, in.OperationKey, in.Quota, u.now()); err != nil {
			return err
		}
		if balance > u.maxQuota || balance < -u.maxQuota {
			return domain.ErrQuotaLimitExceeded
		}
		after, err := u.convert(decimal.NewFromInt(int64(balance)).Add(decimal.NewFromInt(int64(in.Quota))))
		if err != nil {
			return domain.ErrQuotaLimitExceeded
		}
		if err := tx.UpdateQuota(in.UserID, after); err != nil {
			return err
		}
		result = GetAPIQuotaCreditResult{Applied: true, Quota: after}
		return nil
	}
	err := u.store.CreditTransaction(apply)
	if u.store.RetryCredit(err) {
		err = u.store.CreditTransaction(apply)
	}
	if err != nil {
		return nil, err
	}
	if result.Applied {
		if err := u.creditCache(in.UserID, int64(in.Quota)); err != nil {
			u.cacheFailure(in.UserID, err)
		}
	}
	return &result, nil
}
