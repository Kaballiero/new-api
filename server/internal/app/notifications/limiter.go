package notifications

import (
	"github.com/QuantumNous/new-api/common"
	notification "github.com/QuantumNous/new-api/internal/adapter/notifications"
	redisnotification "github.com/QuantumNous/new-api/internal/adapter/redis/notifications"
)

// checkNotificationLimit selects the active counter using the current Redis setting.
func checkNotificationLimit(userID int, event string) (bool, error) {
	if common.RedisEnabled {
		return (redisnotification.Counter{}).Check(userID, event)
	}
	return (notification.MemoryCounter{}).Check(userID, event), nil
}
