package usage

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
)

// LogObservation is technical request data. Identity and IP are read lazily in
// the writer so disabled recording and settings retain their previous timing.
type LogObservation struct {
	Context  context.Context
	Identity func() (username, requestID, upstreamRequestID string)
	ClientIP func() string
}

// ObserveLogRequest captures lazy request fields for the legacy and gateway writers.
func ObserveLogRequest(c *gin.Context) LogObservation {
	return LogObservation{Context: c, Identity: func() (string, string, string) {
		return c.GetString("username"), c.GetString(common.RequestIdKey), c.GetString(common.UpstreamRequestIdKey)
	}, ClientIP: c.ClientIP}
}
