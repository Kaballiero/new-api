package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func ConfirmPaymentCompliance(c *gin.Context) { httpapi.Payments().Compliance.Handle(c) }
