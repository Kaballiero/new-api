package controller

import (
	"github.com/gin-gonic/gin"
)

func RequestStripeAmount(c *gin.Context) { paymentsHTTP().QuoteStripe.Handle(c) }
func RequestStripePay(c *gin.Context)    { paymentsHTTP().CreateStripe.Handle(c) }
func StripeWebhook(c *gin.Context)       { paymentsHTTP().StripeWebhook.Handle(c) }
