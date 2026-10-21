package controller

import (
	"github.com/gin-gonic/gin"
)

func GetModelPricingConfig(c *gin.Context) { pricingHTTP().GetPricingConfig.Handle(c) }

func UpdateModelPricingConfig(c *gin.Context) { pricingHTTP().UpdatePricingConfig.Handle(c) }

type effectivePricingPreviewResponse struct {
	effectivePricingResponse
	AvailableUserGroups []string `json:"available_user_groups"`
}

// GetEffectivePricingPreview projects a configured user group without changing
// the caller's identity or persisted group. The option route requires root access.
func GetEffectivePricingPreview(c *gin.Context) { pricingHTTP().PreviewPricing.Handle(c) }
