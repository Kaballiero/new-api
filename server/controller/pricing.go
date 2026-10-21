package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/QuantumNous/new-api/model"

	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func GetPricing(c *gin.Context) { app.NewGetPricingHTTP().Handle(c) }

type effectivePricingResponse = pricingdomain.EffectivePricingResponse

type effectiveGroupPricing = pricingdomain.EffectiveGroupPricing

type effectiveUnitPrice = pricingdomain.EffectiveUnitPrice

// GetEffectivePricing returns a read-only per-user tariff projection. It does
// not reuse /api/pricing's response contract and does not call debit paths.
func GetEffectivePricing(c *gin.Context) { app.NewGetEffectivePricingHTTP().Handle(c) }

// GetEffectivePricingByGroup returns the same projection as the per-user
// endpoint for a configured group without requiring a user in that group.
func GetEffectivePricingByGroup(c *gin.Context) { app.NewGetEffectivePricingByGroupHTTP().Handle(c) }

func buildEffectivePricingResponse(userGroup string, basis service.BillingFXBasis, pricing []model.Pricing) effectivePricingResponse {
	return app.NewPricingProjection().BuildEffectivePricingResponse(userGroup, pricingdomain.
		BillingFXBasis(basis), pricing)
}

func buildEffectiveGroupPricing(item model.Pricing, group string, pureRatio, effectiveRatio float64) effectiveGroupPricing {
	return app.
		NewPricingProjection().BuildEffectiveGroupPricing(item, group, pureRatio, effectiveRatio)
}

func ResetModelRatio(c *gin.Context) { app.NewResetModelRatioHTTP().Handle(c) }
