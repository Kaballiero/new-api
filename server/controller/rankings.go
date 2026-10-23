package controller

import "github.com/gin-gonic/gin"

// BRIDGE: router/api-router.go until the usage controller entrypoints are removed.
func GetRankings(c *gin.Context) { usageHTTP().Rankings.Handle(c) }
