package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetEffectivePricingOverview(c *gin.Context) { httpapi.Pricing().GetPricingOverview.Handle(c) }
