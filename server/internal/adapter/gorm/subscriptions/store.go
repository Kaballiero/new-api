package subscriptions

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
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

func (s Store) InsertPlan(plan *domain.SubscriptionPlan) error {
	row := record.SubscriptionPlan(*plan)
	err := s.db().Create(&row).Error
	*plan = domain.SubscriptionPlan(row)
	return err
}
func (s Store) UpdatePlan(plan domain.SubscriptionPlan) error {
	return s.db().Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"title": plan.Title, "subtitle": plan.Subtitle, "price_amount": plan.PriceAmount, "currency": plan.Currency,
			"duration_unit": plan.DurationUnit, "duration_value": plan.DurationValue, "custom_seconds": plan.CustomSeconds,
			"enabled": plan.Enabled, "sort_order": plan.SortOrder, "stripe_price_id": plan.StripePriceId, "creem_product_id": plan.CreemProductId,
			"waffo_pancake_product_id": plan.WaffoPancakeProductId, "max_purchase_per_user": plan.MaxPurchasePerUser, "total_amount": plan.TotalAmount,
			"upgrade_group": plan.UpgradeGroup, "downgrade_group": plan.DowngradeGroup, "quota_reset_period": plan.QuotaResetPeriod,
			"quota_reset_custom_seconds": plan.QuotaResetCustomSeconds, "updated_at": common.GetTimestamp(),
		}
		if plan.AllowBalancePay != nil {
			updates["allow_balance_pay"] = *plan.AllowBalancePay
		}
		if plan.AllowWalletOverflow != nil {
			updates["allow_wallet_overflow"] = *plan.AllowWalletOverflow
		}
		return tx.Model(&record.SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(updates).Error
	})
}
func (s Store) SetPlanEnabled(planID int, enabled bool) error {
	return s.db().Model(&record.SubscriptionPlan{}).Where("id = ?", planID).Update("enabled", enabled).Error
}

func (s Store) CountPurchases(userID, planID int) (int64, error) {
	var count int64
	err := s.db().Model(&record.UserSubscription{}).Where("user_id = ? AND plan_id = ?", userID, planID).Count(&count).Error
	return count, err
}

func (s Store) UserByID(userID int) (*domain.CheckoutUser, error) {
	user, err := model.GetUserById(userID, false)
	if err != nil || user == nil {
		return nil, err
	}
	return &domain.CheckoutUser{ID: user.Id, Username: user.Username, Email: user.Email, StripeCustomer: user.StripeCustomer}, nil
}
func (s Store) InsertOrder(order *domain.SubscriptionOrder) error {
	row := record.SubscriptionOrder(*order)
	err := s.db().Create(&row).Error
	*order = domain.SubscriptionOrder(row)
	return err
}
func (s Store) SaveOrder(order *domain.SubscriptionOrder) error {
	row := record.SubscriptionOrder(*order)
	err := s.db().Save(&row).Error
	*order = domain.SubscriptionOrder(row)
	return err
}
