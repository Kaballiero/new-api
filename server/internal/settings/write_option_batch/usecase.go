package write_option_batch

import (
	"context"
	"fmt"
	"maps"
	"slices"
)

type Store interface {
	SaveBulk(context.Context, []string, map[string]string) error
}
type Publisher struct {
	Lock                func() func()
	Apply               func(string, string) error
	CanonicalGroupRatio func(string) string
	Aliases             map[string]string
}
type Usecase struct {
	store     Store
	publisher Publisher
	validate  func(string, string) error
}

func NewUsecase(store Store, publisher Publisher, validate func(string, string) error) Usecase {
	return Usecase{store, publisher, validate}
}
func (u Usecase) Execute(ctx context.Context, values map[string]string) error {
	unlock := u.publisher.Lock()
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	values = maps.Clone(values)
	for canonical, alias := range u.publisher.Aliases {
		canonicalValue, hasCanonical := values[canonical]
		aliasValue, hasAlias := values[alias]
		if hasCanonical && hasAlias && canonicalValue != aliasValue {
			return fmt.Errorf("conflicting group ratio options: %s and %s", canonical, alias)
		}
		if hasCanonical {
			values[alias] = canonicalValue
		} else if hasAlias {
			values[canonical] = aliasValue
		}
	}
	for key, value := range values {
		if err := u.validate(key, value); err != nil {
			return err
		}
	}
	keys := slices.Sorted(maps.Keys(values))
	if err := u.store.SaveBulk(ctx, keys, values); err != nil {
		return err
	}
	for _, key := range keys {
		if canonical := u.publisher.CanonicalGroupRatio(key); canonical != "" && key != canonical {
			continue
		}
		if err := u.publisher.Apply(key, values[key]); err != nil {
			return err
		}
	}
	return nil
}
