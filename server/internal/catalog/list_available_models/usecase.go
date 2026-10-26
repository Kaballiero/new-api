package list_available_models

import (
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/internal/catalog/domain/availability"
)

type Store interface {
	PreferredOwners([]string, []string) (map[string]int, error)
	ReserveBilled([]string, []string) (map[string]bool, error)
}
type Accounts interface {
	Group(int) (string, error)
	AcceptUnset(int) bool
}
type Models interface {
	Profile(string) availability.Model
	Owner(int) string
	Configured(string) bool
}
type Runtime struct {
	SelfUse       func() bool
	EnabledModels func([]string) []string
	RoutingMatch  func(string) string
	Log           func(string)
}
type Usecase struct {
	store    Store
	accounts Accounts
	models   Models
	runtime  Runtime
}

func NewUsecase(store Store, accounts Accounts, models Models, runtime Runtime) Usecase {
	return Usecase{store: store, accounts: accounts, models: models, runtime: runtime}
}
func (u Usecase) Execute(input Input) ([]availability.Model, error) {
	accept := u.runtime.SelfUse()
	if !accept && input.UserID > 0 {
		accept = u.accounts.AcceptUnset(input.UserID)
	}
	userGroup := input.UserGroup
	if userGroup == "" && (input.TokenGroup == "" || input.TokenGroup == "auto") {
		var err error
		userGroup, err = u.accounts.Group(input.UserID)
		if err != nil {
			return nil, availability.ErrUserGroup
		}
	}
	groups := []string{userGroup}
	if input.TokenGroup == "auto" {
		groups = input.AutoGroups(userGroup)
	} else if input.TokenGroup != "" {
		groups = []string{input.TokenGroup}
	}
	names := make([]string, 0)
	unpriced := map[string]bool{}
	var unpricedNames []string
	for _, name := range u.runtime.EnabledModels(groups) {
		if input.LimitEnabled {
			matching := u.runtime.RoutingMatch(name)
			if !input.Limits[name] && !input.Limits[matching] {
				continue
			}
		}
		if !accept && !u.models.Configured(name) {
			unpricedNames = append(unpricedNames, name)
			unpriced[name] = true
		}
		names = append(names, name)
	}
	if len(unpricedNames) > 0 {
		reserve, err := u.store.ReserveBilled(unpricedNames, groups)
		if err != nil {
			u.runtime.Log(fmt.Sprintf("GetOpenRouterReserveBilledModels error: %v", err))
		}
		names = slices.DeleteFunc(names, func(name string) bool { return unpriced[name] && !reserve[name] })
	}
	owners := map[string]string{}
	if len(groups) > 0 {
		types, err := u.store.PreferredOwners(names, groups)
		if err != nil {
			u.runtime.Log(fmt.Sprintf("GetPreferredModelOwnerChannelTypes error: %v", err))
		} else {
			byType := map[int]string{}
			for name, typ := range types {
				owner, ok := byType[typ]
				if !ok {
					owner = u.models.Owner(typ)
					byType[typ] = owner
				}
				if owner != "" {
					owners[name] = owner
				}
			}
		}
	}
	out := make([]availability.Model, 0, len(names))
	for _, name := range names {
		v := u.models.Profile(name)
		if owner := owners[name]; owner != "" {
			v.OwnedBy = owner
		}
		out = append(out, v)
	}
	return out, nil
}
