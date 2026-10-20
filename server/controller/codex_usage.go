package controller

import (
	"github.com/QuantumNous/new-api/internal/app"

	"github.com/gin-gonic/gin"
)

func GetCodexChannelUsage(c *gin.Context) {
	app.NewGetCodexChannelUsageHTTP(c, channelEffects()).Handle(c)
}

func GetCodexChannelRateLimitResetCredits(c *gin.Context) {
	app.NewGetCodexChannelRateLimitResetCreditsHTTP(c, channelEffects()).Handle(c)
}

func ResetCodexChannelUsage(c *gin.Context) {
	app.NewResetCodexChannelUsageHTTP(c, channelEffects()).Handle(c)
}
