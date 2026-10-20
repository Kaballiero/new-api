package login_wechat

type Operation interface {
	ExchangeCode(string) error
	InitializeAccount()
	Taken() bool
	LoadAccount() error
	AccountID() int
	RegistrationEnabled() bool
	PrepareAccount()
	InsertAccount() error
	AccountEnabled() bool
}

func (usecase Usecase) Execute(input Input) Result {
	if err := usecase.operation.ExchangeCode(input.Code); err != nil {
		return Result{Cause: err}
	}
	usecase.operation.InitializeAccount()
	if usecase.operation.Taken() {
		if err := usecase.operation.LoadAccount(); err != nil {
			return Result{Cause: err}
		}
		if usecase.operation.AccountID() == 0 {
			return Result{Issue: "deleted"}
		}
	} else {
		if !usecase.operation.RegistrationEnabled() {
			return Result{Issue: "registration_disabled"}
		}
		usecase.operation.PrepareAccount()
		if err := usecase.operation.InsertAccount(); err != nil {
			return Result{Cause: err}
		}
	}
	if !usecase.operation.AccountEnabled() {
		return Result{Issue: "banned"}
	}
	return Result{}
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
