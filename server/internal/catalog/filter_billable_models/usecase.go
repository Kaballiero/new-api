package filter_billable_models

import (
	"fmt"
	"slices"
)

type Store interface {
	ReserveBilled([]string, []string) (map[string]bool, error)
}
type Usecase struct {
	store      Store
	configured func(string) bool
	log        func(string)
}

func NewUsecase(store Store, configured func(string) bool, log func(string)) Usecase {
	return Usecase{store: store, configured: configured, log: log}
}
func (u Usecase) Execute(modelNames, groups []string) []string {
	unpriced := make(map[string]bool)
	var unpricedModels []string
	for _, name := range modelNames {
		if !u.configured(name) {
			unpricedModels = append(unpricedModels, name)
			unpriced[name] = true
		}
	}
	if len(unpricedModels) == 0 {
		return modelNames
	}
	reserves, err := u.store.ReserveBilled(unpricedModels, groups)
	if err != nil {
		u.log(fmt.Sprintf("GetOpenRouterReserveBilledModels error: %v", err))
	}
	return slices.DeleteFunc(modelNames, func(name string) bool { return unpriced[name] && !reserves[name] })
}
