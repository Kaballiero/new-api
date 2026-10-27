package router

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

func SetMetricsRouter(router *gin.Engine) {
	router.GET("/metrics", gin.WrapH(app.NewChannelStateMetricsHandler()))
}
