package start_email_binding

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
	DeliveryError() error
	CaptureData()
	NotifyCurrentEmail() bool
	SetNotificationWarning(bool)
}

func (usecase Usecase) Execute() error {
	if err := usecase.operation.ValidateEmail(); err != nil {
		return err
	}
	if err := usecase.operation.BindOperation(); err != nil {
		return err
	}
	if err := usecase.operation.ValidateAuthorization(); err != nil {
		return err
	}
	if err := usecase.operation.LoadAccount(); err != nil {
		return err
	}
	usecase.operation.PrepareChange()
	requireOld := usecase.operation.CurrentEmail() != "" && usecase.operation.AuthorizationMethod() != "2fa" && usecase.operation.AuthorizationMethod() != "passkey"
	if err := usecase.operation.GenerateCodes(requireOld); err != nil {
		return err
	}
	if err := usecase.operation.CreateChange(); err != nil {
		return err
	}
	if err := usecase.operation.DeliverCodes(); err != nil {
		usecase.operation.InvalidateChange()
		return usecase.operation.DeliveryError()
	}
	usecase.operation.CaptureData()
	if usecase.operation.CurrentEmail() != "" && !requireOld {
		usecase.operation.SetNotificationWarning(usecase.operation.NotifyCurrentEmail())
	}
	return nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
