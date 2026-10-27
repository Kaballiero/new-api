package require_login_verification

import "github.com/QuantumNous/new-api/internal/identity/domain"

type RequireLoginOperation interface {
	LoadFlow() error
	DecodePayload() (authVersion int64, loginMethod string, err error)
	LoadState() (status int, authVersion int64, err error)
	RequireMethod(method string) error
}

func (usecase Usecase) Execute(operation RequireLoginOperation, method string) error {
	if err := operation.LoadFlow(); err != nil {
		return err
	}
	version, loginMethod, err := operation.DecodePayload()
	if err != nil || version <= 0 || loginMethod == "" {
		return domain.ErrAuthFlowInvalid
	}
	status, currentVersion, err := operation.LoadState()
	if err != nil {
		return err
	}
	if status != usecase.enabledStatus || currentVersion != version {
		return domain.ErrSessionInactive
	}
	return operation.RequireMethod(method)
}

type Usecase struct {
	enabledStatus int
}

func NewUsecase(enabledStatus int) Usecase {
	return Usecase{enabledStatus: enabledStatus}
}
