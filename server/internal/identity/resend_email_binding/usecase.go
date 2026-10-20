package resend_email_binding

import "time"

type Operation interface {
	LoadChange() error
	BindOperation() error
	ValidateAuthorization() error
	ResendAt() int64
	ResendWaitError() error
	FailedAttempts() int
	MaximumAttempts() int
	LockedError() error
	HasOldCode() bool
	GenerateCodes(bool) error
	ReplaceCodes() error
	DeliverCodes() error
	InvalidateChange()
	DeliveryError() error
	CaptureData()
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.LoadChange(); err != nil {
		return err
	}
	if err := usecase.operation.BindOperation(); err != nil {
		return err
	}
	if err := usecase.operation.ValidateAuthorization(); err != nil {
		return err
	}
	if usecase.operation.ResendAt() > time.Now().Unix() {
		return usecase.operation.ResendWaitError()
	}
	if usecase.operation.FailedAttempts() >= usecase.operation.MaximumAttempts() {
		return usecase.operation.LockedError()
	}
	if err := usecase.operation.GenerateCodes(usecase.operation.HasOldCode()); err != nil {
		return err
	}
	if err := usecase.operation.ReplaceCodes(); err != nil {
		return err
	}
	if err := usecase.operation.DeliverCodes(); err != nil {
		usecase.operation.InvalidateChange()
		return usecase.operation.DeliveryError()
	}
	usecase.operation.CaptureData()
	return nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
