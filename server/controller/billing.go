package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetSubscription(c *gin.Context) { httpapi.BillingCompatibility().Subscription.Handle(c) }

func GetUsage(c *gin.Context) { httpapi.BillingCompatibility().Usage.Handle(c) }
