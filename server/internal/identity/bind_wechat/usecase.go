package bind_wechat

type Operation interface {
	ExchangeCode(string) error
	Taken() bool
	BindAtomically() error
	MarkSucceeded()
	LoadAccount() error
	NotifyAccount() bool
	SetNotificationWarning(bool)
}

func (usecase Usecase) Execute(input Input) Result {
	if err := usecase.operation.ExchangeCode(input.Code); err != nil {
		return Result{Issue: "provider", Cause: err}
	}
	if usecase.operation.Taken() {
		return Result{Issue: "taken"}
	}
	if err := usecase.operation.BindAtomically(); err != nil {
		return Result{Cause: err}
	}
	usecase.operation.MarkSucceeded()
	if err := usecase.operation.LoadAccount(); err != nil {
		return Result{Cause: err}
	}
	usecase.operation.SetNotificationWarning(usecase.operation.NotifyAccount())
	return Result{}
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
