package controller

import "github.com/gin-gonic/gin"

func RequestWaffoAmount(c *gin.Context) { paymentsHTTP().QuoteWaffo.Handle(c) }
func RequestWaffoPay(c *gin.Context)    { paymentsHTTP().CreateWaffo.Handle(c) }
func WaffoWebhook(c *gin.Context)       { paymentsHTTP().WaffoWebhook.Handle(c) }
