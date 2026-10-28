package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func TestChannel(c *gin.Context) { httpapi.Channels().TestChannel.Handle(c) }

func TestAllChannels(c *gin.Context) { httpapi.Channels().TestChannels.Handle(c) }
