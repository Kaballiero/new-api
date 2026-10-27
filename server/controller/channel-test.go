package controller

import "github.com/gin-gonic/gin"

func TestChannel(c *gin.Context)     { channelsHTTP().TestChannel.Handle(c) }
func TestAllChannels(c *gin.Context) { channelsHTTP().TestChannels.Handle(c) }
