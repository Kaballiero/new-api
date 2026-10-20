package sync_policy

import "time"

type Usecase struct {
	reload func() error
	sleep  func(time.Duration)
	log    func(string)
}

func NewUsecase(reload func() error, sleep func(time.Duration), log func(string)) Usecase {
	return Usecase{reload: reload, sleep: sleep, log: log}
}
func (usecase Usecase) Execute(frequency int) {
	if frequency <= 0 {
		return
	}
	for {
		usecase.sleep(time.Duration(frequency) * time.Second)
		if err := usecase.reload(); err != nil {
			usecase.log("failed to reload authz policy: " + err.Error())
		}
	}
}
