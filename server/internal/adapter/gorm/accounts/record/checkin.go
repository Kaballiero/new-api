package model

import (
	"github.com/QuantumNous/new-api/common"
	checkincommand "github.com/QuantumNous/new-api/internal/accounts/check_in"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"gorm.io/gorm"
)

type CheckinStore struct {
	db     func() *gorm.DB
	credit func(int, int, bool) error
	cache  func(int, int64) error
}

// GetUserCheckinRecords возвращает отметки пользователя за указанный диапазон дат.
func (s CheckinStore) Records(userId int, startDate, endDate string) ([]domain.Checkin, error) {
	var records []Checkin
	err := s.db().Where("user_id = ? AND checkin_date >= ? AND checkin_date <= ?",
		userId, startDate, endDate).
		Order("checkin_date DESC").
		Find(&records).Error
	var values []domain.Checkin
	if records != nil {
		values = make([]domain.Checkin, len(records))
		for i, record := range records {
			values[i] = domain.Checkin(record)
		}
	}
	return values, err
}

// HasCheckedInToday проверяет, отмечался ли пользователь сегодня.
func (s CheckinStore) CheckedInOn(userId int, today string) (bool, error) {
	var count int64
	err := s.db().Model(&Checkin{}).
		Where("user_id = ? AND checkin_date = ?", userId, today).
		Count(&count).Error
	return count > 0, err
}

// Persist сохраняет прежний контракт адаптера через сценарий ежедневной отметки.
func (store CheckinStore) Persist(value *domain.Checkin, userID, quota int) (*domain.Checkin, error) {
	return checkincommand.NewUsecase(store, nil).Persist(value, userID, quota)
}
func (s CheckinStore) IsSQLite() bool { return common.UsingMainDatabase(common.DatabaseTypeSQLite) }
func (s CheckinStore) Create(record *domain.Checkin) error {
	return s.db().Create((*Checkin)(record)).Error
}
func (s CheckinStore) Credit(userID, quota int) error {
	return s.credit(userID, quota, true)
}
func (s CheckinStore) Delete(record *domain.Checkin) error {
	return s.db().Delete((*Checkin)(record)).Error
}
func (s CheckinStore) Transaction(run func(checkincommand.Mutation) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(checkinMutation{tx: tx}) })
}
func (s CheckinStore) IncreaseCache(userID int, quota int64) error {
	return s.cache(userID, quota)
}

type checkinMutation struct{ tx *gorm.DB }

func (m checkinMutation) Create(record *domain.Checkin) error {
	return m.tx.Create((*Checkin)(record)).Error
}
func (m checkinMutation) Credit(userID, quota int) error {
	return m.tx.Model(&User{}).Where("id = ?", userID).Update("quota", gorm.Expr("quota + ?", quota)).Error
}

func (s CheckinStore) Totals(userId int) (totalCheckins int64, totalQuota int64) {
	s.db().Model(&Checkin{}).Where("user_id = ?", userId).Count(&totalCheckins)
	s.db().Model(&Checkin{}).Where("user_id = ?", userId).Select("COALESCE(SUM(quota_awarded), 0)").Scan(&totalQuota)
	return totalCheckins, totalQuota
}

func NewCheckinStore(db func() *gorm.DB, credit func(int, int, bool) error, cache func(int, int64) error) CheckinStore {
	return CheckinStore{db: db, credit: credit, cache: cache}
}

type Checkin struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId       int    `json:"user_id" gorm:"not null;uniqueIndex:idx_user_checkin_date"`
	CheckinDate  string `json:"checkin_date" gorm:"type:varchar(10);not null;uniqueIndex:idx_user_checkin_date"` // Format: YYYY-MM-DD
	QuotaAwarded int    `json:"quota_awarded" gorm:"not null"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
}

func (Checkin) TableName() string { return "checkins" }
