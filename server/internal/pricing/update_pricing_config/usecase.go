package update_pricing_config

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"strings"
)

type Store interface {
	Lock() func()
	Transaction(func(domain.OptionTransaction) error) error
	Publish(string, string) error
}
type Usecase struct {
	audit               func([]string)
	store               Store
	policy              *domain.Configuration
	conflict            func() error
	log                 func(string)
	refresh, invalidate func()
}

func NewUsecase(store Store, policy *domain.Configuration, conflict func() error, log func(string), refresh, invalidate func(), audit func([]string)) Usecase {
	return Usecase{audit: audit, store: store, policy: policy, conflict: conflict, log: log, refresh: refresh, invalidate: invalidate}
}
func (u Usecase) Execute(changes []domain.ModelPricingChange) error {
	if len(changes) == 0 {
		return errors.New("select model pricing changes before saving")
	}
	seen := make(map[string]bool)
	for _, change := range changes {
		if seen[change.ModelName] {
			return errors.New("duplicate model pricing change")
		}
		seen[change.ModelName] = true
		if change.ExpectedVersion == "" {
			return u.conflict()
		}
		if err := u.policy.Validate(change.ModelName, change.Pricing); err != nil {
			return err
		}
	}
	err := u.mutate(func(_ domain.OptionTransaction, values map[string]map[string]any) error {
		defaults := u.policy.Defaults()
		for _, change := range changes {
			if domain.ModelPricingVersion(u.policy.Values(values, change.ModelName)) != change.ExpectedVersion {
				return fmt.Errorf("%w: %s", u.conflict(), change.ModelName)
			}
			pricing := change.Pricing
			if change.Reset {
				pricing = u.policy.Values(defaults, change.ModelName)
			}
			for _, key := range u.policy.Keys() {
				delete(values[key], change.ModelName)
				if value, exists := pricing[key]; exists {
					values[key][change.ModelName] = value
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	names := make([]string, 0, len(changes))
	for _, change := range changes {
		names = append(names, change.ModelName)
	}
	u.audit(names)
	return nil
}
func (u Usecase) mutate(mutate func(domain.OptionTransaction, map[string]map[string]any) error) error {
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
			encoded, err := common.Marshal(defaults[key])
			if err != nil {
				return err
			}
			if err := tx.Ensure(key, string(encoded)); err != nil {
				return err
			}
		}
		if err := mutate(tx, values); err != nil {
			return err
		}
		for _, key := range u.policy.Keys() {
			encoded, err := common.Marshal(values[key])
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
		encoded, _ := common.Marshal(committed[key])
		if err := u.store.Publish(key, string(encoded)); err != nil {
			return err
		}
	}
	u.refresh()
	u.invalidate()
	return nil
}
