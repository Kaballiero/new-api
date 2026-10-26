package create_model

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	ModelDuplicate(int, string) (bool, error)
	WithModelMutation(func(domain.ModelMutation) error) error
}
type Usecase struct {
	now func() int64

	store   Store
	refresh func()
}

func NewUsecase(store Store, now func() int64, refresh func()) Usecase {
	return Usecase{store: store, now: now, refresh: refresh}
}
func (u Usecase) Execute(input Input) (*Output, error) {
	v := input.Value
	if v.ModelName == "" {
		return nil, domain.ErrModelNameEmpty
	}
	if err := domain.ValidateMetadataValues(domain.MetadataValues{Endpoints: v.Endpoints, Status: v.Status, NameRule: v.NameRule}); err != nil {
		return nil, err
	}
	if dup, err := u.store.ModelDuplicate(0, v.ModelName); err != nil {
		return nil, domain.Internal(err)
	} else if dup {
		return nil, domain.ErrModelNameExists
	}
	if err := u.persist(&v); err != nil {
		return nil, domain.Internal(err)
	}
	u.refresh()
	return &v, nil
}

func (u Usecase) persist(value *domain.Model) error {
	return u.store.WithModelMutation(func(tx domain.ModelMutation) error {
		if value.VendorID != 0 {
			if value.VendorID < 0 {
				return errors.New("select a saved vendor")
			}
			exists, err := tx.VendorExists(value.VendorID)
			if err != nil {
				return err
			}
			if !exists {
				return errors.New("vendor does not exist")
			}
		}
		now := u.now()
		value.CreatedTime, value.UpdatedTime = now, now
		status, syncOfficial := value.Status, value.SyncOfficial
		if err := tx.InsertModel(value); err != nil {
			return err
		}
		value.Status, value.SyncOfficial = status, syncOfficial
		return tx.RestoreModelDefaults(value.Id, status, syncOfficial)
	})
}
