package start_login_verification

import "github.com/QuantumNous/new-api/internal/identity/domain"

type StartLoginOperation interface {
	PrimaryFacts() (userID int, authVersion int64, loginMethod string)
	LoadState() (status int, authVersion int64, err error)
	Policy(scope string) (count int, anyAvailable bool, err error)
	CreateChallenge() error
}

func (usecase Usecase) Execute(operation StartLoginOperation) error {
	userID, authVersion, loginMethod := operation.PrimaryFacts()
	if userID <= 0 || authVersion <= 0 || loginMethod == "" {
		return domain.ErrAuthFlowInvalid
	}
	status, currentVersion, err := operation.LoadState()
	if err != nil {
		return err
	}
	if status != usecase.enabledStatus || currentVersion != authVersion {
		return domain.ErrSessionInactive
	}
	count, available, err := operation.Policy("auth.login")
	if err != nil || count == 0 {
		return err
	}
	if !available {
		return domain.ErrVerificationUnavailable
	}
	return operation.CreateChallenge()
}

type Usecase struct {
	enabledStatus int
}

func NewUsecase(enabledStatus int) Usecase {
	return Usecase{enabledStatus: enabledStatus}
}
