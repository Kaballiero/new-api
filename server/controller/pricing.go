package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetPricing(c *gin.Context) { httpapi.Pricing().GetPricing.Handle(c) }

func GetEffectivePricing(c *gin.Context) { httpapi.Pricing().GetPersonalPricing.Handle(c) }

func GetEffectivePricingByGroup(c *gin.Context) { httpapi.Pricing().GetGroupPricing.Handle(c) }

func ResetModelRatio(c *gin.Context) { httpapi.Pricing().ResetModelRatios.Handle(c) }
