package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func RequestWaffoAmount(c *gin.Context) { httpapi.Payments().QuoteWaffo.Handle(c) }

func RequestWaffoPay(c *gin.Context) { httpapi.Payments().CreateWaffo.Handle(c) }

func WaffoWebhook(c *gin.Context) { httpapi.Payments().WaffoWebhook.Handle(c) }
