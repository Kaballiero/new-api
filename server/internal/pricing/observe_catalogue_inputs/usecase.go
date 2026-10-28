package observe_catalogue_inputs

type Store interface{ Fingerprint() (string, error) }
type State interface {
	Generation() uint64
	Observed() *string
	LockPublication() func()
	Observe(*string)
	Invalidate()
}
type Usecase struct {
	store Store
	state State
}

func NewUsecase(store Store, state State) Usecase { return Usecase{store: store, state: state} }
func (u Usecase) Execute() {
	generation, observed := u.state.Generation(), u.state.Observed()
	fingerprint, err := u.store.Fingerprint()
	if err != nil {
		return
	}
	unlock := u.state.LockPublication()
	defer unlock()
	if generation != u.state.Generation() || observed != u.state.Observed() {
		return
	}
	if observed != nil && *observed == fingerprint {
		return
	}
	u.state.Observe(&fingerprint)
	u.state.Invalidate()
}
