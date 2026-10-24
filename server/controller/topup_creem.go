package controller

import (
	"github.com/gin-gonic/gin"
)

func RequestCreemPay(c *gin.Context) { paymentsHTTP().CreateCreem.Handle(c) }
func CreemWebhook(c *gin.Context)    { paymentsHTTP().CreemWebhook.Handle(c) }
