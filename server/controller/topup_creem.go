package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func RequestCreemPay(c *gin.Context) { httpapi.Payments().CreateCreem.Handle(c) }

func CreemWebhook(c *gin.Context) { httpapi.Payments().CreemWebhook.Handle(c) }
