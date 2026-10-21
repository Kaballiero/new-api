package resend_email_binding

import (
	"time"

	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type Operation interface {
	LoadChange() error
	BindOperation() error
	ValidateAuthorization() error
	ResendAt() int64
	FailedAttempts() int
	MaximumAttempts() int
	HasOldCode() bool
	GenerateCodes(bool) error
	ReplaceCodes() error
	DeliverCodes() error
	InvalidateChange()
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
		return domain.ErrEmailBindingResendWait
	}
	if usecase.operation.FailedAttempts() >= usecase.operation.MaximumAttempts() {
		return domain.ErrEmailBindingLocked
	}
	if err := usecase.operation.GenerateCodes(usecase.operation.HasOldCode()); err != nil {
		return err
	}
	if err := usecase.operation.ReplaceCodes(); err != nil {
		return err
	}
	if err := usecase.operation.DeliverCodes(); err != nil {
		usecase.operation.InvalidateChange()
		return domain.ErrEmailBindingDelivery
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
