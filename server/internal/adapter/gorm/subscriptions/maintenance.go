package subscriptions

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/subscriptions/record"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

func (s Store) ExpiredCandidates(limit int, now int64) ([]domain.UserSubscription, error) {
	var rows []record.UserSubscription
	err := s.db().Where("status = ? AND end_time > 0 AND end_time <= ?", "active", now).Order("end_time asc, id asc").Limit(limit).Find(&rows).Error
	result := make([]domain.UserSubscription, len(rows))
	for i := range rows {
		result[i] = domain.UserSubscription(rows[i])
	}
	return result, err
}
func (s Store) DueCandidates(limit int, now int64) ([]domain.UserSubscription, error) {
	var rows []record.UserSubscription
	err := s.db().Where("next_reset_time > 0 AND next_reset_time <= ? AND status = ?", now, "active").Order("next_reset_time asc").Limit(limit).Find(&rows).Error
	result := make([]domain.UserSubscription, len(rows))
	for i := range rows {
		result[i] = domain.UserSubscription(rows[i])
	}
	return result, err
}
func (s Store) ResetPlan(planID int) (*domain.SubscriptionPlan, error) {
	row, err := s.plans.GetSubscriptionPlanByIdTx(nil, planID)
	if err != nil || row == nil {
		return nil, err
	}
	v := domain.SubscriptionPlan(*row)
	return &v, nil
}
func (s Store) Cleanup(cutoff int64) (int64, error) {
	res := s.db().Where("updated_at < ?", cutoff).Delete(&record.SubscriptionPreConsumeRecord{})
	return res.RowsAffected, res.Error
}
