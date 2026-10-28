package start_email_binding

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	ValidateEmail() error
	BindOperation() error
	ValidateAuthorization() error
	LoadAccount() error
	PrepareChange()
	CurrentEmail() string
	AuthorizationMethod() string
	GenerateCodes(requireOld bool) error
	CreateChange() error
	DeliverCodes() error
	InvalidateChange()
	CaptureData()
	NotifyCurrentEmail() bool
	SetNotificationWarning(bool)
}

func (usecase Usecase) Execute(operation Operation) error {
	if err := operation.ValidateEmail(); err != nil {
		return err
	}
	if err := operation.BindOperation(); err != nil {
		return err
	}
	if err := operation.ValidateAuthorization(); err != nil {
		return err
	}
	if err := operation.LoadAccount(); err != nil {
		return err
	}
	operation.PrepareChange()
	requireOld := operation.CurrentEmail() != "" && operation.AuthorizationMethod() != "2fa" && operation.AuthorizationMethod() != "passkey"
	if err := operation.GenerateCodes(requireOld); err != nil {
		return err
	}
	if err := operation.CreateChange(); err != nil {
		return err
	}
	if err := operation.DeliverCodes(); err != nil {
		operation.InvalidateChange()
		return domain.ErrEmailBindingDelivery
	}
	operation.CaptureData()
	if operation.CurrentEmail() != "" && !requireOld {
		operation.SetNotificationWarning(operation.NotifyCurrentEmail())
	}
	return nil
}

type Usecase struct {
}

func NewUsecase() Usecase {
	return Usecase{}
}
