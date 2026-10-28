package httpapi

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/app"
	core "github.com/QuantumNous/new-api/internal/app/system"
	"github.com/QuantumNous/new-api/internal/app/systemruntime"
)

func RegisterScheduledSystemTasks() {
	systemruntime.RegisterScheduledSystemTasks(ChannelEffects(), core.RegisterSystemTaskHandler)
}

func AutomaticallyUpdateChannels(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Minute)
		common.SysLog("updating all channels")
		_ = app.NewChannelBalancesUsecase().Execute()
		common.SysLog("channels update done")
	}
}
