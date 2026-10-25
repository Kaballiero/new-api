package test_database_health

import (
	"net/http"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/system/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	ping  func() error
	stats func() any
}

func NewHandler(ping func() error, stats func() any) Handler {
	return Handler{ping: ping, stats: stats}
}
func (h Handler) Handle(c *gin.Context) {
	err := h.ping()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": transport.Message(c, i18n.MsgSystemDatabaseFailed),
		})
		return
	}
	// Read the shared HTTP counters.
	httpStats := h.stats()
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    transport.Message(c, i18n.MsgSystemRunning),
		"http_stats": httpStats,
	})
	return
}
