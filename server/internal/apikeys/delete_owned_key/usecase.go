package delete_owned_key

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
)

type Store interface {
	GetForDeletion(int, int) (*domain.Token, error)
	Delete(*domain.Token) error
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (usecase Usecase) Execute(id, userID int) error {
	if id == 0 || userID == 0 {
		return errors.New("id 或 userId 为空！")
	}
	token, err := usecase.store.GetForDeletion(id, userID)
	if err != nil {
		return err
	}
	return usecase.store.Delete(token)
}
