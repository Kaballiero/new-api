package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/QuantumNous/new-api/internal/usage/transport"

	"github.com/gin-gonic/gin"
)

func GetAllLogs(c *gin.Context) { httpapi.Usage().AdminLogs.Handle(c) }

func GetUserLogs(c *gin.Context) { httpapi.Usage().OwnLogs.Handle(c) }

func GetLogByKey(c *gin.Context) { httpapi.Usage().KeyLogs.Handle(c) }

func GetLogsStat(c *gin.Context) { httpapi.Usage().AdminStats.Handle(c) }

func GetLogsSelfStat(c *gin.Context) { httpapi.Usage().OwnStats.Handle(c) }

func SearchAllLogs(c *gin.Context) { transport.Deprecated(c) }

func SearchUserLogs(c *gin.Context) { transport.Deprecated(c) }
