package router

import (
	systemmetrics "github.com/QuantumNous/new-api/internal/app/systemmetrics"

	"github.com/gin-gonic/gin"
)

func SetMetricsRouter(router *gin.Engine) {
	router.GET("/metrics", gin.WrapH(systemmetrics.NewChannelStateMetricsHandler()))
}
