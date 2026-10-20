package controller

import (
	"github.com/QuantumNous/new-api/internal/app"

	"github.com/gin-gonic/gin"
)

func GetModelPricingConfig(c *gin.Context) { app.NewGetModelPricingConfigHTTP().Handle(c) }

func UpdateModelPricingConfig(c *gin.Context) {
	app.NewUpdateModelPricingConfigHTTP(func(names []string) { recordManageAudit(c, "model.pricing.update", map[string]any{"models": names}) }).Handle(c)
}

type effectivePricingPreviewResponse struct {
	effectivePricingResponse
	AvailableUserGroups []string `json:"available_user_groups"`
}

// GetEffectivePricingPreview projects a configured user group without changing
// the caller's identity or persisted group. The option route requires root access.
func GetEffectivePricingPreview(c *gin.Context) { app.NewGetEffectivePricingPreviewHTTP().Handle(c) }
