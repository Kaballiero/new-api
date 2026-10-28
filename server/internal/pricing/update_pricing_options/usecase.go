package update_pricing_options

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
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

// UpdateModelPricingOptions keeps legacy single-option callers on the same
// locking, validation and transaction path as the model-level API.
func (u Usecase) Execute(updates map[string]string) error {
	return u.mutate(func(_ domain.OptionTransaction, values map[string]map[string]any) error {
		names := make(map[string]bool)
		for key, raw := range updates {
			if !u.policy.IsOption(key) {
				return fmt.Errorf("unsupported pricing field: %s", key)
			}
			var entries map[string]any
			if err := u.deps.Codec.UnmarshalJsonStr(raw, &entries); err != nil {
				return err
			}
			if entries == nil {
				return fmt.Errorf("%s must be a JSON object", key)
			}
			for name := range values[key] {
				names[name] = true
			}
			values[key] = entries
			for name := range entries {
				names[name] = true
			}
		}
		for name := range names {
			if err := u.policy.Validate(name, u.policy.Values(values, name)); err != nil {
				return err
			}
		}
		return nil
	})
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
			encoded, err := u.deps.Codec.Marshal(defaults[key])
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
