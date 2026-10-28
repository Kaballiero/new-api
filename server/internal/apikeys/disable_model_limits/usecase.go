package disable_model_limits

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Store interface {
	GetByID(int) (*domain.Token, error)
	Update(*domain.Token) error
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (usecase Usecase) Execute(id int) error {
	token, err := usecase.store.GetByID(id)
	if err != nil {
		return err
	}
	token.ModelLimitsEnabled = false
	token.ModelLimits = ""
	return usecase.store.Update(token)
}
