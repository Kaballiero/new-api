package start_login_verification

import "github.com/QuantumNous/new-api/internal/identity/domain"

type StartLoginOperation interface {
	PrimaryFacts() (userID int, authVersion int64, loginMethod string)
	LoadState() (status int, authVersion int64, err error)
	Policy(scope string) (count int, anyAvailable bool, err error)
	CreateChallenge() error
}

func (usecase Usecase) Execute() error {
	userID, authVersion, loginMethod := usecase.operation.PrimaryFacts()
	if userID <= 0 || authVersion <= 0 || loginMethod == "" {
		return usecase.errors.FlowInvalid()
	}
	status, currentVersion, err := usecase.operation.LoadState()
	if err != nil {
		return err
	}
	if status != usecase.enabledStatus || currentVersion != authVersion {
		return usecase.errors.Inactive()
	}
	count, available, err := usecase.operation.Policy("auth.login")
	if err != nil || count == 0 {
		return err
	}
	if !available {
		return usecase.errors.Unavailable()
	}
	return usecase.operation.CreateChallenge()
}

type Usecase struct {
	operation     StartLoginOperation
	enabledStatus int
	errors        domain.VerificationErrors
}

func NewUsecase(operation StartLoginOperation, enabledStatus int, errors domain.VerificationErrors) Usecase {
	return Usecase{operation: operation, enabledStatus: enabledStatus, errors: errors}
}
