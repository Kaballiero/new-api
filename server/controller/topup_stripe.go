package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func RequestStripeAmount(c *gin.Context) { httpapi.Payments().QuoteStripe.Handle(c) }

func RequestStripePay(c *gin.Context) { httpapi.Payments().CreateStripe.Handle(c) }

func StripeWebhook(c *gin.Context) { httpapi.Payments().StripeWebhook.Handle(c) }
