package read_catalogue

import (
	"errors"
)

type Snapshot interface {
	Generation() uint64
	PluginGeneration() uint64
}
type State interface {
	Snapshot() Snapshot
	BuildDue() bool
	LockBuild()
	UnlockBuild()
	LockEndpoints()
	UnlockEndpoints()
	Generation() uint64
	PluginGeneration() uint64
	StartRefresh() bool
	EndRefresh()
}
type Usecase struct {
	state   State
	rebuild func()
}

func NewUsecase(state State, rebuild func()) Usecase { return Usecase{state: state, rebuild: rebuild} }
func (u Usecase) Execute() (Snapshot, error) {
	snapshot := u.state.Snapshot()
	if snapshot == nil {

		u.state.LockBuild()
		if u.state.Snapshot() == nil && u.state.BuildDue() {
			u.state.LockEndpoints()
			u.rebuild()
			u.state.UnlockEndpoints()
		}
		u.state.UnlockBuild()
		snapshot = u.state.Snapshot()
	} else if snapshot.Generation() != u.state.Generation() || snapshot.PluginGeneration() != u.state.PluginGeneration() {
		if u.state.BuildDue() && u.state.StartRefresh() {
			go func() {
				defer u.state.EndRefresh()
				u.state.LockBuild()
				defer u.state.UnlockBuild()
				current := u.state.Snapshot()
				if current != nil && current.Generation() == u.state.Generation() && current.PluginGeneration() == u.state.PluginGeneration() {
					return
				}
				if !u.state.BuildDue() {
					return
				}
				u.state.LockEndpoints()
				defer u.state.UnlockEndpoints()
				u.rebuild()
			}()
		}
	}
	if snapshot == nil {
		return nil, errors.New("pricing catalogue unavailable")
	}
	return snapshot, nil
}
