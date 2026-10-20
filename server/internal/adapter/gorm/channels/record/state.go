package record

import "github.com/QuantumNous/new-api/internal/channels/domain"

func (store Store) LockStatus() func()        { return store.environment.StatusLock() }
func (store Store) LockPolling(id int) func() { return store.environment.PollingLock(id) }
func (store Store) ReadState(id int) (*domain.Channel, error) {
	native, err := store.GetChannelById(id, true)
	state := ToDomain(native)
	if state != nil {
		store.states[state] = native
	}
	return state, err
}
func (store Store) CachedState(id int) (*domain.Channel, error) {
	native, err := store.environment.CachedRead(id)
	state := ToDomain(native)
	if state != nil {
		store.states[state] = native
	}
	return state, err
}
func (store Store) CommitState(state *domain.Channel) {
	if native, ok := store.states[state]; ok {
		native.Status = state.Status
		native.OtherInfo = state.OtherInfo
		if state.ChannelInfo.IsMultiKey {
			native.ChannelInfo = FromDomain(state).ChannelInfo
		}
	}
}
func (store Store) StateKeys(state *domain.Channel) []string { return FromDomain(state).GetKeys() }
func (store Store) SetCacheStatus(id, status int)            { store.environment.CacheStatus(id, status) }
func (store Store) SaveState(state *domain.Channel) error {
	store.CommitState(state)
	return store.SaveStatusState(FromDomain(state))
}
func (store Store) SetAbilityStatus(id int, enabled bool) error {
	return store.environment.AbilityStatus(id, enabled)
}
