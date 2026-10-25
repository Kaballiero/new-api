package transport

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/gin-gonic/gin"
)

func Context(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		return c.Request.Context()
	}
	return context.Background()
}
func Error(c *gin.Context, err error) {
	var used *domain.InUseError
	if errors.As(err, &used) {
		c.JSON(200, gin.H{"success": false, "message": err.Error(), "data": gin.H{"channels": used.Channels, "in_flight_count": used.InFlight}})
		return
	}
	common.ApiError(c, err)
}
