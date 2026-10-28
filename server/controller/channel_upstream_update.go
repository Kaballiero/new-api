package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func ApplyChannelUpstreamModelUpdates(c *gin.Context) {
	httpapi.Channels().ApplyUpstreamModels.Handle(c)
}

func ApplyAllChannelUpstreamModelUpdates(c *gin.Context) {
	httpapi.Channels().ApplyAllUpstreamModels.Handle(c)
}

func DetectChannelUpstreamModelUpdates(c *gin.Context) {
	httpapi.Channels().DetectUpstreamModels.Handle(c)
}

func DetectAllChannelUpstreamModelUpdates(c *gin.Context) {
	httpapi.Channels().DetectAllUpstreamModels.Handle(c)
}
