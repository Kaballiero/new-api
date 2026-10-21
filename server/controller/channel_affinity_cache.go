package controller

import (
	"github.com/gin-gonic/gin"
)

func GetChannelAffinityCacheStats(c *gin.Context) {
	channelsHTTP().GetAffinityCacheStats.Handle(c)
}
func ClearChannelAffinityCache(c *gin.Context) { channelsHTTP().ClearAffinityCache.Handle(c) }
func GetChannelAffinityUsageCacheStats(c *gin.Context) {
	channelsHTTP().GetAffinityUsageStats.Handle(c)
}
