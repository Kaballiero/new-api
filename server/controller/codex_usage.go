package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetCodexChannelUsage(c *gin.Context) { httpapi.Channels().GetCodexUsage.Handle(c) }

func GetCodexChannelRateLimitResetCredits(c *gin.Context) {
	httpapi.Channels().GetCodexResetCredits.Handle(c)
}

func ResetCodexChannelUsage(c *gin.Context) { httpapi.Channels().ResetCodexUsage.Handle(c) }
