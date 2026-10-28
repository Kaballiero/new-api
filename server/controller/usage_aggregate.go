package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetLogAggregate(c *gin.Context) { httpapi.Usage().LogAggregate.Handle(c) }

func GetUsageAggregate(c *gin.Context) { httpapi.Usage().UsageAggregate.Handle(c) }
