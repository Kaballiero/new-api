package controller

import "github.com/gin-gonic/gin"

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func GetPerfMetrics(c *gin.Context) { systemHTTP().Metrics.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func GetPerfMetricsSummary(c *gin.Context) { systemHTTP().MetricsSummary.Handle(c) }
