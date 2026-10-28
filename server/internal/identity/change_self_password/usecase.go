package change_self_password

type SelfPasswordOperation interface {
	LoadCurrent() (hasPassword bool, err error)
	RequireProof(scope string) (accepted bool, err error)
	ChangePassword(firstPassword bool) error
	MarkSucceeded()
	NotifyPasswordUpdated() (failed bool)
	PublishAuthCache() error
	AdvanceCurrentSession() error
}

type PasswordChangeResult struct{ ProofRejected, NotificationFailed bool }

func (usecase Usecase) Execute() (PasswordChangeResult, error) {
	var result PasswordChangeResult
	hasPassword, err := usecase.operation.LoadCurrent()
	if err != nil {
		return result, err
	}
	firstPassword := !hasPassword
	scope := "account.password.change"
	if firstPassword {
		scope = "account.password.set"
	}
	accepted, err := usecase.operation.RequireProof(scope)
	if err != nil {
		return result, err
	}
	if !accepted {
		result.ProofRejected = true
		return result, nil
	}
	if err := usecase.operation.ChangePassword(firstPassword); err != nil {
		return result, err
	}
	usecase.operation.MarkSucceeded()
	result.NotificationFailed = usecase.operation.NotifyPasswordUpdated()
	if err := usecase.operation.PublishAuthCache(); err != nil {
		return result, err
	}
	return result, usecase.operation.AdvanceCurrentSession()
}

type Usecase struct {
	operation SelfPasswordOperation
}

func NewUsecase(operation SelfPasswordOperation) Usecase {
	return Usecase{operation: operation}
}
