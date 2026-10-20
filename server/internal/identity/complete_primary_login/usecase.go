package complete_primary_login

type Failure struct {
	Stage string
	Cause error
}
type Operation interface {
	PrimaryValid() bool
	LoadProfile() error
	CreateSession(expectedVersion int64) error
}

func (usecase Usecase) Execute(expectedVersion int64) *Failure {
	if !usecase.operation.PrimaryValid() {
		return &Failure{Stage: "banned"}
	}
	if err := usecase.operation.LoadProfile(); err != nil {
		return &Failure{Stage: "profile", Cause: err}
	}
	if err := usecase.operation.CreateSession(expectedVersion); err != nil {
		return &Failure{Stage: "session", Cause: err}
	}
	return nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
