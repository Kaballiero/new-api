package get_channel_operations

type Usecase struct{ retryTimes func() int }

func NewUsecase(retryTimes func() int) Usecase { return Usecase{retryTimes: retryTimes} }
func (usecase Usecase) Execute() Output        { return Output{RetryTimes: usecase.retryTimes()} }
