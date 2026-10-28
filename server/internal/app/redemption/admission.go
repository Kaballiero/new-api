package redemption

import "sync"

// admission preserves the process-local per-user lock lifetime, including retained map entries.
type admission struct {
	locks  sync.Map
	create sync.Mutex
}

type tryLock struct {
	ch chan struct{}
}

func (a *admission) Acquire(userID int) (func(), bool) {
	value, ok := a.locks.Load(userID)
	if !ok {
		a.create.Lock()
		value, ok = a.locks.Load(userID)
		if !ok {
			value = &tryLock{ch: make(chan struct{}, 1)}
			a.locks.Store(userID, value)
		}
		a.create.Unlock()
	}
	lock := value.(*tryLock)
	select {
	case lock.ch <- struct{}{}:
		return lock.release, true
	default:
		return nil, false
	}
}

func (lock *tryLock) release() {
	select {
	case <-lock.ch:
	default:
	}
}
