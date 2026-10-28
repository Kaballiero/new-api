package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func GetPerfMetrics(c *gin.Context) { httpapi.System().Metrics.Handle(c) }

func GetPerfMetricsSummary(c *gin.Context) { httpapi.System().MetricsSummary.Handle(c) }
