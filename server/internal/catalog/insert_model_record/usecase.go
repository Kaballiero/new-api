package insert_model_record

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	WithModelMutation(func(domain.ModelMutation) error) error
}
type Usecase struct {
	store Store
	now   func() int64
}

func NewUsecase(store Store, now func() int64) Usecase { return Usecase{store: store, now: now} }
func (u Usecase) Execute(value *domain.Model) error {
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
