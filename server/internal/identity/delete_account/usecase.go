package delete_account

import "errors"

type Operation interface {
	Delete(domainInput Input) error
	RootError() error
}

func (usecase Usecase) Execute(input Input) Result {
	if err := usecase.operation.Delete(input); err != nil {
		if errors.Is(err, usecase.operation.RootError()) {
			return Result{RootDenied: true}
		}
		return Result{Cause: err}
	}
	return Result{}
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
