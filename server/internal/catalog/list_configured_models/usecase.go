package list_configured_models

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"sort"
	"strings"
)

type Store interface {
	Models(string, string, string, string, int, int) ([]*domain.Model, int64, error)
	ConfiguredChannels() (map[string][]int, error)
	ExactNames() ([]string, error)
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (u Usecase) Execute(keyword, vendor, status, syncOfficial string, offset, limit int) ([]*domain.Model, int64, error) {
	records, _, err := u.store.Models(keyword, vendor, status, syncOfficial, 0, -1)
	if err != nil {
		return nil, 0, err
	}
	_, filterStatus := domain.ParseModelStatusFilter(status)
	_, filterSync := domain.ParseModelSyncFilter(syncOfficial)
	if !filterStatus && !filterSync && (vendor == "" || vendor == "0") {
		configured, err := u.store.ConfiguredChannels()
		if err != nil {
			return nil, 0, err
		}
		exactNames, err := u.store.ExactNames()
		if err != nil {
			return nil, 0, err
		}
		for _, name := range exactNames {
			delete(configured, name)
		}
		names := make([]string, 0, len(configured))
		for name := range configured {
			if keyword == "" || strings.Contains(strings.ToLower(name), strings.ToLower(keyword)) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			records = append(records, &domain.Model{ModelName: name, NameRule: domain.NameRuleExact})
		}
	}
	total := len(records)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []*domain.Model{}, int64(total), nil
	}
	end := total
	if limit >= 0 && limit < total-offset {
		end = offset + limit
	}
	return records[offset:end], int64(total), nil
}
