package create_codes

import (
	"unicode/utf8"

	"github.com/QuantumNous/new-api/internal/redemption/domain"
)

// Store inserts each code independently, without a shared batch transaction.
type Store interface {
	Insert(code domain.Code) error
}

type Input struct {
	IssuerID     int
	Name         string
	Count, Quota int
	ExpiredTime  int64
}

type Result struct {
	Keys []string
}

type Usecase struct {
	store Store
	// wallet validates the shared wallet limit before any insert.
	wallet func(quota int) error
	// now reads the clock for expiry validation and each independent insert.
	now func() int64
	// key generates a credential before its independent insert.
	key func() string
}

func NewUsecase(store Store, wallet func(quota int) error, now func() int64, key func() string) Usecase {
	return Usecase{store: store, wallet: wallet, now: now, key: key}
}

func (u Usecase) Execute(in Input) (Result, error) {
	if n := utf8.RuneCountInString(in.Name); n == 0 || n > 20 {
		return Result{}, domain.ErrNameLength
	}
	if in.Count <= 0 {
		return Result{}, domain.ErrCountPositive
	}
	if in.Count > 100 {
		return Result{}, domain.ErrCountMax
	}
	if err := domain.ValidateQuota(in.Quota, u.wallet); err != nil {
		return Result{}, err
	}
	if in.ExpiredTime != 0 && in.ExpiredTime < u.now() {
		return Result{}, domain.ErrExpiry
	}
	var result Result
	for range in.Count {
		key := u.key()
		code := domain.Code{IssuerID: in.IssuerID, Name: in.Name, Key: key, CreatedTime: u.now(), Quota: in.Quota, ExpiredTime: in.ExpiredTime}
		if err := u.store.Insert(code); err != nil {
			return result, domain.CreateError{Cause: err}
		}
		result.Keys = append(result.Keys, key)
	}
	return result, nil
}
