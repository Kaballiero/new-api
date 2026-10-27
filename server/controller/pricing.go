package controller

import (
	"github.com/QuantumNous/new-api/internal/app"

	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/gin-gonic/gin"
)

func GetPricing(c *gin.Context) { pricingHTTP().GetPricing.Handle(c) }

type effectivePricingResponse = pricingdomain.EffectivePricingResponse

type effectiveGroupPricing = pricingdomain.EffectiveGroupPricing

type effectiveUnitPrice = pricingdomain.EffectiveUnitPrice

// GetEffectivePricing returns a read-only per-user tariff projection. It does
// not reuse /api/pricing's response contract and does not call debit paths.
func GetEffectivePricing(c *gin.Context) { pricingHTTP().GetPersonalPricing.Handle(c) }

// GetEffectivePricingByGroup returns the same projection as the per-user
// endpoint for a configured group without requiring a user in that group.
func GetEffectivePricingByGroup(c *gin.Context) { pricingHTTP().GetGroupPricing.Handle(c) }

func buildEffectivePricingResponse(userGroup string, basis pricingdomain.BillingFXBasis, pricing []pricingdomain.Pricing) effectivePricingResponse {
	return app.NewPricingProjection().BuildEffectivePricingResponse(userGroup, basis, pricing)
}

func buildEffectiveGroupPricing(item pricingdomain.Pricing, group string, pureRatio, effectiveRatio float64) effectiveGroupPricing {
	return app.
		NewPricingProjection().BuildEffectiveGroupPricing(item, group, pureRatio, effectiveRatio)
}

func ResetModelRatio(c *gin.Context) { pricingHTTP().ResetModelRatios.Handle(c) }
