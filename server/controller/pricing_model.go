package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	pricingquery "github.com/QuantumNous/new-api/internal/pricing/transport"

	"net/url"

	"github.com/gin-gonic/gin"
)

type consumerPricingModelDetail = pricingdomain.ConsumerPricingModelDetail

func parseEffectivePricingModelQuery(values url.Values) (string, string, error) {
	return pricingquery.
		ParseEffectivePricingModelQuery(values)
}

func GetEffectivePricingModel(c *gin.Context) { pricingHTTP().GetModelPricing.Handle(c) }

func buildEffectivePricingModelDetail(context effectivePricingPageContext, name string) (consumerPricingModelDetail, bool) {
	context.Policy = app.NewPricingProjection()
	return app.
		NewPricingProjection().BuildEffectivePricingModelDetail(pricingdomain.EffectivePricingPageContext(context), name)
}
