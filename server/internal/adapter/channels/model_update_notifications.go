package channels

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/channels/domain"
)

// ModelUpdateNotifications suppresses repeated background model-update reports.
type ModelUpdateNotifications struct {
	sync.Mutex
	lastNotifiedAt      int64
	lastChangedChannels int
	lastFailedChannels  int
}

func (s *ModelUpdateNotifications) Allow(now int64, changedChannels int, failedChannels int) bool {
	if changedChannels <= 0 && failedChannels <= 0 {
		return true
	}

	s.Lock()
	defer s.Unlock()

	if domain.RepeatedModelUpdateNotification(now, changedChannels, failedChannels, s.lastNotifiedAt, s.lastChangedChannels, s.lastFailedChannels, 86400) {
		return false
	}

	s.lastNotifiedAt = now
	s.lastChangedChannels = changedChannels
	s.lastFailedChannels = failedChannels
	return true
}
