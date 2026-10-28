package check_in

import (
	"math/rand"
	"time"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type Mutation interface {
	Create(*domain.Checkin) error
	Credit(userID, quota int) error
}
type Store interface {
	Mutation
	CheckedInOn(userID int, date string) (bool, error)
	IsSQLite() bool
	Delete(*domain.Checkin) error
	Transaction(func(Mutation) error) error
	IncreaseCache(userID int, quota int64) error
}
type Usecase struct {
	store    Store
	settings func() *domain.CheckinPolicy
}

func NewUsecase(store Store, settings func() *domain.CheckinPolicy) Usecase {
	return Usecase{store: store, settings: settings}
}
func (u Usecase) Execute(userID int) (*domain.Checkin, error) {
	setting := u.settings()
	if !setting.Enabled {
		return nil, domain.ErrCheckinDisabled
	}
	today := time.Now().Format("2006-01-02")
	checked, err := u.store.CheckedInOn(userID, today)
	if err != nil {
		return nil, err
	}
	if checked {
		return nil, domain.ErrAlreadyCheckedIn
	}
	quota := setting.MinQuota
	if setting.MaxQuota > setting.MinQuota {
		quota = setting.MinQuota + rand.Intn(setting.MaxQuota-setting.MinQuota+1)
	}
	today = time.Now().Format("2006-01-02")
	record := &domain.Checkin{UserId: userID, CheckinDate: today, QuotaAwarded: quota, CreatedAt: time.Now().Unix()}
	return u.Persist(record, userID, quota)
}
func (u Usecase) Persist(record *domain.Checkin, userID, quota int) (*domain.Checkin, error) {
	if u.store.IsSQLite() {
		if err := u.store.Create(record); err != nil {
			return nil, domain.ErrCheckinFailed
		}
		if err := u.store.Credit(userID, quota); err != nil {
			_ = u.store.Delete(record)
			return nil, domain.ErrCheckinQuotaFailed
		}
		return record, nil
	}
	if err := u.store.Transaction(func(tx Mutation) error {
		if err := tx.Create(record); err != nil {
			return domain.ErrCheckinFailed
		}
		if err := tx.Credit(userID, quota); err != nil {
			return domain.ErrCheckinQuotaFailed
		}
		return nil
	}); err != nil {
		return nil, err
	}
	go func() { _ = u.store.IncreaseCache(userID, int64(quota)) }()
	return record, nil
}
