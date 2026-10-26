package update_model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	ModelDuplicate(int, string) (bool, error)
	WithModelMutation(func(domain.ModelMutation) error) error
	SetModelStatus(int, int) error
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
	if v.Id == 0 {
		return nil, domain.ErrModelIDMissing
	}
	if input.StatusOnly {
		if v.Status != 0 && v.Status != 1 {
			return nil, domain.ErrInvalidVisibility
		}
		if err := u.store.SetModelStatus(v.Id, v.Status); err != nil {
			return nil, domain.Internal(err)
		}
	} else {
		if strings.TrimSpace(v.ModelName) == "" {
			return nil, domain.ErrModelNameRequired
		}
		if err := domain.ValidateMetadataValues(domain.MetadataValues{Endpoints: v.Endpoints, Status: v.Status, NameRule: v.NameRule}); err != nil {
			return nil, err
		}
		if dup, err := u.store.ModelDuplicate(v.Id, v.ModelName); err != nil {
			return nil, domain.Internal(err)
		} else if dup {
			return nil, domain.ErrModelNameExists
		}
		if err := u.persist(&v); err != nil {
			return nil, domain.Internal(err)
		}
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
		value.UpdatedTime = u.now()
		return tx.UpdateModel(value)
	})
}
