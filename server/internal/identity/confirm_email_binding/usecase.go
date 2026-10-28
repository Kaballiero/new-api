package confirm_email_binding

type FinishOperation interface {
	LoadChange() error
	BindOperation() error
	ValidateAuthorization() error
	ValidateTargetEmail() error
	CompleteChange() error
}

func Finish(op FinishOperation) error {
	if err := op.LoadChange(); err != nil {
		return err
	}
	if err := op.BindOperation(); err != nil {
		return err
	}
	if err := op.ValidateAuthorization(); err != nil {
		return err
	}
	if err := op.ValidateTargetEmail(); err != nil {
		return err
	}
	return op.CompleteChange()
}

type Confirmation interface {
	FinishBinding() error
	MarkSucceeded()
	NotifyPrevious() bool
	NotifyConfirmed() error
	SetNotificationWarning(bool)
	PublishCache() error
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.FinishBinding(); err != nil {
		return err
	}
	usecase.operation.MarkSucceeded()
	warning := usecase.operation.NotifyPrevious()
	if err := usecase.operation.NotifyConfirmed(); err != nil {
		warning = true
	}
	usecase.operation.SetNotificationWarning(warning)
	return usecase.operation.PublishCache()
}

type Usecase struct {
	operation Confirmation
}

func NewUsecase(operation Confirmation) Usecase {
	return Usecase{operation: operation}
}
