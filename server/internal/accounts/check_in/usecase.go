package check_in

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"math/rand"
	"time"
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
		return nil, errors.New("签到功能未启用")
	}
	today := time.Now().Format("2006-01-02")
	checked, err := u.store.CheckedInOn(userID, today)
	if err != nil {
		return nil, err
	}
	if checked {
		return nil, errors.New("今日已签到")
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
			return nil, errors.New("签到失败，请稍后重试")
		}
		if err := u.store.Credit(userID, quota); err != nil {
			_ = u.store.Delete(record)
			return nil, errors.New("签到失败：更新额度出错")
		}
		return record, nil
	}
	if err := u.store.Transaction(func(tx Mutation) error {
		if err := tx.Create(record); err != nil {
			return errors.New("签到失败，请稍后重试")
		}
		if err := tx.Credit(userID, quota); err != nil {
			return errors.New("签到失败：更新额度出错")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	go func() { _ = u.store.IncreaseCache(userID, int64(quota)) }()
	return record, nil
}
