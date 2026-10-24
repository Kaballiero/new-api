package subscriptions

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/subscriptions/record"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type Store struct{ db func() *gorm.DB }

func NewStore(db func() *gorm.DB) Store { return Store{db: db} }
func (s Store) ListPlans(enabledOnly bool) ([]domain.SubscriptionPlan, error) {
	var rows []record.SubscriptionPlan
	query := s.db()
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Order("sort_order desc, id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.SubscriptionPlan, 0, len(rows))
	for _, row := range rows {
		result = append(result, domain.SubscriptionPlan(row))
	}
	return result, nil
}
func (s Store) ListSubscriptions(userID int, activeOnly bool, now int64) ([]domain.UserSubscription, error) {
	if userID <= 0 {
		return nil, errors.New("invalid userId")
	}
	var rows []record.UserSubscription
	query := s.db().Where("user_id = ?", userID)
	if activeOnly {
		query = s.db().Where("user_id = ? AND status = ? AND end_time > ?", userID, "active", now)
	}
	if err := query.Order("end_time desc, id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.UserSubscription, 0, len(rows))
	for _, row := range rows {
		result = append(result, domain.UserSubscription(row))
	}
	return result, nil
}
func (s Store) BillingPreference(userID int) (string, error) {
	settings, err := model.GetUserSetting(userID, false)
	return settings.BillingPreference, err
}
func (s Store) PutBillingPreference(userID int, preference string) error {
	user, err := model.GetUserById(userID, true)
	if err != nil {
		return err
	}
	current := user.GetSetting()
	current.BillingPreference = preference
	return model.UpdateUserSetting(user.Id, current)
}
