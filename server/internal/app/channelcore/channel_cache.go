package channelcore

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
)

func SyncChannelCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing channels from database")
		channelstore.InitChannelCache()
	}
}
