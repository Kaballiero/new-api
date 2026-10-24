package admission

import (
	"sync"
)

// OrderAdmission retains the legacy reference-counted per-order registry.
type OrderAdmission struct {
	orderLocks sync.Map
	createLock sync.Mutex
}

// The final release removes an entry after every waiting caller has released it.
type refCountedMutex struct {
	mu       sync.Mutex
	refCount int
}

func (a *OrderAdmission) Lock(tradeNo string) {
	a.createLock.Lock()
	var rcm *refCountedMutex
	if v, ok := a.orderLocks.Load(tradeNo); ok {
		rcm = v.(*refCountedMutex)
	} else {
		rcm = &refCountedMutex{}
		a.orderLocks.Store(tradeNo, rcm)
	}
	rcm.refCount++
	a.createLock.Unlock()
	rcm.mu.Lock()
}

func (a *OrderAdmission) Unlock(tradeNo string) {
	v, ok := a.orderLocks.Load(tradeNo)
	if !ok {
		return
	}
	rcm := v.(*refCountedMutex)
	rcm.mu.Unlock()

	a.createLock.Lock()
	rcm.refCount--
	if rcm.refCount == 0 {
		a.orderLocks.Delete(tradeNo)
	}
	a.createLock.Unlock()
}
