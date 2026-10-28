package reload_policy

type Usecase struct{ reload func() error }

func NewUsecase(reload func() error) Usecase { return Usecase{reload: reload} }
func (usecase Usecase) Execute() error       { return usecase.reload() }
