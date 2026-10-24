package subscriptions

import (
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/subscriptions/record"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

type OrderStore struct {
	Store
	history func(*gorm.DB, domain.SubscriptionOrder) error
}

func NewOrderStore(store Store, history func(*gorm.DB, domain.SubscriptionOrder) error) OrderStore {
	return OrderStore{store, history}
}

type orderTransaction struct {
	transaction
	history func(*gorm.DB, domain.SubscriptionOrder) error
}

func (t orderTransaction) UpsertHistory(order domain.SubscriptionOrder) error {
	return t.history(t.db, order)
}
func (s OrderStore) Complete(run func(domain.CompleteTx) error) error {
	return s.db().Transaction(func(tx *gorm.DB) error { return run(orderTransaction{transaction{tx}, s.history}) })
}
func (s Store) FindOrder(tradeNo string) *domain.SubscriptionOrder {
	var row *record.SubscriptionOrder
	if s.db().Where("trade_no = ?", tradeNo).First(&row).Error != nil {
		return nil
	}
	v := domain.SubscriptionOrder(*row)
	return &v
}
