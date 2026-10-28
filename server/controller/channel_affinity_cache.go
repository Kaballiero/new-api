package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func GetChannelAffinityCacheStats(c *gin.Context) { httpapi.Channels().GetAffinityCacheStats.Handle(c) }

func ClearChannelAffinityCache(c *gin.Context) { httpapi.Channels().ClearAffinityCache.Handle(c) }

func GetChannelAffinityUsageCacheStats(c *gin.Context) {
	httpapi.Channels().GetAffinityUsageStats.Handle(c)
}
