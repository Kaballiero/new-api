package controller

import (
	"github.com/gin-gonic/gin"
)

func GetCodexChannelUsage(c *gin.Context) {
	channelsHTTP().GetCodexUsage.Handle(c)
}

func GetCodexChannelRateLimitResetCredits(c *gin.Context) {
	channelsHTTP().GetCodexResetCredits.Handle(c)
}

func ResetCodexChannelUsage(c *gin.Context) {
	channelsHTTP().ResetCodexUsage.Handle(c)
}
