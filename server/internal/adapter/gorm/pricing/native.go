package pricing

import (
	"context"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/publish_native_catalogue"
	"gorm.io/gorm"
)

type NativeEnvironment struct {
	DB       func() *gorm.DB
	LockRows func(*gorm.DB) *gorm.DB
	Lock     func() func()
	Snapshot func() *domain.PricingCatalogSnapshot
	Publish  func(string, string) error
}
type Native struct{ env NativeEnvironment }

func NewNative(env NativeEnvironment) Native              { return Native{env: env} }
func (s Native) Lock() func()                             { return s.env.Lock() }
func (s Native) Snapshot() *domain.PricingCatalogSnapshot { return s.env.Snapshot() }
func (s Native) Transaction(ctx context.Context, work func(publish_native_catalogue.Transaction) error) error {
	return s.env.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error { return work(&nativeTransaction{tx: tx, store: s}) })
}
func (s Native) PublishOption(value string) error {
	return s.env.Publish(domain.PricingCatalogOptionKey, value)
}

type nativeTransaction struct {
	tx     *gorm.DB
	store  Native
	option Option
}

func (t *nativeTransaction) ReadLockedOption() (string, error) {
	option := Option{Key: domain.PricingCatalogOptionKey}
	if err := t.tx.FirstOrCreate(&option, Option{Key: domain.PricingCatalogOptionKey}).Error; err != nil {
		return "", err
	}
	if err := t.tx.Model(&option).UpdateColumn("value", gorm.Expr("value")).Error; err != nil {
		return "", err
	}
	if err := t.store.env.LockRows(t.tx).First(&option, Option{Key: domain.PricingCatalogOptionKey}).Error; err != nil {
		return "", err
	}
	t.option = option
	return option.Value, nil
}
func (t nativeTransaction) LockChannel(id int) (*domain.BuildChannel, error) {
	result := t.tx.Model(&channelrecord.Channel{}).Where("id = ?", id).UpdateColumn("id", gorm.Expr("id"))
	if result.Error != nil {
		return nil, result.Error
	}
	channel := &channelrecord.Channel{}
	if err := t.store.env.LockRows(t.tx).First(channel, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return (*domain.BuildChannel)(channelrecord.ToDomain(channel)), nil
}
func (t *nativeTransaction) SaveOption(value string) error {
	return t.tx.Model(&t.option).Update("value", value).Error
}
