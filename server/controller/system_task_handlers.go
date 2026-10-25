package controller

import (
	core "github.com/QuantumNous/new-api/internal/app/system"
	"github.com/QuantumNous/new-api/internal/app/systemruntime"
)

// BRIDGE: main and scheduler contract entrypoints; remove when the startup owner supplies channel effects directly.
func RegisterScheduledSystemTasks() {
	systemruntime.RegisterScheduledSystemTasks(channelEffects(), core.RegisterSystemTaskHandler)
}
