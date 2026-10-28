package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func SubscriptionRequestStripePay(c *gin.Context) { httpapi.SubscriptionProviders().Stripe.Handle(c) }
