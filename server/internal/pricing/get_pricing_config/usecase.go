package get_pricing_config

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"sort"
)

type Store interface {
	ReadRows([]string) ([]domain.OptionRow, error)
}
type UsageSchemas func() func(string) map[string]jsplugin.UsageFieldSchema
type Usecase struct {
	store   Store
	policy  *domain.Configuration
	schemas UsageSchemas
}

func NewUsecase(store Store, policy *domain.Configuration, schemas UsageSchemas) Usecase {
	return Usecase{store: store, policy: policy, schemas: schemas}
}
func (u Usecase) Execute(input Input) (*Output, error) {
	names := input.Models
	rows, err := u.store.ReadRows(u.policy.Keys())
	if err != nil {
		return nil, err
	}
	values, _, _, err := u.policy.DecodeRows(rows)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		nameSet := make(map[string]bool)
		for _, entries := range values {
			for name := range entries {
				nameSet[name] = true
			}
		}
		for name := range u.policy.Builtins() {
			nameSet[name] = true
		}
		for name := range nameSet {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	result := &domain.ModelPricingSnapshot{Entries: make([]domain.ModelPricingEntry, 0, len(names)), Options: make(map[string]string), EmptyVersion: domain.ModelPricingVersion(domain.PricingValues{})}
	schema := u.schemas()
	for _, name := range names {
		configured := u.policy.Values(values, name)
		entry := domain.ModelPricingEntry{ModelName: name, Version: domain.ModelPricingVersion(configured), Configured: configured, Effective: u.policy.Effective(values, name)}
		entry.UsageSchema = schema(name)
		result.Entries = append(result.Entries, entry)
	}

	for name, expression := range u.policy.Builtins() {
		effective := u.policy.Effective(values, name)
		if effective["billing_setting.billing_mode"] != "tiered_expr" {
			continue
		}
		if _, ok := values["billing_setting.billing_mode"][name]; !ok {
			values["billing_setting.billing_mode"][name] = "tiered_expr"
		}
		if _, ok := values["billing_setting.billing_expr"][name]; !ok {
			values["billing_setting.billing_expr"][name] = expression
		}
	}
	for key, entries := range values {
		encoded, err := common.Marshal(entries)
		if err != nil {
			return nil, err
		}
		result.Options[key] = string(encoded)
	}
	return result, nil
}
