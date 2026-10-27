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

func (usecase Usecase) Execute(operation Operation) error {
	if err := operation.LoadChange(); err != nil {
		return err
	}
	if err := operation.BindOperation(); err != nil {
		return err
	}
	if err := operation.ValidateAuthorization(); err != nil {
		return err
	}
	if operation.ResendAt() > time.Now().Unix() {
		return domain.ErrEmailBindingResendWait
	}
	if operation.FailedAttempts() >= operation.MaximumAttempts() {
		return domain.ErrEmailBindingLocked
	}
	if err := operation.GenerateCodes(operation.HasOldCode()); err != nil {
		return err
	}
	if err := operation.ReplaceCodes(); err != nil {
		return err
	}
	if err := operation.DeliverCodes(); err != nil {
		operation.InvalidateChange()
		return domain.ErrEmailBindingDelivery
	}
	operation.CaptureData()
	return nil
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
