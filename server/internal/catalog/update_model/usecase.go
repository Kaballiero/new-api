package update_model

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"strings"
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
func (u Usecase) Execute(input Input) (*Output, *domain.Failure) {
	v := input.Value
	if v.Id == 0 {
		return nil, &domain.Failure{Kind: "model_id_missing", Message: "缺少模型 ID"}
	}
	if input.StatusOnly {
		if v.Status != 0 && v.Status != 1 {
			return nil, &domain.Failure{Kind: "api", Message: "invalid catalog visibility"}
		}
		if err := u.store.SetModelStatus(v.Id, v.Status); err != nil {
			return nil, &domain.Failure{Kind: "internal_error", Cause: err}
		}
	} else {
		if strings.TrimSpace(v.ModelName) == "" {
			return nil, &domain.Failure{Kind: "api", Message: "模型名称不能为空"}
		}
		if err := domain.ValidateMetadataValues(domain.MetadataValues{Endpoints: v.Endpoints, Status: v.Status, NameRule: v.NameRule}); err != nil {
			return nil, &domain.Failure{Kind: "api", Cause: err}
		}
		if dup, err := u.store.ModelDuplicate(v.Id, v.ModelName); err != nil {
			return nil, &domain.Failure{Kind: "internal_error", Cause: err}
		} else if dup {
			return nil, &domain.Failure{Kind: "model_name_exists", Message: "模型名称已存在"}
		}
		if err := u.persist(&v); err != nil {
			return nil, &domain.Failure{Kind: "internal_error", Cause: err}
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
