package counters

import "sync"

// These views expose the owner's live backing arrays after package initialization.
func BatchUpdateStores() []map[int]int { return batchUpdateStores }
func BatchUpdateLocks() []sync.Mutex   { return batchUpdateLocks }
