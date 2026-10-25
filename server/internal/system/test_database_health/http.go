package test_database_health

import (
	"net/http"

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
			"message": "数据库连接失败",
		})
		return
	}
	// Read the shared HTTP counters.
	httpStats := h.stats()
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    "Server is running",
		"http_stats": httpStats,
	})
	return
}
