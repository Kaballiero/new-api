package notifications

import (
	"github.com/QuantumNous/new-api/common"
	redisnotification "github.com/QuantumNous/new-api/internal/adapter/redis/notifications"
)

var NotifyLimitStore = &notifyLimitStore
var CleanupOnce = &cleanupOnce
var StartCleanupTask = startCleanupTask

func CheckNotificationLimit(userID int, event string) (bool, error) {
	if common.RedisEnabled {
		return (redisnotification.Counter{}).Check(userID, event)
	}
	return (MemoryCounter{}).Check(userID, event), nil
}

type LimitCount = limitCount
