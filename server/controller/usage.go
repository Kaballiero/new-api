package controller

import (
	"sync"

	appusage "github.com/QuantumNous/new-api/internal/app/usage"
)

// BRIDGE: router/api-router.go retains the legacy controller entrypoints.
var usageHTTP = sync.OnceValue(appusage.NewHTTP)
