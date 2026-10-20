package test_channels

type Queue interface{ EnqueueChannelTest() (Task, bool, error) }
type Usecase struct{ queue Queue }

func NewUsecase(queue Queue) Usecase { return Usecase{queue: queue} }
func (usecase Usecase) Execute() (Output, error) {
	task, created, err := usecase.queue.EnqueueChannelTest()
	return Output{Task: task, Created: created}, err
}
