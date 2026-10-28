package pricing

import (
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Option struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

type OptionsEnvironment struct {
	DB        func() *gorm.DB
	KeyColumn func() string
	LockRows  func(*gorm.DB) *gorm.DB
	Lock      func() func()
	Publish   func(string, string) error
}
type Options struct {
	env   OptionsEnvironment
	apply func(*gorm.DB, map[string]map[string]any) error
}

func NewOptions(env OptionsEnvironment) Options { return Options{env: env} }
func (s Options) WithNativeMutation(apply func(*gorm.DB, map[string]map[string]any) error) Options {
	s.apply = apply
	return s
}
func (s Options) Lock() func() { return s.env.Lock() }
func (s Options) ReadRows(keys []string) ([]domain.OptionRow, error) {
	return readOptions(s.env.DB(), s.env.KeyColumn(), keys)
}
func readOptions(db *gorm.DB, key string, keys []string) ([]domain.OptionRow, error) {
	var rows []Option
	if err := db.Where(key+" IN ?", keys).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.OptionRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.OptionRow{Key: r.Key, Value: r.Value})
	}
	return out, nil
}
func (s Options) Transaction(work func(domain.OptionTransaction) error) error {
	return s.env.DB().Transaction(func(tx *gorm.DB) error { return work(optionTransaction{tx: tx, store: s}) })
}
func (s Options) Publish(key, value string) error { return s.env.Publish(key, value) }

type optionTransaction struct {
	tx    *gorm.DB
	store Options
}

func (t optionTransaction) ReadRows(keys []string) ([]domain.OptionRow, error) {
	return readOptions(t.store.env.LockRows(t.tx), t.store.env.KeyColumn(), keys)
}
func (t optionTransaction) Ensure(key, value string) error {
	return t.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: key, Value: value}).Error
}
func (t optionTransaction) Save(key, value string) error {
	return t.tx.Model(&Option{}).Where(t.store.env.KeyColumn()+" = ?", key).Update("value", value).Error
}
func (t optionTransaction) Apply(values map[string]map[string]any) error {
	return t.store.apply(t.tx, values)
}
