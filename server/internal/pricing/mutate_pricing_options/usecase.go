package mutate_pricing_options

import (
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"strings"
)

type Store interface {
	Lock() func()
	Transaction(func(domain.OptionTransaction) error) error
	Publish(string, string) error
}
type Usecase struct {
	deps                domain.Capabilities
	store               Store
	policy              *domain.Configuration
	log                 func(string)
	refresh, invalidate func()
}

func NewUsecase(deps domain.Capabilities, store Store, policy *domain.Configuration, log func(string), refresh, invalidate func()) Usecase {
	return Usecase{deps: deps, store: store, policy: policy, log: log, refresh: refresh, invalidate: invalidate}
}

func (u Usecase) Execute() error {
	unlock := u.store.Lock()
	defer unlock()
	var committed map[string]map[string]any
	err := u.store.Transaction(func(tx domain.OptionTransaction) error {
		rows, err := tx.ReadRows(u.policy.Keys())
		if err != nil {
			return err
		}
		values, existing, duplicated, err := u.policy.DecodeRows(rows)
		if err != nil {
			return err
		}
		if len(duplicated) > 0 {
			u.log("options table has duplicate pricing keys [" + strings.Join(duplicated, ", ") + "]; the table is missing a primary key")
		}
		defaults := u.policy.Defaults()
		for _, key := range u.policy.Keys() {
			if existing[key] {
				continue
			}
			encoded, err := u.deps.Codec.Marshal(defaults[key])
			if err != nil {
				return err
			}
			if err := tx.Ensure(key, string(encoded)); err != nil {
				return err
			}
		}
		if err := tx.Apply(values); err != nil {
			return err
		}
		for _, key := range u.policy.Keys() {
			encoded, err := u.deps.Codec.Marshal(values[key])
			if err != nil {
				return err
			}
			if err := tx.Save(key, string(encoded)); err != nil {
				return err
			}
		}
		committed = values
		return nil
	})
	if err != nil {
		return err
	}
	for _, key := range u.policy.Keys() {
		encoded, _ := u.deps.Codec.Marshal(committed[key])
		if err := u.store.Publish(key, string(encoded)); err != nil {
			return err
		}
	}
	u.refresh()
	u.invalidate()
	return nil
}
