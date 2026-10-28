package delete_model

import (
	"errors"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	LockMetadata() func()
	WithDeletion(bool, func(domain.MetadataDeletion) error) error
	ModelExists(int) error
	IsMissing(error) bool
}
type Usecase struct {
	store                           Store
	refreshChannels, refreshPricing func()
}

func NewUsecase(store Store, refreshChannels, refreshPricing func()) Usecase {
	return Usecase{store: store, refreshChannels: refreshChannels, refreshPricing: refreshPricing}
}
func (u Usecase) Execute(input Input) (Output, error) {
	ids, removeFromChannels, removePricing := input.ModelIDs, input.RemoveFromChannels, input.RemovePricing
	if removePricing && !input.AllowRemovePricing {
		return Output{}, domain.ErrPricingRootOnly
	}
	if err := u.store.ModelExists(ids[0]); err != nil {
		if u.store.IsMissing(err) {
			return Output{}, domain.ErrModelNotFound
		}
		return Output{}, domain.Internal(err)
	}

	result := domain.ModelDeleteResult{}
	if len(ids) == 0 || len(ids) > 1000 {
		return result, errors.New("select between 1 and 1000 models")
	}
	selected := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return result, errors.New("invalid model ID")
		}
		selected[id] = struct{}{}
	}
	modelIDs := make([]int, 0, len(selected))
	for id := range selected {
		modelIDs = append(modelIDs, id)
	}
	sort.Ints(modelIDs)
	names := make(map[string]struct{}, len(modelIDs))
	deleteRecords := func(tx domain.MetadataDeletion) error {
		records, err := tx.ReadModels(modelIDs)
		if err != nil {
			return err
		}
		if len(records) != len(modelIDs) {
			return errors.New("selected models changed; reload before deleting")
		}
		for _, record := range records {
			if removeFromChannels && record.NameRule != domain.NameRuleExact {
				return errors.New("only exact-match models can be removed from channels")
			}
			names[record.ModelName] = struct{}{}
		}
		if removeFromChannels {
			channels, err := tx.RoutingChannels()
			if err != nil {
				return err
			}
			for _, channel := range channels {
				models := channel.GetModels()
				remaining := make([]string, 0, len(models))
				for _, name := range models {
					if _, remove := names[strings.TrimSpace(name)]; !remove {
						remaining = append(remaining, name)
					}
				}
				if len(remaining) == len(models) {
					continue
				}
				channel.Models = strings.Join(remaining, ",")
				if err := tx.SaveRouting(channel); err != nil {
					return err
				}
				result.UpdatedChannels++
			}
		}
		if err := tx.DeleteModels(modelIDs); err != nil {
			return err
		}
		result.DeletedCount = len(records)
		return nil
	}
	if removePricing {
		unlock := u.store.LockMetadata()
		defer unlock()
	}
	err := u.store.WithDeletion(removePricing, func(tx domain.MetadataDeletion) error {
		if err := deleteRecords(tx); err != nil {
			return err
		}
		if removePricing {
			tx.RemovePrices(names)
		}
		return nil
	})
	if err != nil {
		if u.store.IsMissing(err) {
			return Output{}, domain.ErrModelNotFound
		}
		return domain.ModelDeleteResult{}, err
	}
	if result.UpdatedChannels > 0 {
		u.refreshChannels()
	}
	u.refreshPricing()
	return result, nil
}
