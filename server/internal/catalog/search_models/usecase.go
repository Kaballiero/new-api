package search_models

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"sort"
	"strings"
)

type Store interface {
	ConfiguredChannels() (map[string][]int, error)
	Connections() ([]domain.ModelConnection, error)
	AllModels() ([]domain.Model, error)
	SupportEndpoints(string) []string
	QuotaTypes(string) []int
	Models(string, string, string, string, int, int) ([]*domain.Model, int64, error)
	ExactNames() ([]string, error)
	VendorCounts() (map[int64]int64, error)
}
type Usecase struct {
	store Store
	log   func(string)
}

func NewUsecase(store Store, log func(string)) Usecase { return Usecase{store: store, log: log} }
func (u Usecase) Execute(input Input) (Output, error) {
	squareState := domain.ModelSquareState(input.SquareState)
	switch squareState {
	case "", domain.ModelSquareVisible, domain.ModelSquareUnavailable, domain.ModelSquareHidden, domain.ModelSquarePartial:
	default:
		return Output{}, domain.ErrInvalidSquareState
	}
	if squareState != "" && (input.Page < 1 || input.PageSize < 1) {
		return Output{}, domain.ErrInvalidPagination
	}
	offset, limit := (input.Page-1)*input.PageSize, input.PageSize
	if squareState != "" {
		offset, limit = 0, -1
	}
	start, count := offset, limit
	if input.IncludeChannelModels {
		start, count = 0, -1
	}
	items, total, err := u.store.Models(input.Keyword, input.Vendor, input.Status, input.SyncOfficial, start, count)
	if err != nil {
		return Output{}, domain.Internal(err)
	}
	if input.IncludeChannelModels {
		_, filterStatus := domain.ParseModelStatusFilter(input.Status)
		_, filterSync := domain.ParseModelSyncFilter(input.SyncOfficial)
		if !filterStatus && !filterSync && (input.Vendor == "" || input.Vendor == "0") {
			configured, err := u.store.ConfiguredChannels()
			if err != nil {
				return Output{}, domain.Internal(err)
			}
			exactNames, err := u.store.ExactNames()
			if err != nil {
				return Output{}, domain.Internal(err)
			}
			for _, name := range exactNames {
				delete(configured, name)
			}
			names := make([]string, 0, len(configured))
			for name := range configured {
				if input.Keyword == "" || strings.Contains(strings.ToLower(name), strings.ToLower(input.Keyword)) {
					names = append(names, name)
				}
			}
			sort.Strings(names)
			for _, name := range names {
				items = append(items, &domain.Model{ModelName: name, NameRule: domain.NameRuleExact})
			}
		}
		n := len(items)
		total = int64(n)
		if offset < 0 {
			offset = 0
		}
		if offset >= n {
			items = []*domain.Model{}
		} else {
			end := n
			if limit >= 0 && limit < n-offset {
				end = offset + limit
			}
			items = items[offset:end]
		}
	}
	if len(items) > 0 {
		configured, err := u.store.ConfiguredChannels()
		if err != nil {
			u.log("load configured model channels: " + err.Error())
			return Output{}, domain.Internal(err)
		}
		domain.FillConfiguredCounts(items, configured)
		connections, err := u.store.Connections()
		if err != nil {
			u.log("load model connections: " + err.Error())
			return Output{}, domain.Internal(err)
		}
		metadata, err := u.store.AllModels()
		if err != nil {
			u.log("load model square states: " + err.Error())
			return Output{}, domain.Internal(err)
		}
		if err := domain.FillModelSquareStates(items, configured, connections, metadata); err != nil {
			u.log("load model square states: " + err.Error())
			return Output{}, domain.Internal(err)
		}
		domain.FillConnections(items, connections, u.store.SupportEndpoints, u.store.QuotaTypes)
	}
	if squareState != "" {
		filtered := make([]*domain.Model, 0, len(items))
		for _, m := range items {
			if m.SquareState == squareState {
				filtered = append(filtered, m)
			}
		}
		total = int64(len(filtered))
		start := len(filtered)
		if input.Page-1 <= len(filtered)/input.PageSize {
			start = (input.Page - 1) * input.PageSize
		}
		end := min(start+input.PageSize, len(filtered))
		items = filtered[start:end]
	}
	vendorCounts, _ := u.store.VendorCounts()
	return Output{Items: items, Total: total, Page: input.Page, PageSize: input.PageSize, VendorCounts: vendorCounts}, nil
}
