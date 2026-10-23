package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/usage/transport"
)

// BRIDGE: unchanged router/api-router.go entrypoints until routing moves to usage.
func GetAllLogs(c *gin.Context) { usageHTTP().AdminLogs.Handle(c) }

func GetUserLogs(c *gin.Context) { usageHTTP().OwnLogs.Handle(c) }

func GetLogByKey(c *gin.Context) { usageHTTP().KeyLogs.Handle(c) }

func GetLogsStat(c *gin.Context) { usageHTTP().AdminStats.Handle(c) }

func GetLogsSelfStat(c *gin.Context) { usageHTTP().OwnStats.Handle(c) }

func SearchAllLogs(c *gin.Context) { transport.Deprecated(c) }

func SearchUserLogs(c *gin.Context) { transport.Deprecated(c) }
