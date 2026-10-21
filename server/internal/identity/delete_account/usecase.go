package delete_account

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	Delete(domainInput Input) error
	// RootDenied reports that err rejects deleting the root account.
	RootDenied(error) bool
}

func (usecase Usecase) Execute(input Input) error {
	if err := usecase.operation.Delete(input); err != nil {
		if usecase.operation.RootDenied(err) {
			return domain.ErrRootDeletion
		}
		return err
	}
	return nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
