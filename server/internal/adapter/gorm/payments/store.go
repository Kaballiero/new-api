package payments

import (
	"errors"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/payments/record"
	"github.com/QuantumNous/new-api/internal/adapter/wallet"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/model"
)

type Store struct{ db func() *gorm.DB }

func NewStore(db func() *gorm.DB) Store { return Store{db: db} }

func toOrder(row record.TopUp) domain.Order {
	return domain.Order{
		ID:              row.Id,
		UserID:          row.UserId,
		Amount:          row.Amount,
		Money:           row.Money,
		TradeNo:         row.TradeNo,
		PaymentMethod:   row.PaymentMethod,
		PaymentProvider: row.PaymentProvider,
		CreateTime:      row.CreateTime,
		CompleteTime:    row.CompleteTime,
		Status:          row.Status,
	}
}

func fromOrder(o domain.Order) record.TopUp {
	return record.TopUp{
		Id:              o.ID,
		UserId:          o.UserID,
		Amount:          o.Amount,
		Money:           o.Money,
		TradeNo:         o.TradeNo,
		PaymentMethod:   o.PaymentMethod,
		PaymentProvider: o.PaymentProvider,
		CreateTime:      o.CreateTime,
		CompleteTime:    o.CompleteTime,
		Status:          o.Status,
	}
}

func (s Store) Insert(order *domain.Order) error {
	row := fromOrder(*order)
	err := s.db().Create(&row).Error
	*order = toOrder(row)
	return err
}

func (s Store) Save(order *domain.Order) error {
	row := fromOrder(*order)
	err := s.db().Save(&row).Error
	*order = toOrder(row)
	return err
}

func (s Store) Find(tradeNo string) *domain.Order {
	var row *record.TopUp
	if s.db().Where("trade_no = ?", tradeNo).First(&row).Error != nil {
		return nil
	}
	o := toOrder(*row)
	return &o
}

func (s Store) Transaction(run func(tx domain.Tx) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(orderTx{db: tx}) })
}

type orderTx struct{ db *gorm.DB }

func (t orderTx) FindLocked(tradeNo string) (domain.Order, error) {
	column := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		column = `"trade_no"`
	}
	var row record.TopUp
	err := locking.LockForUpdate(t.db).Where(column+" = ?", tradeNo).First(&row).Error
	return toOrder(row), err
}

func (t orderTx) Save(order *domain.Order) error {
	return NewStore(func() *gorm.DB { return t.db }).Save(order)
}

func (t orderTx) Email(userID int) (string, error) {
	var user model.User
	err := t.db.Where("id = ?", userID).First(&user).Error
	return user.Email, err
}

func (t orderTx) Credit(userID, quota int, profile domain.CreditProfile) error {
	var updates map[string]any
	if profile.StripeCustomer != nil || profile.Email != nil {
		updates = map[string]any{}
		if profile.StripeCustomer != nil {
			updates["stripe_customer"] = *profile.StripeCustomer
		}
		if profile.Email != nil {
			updates["email"] = *profile.Email
		}
	}
	return walletError(wallet.Credit(t.db, userID, quota, updates))
}

func walletError(err error) error {
	switch {
	case errors.Is(err, wallet.ErrInvalidQuota):
		return domain.ErrInvalidQuota
	case errors.Is(err, wallet.ErrQuotaLimit):
		return domain.ErrWalletLimit
	default:
		return err
	}
}
