package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func GetPerformanceStats(c *gin.Context) { httpapi.System().Stats.Handle(c) }

func ClearDiskCache(c *gin.Context) { httpapi.System().ClearCache.Handle(c) }

func ResetPerformanceStats(c *gin.Context) { httpapi.System().ResetStats.Handle(c) }

func ForceGC(c *gin.Context) { httpapi.System().GC.Handle(c) }

func GetLogFiles(c *gin.Context) { httpapi.System().LogFiles.Handle(c) }

func CleanupLogFiles(c *gin.Context) { httpapi.System().CleanupFiles.Handle(c) }
