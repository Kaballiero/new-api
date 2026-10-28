package check_setup

import "time"

type Store interface {
	SetupTime() *int64
	CreateSetup(string, int64) error
}
type Runtime struct {
	RootExists func() bool
	SetSetup   func(bool)
	Version    func() string
	Now        func() int64
	Log        func(string)
}
type Usecase struct {
	store   Store
	runtime Runtime
}

func NewUsecase(store Store, runtime Runtime) Usecase { return Usecase{store: store, runtime: runtime} }
func (u Usecase) Execute() error {
	setup := u.store.SetupTime()
	if setup == nil {
		// No setup record exists, check if we have a root user
		if u.runtime.RootExists() {
			u.runtime.Log("system is not initialized, but root user exists")
			// Create setup record
			err := u.store.CreateSetup(u.runtime.Version(), u.runtime.Now())
			if err != nil {
				u.runtime.Log("failed to create setup record: " + err.Error())
			}
			u.runtime.SetSetup(true)
		} else {
			u.runtime.Log("system is not initialized and no root user exists")
			u.runtime.SetSetup(false)
		}
	} else {
		// Setup record exists, system is initialized
		u.runtime.Log("system is already initialized at: " + time.Unix(*setup, 0).String())
		u.runtime.SetSetup(true)
	}

	return nil
}
