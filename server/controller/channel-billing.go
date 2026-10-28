package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func UpdateChannelBalance(c *gin.Context) { httpapi.Channels().UpdateChannelBalance.Handle(c) }

func UpdateAllChannelsBalance(c *gin.Context) { httpapi.Channels().UpdateChannelsBalances.Handle(c) }
