package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetEffectivePricingModel(c *gin.Context) { httpapi.Pricing().GetModelPricing.Handle(c) }
