package controller

import "github.com/gin-gonic/gin"

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func GetPerformanceStats(c *gin.Context) { systemHTTP().Stats.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func ClearDiskCache(c *gin.Context) { systemHTTP().ClearCache.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func ResetPerformanceStats(c *gin.Context) { systemHTTP().ResetStats.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func ForceGC(c *gin.Context) { systemHTTP().GC.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func GetLogFiles(c *gin.Context) { systemHTTP().LogFiles.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func CleanupLogFiles(c *gin.Context) { systemHTTP().CleanupFiles.Handle(c) }
