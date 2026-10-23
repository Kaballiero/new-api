package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

func RequestCreemPay(c *gin.Context) { paymentsHTTP().CreateCreem.Handle(c) }
func CreemWebhook(c *gin.Context)    { paymentsHTTP().CreemWebhook.Handle(c) }

// BRIDGE: subscription_payment_creem checkout; remove when its request uses domain.CreemProduct directly.
type CreemProduct = domain.CreemProduct
