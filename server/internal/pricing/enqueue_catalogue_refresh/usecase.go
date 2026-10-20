package enqueue_catalogue_refresh

type Usecase struct {
	enabled func() bool
	enqueue func() error
	log     func(string)
}

func NewUsecase(enabled func() bool, enqueue func() error, log func(string)) Usecase {
	return Usecase{enabled: enabled, enqueue: enqueue, log: log}
}
func (u Usecase) Execute() {
	if !u.enabled() {
		return
	}
	if err := u.enqueue(); err != nil {
		u.log("pricing catalog refresh enqueue failed")
	}
}
