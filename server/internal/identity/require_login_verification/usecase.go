package require_login_verification

import "github.com/QuantumNous/new-api/internal/identity/domain"

type RequireLoginOperation interface {
	LoadFlow() error
	DecodePayload() (authVersion int64, loginMethod string, err error)
	LoadState() (status int, authVersion int64, err error)
	RequireMethod(method string) error
}

func (usecase Usecase) Execute(method string) error {
	if err := usecase.operation.LoadFlow(); err != nil {
		return err
	}
	version, loginMethod, err := usecase.operation.DecodePayload()
	if err != nil || version <= 0 || loginMethod == "" {
		return usecase.errors.FlowInvalid()
	}
	status, currentVersion, err := usecase.operation.LoadState()
	if err != nil {
		return err
	}
	if status != usecase.enabledStatus || currentVersion != version {
		return usecase.errors.Inactive()
	}
	return usecase.operation.RequireMethod(method)
}

type Usecase struct {
	operation     RequireLoginOperation
	enabledStatus int
	errors        domain.VerificationErrors
}

func NewUsecase(operation RequireLoginOperation, enabledStatus int, errors domain.VerificationErrors) Usecase {
	return Usecase{operation: operation, enabledStatus: enabledStatus, errors: errors}
}
