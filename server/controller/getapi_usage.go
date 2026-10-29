package controller

import (
	"github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetAPIUsageAggregate(c *gin.Context) { httpapi.Usage().GetAPIAggregate.Handle(c) }

func GetAPIUsageLogs(c *gin.Context) { httpapi.Usage().GetAPILogs.Handle(c) }
