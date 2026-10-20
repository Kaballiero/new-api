package detect_all_upstream_model_updates

type Usecase struct {
	enqueue func() (Task, bool, error)
	audit   func(string)
}

func NewUsecase(enqueue func() (Task, bool, error), audit func(string)) Usecase {
	return Usecase{enqueue: enqueue, audit: audit}
}
func (usecase Usecase) Execute() (Output, error) {
	task, created, err := usecase.enqueue()
	if err != nil {
		return Output{}, err
	}
	if created {
		usecase.audit(task.ID)
	}
	return Output{Task: task, Created: created}, nil
}
