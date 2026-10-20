package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

func GetChannelAffinityCacheStats(c *gin.Context) {
	app.NewGetChannelAffinityCacheStatsHTTP().Handle(c)
}
func ClearChannelAffinityCache(c *gin.Context) { app.NewClearChannelAffinityCacheHTTP().Handle(c) }
func GetChannelAffinityUsageCacheStats(c *gin.Context) {
	app.NewGetChannelAffinityUsageCacheStatsHTTP().Handle(c)
}
