package controller

import "github.com/gin-gonic/gin"

// BRIDGE: unchanged router/api-router.go entrypoints.
func GetLogAggregate(c *gin.Context) { usageHTTP().LogAggregate.Handle(c) }

func GetUsageAggregate(c *gin.Context) { usageHTTP().UsageAggregate.Handle(c) }
