package subscriptions

import (
	"errors"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/subscriptions/record"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/model"
)

type transaction struct {
	db    *gorm.DB
	store Store
}

func (s Store) Bind(run func(domain.BindTx) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(transaction{db: tx, store: s}) })
}

func (s Store) Balance(run func(domain.BalanceTx) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(transaction{db: tx, store: s}) })
}

func (s Store) Expire(run func(domain.ExpireTx) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(transaction{db: tx, store: s}) })
}

func (s Store) Change(run func(domain.ChangeTx) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(transaction{db: tx, store: s}) })
}

func (s Store) Reset(run func(domain.ResetTx) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(transaction{db: tx, store: s}) })
}

func (s Store) ExpireUser(run func(domain.ExpiryTx) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(transaction{db: tx, store: s}) })
}

func (s Store) ResetDue(run func(domain.DueResetTx) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(transaction{db: tx, store: s}) })
}

func (s Store) PlanByID(planID int) (*domain.SubscriptionPlan, error) {
	row, err := s.plans.GetSubscriptionPlanById(planID)
	if err != nil {
		return nil, err
	}
	value := domain.SubscriptionPlan(*row)
	return &value, nil
}

func (t transaction) PlanByID(planID int) (*domain.SubscriptionPlan, error) {
	row, err := t.store.plans.GetSubscriptionPlanByIdTx(t.db, planID)
	if err != nil {
		return nil, err
	}
	value := domain.SubscriptionPlan(*row)
	return &value, nil
}

func (t transaction) DatabaseTime() int64 { return t.store.clock() }

func (t transaction) CountPurchases(userID int, planID int) (int64, error) {
	var count int64
	err := t.db.Model(&record.UserSubscription{}).Where("user_id = ? AND plan_id = ?", userID, planID).Count(&count).Error
	return count, err
}

func (t transaction) LockUserID(userID int) error {
	var user model.User
	return locking.LockForUpdate(t.db).Select("id").Where("id = ?", userID).First(&user).Error
}

func (t transaction) UserGroup(userID int) (string, error) {
	if userID <= 0 {
		return "", errors.New("invalid userId")
	}
	var group string
	// GORM resolves the schema field and quotes the reserved name for the current dialect.
	err := locking.LockForUpdate(t.db).Model(&model.User{}).Where("id = ?", userID).Select("group").Find(&group).Error
	return group, err
}

func (t transaction) SetUserGroup(userID int, group string) error {
	return t.db.Model(&model.User{}).Where("id = ?", userID).Update("group", group).Error
}

func (t transaction) InsertSubscription(subscription *domain.UserSubscription) error {
	row := record.UserSubscription(*subscription)
	err := t.db.Create(&row).Error
	*subscription = domain.UserSubscription(row)
	return err
}

func (t transaction) SaveSubscription(subscription *domain.UserSubscription) error {
	row := record.UserSubscription(*subscription)
	err := t.db.Save(&row).Error
	*subscription = domain.UserSubscription(row)
	return err
}

func (t transaction) ActiveUpgrade(userID int, now int64, excludeID int) (bool, error) {
	var row record.UserSubscription
	query := t.db.Where("user_id = ? AND status = ? AND end_time > ? AND upgrade_group <> ''", userID, "active", now)
	if excludeID != 0 {
		query = t.db.Where("user_id = ? AND status = ? AND end_time > ? AND id <> ? AND upgrade_group <> ''",
			userID, "active", now, excludeID)
	}
	result := query.Order("end_time desc, id desc").Limit(1).Find(&row)
	return result.RowsAffected > 0, result.Error
}

func (t transaction) WalletBalance(userID int) (int, error) {
	var user model.User
	if err := locking.LockForUpdate(t.db).Where("id = ?", userID).First(&user).Error; err != nil {
		return 0, err
	}
	return user.Quota, nil
}

func (t transaction) DebitWallet(userID int, quota int) error {
	return t.db.Model(&model.User{}).Where("id = ?", userID).Update("quota", gorm.Expr("quota - ?", quota)).Error
}

func (t transaction) InsertOrder(order *domain.SubscriptionOrder) error {
	row := record.SubscriptionOrder(*order)
	err := t.db.Create(&row).Error
	*order = domain.SubscriptionOrder(row)
	return err
}

func (t transaction) SaveOrder(order *domain.SubscriptionOrder) error {
	row := record.SubscriptionOrder(*order)
	err := t.db.Save(&row).Error
	*order = domain.SubscriptionOrder(row)
	return err
}

func (t transaction) LockOrder(tradeNo string) (*domain.SubscriptionOrder, error) {
	column := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		column = `"trade_no"`
	}
	var row record.SubscriptionOrder
	if err := locking.LockForUpdate(t.db).Where(column+" = ?", tradeNo).First(&row).Error; err != nil {
		return nil, err
	}
	value := domain.SubscriptionOrder(row)
	return &value, nil
}

func (t transaction) LockSubscription(subscriptionID int) (*domain.UserSubscription, error) {
	var row record.UserSubscription
	if err := locking.LockForUpdate(t.db).Where("id = ?", subscriptionID).First(&row).Error; err != nil {
		return nil, err
	}
	value := domain.UserSubscription(row)
	return &value, nil
}

func (t transaction) CancelSubscription(subscriptionID int, now int64) error {
	row := record.UserSubscription{Id: subscriptionID}
	return t.db.Model(&row).Updates(map[string]any{"status": "cancelled", "end_time": now, "updated_at": now}).Error
}

func (t transaction) DeleteSubscription(subscriptionID int) error {
	return t.db.Where("id = ?", subscriptionID).Delete(&record.UserSubscription{}).Error
}

func (t transaction) ResetCandidates(userID int, planID int, now int64) ([]domain.UserSubscription, error) {
	query := locking.LockForUpdate(t.db).
		Where("plan_id = ? AND status = ? AND end_time > ?", planID, "active", now).
		Order("user_id asc, end_time asc, id asc")
	if userID > 0 {
		query = locking.LockForUpdate(t.db).
			Where("user_id = ? AND plan_id = ? AND status = ? AND end_time > ?", userID, planID, "active", now).
			Order("end_time asc, id asc")
	}
	var rows []record.UserSubscription
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.UserSubscription, len(rows))
	for i := range rows {
		result[i] = domain.UserSubscription(rows[i])
	}
	return result, nil
}

func (t transaction) ExpireUser(userID int, now int64) (int, error) {
	result := t.db.Model(&record.UserSubscription{}).
		Where("user_id = ? AND status = ? AND end_time > 0 AND end_time <= ?", userID, "active", now).
		Updates(map[string]any{"status": "expired", "updated_at": common.GetTimestamp()})
	return int(result.RowsAffected), result.Error
}

func (t transaction) LastExpiredTransition(userID int) (*domain.UserSubscription, error) {
	var row record.UserSubscription
	result := t.db.Where("user_id = ? AND status = ? AND (downgrade_group <> '' OR upgrade_group <> '')", userID, "expired").
		Order("end_time desc, id desc").Limit(1).Find(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	value := domain.UserSubscription(row)
	return &value, nil
}

func (t transaction) LockDueSubscription(subscriptionID int, now int64) (*domain.UserSubscription, error) {
	var row record.UserSubscription
	err := locking.LockForUpdate(t.db).Where("id = ? AND next_reset_time > 0 AND next_reset_time <= ?", subscriptionID, now).First(&row).Error
	if err != nil {
		return nil, err
	}
	value := domain.UserSubscription(row)
	return &value, nil
}
