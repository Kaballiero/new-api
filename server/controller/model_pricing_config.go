package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetModelPricingConfig(c *gin.Context) { httpapi.Pricing().GetPricingConfig.Handle(c) }

func UpdateModelPricingConfig(c *gin.Context) { httpapi.Pricing().UpdatePricingConfig.Handle(c) }

func GetEffectivePricingPreview(c *gin.Context) { httpapi.Pricing().PreviewPricing.Handle(c) }
